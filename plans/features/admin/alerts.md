# Admin: Alerts

Status: **draft 2026-10-31**, decisions to confirm. Plan A5 of the [admin plans](README.md). Needs [monitoring](monitoring.md) (its signals and integrity checks) and [observability](../implemented/admin/observability.md). Docs it will change: [admin.md](../../../docs/api/admin.md), `docs/api/monitoring.md`, [events.md](../../../docs/api/events.md), [mail.md](../../../docs/api/mail.md), [database.md](../../../docs/api/database.md), [api/README.md](../../../docs/api/README.md), [deployment.md](../../../docs/deployment.md).

## Why

A dashboard only helps when someone is looking at it. Credential stuffing at 3 a.m., a stalled mail worker or a release that doubles the error rate should reach a person within minutes, not at the next time someone opens Monitoring. And since all of this runs inside the app, something outside it must notice when the app itself stops (decision 7).

## How alerts work

```
every minute, on one server (the leader)
  for each Signal with Alert set ── measure ──▶ level ok / warn / critical
                                                │
  not ok for its `For` duration ─────────────▶ open an alert (or raise its level)  → alert.opened / alert.escalated
  ok again for 10 minutes ───────────────────▶ resolve it                          → alert.resolved
every hour: the integrity checks, the same way

alert.* events ── listeners (the existing delivery queue, with its retries) ──▶ email to admins
                                                                            └─▶ webhook
every minute: the leader pings ALERT_HEARTBEAT_URL, so an outside service notices when the pings stop
```

- **One evaluator across servers** (decision 1): the alert loop runs in every `cmd/server`, but each minute only the process holding a PostgreSQL session advisory lock (`pg_try_advisory_lock`, through a helper in `internal/database`, where advisory locks already live) evaluates. If it dies, its connection closes, the lock is released, and another server takes over within a minute.
- **Signals, not new rules:** an alert rule is a monitoring `Signal` with `Alert: true` and a `For` duration, so a page and its alert always agree. Integrity checks alert at their level.
- **No flapping** (decision 3): an alert opens only after the signal has been at warn or critical for `For` (2 minutes for rates; immediately for discrete facts such as a refusal at the admin door), and resolves only after 10 minutes back at ok. A warn that becomes critical raises the open alert's level rather than opening another.
- **Notifications ride on events:** opening, escalating and resolving record events, and listeners on them send email and the webhook through the existing delivery queue, so a failed send is retried for an hour like any email.

## Data

```sql
-- create_alerts (a new migration)
CREATE TABLE alerts (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 public_id text NOT NULL UNIQUE DEFAULT public_id('alt') CHECK (public_id ~ '^alt_[0-9A-Za-z]{14}$'),
 signal text NOT NULL,                 -- the Signal's name, e.g. "auth.credential_stuffing"
 level text NOT NULL CHECK (level IN ('warn', 'critical')),
 opened_at timestamptz NOT NULL DEFAULT now(),
 last_value double precision NOT NULL,
 peak_value double precision NOT NULL,
 details jsonb NOT NULL DEFAULT '{}',  -- the drill-down filter and up to 10 sample public IDs
 last_notified_at timestamptz,
 acknowledged_at timestamptz,
 acknowledged_by bigint REFERENCES admins(user_id) ON DELETE SET NULL,
 silenced_until timestamptz,
 resolved_at timestamptz
);
CREATE UNIQUE INDEX alerts_open_signal_idx ON alerts(signal) WHERE resolved_at IS NULL;  -- one open alert per signal
CREATE INDEX alerts_opened_idx ON alerts(opened_at DESC);
```

`admins` gains `alert_email text NOT NULL DEFAULT 'critical' CHECK (alert_email IN ('all', 'critical', 'none'))` (decision 4).

Kept a year (a `make prune` target, `alerts`, on resolved alerts), since "when did we detect it and how fast did we respond" is evidence an auditor asks for ([compliance](compliance.md)).

## What alerts, at first

| Signal (from [monitoring](monitoring.md)) | Level | For |
| --- | --- | --- |
| 5xx share over 1% / 5% | warn / critical | 5 min |
| p95 latency over 1 s / 3 s | warn / critical | 10 min |
| A pending delivery overdue by 10+ minutes (the worker is stalled) | critical | immediately |
| Mail deliveries failed: 1 / 10 in an hour | warn / critical | immediately |
| A shared (`global`) limit reached | critical | immediately |
| Credential stuffing: an IP trying 10+ emails / and one succeeded | warn / critical | immediately |
| Success after 5+ failures | critical | immediately |
| Possible takeover | warn | immediately |
| Admin door: `not_admin`, `mfa_off`, `admin.access_refused` | critical | immediately |
| `admin.session.moved` | warn | immediately |
| A server silent for 2 minutes; mixed versions 30 minutes after a start | warn | as stated |
| A new panic, or a new error group with 10+ occurrences | warn | immediately |
| Database pool waits / hashing waits sustained | warn | 5 min |
| Sign-up cluster: 20+ accounts from one IP in 24 hours | warn | immediately |
| Any critical integrity check failing | critical | at the hourly check |
| The oldest event older than 97 days (prune isn't running) | warn | at the hourly check |

## Notifications

### Email to admins

- **Who:** every admin whose `alert_email` covers the level: `critical` (the default), `all`, or `none`. Each admin sets their own on the Admins page ([access hardening](../implemented/admin/access-hardening.md)); it's the only setting there that changes data without a code, since it only affects their own inbox.
- **When:** on opening, on escalating to critical, and on resolving (for alerts that were emailed). A **critical alert still open and not acknowledged is emailed again every 4 hours** (decision 5). More than 5 alerts opening within 10 minutes become one digest email.
- **Kind:** operational mail to people who took on the admin role, not a workspace notification, so the opt-in rule in [mail.md](../../../docs/api/mail.md#who-gets-email-and-how-often) is met by the per-admin setting, defaulting to critical only. It isn't triggered by any request, so there's no per-email limit; the hysteresis and digest bound it.
- **Content:** the signal's title, level, value, when it opened, and a sentence of what it usually means. **No link containing the admin key, and no personal data** (decision 6): the email says "Open the admin dashboard to see the details". An email passes through the provider and sits in an inbox; the key and the IPs and addresses behind an alert stay in the dashboard.
- Template `alert.html` / `.txt`, subject from a fixed set ("Critical alert: …", "Resolved: …"), signal titles fixed strings in Go.

### Webhook

| Variable | Notes |
| --- | --- |
| `ALERT_WEBHOOK_URL` | Optional. HTTPS required in production. Every alert event, every level |
| `ALERT_WEBHOOK_FORMAT` | `json` (default) or `slack` (Slack, and the many tools accepting Slack's incoming-webhook shape) |
| `ALERT_WEBHOOK_SECRET` | Optional. Signs the body: `X-Lewcontext-Signature: t=<unix>,v1=<hex HMAC-SHA256 of "t.body">`, so the receiver can check it came from us and isn't replayed |

- `json` body: `{alert, signal, title, level, value, openedAt, resolvedAt, state}`. The same content rules as email: no key, no personal data.
- **Sent by a listener** (`alert.webhook`), so retries and failures are in the Mail page's queue. 10 s timeout, no redirects followed, response body ignored.

### Heartbeat: watching the watcher (decision 7)

| Variable | Notes |
| --- | --- |
| `ALERT_HEARTBEAT_URL` | Optional. The leader `GET`s it every minute after a successful evaluation |

A dead-man's switch: services such as Healthchecks.io, Better Stack or Cronitor alert when the pings stop. That catches what nothing inside the app can: every server down, the database unreachable, or the alert loop wedged. [deployment.md](../../../docs/deployment.md) will recommend it alongside the external uptime check.

## Alerts page

| URL | File | Export |
| --- | --- | --- |
| `/admin/:key/alerts` | `pages/admin/[key]/alerts.tsx` | `AdminAlertIndex` |
| `/admin/:key/alerts/:alert` | `pages/admin/[key]/alerts/[alert].tsx` | `AdminAlertShow` |

- `GET /api/admin/alerts` (`state`: `open` (default), `resolved`, `all`; cursor pagination as Events) and `GET /api/admin/alerts/{alert}`, recorded as `admin.alert.listed` and `admin.alert.viewed`.
- Open alerts first, critical before warn; each with its signal, value, peak, how long it's been open, acknowledged by whom, and a link to the monitoring page and the Events filter behind it.
- **The sidebar's Alerts item shows a count** of open alerts, from `/api/admin/me` (which gains `openAlerts`), so it's visible from any page without another recorded read.
- Acknowledging and silencing are [actions](actions.md).

## Events

| Event | Payload | Recorded by |
| --- | --- | --- |
| `alert.opened` | `alert`, `signal`, `level`, `value` | the evaluator (no actor) |
| `alert.escalated` | `alert`, `signal`, `from`, `to`, `value` | the evaluator |
| `alert.resolved` | `alert`, `signal`, `openFor` (seconds) | the evaluator |
| `admin.alert.listed`, `admin.alert.viewed` | `admin`, filters / `alert` | the reads |

Listeners: `mail.alert` (email to admins) and `alert.webhook`.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| The admin key leaking through alert emails or chat | Never in an email or webhook; the message says to open the dashboard |
| Personal data in third-party inboxes and chat tools | Alerts carry counts and signal names only |
| Alert storms flooding admins | `For` durations, 10-minute hysteresis, one open alert per signal, digests, re-notify only every 4 hours |
| Two servers both alerting | The advisory-lock leader; the unique open-alert index as a backstop |
| A forged webhook to the receiver | The HMAC signature with a timestamp |
| The app dying silently | The heartbeat URL and the external uptime check |
| An attacker triggering alerts to wear admins down | Same bounds; thresholds are on aggregates, and each alert opens once however long the attack lasts |

## Tests

With a fake clock and factory data: an alert opens only after `For`; doesn't flap across the 10-minute hysteresis; escalates instead of duplicating; resolves; only one of two simulated servers evaluates, and the other takes over when the first's lock connection closes; one email per admin at their level, none for `none`; a digest past 5 alerts; re-notification after 4 hours unless acknowledged; the webhook body, signature and Slack shape; failures retried through deliveries; heartbeats sent only after a successful evaluation; no email or webhook body contains the admin key, an email address or an IP.

## Decisions (to confirm)

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Which server evaluates | Whichever holds a PostgreSQL session advisory lock; others stand by |
| 2 | Rules | Monitoring signals marked `Alert`, with `For` durations in code; the table above to start |
| 3 | Flapping | Open after `For`, resolve after 10 minutes at ok, escalate in place |
| 4 | Who gets email | Every admin, critical only by default; each admin changes their own (`all`, `critical`, `none`) on the Admins page |
| 5 | Repeats | A critical alert open and unacknowledged is emailed again every 4 hours; more than 5 openings in 10 minutes become a digest |
| 6 | Content | No admin key, no link carrying it, no personal data, in email or webhook |
| 7 | Watching the app from outside | `ALERT_HEARTBEAT_URL` pinged each minute, plus the external uptime check in the deployment docs |
| 8 | Webhook formats | Generic JSON and Slack's shape, signed with HMAC when a secret is set |
| 9 | Keeping alerts | One year, through `prune` |

## Not included

- **PagerDuty, Opsgenie and SMS** integrations (a webhook relay covers them meanwhile).
- **Thresholds editable in the dashboard**; changes go through code review.
- **On-call schedules and escalation chains.**
- **Alerts per workspace for customers.**
