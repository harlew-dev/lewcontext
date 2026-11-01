# Monitoring

Code: `internal/monitor` (every signal and its thresholds, the pages, their windows), `internal/store/monitor.go` (shared helpers: bucketed counts, metric rows across the minute and hour tables, live limit windows) and `monitor_overview.go`, `monitor_security.go`, `monitor_limits.go` (each page's queries), `internal/httpapi/endpoints/admin/monitoring.go` (`admin.Monitoring.Show`), `internal/seed/monitoring.go` (`make seed only=monitoring`). Web: `pages/admin/[key]/monitoring/index.tsx`, `security.tsx`, `limits.tsx`, `components/app/admin/monitoring.tsx`. Plan: [monitoring](../../plans/features/admin/monitoring.md) (A4), shipped page by page: **part 1 is the groundwork, the Overview, Security and Rate limits**; the other nine pages come with parts 2 and 3.

The app already records what an operator needs to spot trouble (every sign-in attempt and its result, every limit reached, request rates and latency, grouped errors). These pages turn each of those into a **signal**: a number with the levels at which it's a problem, a chart over time, and a link to the rows behind it. The dashboard runs inside the app it watches, so it can't say the app is down; an external uptime check does ([deployment.md](../deployment.md#logs-and-health-checks)).

## How every page works

- **One endpoint per page:** `GET /api/admin/monitoring/{page}?window=1h|24h|7d` (default `24h`), `admin.Monitoring.Show`, behind the admin key and an admin session like every admin route ([admin.md](admin.md#api)). `{page}` is one of the pages built (`monitor.PageNames()`: `overview`, `security`, `limits`); any other is 404 "Page not found.", an unknown window 400 "Check the window.", any other parameter (or one given twice) 400. Viewers read every page: nothing here changes data.
- **One recorded read per page:** `admin.monitoring.viewed` (`admin`, `page`, `window`), actor only, written once the page was read, so a page's signals, charts and review lists are one read. A page that wasn't served (busy, too slow, an error) records nothing, since nothing was read.
- **Bounded:** a page is a set of aggregates, so it shares the Events charts' bound (`httpapi.Summaries`, [admin.md](admin.md#the-summary)): **one page or chart summary in flight per admin**, **two at once per server** (waiting at most 1 s for a slot), and a **5 s deadline** for the whole page. Within it, **each signal and each section has a budget of its own** (2 s, `Summaries.Section`; `monitor/sections.go`): one that isn't read in time is sent as not loaded (a signal with `unavailable: true`, a section's key in the answer's `unavailable` list, its data missing) and the rest of the page still answers, so a slow query, or one someone's own events slow down, hides its part and never the page (`TestAdminMonitoringSectionsFailAlone`). Past the slot it answers `503` "Monitoring is busy. Try again in a moment." (with `Retry-After: 1`); only when nothing at all could be read in time, `503` "This page took too long. Try a shorter window." `admin-read:per-user` still counts the request once. Every query is bounded by a window of 7 days at most (the review lists' takeover lookback reads one account's own sign-ins through its index), read through an index, and returns at most 20 rows per review list.
- **Computed when read** (decision 3), never stored. A signal too slow at real volume moves to the hourly rollup ([observability.md](observability.md#metrics-in-the-database)), not to a slower page.
- **SQL:** every query is Bun's builder in `internal/store/monitor_*.go`, values through `?` placeholders. Pages, windows, signals, limits and outcomes come from fixed Go tables; nothing from input becomes SQL text.
- **Emails and IPs only in review lists** (decision 7): the Security page's lists name the addresses and emails behind an attack, admin-only and recorded; everywhere else is counts. An account is named by its `usr_` ID and name, never its email. No response holds a token, hash or internal ID (`decodeAdmin` walks every one in the tests).

### The response

```json
{
  "page": "security",
  "window": { "name": "24h", "from": "2026-10-31T14:30:00Z", "to": "2026-11-01T14:47:12Z", "bucketSeconds": 1800 },
  "signals": [
    { "name": "auth.failed_sign_ins", "label": "Failed sign-ins", "unit": "count", "value": 1204, "sample": 1204,
      "level": "critical", "warn": 100, "critical": 1000, "windowSeconds": 3600,
      "drill": { "name": "auth.login_attempted", "outcome": "failed", "window": "1h" } }
  ],
  "sections": { "…": "the page's own" },
  "unavailable": []
}
```

- **The window** is resolved by the server, like the Events page's (`monitor.ResolveWindow`; `TestMonitorWindowsMatchEvents` keeps them equal): it starts on its first bucket's boundary and runs to now. Buckets: 1 minute for `1h`, 30 minutes for `24h`, 3 hours for `7d`. Charts list every bucket, empty ones as zeros.
- **Signals keep their own windows** (an hour, a day, or "now"), whichever window the charts show: a signal says how things stand now, the charts how they got there. Each carries its `value` in its `unit` (`count`, `percent`, `ms`, `minutes`, `per_minute`), the `sample` it was counted from, its `level`, its thresholds and `minSample`, its window in seconds, and `drill`, the Events page's filter for its rows (`name` tokens, `outcome`, the `window` of the same length) when it has one.

## Signals (`internal/monitor`)

Each signal is defined once, in Go (decision 2): its name (stable: alerts and these docs use it), its page, label, unit and window, its **warn** and **critical** thresholds (0: no such level), an optional **minimum sample** (below it the level is `ok` whatever the value: a share of a handful means nothing), how it's measured (a store query), and its drill-down. **A value at or over a threshold is at that level**, the higher winning. Thresholds change only by a reviewed commit, and this table with them. Alerts ([alerts](../../plans/features/admin/alerts.md)) will read the same definitions.

A signal's span is its window before now, its start floored to the window's bucket as the Events page floors the same window (an hour to the minute, a day to 30 minutes), so its drill-down lists exactly the rows it counted (and any recorded since). `TestSignalsAreWellFormed` checks names are unique, every drill-down names only catalogue names or groups and a window the Events page takes, and warn is below critical.

| Signal | Page | Counts | Window | Warn / critical |
| --- | --- | --- | --- | --- |
| `auth.failed_sign_ins` | Security | `auth.login_attempted` with `wrong_password` or `unknown_account` | 1 h | 100 / 1,000 |
| `auth.stuffing_addresses` | Security | addresses (an IPv6 address by its /64) that tried **10+ distinct emails** (lower-cased) | 1 h | 1 / – |
| `auth.stuffing_successes` | Security | …of those, addresses with an attempt that got the password right (`succeeded`, `mfa_required`) | 1 h | – / 1 |
| `auth.distributed_guessing` | Security | emails failing from **5+ distinct addresses** | 1 h | 1 / – |
| `auth.unknown_account_share` | Security | share of attempts naming an email with no account | 1 h | 50% / –, from 50 attempts |
| `auth.success_after_failures` | Security | accounts (the payload's `user`) whose right password came after **5+ wrong passwords** | 1 h | – / 1 |
| `auth.possible_takeovers` | Security | a password change, MFA turned off, new recovery codes or other sessions signed out, **within an hour of a sign-in from a network** (IPv4 /24, IPv6 /48) **the account hadn't signed in from in 30 days**, though it had signed in in that time | 24 h | 1 / – |
| `auth.second_factor_failures` | Security | accounts with **3+ failed codes** (`auth.mfa.checked`, `ok` false) | 1 h | 1 / – |
| `auth.reauthentication_failures` | Security | accounts failing their password **3+ times** while signed in (`auth.password_checked`): a stolen session guessing | 1 h | 1 / – |
| `auth.reset_limit_reached` | Security | windows of `password-reset:global` reached (`rate_limit.reached`) | 1 h | – / 1 |
| `auth.sign_up_limit_used` | Security | `register:global`'s live window, its hits as a share of its 1,000 | 1 h | 50% / 100% |
| `admin.door_failures` | Security | `admin.login_attempted` with `wrong_password` or `unknown_account` | 24 h | 1 / – |
| `admin.door_refusals` | Security | `admin.login_attempted` with `not_admin` or `mfa_off`, and `admin.access_refused`: someone holds the key, and maybe a real password | 24 h | – / 1 |
| `limits.shared_reached` | Rate limits | windows reached of the limits shared by everyone (`*:global`): a feature switched off for everyone | 1 h | – / 1 |
| `limits.windows_reached` | Rate limits | every `rate_limit.reached` | 1 h | – |
| `servers.live` | Configuration | servers heard from in the last 2 minutes (`server_instances`) | now | – |
| `servers.versions` | Configuration | versions those servers run: a release in progress, or stopped halfway | now | 2 / – |
| `http.requests_per_minute` | Performance | every server's requests (`request_minutes`) over the minutes of the window | 1 h | – |
| `http.server_error_share` | Performance | 5xx share of those requests | 1 h | 1% / 5%, from 100 requests |
| `http.latency_p95` | Performance | the 95th percentile of their durations, estimated from the summed histograms (linear within a bucket, as Prometheus does; the last bound, 10 s, above it) | 1 h | 1 s / 3 s, from 20 requests |
| `mail.oldest_pending` | Mail | minutes since the oldest waiting delivery's event | now | 2 / 10 |
| `mail.failed_deliveries` | Mail | deliveries failed for good | 24 h | 1 / 10 |
| `errors.new` | Errors | error groups first seen | 24 h | 1 / – |
| `errors.new_panics` | Errors | …of them, panics | 24 h | – / 1 |
| `errors.open` | Errors | open (unresolved) error groups | now | – |
| `usage.sign_ups` | Usage | `auth.registered` | 24 h | – |
| `usage.active_accounts` | Usage | accounts with `last_seen_at` in the window ([below](#active-accounts-userslast_seen_at)) | 24 h | – |

The thresholds are starting points sized from the existing limits (`TestThresholdsMatchTheLimits` checks the shared limits they read exist). Metric signals read `request_minutes`; with `METRICS_STORE=off` they read nothing and show zero.

## Overview

A grid of tiles, each an area with one or more signals: its worst level in words, its first signal as the number, the others listed under it with their levels, a sparkline over the page's window, and a link. Tiles: **Servers** (live, versions), **Requests**, **Server errors**, **Latency**, **Mail** (oldest waiting, failed), **Errors** (new, new panics, open), **Failed sign-ins**, **Shared limits**, **Admin door** (refusals, failures), **Sign-ups** (sign-ups, active accounts).

- **Pages not built yet:** a tile whose page is Security or Rate limits links there, Mail to the Mail page; Performance, Errors, Usage and Configuration come with parts 2 and 3, so their tiles show their numbers now (the data is already there: metrics, grouped errors, `last_seen_at`) and say "Its page comes later.", linking to their Events rows when they have any. **Integrity and Alerts have no tile** until their pages exist (their checks and rules don't yet). The Database tile (pool waits, ping time) comes with Performance.
- **Sparklines:** requests, 5xx share and p95 per bucket from the request minutes (over 7 days, the hours summed so far and the minutes after them, so nothing is counted twice: `requestRows`); failed sign-ins, shared limits reached, the admin door's failures and refusals, and sign-ups per bucket from events.

## Security

From `auth.login_attempted`, `auth.mfa.checked`, `auth.password_checked`, `auth.password_reset.*`, `auth.registration.requested`, `auth.registered`, `auth.logged_in`, `admin.login_attempted`, `admin.access_refused`, `admin.session.moved`, the takeover changes, `rate_limits` and `sessions`. Every review list groups by the hour from the window's start ("in an hour"), at most 20 rows:

| Section | Shows |
| --- | --- |
| Sign-in attempts | per bucket by result (signed in, right password and a code asked, wrong password, unknown account), and the share for unknown accounts |
| Credential stuffing | hour, address (linking to its sign-in attempts), distinct emails, attempts, how many signed in |
| Distributed guessing | hour, email (linking to its events), distinct addresses, failures |
| Success after failures | account, wrong passwords, when it signed in |
| Possible takeovers | account, the new network, the sign-in, the change (linking to its event) |
| Second factor | accounts failing 3+ codes in an hour; recovery codes used; accounts left with 2 or fewer after using one (the payload's `recoveryCodesLeft`) |
| Password checks | accounts failing 3+ password checks in an hour |
| Password resets | asked for against sent per bucket (a gap is someone trying addresses), the addresses asking most, `password-reset:global` against its cap |
| Sign-ups | codes asked for and accounts made per bucket, the addresses making the most accounts, `register:global` against its cap |
| Admin door | counts per outcome (wrong password, unknown account, not an admin, admin without MFA, address not allowed, session moved), per bucket as failed sign-ins, refusals and moved sessions (three series: a chart stacks five at most, which `TestAdminMonitoringPages` checks for every chart), and the latest 20 with time, outcome and address (never the email typed), each linking to its event |
| Sessions | sign-ins per bucket, live app sessions, accounts at the 5-session cap |

**Indexes:** `events_name_created_idx (name, created_at DESC)` finds every name in its window; the takeover check starts from the sign-ins in the window (and the hour before), keeps those from a new network by walking the account's earlier sign-ins through `events_logged_in_idx (user_id, created_at DESC) WHERE name = 'auth.logged_in'`, and only then looks up the account's changes in the hour after each (`events_user_created_idx`). It's driven by sign-ins, which sign-in's limits bound, so an account recording thousands of its own changes doesn't slow it (`TestAdminMonitoringTakeoverFlood`). `sessions` holds live sessions only (the cleanup).

## Rate limits

| Section | Shows | From |
| --- | --- | --- |
| Shared limits | every `*:global` limit: its live window's hits against its cap (a meter with its level: half used a warning, reached critical), when the window ends, and how often it was reached in the page's window | `rate_limits` (keys are the bare name), `rate_limit.reached` |
| Windows reached | per bucket by limit (the four reached most, then Other), and per limit with the last time. A window reached is counted once, however many requests it then refused | `rate_limit.reached` |
| Requests refused | every refusal (429 or 503) per limit | `rate_limit_refused_total` in `metric_minutes` (and `metric_hours` over 7 days) |
| Blocked right now | per limit counted by subject, how many subjects are at or over it in their live window. Keys are digests: how many, never who. A limit counted in each server's memory (`api-read:per-user`, `admin-read:per-user`) can't be seen, and says so | `rate_limits`, keys split on the name, compared with the limits declared in Go |
| Accounts / addresses reaching the most | top 10 by windows reached | `rate_limit.reached`'s `user_id` and `ip` |
| Budgets | accounts reaching `api:per-user` or `api-read:per-user`: a script, a scraper or a runaway tab | `rate_limit.reached` |
| Quotas | refusals per quota | `quota_refused_total` |
| By route | routes answering 429, with requests and 5xx (a shared limit's 503 counts among them: `request_minutes` doesn't keep 503 apart) | `request_minutes` (and `request_hours`) |

## Active accounts: `users.last_seen_at`

`users.last_seen_at` is when the account last used a session, to the hour. The session check (`store.FindSession`, behind `RequireSession` and the `/app/*` page gate) moves it **in the same statement** that refreshes the session's idle time, and only when it's null or over an hour old (`seenEvery`), so an active account costs one row write an hour and the check no extra round trip. It never records an event: it isn't something the account did, and an event an hour per account would fill the audit trail. It's null until the account's first signed-in request; an expired or idle session moves nothing. A partial index (`users_last_seen_idx`) serves counting the accounts seen since a time. `TestSessionCheckMovesLastSeenHourly` (`internal/store`) checks it.

## Web app

- **Navigation:** a **Monitoring** heading in the admin sidebar under the other pages (decision 5), with Overview, Security and Rate limits; later pages join it as they're built.
- **A page** (`AdminMonitoringOverview`, `AdminMonitoringSecurity`, `AdminMonitoringLimits`) is one `useAdminResource` call (`useMonitoringPage`): the window in the URL (`?window=1h`, the default left out; each change a navigation, so Back undoes it), the heading with a **time range** `Listbox` on the right, and a line with the window the server resolved, in UTC. Nothing reloads on its own: each load is a recorded read.
- **States:** a skeleton until the first answer; a new window dims what's shown (`aria-busy`); a failure keeps what was shown with a `Notice` and **Try again**. A section the server couldn't read in time shows its heading and a notice with **Try again** in its place (`SectionGate`), and a signal "Not loaded"; Try again loads the whole page again (one more recorded read).
- **Signals** are `Stat` tiles: the label linking to the rows behind it, the level as a `LevelBadge` (OK, Warning, Critical, with an icon: never colour alone), the value in its unit, and its window and thresholds in words.
- **Charts** are `BarChart`s with **Show as table**, and `Sparkline`s in the Overview's tiles; both keep a summary sentence and a table for screen readers. Shared limits are `Meter`s. Red, amber and green appear only for levels, always with their word ([ui/design.md](../ui/design.md#charts)).

## Demo data: `make seed only=monitoring`

Plants what the Security and Rate limits pages look for, in a development database: a day of ordinary sign-ins, then in the last hour credential stuffing from `203.0.113.66` (40 emails, one guess working), one password guessed from six addresses, an account signed in after six wrong passwords and then failing codes, a reset flood from `198.51.100.23`, `register:global` reached, wrong passwords and a non-admin's right password at the admin door, and a fake account taken over from a new network. Addresses are from the documentation ranges, emails `@example.test`, every payload `"seeded": true`. It never writes `rate_limits` (a counter there would really refuse requests) or metrics and errors (made-up traffic would be misleading). Insert-only; refused in production. `TestMonitoringSeeder` checks each planted pattern reaches its level ([seeding.md](seeding.md)).

## Tests

- **Signals** (`endpoints/admin/monitoring_integration_test.go`): `TestAdminMonitoringSecuritySignals` puts each Security signal just under and just over its threshold (99 and 100 failed sign-ins, 9 and 10 emails from one address with a repeat in other case, then a success; 4 and 5 addresses for one email, an IPv6 /64 counting once; the unknown share at 50% and under the minimum sample; 4 and 5 failures before a success; 2 and 3 failed codes and password checks; the shared limits; the admin door's failure, refusal and code step, and events outside the window), and checks a drill-down lists exactly the rows counted. `TestAdminMonitoringTakeovers`: a new network then a password change within the hour counts; the same /24, no sign-in in 30 days, and a change two hours later don't. `TestAdminMonitoringLimits`: every section, the shared limit critical, subjects at a limit (an ended window and one under it left out, a memory limit null, a shared limit not listed). `TestAdminMonitoringOverview`: servers and versions, requests, 5xx share and p95 from minutes, a week joining the summed hours, the mail queue, error groups, sign-ups and active accounts, each at its level.
- **Budgets:** `TestAdminMonitoringSectionsFailAlone` locks the sessions table: Security answers with its Sessions section listed as unavailable, every signal read and one read recorded. `TestAdminMonitoringTakeoverFlood`: 20,000 of an account's own changes leave the takeover check fast.
- **The endpoint:** `TestAdminMonitoringPages` (every page and window, one `admin.monitoring.viewed` each with the page and window, a viewer reading; an unknown page 404, a bad or repeated window or another parameter 400, no session 401, none recorded) and `TestAdminMonitoringBounds` (busy with the admin's slot taken, too slow under a 1 ns deadline: 503, nothing recorded). The key and session checks cover the three routes (`adminRoutesList`).
- **Unit** (`internal/monitor`): `TestSignalsAreWellFormed`, `TestThresholdsMatchTheLimits`, `TestLevelOf` (at or over, the higher wins, under the minimum sample ok), `TestPercentile`, `TestSignalSpans`.
- **Vitest** (`pages/admin/monitoring.test.tsx`): tiles with their levels in words and links (none for a page not built), a sparkline's table, the sidebar's Monitoring heading; a failed reload keeping the data with Try again and the window in the URL; Security's stuffing address and signal links, the shared limit's meter, Show as table; Rate limits' counts and the memory limit.
- **Playwright** (`admin.spec.ts`): a seeded stuffing address appears on Security, reached from the Overview's tile, links to its events, the window in the URL, and each load records one read.
