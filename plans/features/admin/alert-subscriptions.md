# Admin: Alert subscriptions and catalog

Status: **draft 2026-11-01** — decisions to confirm before implementation.

Builds on [admin/alerts](../implemented/admin/alerts.md) (A5) and [monitoring](../implemented/admin/monitoring.md) (A4). Names follow [naming.md](../../../docs/naming.md).

## Why

The Alerts page shows only alerts that have opened. There is no list of which signals can alert, what each means, or its thresholds, so an admin can't tell what the system watches. And the only notification control is one global level (`all` / `critical` / `none`) on the Admins page, so an admin who wants some alerts but not others has no choice but to take everything or nothing.

## What it adds

1. **A catalog of every alert the system can raise** — each rule in `monitor.AlertRules` with its signal, title, meaning, level, thresholds, `For` duration and the Monitoring page it belongs to — so admins can see what exists without waiting for one to fire.
2. **Per-admin subscriptions** — instead of (or in addition to) the global level, each admin chooses which signals reach their inbox, and at what minimum level.
3. **Dashboard-managed delivery settings** — the webhook and heartbeat URLs move from environment-only startup config into an operator-managed settings surface.


## Delivery settings in the dashboard (new)

Today the webhook and heartbeat URLs are environment variables (`ALERT_WEBHOOK_URL`, `ALERT_HEARTBEAT_URL`), read at startup and shown only as set/not-set on Configuration. This plan moves them into the dashboard so an admin can subscribe without a redeploy:

- A **Subscribe** button in the top right of the Alerts page opens a modal.
- The modal's `type` chooses **email**, **webhook** or **heartbeat**.
- **Email:** an address field, defaulting to the signed-in admin's own email and locked (a lock icon shows it can't be changed) — email subscriptions stay per-admin, as today.
- **Webhook:** a URL field plus a collapsible **Example payload** showing the JSON and Slack shapes and the signature headers.
- **Heartbeat:** a URL field plus a collapsible **Example payload** / ping description.

Storing URLs in the database (not env) means they become data admins can change, so every write needs the `operator` role, a fresh step-up code and a reason, and is recorded with who changed what. Secrets (`ALERT_WEBHOOK_SECRET`) and the private-targets flag stay in configuration, since a dashboard-stored secret would be readable by anyone with the admin key and a URL's value is itself sensitive.

## Findings and constraints (review)

These are the gaps the first draft did not cover. They must be decided before code, because several change existing invariants rather than adding a page.

| Area | Current behaviour | What the plan must change |
| --- | --- | --- |
| **The config-only invariant** | `AGENTS.md` and `docs/api/monitoring.md` say the webhook and heartbeat URLs come **only from configuration**, never from a request; `alerts.NewClient` assumes startup-validated URLs | Moving them to the dashboard is an architecture change. The plan must update those rules and add a "settings are data" equivalent: validated on write, validated again at use, never trusted from a request body without checks |
| **The URL is the secret** | A Slack webhook is its URL; the current config never logs or shows it. Many receivers put a bearer token in the path or query | Store the URL sealed with `APP_KEY` (the MFA-secret pattern), never return it in full from the API, never put it in an event or a log, and show only a masked host/`set` state. If a receiver's URL is genuinely not a secret, sealing is still cheap defence in depth; `APP_KEY` loss would make stored destinations unusable, as it already does for MFA |
| **Two different things** | Email is a per-admin subscription; webhook and heartbeat are global infrastructure | Split the UI and routes: **Alert subscriptions** (any admin, own inbox) and **Alert delivery settings** (operator, step-up, reason). The single "Subscribe" modal the request describes should switch between these, not pretend a heartbeat is a subscription |
| **One or many** | Config supports one webhook URL and one heartbeat URL | Decide whether the dashboard supports one row of each or several webhook destinations. Multiple webhooks need per-row secrets/format, retries and a delivery UI; recommend one of each first |
| **Env vs database precedence** | Existing deployments set env vars; `config.loadAlerts` refuses bad values at startup | Decide: database overrides env, env remains a fallback, or env is removed. Add a documented migration path and a Configuration-page warning when both are set |
| **Startup validation vs runtime** | `config.alertURL` validates at startup; the connect-time guard refuses non-public addresses | Reuse the same rules in a new `alerts.ValidateURL` called on write and on use. Keep `ALERT_ALLOW_PRIVATE_TARGETS` in config; the env `ALERT_WEBHOOK_SECRET` stays as a fallback, but each dashboard webhook gets its own secret |
| **Schema** | `admins.alert_email`; no subscription or destination tables | Add `admin_alert_subscriptions` (admin FK, signal, min level, unique per pair) **and** an `alert_destinations`/`alert_settings` table for the webhook and heartbeat (or a singleton row). Both need migrations, factories, data-map rows and cascade rules on admin revocation |
| **Audit events** | `admin.updated` is generic; alert events are specific | Prefer explicit names: `admin.alert.catalog` (or use the existing read convention), `admin.alert.subscriptions.updated`, `admin.alert.webhook.updated`, `admin.alert.heartbeat.updated`, with admin and changed fields, never the URL. Record each catalog read once |
| **Routes** | `/api/admin/alerts/{alert}` and the web page `alerts/[alert].tsx` already claim a dynamic segment | A `catalog` route collides by shape; either use `?view=catalog`/a query tab, or register static `alerts/catalog` before the dynamic route and prove it in `router/routes_test.go` and the web route test |
| **Rate limits and tiers** | Own `alert_email` is a `Role` write, no code; global actions are `Action` | Own subscription changes stay viewer-allowed and uncoded (own inbox); webhook/heartbeat writes use `Action` (operator, fresh code, reason, `admin-action:per-user`). Say this explicitly in the route table |
| **Docs and compliance** | The webhook/heartbeat are processors named in `docs/compliance.md`; deployment, monitoring, admin, Configuration page and AGENTS.md all describe config-only URLs | Update all of them, plus the new tables' data-map rows, retention (none for settings; subscriptions follow the admin) and the backup note (the database now holds destinations) |

## Proposed design

- **Page:** a new tab on the Alerts page, `Catalog`, or a separate `Alerts → Catalog` route. It is one recorded read (`admin.alert.catalog`), lists the rules from code (not the database), and shows the current admin's subscription state. No thresholds are editable here; rules change by reviewed commit, as today.
- **Data:** no new table for the catalog (rules are in code). Subscriptions are stored per admin. Either a JSON column on `admins` (`alert_subscriptions`) or a small table `admin_alert_subscriptions (admin_user_id, signal, min_level)`. A missing row means "subscribed at the admin's global level", so existing admins keep their current behaviour.
- **API:** `GET /api/admin/alerts/catalog` returns the rules plus each one's `subscribed` state for the signed-in admin; `PATCH /api/admin/me` (or a dedicated `PATCH /api/admin/alerts/subscriptions`) accepts the chosen signals/levels. Any admin may change their own; recorded as `admin.updated` with `changes.alertSubscriptions`. No code or reason, since it only affects their own inbox.
- **Listener:** `mail.alert*` filters recipients by subscription and level, in addition to the existing global `alert_email`. The webhook keeps receiving every alert event unless a separate filter is added (see decisions).

## Webhook signing (per destination)

The app already signs its single webhook with `X-Lewcontext-Signature: t=<unix>,v1=<hex HMAC-SHA256 of "<t>.<body>">` and sends `X-Lewcontext-Delivery: evt_…`. Extending this to dashboard-managed webhooks means **one signing secret per webhook destination**, not one global secret:

- The server generates a random 32+ byte secret when a webhook is added, stores it sealed with `APP_KEY` (never sends it back in full), and shows it **once** on creation so the receiver can be configured. A **Rotate secret** action issues a new one, with the old one valid for a short overlap window if desired.
- The signature stays HMAC-SHA256 over the raw body, timestamp included to reject replays. Receivers should compare with a constant-time check and allow a small clock skew (for example 5 minutes), as Stripe and Slack-style receivers do.
- `X-Lewcontext-Delivery` stays the idempotency key across retries.
- The message says how to verify: raw body (not a re-serialised JSON), header name, timestamp tolerance, and that the secret is shown only once.
- The existing `ALERT_WEBHOOK_SECRET` remains the fallback for an env-configured webhook; it is not reused for dashboard destinations.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| A viewer muting a critical alert they should see | Subscriptions are per-admin and only affect that inbox; the dashboard and webhook still show every alert. Silencing an alert (with a code and reason) remains the only way to stop the webhook |
| Flood from per-signal toggles | The existing bounds (one open alert per signal, cooldown, digest, 3 emails per signal per hour) still apply; subscriptions only narrow who is mailed |
| Leaking signal details to non-admins | The catalog and subscriptions are behind `RequireAdminKey` and `RequireAdminSession`, like every admin route |

## Tests

- Catalog lists every rule in `monitor.AlertRules` exactly once, with its title, meaning, thresholds and page.
- A subscription change is recorded once, affects only that admin's mail, and leaves other admins and the webhook untouched.
- An admin with no subscription rows behaves exactly as before (`alert_email` alone).
- Viewers can change their own subscriptions; no code or reason is required.
- A webhook/heartbeat change needs an operator, a fresh code and a reason, is recorded once, and never exposes the URL in a response, event or log.
- A URL rejected on write is rejected the same way at use, including the production private-address guard.
- The catalog route does not shadow `/alerts/{alert}`, and `catalog` is not treated as an alert ID.
- New tables have factories and data-map rows; revoking admin removes that admin's subscription rows.
- Each webhook signs with its own secret; rotating the secret changes the signature, the old secret can overlap, and the API never returns either.

## Decisions (to confirm)

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Surface for the catalog | A tab on the existing Alerts page, so opened alerts and the catalog stay together |
| 2 | Subscription granularity | Per-signal, per-admin, with a minimum level (`warn`/`critical`/`off`), defaulting to "follow the global level" for admins with no rows |
| 3 | Global level vs. per-signal | Keep `alert_email` (`all`/`critical`/`none`) as the default floor; per-signal choices can only narrow it, never widen beyond it |
| 4 | Webhook filtering | Keep the webhook receiving every alert event; filtering it is a separate, later decision, since a missed webhook hides an alert from the external system |
| 5 | Storage | A separate `admin_alert_subscriptions` table, so rules added or removed in code don't leave stale columns; admins with no rows follow the global level |
| 6 | Recording | Reuse `admin.updated` with `changes.alertSubscriptions`, matching the existing `alertEmail` change |
| 7 | Move webhook/heartbeat URLs out of env into the dashboard | Recommended only behind `operator` + step-up + reason; keep `ALERT_ALLOW_PRIVATE_TARGETS` in config, and keep the env signing secret only as a fallback |
| 8 | Who may set webhook/heartbeat URLs | `operator` only, since a wrong URL can exfiltrate alert content or silence the heartbeat |
| 9 | Collapse example payloads by default | Yes, to keep the modal short; expand on click |
| 10 | Env vs database | Database wins when set; env remains a read-only fallback for existing deployments, with a Configuration warning and a documented migration |
| 11 | Store the URL sealed or plaintext | Sealed with `APP_KEY`; the API returns only a masked host and whether it is set |
| 12 | One webhook/heartbeat or many | One of each in the first build; several webhooks later, each with its own signing secret |
| 14 | How a webhook knows it came from us | Per-webhook HMAC-SHA256 secret, Stripe-style `t=…,v1=…` header over `timestamp.body`, plus a delivery ID; rotate with overlap |
| 13 | Catalog route | Use a `Catalog` tab at the same `/alerts` URL (query state), avoiding the `[alert]` route collision |


## Docs to update

- `docs/api/monitoring.md` (alerts, outbound requests, Configuration),
- `docs/api/admin.md` (new reads/actions and tiers),
- `docs/deployment.md` (env vs dashboard settings, migration),
- `docs/api/events.md` (new events),
- `docs/api/database.md` and `docs/compliance.md` (new tables, data map, backups),
- `docs/ui/forms-and-actions.md` and `docs/ui/base-ui.md` (modal, disclosure, dialogs),
- `AGENTS.md` (the config-only URL invariant and the docs table).

## Not included

- Editing thresholds or rules from the dashboard (still code-reviewed).
- PagerDuty/Opsgenie/SMS integrations (webhook relay covers them).
- Workspace-level alerts for customers.
- Webhook-level filtering or per-webhook subscriptions.
