# Admin: Monitoring pages

Status: **draft 2026-10-31**, decisions to confirm. Plan A4 of the [admin plans](README.md). Needs [events-and-mail](../implemented/admin/events-and-mail.md) (`events.ip`, the Events page to drill into) and [observability](observability.md) (metrics, errors, servers). Docs it will change: [admin.md](../../../docs/api/admin.md), [endpoints.md](../../../docs/api/endpoints.md), [database.md](../../../docs/api/database.md), [events.md](../../../docs/api/events.md), [seeding.md](../../../docs/api/seeding.md), [ui/design.md](../../../docs/ui/design.md), [ui/pages.md](../../../docs/ui/pages.md); a new `docs/api/monitoring.md` (every signal, its query and thresholds).

## Why

The app already records most of what an operator needs to spot trouble: every sign-in attempt with its result, every limit reached, every workspace, project and invitation created. Observability adds request rates, latency and errors. Nobody can see any of it as a picture. Abuse that's visible in the data today goes unnoticed until it costs something:

- **brute force and credential stuffing** against sign-in;
- **using our email as a spam carrier** through invitations, with a URL in the inviter's name;
- **one workspace or account filling the database** up to its quotas, or one IP creating many accounts;
- **mail or the event worker silently stopped**;
- **duplicate creates** from double submits and retries;
- **slow routes and rising errors** after a release.

These pages turn each of those into a **signal** with a threshold, a chart, and a link to the rows behind it.

## How every page works

- **One endpoint per page:** `GET /api/admin/monitoring/{page}?window=1h|24h|7d` (default `24h`), handler `adminMonitoringHandlers.show`, `{page}` checked against the list below. It records one `admin.monitoring.viewed` (`admin`, `page`, `window`). Detail pages (one IP, one error group) have their own `show` routes.
- **Signals, defined once** (decision 2) in a new package, `internal/monitor`:

  ```go
  // A Signal is one number worth watching, with the levels at which it's a
  // problem. Pages show it; alerts (alerts.md) evaluate the same definition.
  type Signal struct {
  	Name     string        // "auth.failed_logins", stable: alerts and docs refer to it
  	Page     string        // "security"
  	Window   time.Duration // what it's counted over
  	Warn     float64       // 0: no warning level
  	Critical float64
  	Measure  func(ctx context.Context, s *store.Store, now time.Time) (float64, error)
  	Drill    func(now time.Time) url.Values // the Events filter behind it
  }
  ```

  Each page response carries its signals with `value`, `level` (`ok`, `warn`, `critical`) and `drill`, then the page's charts and tables. Thresholds are in code, changed by a reviewed commit, and listed in `docs/api/monitoring.md`.
- **Computed when read** (decision 3), every query bounded by the window (7 days at most) and an index, under a 5 s deadline per page. A signal too slow at real volume moves to the hourly rollup job ([observability](observability.md)), not to a slower page.
- **Everything drills down:** every number and bar links to the Events page filtered to its rows, or to a User, Workspace, IP or error page.
- **Viewers can read every page** ([access hardening](access-hardening.md) roles); nothing here changes data.
- Queries live in `internal/store/monitor_<page>.go`, written with Bun's builder like every other query, including those on PostgreSQL's statistics views (`TableExpr("pg_stat_user_tables")`).

## The pages

Shipped in this order (decision 1), each usable on its own:

| # | Page | Answers |
| --- | --- | --- |
| 1 | [Overview](#1-overview) | Is everything all right, and if not, where? |
| 2 | [Security](#2-security) | Is anyone attacking sign-in, MFA, password reset or the admin door? |
| 3 | [Rate limits](#3-rate-limits) | Which limits are being hit, by whom, and is any shared limit shutting everyone out? |
| 4 | [IP addresses](#4-ip-addresses) | Which addresses are noisy or abusive, and what did one address do? |
| 5 | [Accounts](#5-accounts) | Which accounts behave like spam or abuse? |
| 6 | [Workspaces](#6-workspaces) | Which workspaces are filling up, growing fast, or used to send spam? |
| 7 | [Usage](#7-usage) | How many people use the app, and what do they use? |
| 8 | [Data](#8-data) | How big is the database, what's growing, is pruning running? |
| 9 | [Integrity](#9-integrity) | Is anything true that should never be? Any duplicates from retries? |
| 10 | [Performance](#10-performance) | Which routes are slow, is the database or the worker saturated? |
| 11 | [Errors](#11-errors) | What's failing, since when, and did the last release cause it? |
| 12 | [Configuration](#12-configuration) | Which servers and versions are live, and is anything set unsafely? |

Default thresholds below are starting points, sized from the existing limits; `docs/api/monitoring.md` keeps the real list.

### 1. Overview

A grid of tiles, each a signal from another page with its level, a sparkline over the window, and a link:

| Tile | Signal | Warn / critical |
| --- | --- | --- |
| Servers | live servers, versions | a server silent 2 min / none live (shown by the external check, really) |
| Requests | per minute | none |
| Server errors | 5xx share of requests | 1% / 5% |
| Latency | p95 across routes | 1 s / 3 s |
| Database | pool waits per minute; ping time | waits sustained 5 min / ping > 500 ms |
| Mail | oldest pending delivery; failed in 24 h | 2 min / 10 min; 1 / 10 |
| Errors | open groups; new in 24 h | 1 new / 1 new panic |
| Failed sign-ins | per hour | 100 / 1,000 |
| Limits | shared (`global`) limits reached in window | none / any |
| Admin door | refusals of real credentials (`not_admin`, `mfa_off`), `admin.access_refused` | none / any |
| Integrity | checks failing | any warning check / any critical check |
| Sign-ups, active accounts | per day | none |
| Alerts | open ([alerts](alerts.md)) | any / any critical |

### 2. Security

From `auth.login_attempted`, `auth.mfa.checked`, `auth.password_checked`, `auth.password_reset.*`, `admin.login_attempted`, `admin.access_refused`, `admin.session.moved`, sessions.

| Section | Shows | Signal (warn / critical) |
| --- | --- | --- |
| Sign-in attempts | per hour by `result`, stacked | failed per hour (100 / 1,000) |
| **Credential stuffing** | IPs (IPv6 by /64) that tried **10+ distinct emails** in an hour, with counts and how many succeeded | any IP (warn) / any with a success (critical) |
| **Distributed guessing** | emails with failures from **5+ distinct IPs** in an hour | any |
| **Unknown accounts** | share of attempts for emails with no account. A list being replayed is mostly misses | > 50% with 50+ attempts |
| **Success after failures** | accounts signed in after 5+ failures within the hour: the attack that worked | any (critical) |
| **Possible takeover** | a sign-in from a network (IPv4 /24, IPv6 /48) the account hasn't used in 30 days, followed within an hour by a password change, MFA turned off, new recovery codes or other sessions revoked | any |
| Second factor | failed codes per account (3+ an hour), recovery codes used, accounts with 2 or fewer left | failed codes 3+ (warn) |
| Password checks | failed `auth.password_checked` per account: a stolen session guessing the password to delete or disable something | 3+ an hour |
| Password reset | requests per hour, per IP; requested vs sent (misses mean probing) | `password-reset:global` reached (critical) |
| Sign-ups | per hour, per IP; vs `register:global` (1,000/h) | 50% of the global limit / reached |
| **Admin door** | every `admin.login_attempted` that wasn't a success, by result; `admin.access_refused`; `admin.session.moved` | `wrong_password` (warn); `not_admin`, `mfa_off`, `access_refused` (critical: someone holds the key, and maybe a real password) |
| Sessions | sign-ins per hour, live sessions, accounts at the 5-session cap | none |

### 3. Rate limits

| Section | Shows | From |
| --- | --- | --- |
| Windows reached | per limit, per hour | `rate_limit.reached` (one per limit per window) |
| Requests refused | per limit, per hour: every 429 and 503, not just the first | `rate_limit_refused_total` ([observability](observability.md)) |
| **Shared limits** | `register:global`, `password-reset:global`, `invitation:global`: this window's count against the cap, and every time one was reached. A shared limit reached is a feature switched off for everyone | `rate_limits` (their keys are the bare name), events. **Critical** when reached |
| Blocked right now | per limit, how many subjects are at or over it this window. Keys are digests, so this shows how many, never who; the events show who | `rate_limits`, keys split on their name prefix, compared with the limits in Go |
| Top subjects | accounts and IPs with the most windows reached in the window | events' `user` and `ip` |
| Budgets | accounts reaching `api:per-user` or `api-read:per-user`: scripts, scrapers or a runaway browser tab | events |
| Quotas | refusals per quota | `quota_refused_total` |
| By route | 429 and 503 per route | `http_requests_total` |

The page says plainly that a window reached is counted once however many requests it refused.

### 4. IP addresses

- **Top addresses** in the window, sortable by events, failed sign-ins, sign-ups, limits reached and distinct accounts; an "IPv6 by /64" toggle.
- **Sign-up clusters:** addresses that registered **5+ accounts** in 24 hours (warn), 20+ (critical). The usual shape of a spam farm.
- **Shared-address warning:** when one address accounts for over 80% of request events (with 50 or more), a banner says the proxy's client IP probably isn't configured ([deployment](../../../docs/deployment.md#behind-a-proxy-or-load-balancer)), since every number on this page is then wrong.
- **One address:** `/admin/:key/monitoring/ips/:ip` (`GET /api/admin/ips/{ip}`, recorded as `admin.ip.viewed`): first and last seen, counts by event name, the accounts seen from it (signed in, registered, failed), limits reached, and a link to its events. An IPv6 address offers its /64.

### 5. Accounts

A **review list** of accounts with one or more abuse signals in the window, each listed with the signals it tripped (decision 7):

| Signal | Threshold (24 h) | Why |
| --- | --- | --- |
| Workspaces created | 5+ | the limit is 10/h, the quota 20; real people make one or two |
| Invitations sent | 20+, to 15+ distinct addresses | invitations are email we send to addresses the inviter picks |
| **Invitation acceptance** | under 10% accepted, with 20+ sent over 7 days | spam isn't accepted |
| Projects created | 200+ | filling the database |
| **Links or markup in the name** | the account's name, or a workspace it owns, contains `http`, `www.`, a domain-looking word, `<` or `>` | the inviter's and workspace's names are printed in invitation emails: the classic way to turn someone else's mail into spam (decision 8) |
| Limits reached | any per-user limit | |
| **New and busy** | registered in the last 24 h and tripped any of the above | |
| Failed password or MFA checks | 3+ | a stolen session |

Also: top accounts by writes (events as actor), and accounts owning the most workspaces. Each row links to the account and its activity. Acting on one (suspending) is in [actions](actions.md).

### 6. Workspaces

| Section | Shows | Signal |
| --- | --- | --- |
| **Quota headroom** | workspaces at 80%+ of any quota: projects (1,000), members plus invitations (100), pending invitations (50); owners at 80%+ of 20 owned workspaces. A meter per quota | 80% (warn), 100% (critical: it's refusing) |
| **Rows per workspace** | top workspaces by rows across **every table with a `workspace_id`**, from a registry that a test keeps complete (decision 9), so a future table (tasks, files) is counted without anyone remembering | top workspace over 50,000 rows (warn) |
| Growth | top workspaces by rows added in the window, and events recorded (activity) | none |
| Invitations | per workspace: sent, accepted, revoked, expired over 7 days; low acceptance with volume | as Accounts |
| **Throwaway workspaces** | created and deleted within 24 hours, especially after sending invitations | 3+ by one account |
| Totals | workspaces, created and deleted per day | none |

### 7. Usage

Product numbers, from the data the app already has (decision 6 adds one column):

- **Active accounts** per day, 7 days and 30 days (DAU / WAU / MAU), from `users.last_seen_at`.
- **Sign-ups** per day and **activation**: the share that created or joined a workspace within a day, and that invited someone within a week.
- **Feature use:** events per day by name (projects created and updated, invitations sent and accepted, MFA turned on), and the share of active accounts with MFA on.
- **Workspace sizes:** how many workspaces have 1, 2–5, 6–20, 21+ members, and 0, 1–10, 11–100, 100+ projects.

### 8. Data

| Section | Shows | From |
| --- | --- | --- |
| Database | total size, connections by state, the longest-running query and transaction, sessions idle in a transaction, locks waited on, cache hit ratio | `pg_database_size`, `pg_stat_activity`, `pg_locks`, `pg_stat_database` |
| Tables | per table: total, table and index size, live and dead rows, dead share, last autovacuum and analyze, sequential vs index scans (a table read by scans is missing an index) | `pg_stat_user_tables`, `pg_total_relation_size` |
| **Growth** | rows and bytes per table per day over 30 days, and a projection ("events: +120k rows a day, 2.1 GB; at this rate 10 GB in 64 days") | a daily snapshot, `table_stats_daily` (below) |
| What fills the log | events per day by name; the top few names' share | events |
| **Retention** | the oldest event's age against prune's 90 days, and the last `prune.completed` per target | events. Warn when the oldest event is over 97 days: prune isn't scheduled ([compliance](compliance.md) depends on it) |

```sql
-- create_table_stats_daily (a new migration)
CREATE TABLE table_stats_daily (
 day date NOT NULL,
 table_name text NOT NULL,
 live_rows bigint NOT NULL,
 total_bytes bigint NOT NULL,
 PRIMARY KEY (day, table_name)
);
```

Written once a day by the hourly job ([observability](observability.md)), kept 400 days (a prune target). `cmd/prune` starts recording `prune.completed` (`target`, `deleted`, `kept`, `cutoff`) after a confirmed run (decision 10). Disk space on the database server isn't visible from SQL: the provider's own monitoring covers it, and the docs say so.

### 9. Integrity

**Checks of things that should never happen.** Each is one bounded query returning a count and up to 10 public IDs as samples, run when the page loads and every hour by [alerts](alerts.md):

| Check | Level | A failure means |
| --- | --- | --- |
| Every workspace has an owner | critical | a membership change or deletion bug |
| No workspace over a quota | critical | the quota lock was bypassed |
| No live invitation to someone already a member | warn | accepting didn't consume it |
| No admin without MFA outside the setup window | critical | a path turned MFA off for an admin |
| No session, admin session, challenge or reset link expired over 30 minutes ago | warn | the cleanup loop isn't running |
| No ended rate-limit window over 30 minutes old | warn | same |
| **No pending delivery overdue by 10+ minutes** | critical | the event worker is stalled or every server is down |
| No delivery failed with "no listener" | warn | a listener was renamed with deliveries queued |
| No event name outside the catalogue | warn | old code, or a write outside `record` |
| Every live server has the same version and every migration applied | warn after 30 minutes | a release stopped halfway |

**Duplicates and retries** (the idempotency questions):

| Check | Shows |
| --- | --- |
| **Near-duplicate creates** | the same actor recording the same `workspace.created`, `project.created` or `workspace.invitation.created` with the same name or email within 10 seconds: a double submit or a client retrying a create that had succeeded. Count per day and samples |
| **Duplicate sends** | `auth.password_reset.sent` or `workspace.invitation.sent` recorded more than once for one `request`: at-least-once delivery sending twice |
| **Retried after a timeout** | deliveries that succeeded on a later attempt after a `context deadline exceeded` error: the first send may also have gone out |
| **Writes that timed out** | mutating requests ended by the 10 s timeout, per route. The client sees an error, but the change may have committed, and a retry then duplicates it |

The creates aren't idempotent today (the web app disables a button while it submits, but nothing stops a network retry). If these numbers aren't zero, an **`Idempotency-Key`** plan for create routes is the fix; it isn't part of this one.

### 10. Performance

From `request_minutes`, `metric_minutes` and their hourly sums, all servers summed, with a per-server toggle:

- **Requests:** per minute, 5xx share, p50 / p95 / p99 latency.
- **Routes:** a sortable table of every route pattern: requests, p95, p99, 4xx, 429, 5xx, timeouts. The slowest five highlighted.
- **Database:** query p95 by operation, slow queries per table, the pool (10 connections: in use, idle, waits, time waited: waits mean the pool is the bottleneck), and, where the database has `pg_stat_statements`, the 10 statements with the most total time (PostgreSQL shows them with `$1` placeholders, never values).
- **Event worker:** deliveries per minute, lag p95, pending, failures.
- **Password hashing:** wait p95 for the 4 slots. Waits mean sign-in is saturated, from load or an attack.
- **Each server:** goroutines, heap, GC pauses, uptime, version.

| Signal | Warn / critical |
| --- | --- |
| p95 latency, all routes | 1 s / 3 s |
| a route's p95 (with 20+ requests) | 2 s / 5 s |
| 5xx share | 1% / 5% |
| pool waits | any sustained 5 min / time waited p95 > 1 s |
| delivery lag p95 | 1 min / 10 min |
| hashing wait p95 | 500 ms / 2 s |

### 11. Errors

- **Open groups** from `app_errors`, newest occurrence first: message, source, route, count in the window (from the `errors_total` metric), first and last seen. **New since the current release** (first seen after the newest version's first `server.started`) is highlighted, which answers "did the deploy break something?"
- **One group:** `/admin/:key/monitoring/errors/:error` (`GET /api/admin/errors/{error}`, recorded as `admin.error.viewed`): the scrubbed message, stack, route, counts per hour, versions seen, and the last request ID to search the logs with. Resolving a group is an [action](actions.md).
- `app_errors` gains `public_id … DEFAULT public_id('err')` for the URL.

### 12. Configuration

- **Servers:** every live `server_instances` row: hostname, version, started, last seen. Mixed versions after a release are flagged.
- **Settings:** each server's non-secret summary side by side, differences highlighted.
- **Warnings**, each with what to do: `MAIL_DRIVER=off`; one IP for most traffic (proxy header unset); `ADMIN_ALLOWED_IPS` without `CLIENT_IP_HEADER`; `ADMIN_ROUTE_KEY_PREVIOUS` still set, with the sessions using it; a public `METRICS_ADDR` without a token; migrations pending; no admin with MFA; prune not running.

## Data changes

| Change | Why |
| --- | --- |
| `users.last_seen_at timestamptz` (edited into `002`), set by `requireSession` **only when it's over an hour old** (one conditional `UPDATE … WHERE last_seen_at < now() - interval '1 hour'`), never recording an event | active accounts, for Usage; inactive accounts, for [compliance](compliance.md) (decision 6) |
| `table_stats_daily` | growth over time |
| `app_errors.public_id` | the error page's URL |
| `prune.completed` event | retention evidence |
| Indexes as each query needs them, listed in `docs/api/monitoring.md` | bounded queries; e.g. `events (actor_id, name, created_at)` for the per-account signals |

Each new table gets a factory (`TestEveryTableHasAFactory`).

## Web app

- **Pages** under `pages/admin/[key]/monitoring/`: `index.tsx` (Overview, `AdminMonitoringOverview`), `security.tsx`, `limits.tsx`, `ips.tsx` and `ips/[ip].tsx`, `accounts.tsx`, `workspaces.tsx`, `usage.tsx`, `data.tsx`, `integrity.tsx`, `performance.tsx`, `errors.tsx` and `errors/[error].tsx`, `configuration.tsx`. Each is one `useAdminResource` call and local sections.
- **Navigation:** a "Monitoring" heading in the admin sidebar with an item per page (decision 5), not a tab bar of twelve.
- **A window switch** (1 h, 24 h, 7 d) in the page header, kept in the URL.
- **New primitives in `components/ui`** (decision 4), charts through `components/ui/chart.tsx` (Recharts, from [events and mail](../implemented/admin/events-and-mail.md#charts-decision-12)) with Tailwind classes and `dark:` variants: `Stat` (a tile: label, value, level, sparkline, link), `Sparkline`, `BarChart` (time buckets, stacked), `Meter` (a quota's fill), `LevelBadge`. Colour is zinc, with red, amber and green **only** for levels, always with a word beside it ("Critical"), never colour alone. Every chart has a text summary for screen readers and a "Show as table" toggle. Shared monitoring pieces go in `components/app/admin/`.
- **Loading and errors:** skeletons per section, a Retry on failure that keeps the last data, as AGENTS.md rule 8 asks.

## Seeding

`make seed only=monitoring` inserts fake patterns into the development database so every page has something to show: failed sign-ins from a few IPs (one stuffing), an account sending invitations with a URL in its name, a workspace near its project quota, a stuck delivery, an error group. Fake data only, like the other seeders; refused in production.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| The pages concentrate personal data (emails, IPs) | Admin only; counts wherever a count will do; emails and IPs only in the review lists and detail pages; every page and lookup recorded with what was looked at |
| An admin using IP or account lookups to follow someone | `admin.ip.viewed`, `admin.monitoring.viewed` and the other reads are in the trail an auditor reviews ([compliance](compliance.md)) |
| A heavy page slowing the app's database | Bounded windows, indexes per query, a 5 s deadline, the admin read budget, rollups for anything slow |
| SQL in signal and check queries | Bun's builder and `?` placeholders only; pages and windows from fixed lists |
| Wrong conclusions from wrong IPs (behind a proxy) | The shared-address banner and the Configuration warning |
| False reassurance | The README's split: the dashboard can't say the app is down; the external check does |
| Colour-only status | Levels always carry a word and an icon |

## Tests

- **Every signal:** factory data just under and just over each threshold gives `ok`, `warn`, `critical`; its drill-down filter returns exactly the rows counted.
- **Every integrity check:** passes on a clean database and catches one seeded violation, with the right sample IDs.
- `TestEveryWorkspaceTableIsMonitored`: every table with a `workspace_id` column (from `information_schema`) is in the registry.
- Each page endpoint records one event; an unknown page is 404; viewers can read; without the key, the unknown-route 404.
- `users.last_seen_at` is written at most once an hour per account.
- **Volume** (manual, `docs/testing.md`): with `make seed only=volume` at a million events, every page answers within 2 seconds.
- **Vitest:** each primitive renders its text summary and table; a level always has its word; a section's error keeps the last data and offers Retry.
- **Playwright:** the seeded stuffing IP appears on Security and links to its events; Overview's tiles link to their pages.

## Decisions (to confirm)

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Which pages, in what order | The twelve above, shipped in that order; Overview, Security and Rate limits are a useful first release on their own |
| 2 | Where signals and thresholds live | Once, in `internal/monitor`, shared with alerts; thresholds in code, changed by a reviewed commit and listed in `docs/api/monitoring.md`. Environment overrides only if an operator needs them later |
| 3 | Computing numbers | On read, bounded to 7 days, indexed, 5 s deadline. Rollups only for a signal that proves slow |
| 4 | Charts | Revised by [events and mail](../implemented/admin/events-and-mail.md) decision 12: the shared `components/ui/chart.tsx` on Recharts (already in the admin bundle), with `Sparkline`, a stacked `BarChart` and `Meter` added beside it; our own styling and dark mode through Tailwind classes |
| 5 | Navigation | A Monitoring heading in the sidebar with an item per page |
| 6 | Active accounts | `users.last_seen_at`, written at most hourly by `requireSession`, without an event |
| 7 | Emails and IPs in review lists | Shown, admin-only and recorded; everywhere else counts |
| 8 | Links in account and workspace names | Flagged on Accounts only. Refusing them at sign-up and rename would be a product change for another plan |
| 9 | Rows per workspace | A registry of workspace tables, kept complete by a test |
| 10 | Prune evidence | `cmd/prune` records `prune.completed` after a confirmed run |
| 11 | Demo data | A `monitoring` seeder with fake attack patterns |
| 12 | Locations | No GeoIP, ASN or reverse DNS in v1 (a licensed database or outbound lookups, for little gain at this size) |

## Not included

- **Anomaly detection** against learned baselines; fixed thresholds first.
- **GeoIP, ASN and reverse DNS** (decision 12).
- **An `Idempotency-Key`** for creates: a separate plan, if the Integrity numbers call for it.
- **A public status page.**
- **Synthetic transactions** (a robot signing in every minute): the external check covers "up".
- **Acting** on anything here: [actions](actions.md).
- **Alerts:** [alerts](alerts.md).
