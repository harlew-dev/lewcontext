# Admin: opt-in alert subscriptions and managed webhooks

Status: **implemented 2026-11-02**. Built as planned: `alert_webhooks` (migration 068) replaces `alert_destination_versions` and `alert_delivery_settings`; `event_deliveries.target` fans an alert event out to one delivery per matching webhook; per-webhook signing keys with a public `X-Lewcontext-Key-Id`; opt-in email subscriptions; the Subscriptions tab and one Subscribe dialog. Differs (follow-up 2026-11-02): an email subscription also chooses its event kinds (`admin_alert_subscriptions.events`, every kind by default), the Alerts page became three tabs (Alerts with search, an Opened range and filters; Catalog with collapsible rules; Subscriptions), and the Subscribe dialog merges signals and events into one box. The signing key is read with `GET …/signing-key` as an `Action` (so a fresh code, not only an open window); a key's overlap is erased only on an explicit edit (an expired one never signs). Docs updated: [monitoring](../../../../docs/api/monitoring.md), [admin](../../../../docs/api/admin.md), [events](../../../../docs/api/events.md), [database](../../../../docs/api/database.md), [rate-limits](../../../../docs/api/rate-limits.md), [mail](../../../../docs/api/mail.md), [deployment](../../../../docs/deployment.md), [compliance](../../../../docs/compliance.md), [forms and actions](../../../../docs/ui/forms-and-actions.md), [design](../../../../docs/ui/design.md), [AGENTS.md](../../../../AGENTS.md).

Supersedes parts of [alert-subscriptions](alert-subscriptions.md) (it is implemented; its history stays as written). Builds on [alert-recovery](alert-recovery.md) (lifecycle events and snapshots) and [actions](actions.md) (step-up, reasons). Names follow [naming.md](../../../../docs/naming.md); [checks.md](../../../../docs/checks.md) governs any new name.

## Why

The shipped design is hard to use and hard to explain. One shared webhook and one shared heartbeat are edited through a panel that prints internal state ("Mode: inherit. Receiver: not set. Source: off. Revision: 0."), a separate dialog per type, and several dropdowns. Admins also receive alert email **by default** (`critical`) before anyone asked for it, and a per-signal override "narrows" a global setting, which nobody can reason about at a glance.

What the review asked for:

- **Nothing is sent until someone subscribes.** No global setting to inherit; off by default.
- **One dialog.** Pick a *Type*, then the signals (and events), then that type's own settings, all live in the same dialog.
- **Signals chosen like the Events filters choose events:** a searchable, grouped, multi-select listbox with removable badges, not a dropdown per signal.
- **Many webhooks**, each choosing its own signals and events, managed entirely through the admin API. **No environment variables** for destinations.
- A webhook's **signing key is shown read-only** (lock icon, Copy), and the **example payloads** are formatted and readable.
- Heartbeat is explained and fits the same model.

## How alerts reach people: the model

| Concept | What it is | Who manages it |
| --- | --- | --- |
| **Signal** | A monitored number with a rule (`http.latency_p95`, `auth.possible_takeovers`…). The catalog lists them | code |
| **Event kind** | What happened to an alert: `opened`, `escalated`, `recovered`, `reopened`, `resolved`, `reminded` (and the email *digest*) | code |
| **Email subscription** | *My* inbox: the signals I want and the lowest level (warnings and critical, or critical only). Nothing selected means no alert email | each admin, for themselves |
| **Webhook** | A named receiver URL (JSON or Slack) that gets the signals and event kinds it was set to, signed with its own key | operators |
| **Heartbeat** | A named URL the server **GET**s every minute after an alert evaluation worked, so an outside service (Healthchecks.io, Better Stack) pages someone when pings *stop*. Carries no alert content | operators |

A webhook says *what is wrong*; a heartbeat says *the watcher is alive*. They share a table and a dialog but not behaviour (below).

## Design

### Data (squashed migrations: edit the create migrations, rebuild the local database)

| Change | Detail |
| --- | --- |
| **`alert_webhooks`** (new; replaces `alert_destination_versions` and `alert_delivery_settings`, both dropped with their factories, models, `store.DataTables` entries and data-map rows) | Internal key; `public_id` (`whk_`); `kind` (`webhook` / `heartbeat`); unique `name` (1–80, trimmed); sealed `url` and (webhook) sealed signing key and its public `key_id`, and overlap key (with its own `key_id`) with expiry, bound to the row's identity (the existing destination sealing, domain-separated); `format` (`json` / `slack`, webhooks only); `signals text[]` and `events text[]` (webhooks only; empty means **nothing**, never "all"; CHECKs on length ≤ 200 and ≤ 7); `enabled`; `delivery_revision` (bumped only when the URL, format or key is replaced); `created_by` / `updated_by` (`SET NULL`); times. Caps: 20 webhooks and 5 heartbeats |
| **`admin_alert_subscriptions`** (edit 067) | Now *the* opt-in: a row is a signal this admin wants by email, with `min_level` `warn` or `critical`. No row, no email. The `off` value and the "inherit" meaning go. Cascades with `admins`; ≤ 500 per admin |
| **`admins.alert_email`** | Dropped. A new admin, and every existing one after the rebuild, receives nothing until they subscribe |
| **`event_deliveries`** (edit 008) | A nullable `target` (a `whk_` ID) and a key of `(event, listener, target)`, so one alert event fans out to **one delivery per matching webhook**, each with its own retries, status and `X-Lewcontext-Delivery` ID. The Mail page shows it like any delivery |

### Fan-out and cancellation

- At the moment an alert event is recorded (in its transaction), the store reads the **enabled webhooks whose `signals` contain the alert's signal and whose `events` contain its kind** and queues one delivery per webhook, with `target` and the webhook's `delivery_revision` in the delivery's own row. Heartbeats are not events: the leader pings every enabled heartbeat after a successful evaluation.
- Before a send the worker reads the webhook again: **deleted, disabled, or a different `delivery_revision` means skipped** (a completed no-send with a fixed counter reason, never retried, never sent to a replacement). Editing a name or the signals and events does not cancel what is already queued; replacing the URL, format or key does. This is the shipped "retired destination" rule, now per webhook.
- The recovery plan's immutable body snapshot stays: a retry sends the same bytes under the same delivery ID. Caps become **per webhook**: 12 posts per signal per hour.
- Email: the listener reads the admins whose **subscriptions contain the signal at a level the alert reached**, at delivery time (current membership, address and subscriptions), for every kind and each digest item. Preference changes never replay history; `mailer.Send` and the block list still apply.

### Signing keys

**Every webhook has its own signing key, never shared or reused**: a random 32-byte key generated when the webhook is created (and again on Rotate), so a receiver that leaks or loses its key affects only that webhook, and one receiver can't verify or forge another's posts. Replacing a webhook's URL generates a new key too (no overlap with the old receiver). The key is sealed, so uniqueness is by construction (256 random bits), not an index; each post carries a public **`X-Lewcontext-Key-Id`** (a short non-secret identifier, `key_` plus 8 base62 characters, stored beside the sealed key and changed with it) so a receiver holding several keys knows which to verify with, and during a rotation's overlap both signatures are labelled by their key IDs. The ID reveals nothing about the key. The review wants it **visible, read-only, with Copy**, which replaces the shipped "shown once" rule, so:

- An operator reads it with `GET /api/admin/alert-webhooks/{webhook}/signing-key` (a sub-resource, `no-store`), **only inside an open step-up window**, recorded as `admin.alert_webhook.viewed` with the webhook's ID and never the key. A viewer never sees it. The key never appears in a list, a log, an event or an error.
- Rotation (`POST …/signing-key`) keeps the 15-minute dual-signature overlap and the "no third rotation during overlap" rule.
- The dialog shows it in the shared locked-field primitive (lock icon, Copy), as workspace IDs are shown.

### API

Resource controllers in the existing `admin` area (`AlertWebhooks`: `Index`, `Show`, `Store`, `Update`, `Destroy`; `AlertWebhookSigningKeys`: `Show`, `Store`), all behind the key and admin session.

| Route | Guards | Records |
| --- | --- | --- |
| `GET /api/admin/alert-webhooks`, `…/{webhook}` | any admin; read budget | `admin.alert_webhook.listed` / `.viewed` |
| `POST /api/admin/alert-webhooks` | `Action` (operator, step-up, reason) | `admin.alert_webhook.created` |
| `PATCH …/{webhook}` | `Action`; expected `revision`; 409 when stale | `admin.alert_webhook.updated` (changed field names; never a URL, host or key) |
| `DELETE …/{webhook}` | `Action` | `admin.alert_webhook.deleted` |
| `GET` / `POST …/{webhook}/signing-key` | `Action` (a read inside the step-up window; rotate) | `admin.alert_webhook.viewed` / `.updated` (`operation: rotate_key`) |
| `GET /api/admin/alert-subscriptions` | any admin: your own email subscription | `admin.alert_rule.listed` (existing) |
| `PUT`-less `PATCH /api/admin/alert-subscriptions` | `Role(viewer)`, `alert-subscription:per-user` (60/hour); `{signals: [{signal, minLevel}]}` as a bounded patch (`minLevel: off` deletes) | existing `admin.updated` (`changes.alertSubscriptions`) |

The old `GET/PATCH /api/admin/alert-delivery-settings` routes, `alert-delivery-settings/webhook/signing-keys`, `alertEmail` in `PATCH /api/admin/me` and the `admin.alert_delivery.*` events are removed. A webhook response carries `id`, `kind`, `name`, `format`, `signals`, `events`, `enabled`, `revision`, last delivery outcome and **never the URL or its host** (a receiver can carry a secret in either). Replacing a URL means entering it again.

### Environment

`ALERT_WEBHOOK_URL`, `ALERT_WEBHOOK_FORMAT`, `ALERT_WEBHOOK_SECRET` and `ALERT_HEARTBEAT_URL` are **removed** (config, startup log, the configuration summary and Configuration page rows, `.env.example`, deployment docs, e2e settings). Nothing materialises the environment into versions any more. `ALERT_ALLOW_PRIVATE_TARGETS` stays: it is a security guard for the dial-time address check, not a destination. `APP_KEY` still seals the URLs and keys, and losing it still fails closed.

## UI

The Alerts page gains a **Subscriptions** tab beside Open, Resolved, All and Catalog (still `?view=` state; Back/Forward work). It shows **My email subscription** (a short list of signals and levels) and the **Webhooks and heartbeats** table (name, type, signals, events, enabled, last delivery; operators get Edit and Delete in the row menu). **Subscribe** in the header opens one dialog:

1. **Type** (`Listbox`): Email, Webhook, Heartbeat (the last two only for an operator; a viewer sees Email alone).
2. **Signals** (`Combobox`, multiple, grouped by page, searchable, removable badges, like the Events filters' event picker; Email and Webhook only).
3. **Level** (a two-option radio group: Warnings and critical, or Critical only; Email only) or **Events** (the same `Combobox`, the event kinds; Webhook only).
4. **Type-specific settings** live under it: Email shows your locked address; Webhook a **Name**, **URL** (never prefilled), **Format**, the read-only **signing key** and **Rotate**, then the **Example payload** disclosure; Heartbeat a **Name**, **URL** and a plain explanation of what a ping is.

Changing the Type swaps the content in place without losing what is shared (name, signals). Save is one request; operators get the reason and step-up code in the existing `AdminActionDialog` flow, and Email saves with no code. The internal words (mode, source, revision, inherit) are never shown: the dialog says whether a URL is set and, for an environment-less system, nothing else. Example payloads are rendered as **formatted, syntax-spaced code blocks** (a `<pre>` with wrapping and horizontal scroll, one per event kind chosen) with fixed, non-secret values, never the real URL or key.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| A viewer redirects alerts or reads a receiver | Webhook and heartbeat writes, and reading a key, are operator `Action`s with a fresh code and a reason; viewers can subscribe their own inbox only |
| A URL or host leaks a receiver's token | Sealed with `APP_KEY`, never returned, logged, audited or shown (not even a host); validated on write and again before use with the shared structural check and the dial-time public-address guard; no redirects, bounded body, verified TLS |
| The signing key is now readable | Operator, step-up window, audited without the key, `no-store`, never in lists or SWR keys, held only in component memory |
| Old queued alerts go to a changed receiver | Per-webhook `delivery_revision`; a changed or deleted target skips, never retargets |
| Fan-out floods one receiver or the table | Webhook and signal caps (20 and 12 an hour), a delivery per match only, bounded arrays |
| An empty selection quietly meaning "everything" | Empty means nothing, enforced in the store and tested |
| Email sent before consent | No row, no email; no default; the block list and `mailer.Send` unchanged |

## Tests

Real PostgreSQL: no subscription sends nothing for every kind and each digest item, a subscribed admin gets only their signals at their level, a revoked or deleted admin none; webhooks fan out only to matches (signal and kind), empty selections send nothing, two webhooks get separate deliveries with their own IDs, a disabled, deleted or re-keyed target skips while a renamed or re-scoped one still sends, caps are per webhook, retries keep their bytes. Heartbeat pings every enabled heartbeat only after a good evaluation. Operator/viewer/step-up/reason/limit/CSRF guards on every route, 409 on a stale revision, 404 for unknown or foreign IDs, the key in no list, event, log or error (a walk of every response and event), key read only inside the window and audited. Removed routes and variables are gone (`TestConfigRefusesNothing`-style: the server starts without them). Vitest: one dialog changing Type live, the combobox search and removable badges, a viewer's Email-only dialog, a pending/failed/conflict save keeping the dialog, the formatted payloads. Playwright: subscribe by email and receive only that signal; create a webhook, copy its key, receive a signed post, rotate, delete. `make test`, `make check`, `make test-e2e E2E_PORT=auto`, `make build`.

## Decisions (each with a recommendation; confirm or change)

1. **One unique signing key per webhook, with a public key ID header** (yours; recommended as written).
2. **Webhooks and heartbeats share `alert_webhooks`** with a `kind`. *Recommend yes*: one table, one dialog, one list. A heartbeat has no signals, events, format or key.
3. **Fan-out through `event_deliveries.target`** (one delivery per matching webhook) rather than a second queue. *Recommend yes*: reuses retries, the Mail page and the worker; the cost is one nullable column and a wider key.
4. **Signals and events as `text[]` columns** on the webhook, not join tables. *Recommend arrays*: bounded (≤ 200 and ≤ 7), read together, no cross-row constraints; unknown or retired signals are inert and shown as such.
5. **The signing key is readable by operators** inside a step-up window (replaces "shown once"). *Recommend yes* as asked, with the audit and no-store rules above. The stricter alternative keeps one-time reveal and Rotate only.
6. **Email subscription is per signal with a level**, no event-kind choice (an email says what happened for any kind). *Recommend yes*; webhooks choose event kinds because receivers automate on them.
7. **Remove `admins.alert_email` and the default**: every admin is unsubscribed after the rebuild. *Recommend yes*, per the review.
8. **Webhook caps** of 20 webhooks and 5 heartbeats, 12 posts per signal per hour per webhook. *Recommend yes.*
9. **Existing configured destinations are not migrated** (the environment variables are gone and development databases are rebuilt). A deployment with a configured webhook must recreate it in the dashboard; documented in the release notes.

## Docs to update when implemented

`docs/api/monitoring.md` (subscriptions, webhooks, heartbeats, fan-out), `admin.md`, `endpoints.md`, `events.md`, `mail.md`, `rate-limits.md`, `database.md`, `compliance.md` (the data map: `alert_webhooks` replaces two rows), `deployment.md` and `.env.example` (variables removed, receiver contract), `observability.md` (the configuration summary), `docs/naming.md` and `docs/checks.md` (`admin.alert_webhook` stored resource, replacing `admin.alert_delivery`), `docs/ui/forms-and-actions.md`, `design.md`, `api.md`, `testing.md`, `docs/api/testing.md`, `AGENTS.md` (the webhook sentence), and the plan indexes.

## Not included

Per-recipient webhook secrets, retry policy per webhook, a "send test" button, webhooks per workspace or customer, PagerDuty or SMS integrations, arbitrary email destinations, email event-kind selection, and importing the old environment values.
