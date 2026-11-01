# Monitoring

Code: `internal/monitor` (every signal and its thresholds, the pages, their windows), `internal/store/monitor.go` (shared helpers: bucketed counts, metric rows across the minute and hour tables, live limit windows) and `monitor_overview.go`, `monitor_security.go`, `monitor_limits.go`, `monitor_ips.go`, `monitor_accounts.go`, `monitor_workspaces.go`, `monitor_usage.go` (each page's queries), `workspace_tables.go` (the workspace-tables registry), `internal/httpapi/endpoints/admin/monitoring.go` (`admin.Monitoring.Show`) and `ip_addresses.go` (`admin.IPAddresses.Show`, one address), `internal/seed/monitoring.go` and `monitoring_abuse.go` (`make seed only=monitoring`). Web: `pages/admin/[key]/monitoring/index.tsx`, `security.tsx`, `limits.tsx`, `ips.tsx`, `ips/[ip].tsx`, `accounts.tsx`, `workspaces.tsx`, `usage.tsx`, `components/app/admin/monitoring.tsx`. Plan: [monitoring](../../plans/features/admin/monitoring.md) (A4), shipped page by page: **part 1 is the groundwork, the Overview, Security and Rate limits; part 2 IP addresses (and one address), Accounts, Workspaces and Usage**; the other five pages come with part 3.

The app already records what an operator needs to spot trouble (every sign-in attempt and its result, every limit reached, request rates and latency, grouped errors). These pages turn each of those into a **signal**: a number with the levels at which it's a problem, a chart over time, and a link to the rows behind it. The dashboard runs inside the app it watches, so it can't say the app is down; an external uptime check does ([deployment.md](../deployment.md#logs-and-health-checks)).

## How every page works

- **One endpoint per page:** `GET /api/admin/monitoring/{page}?window=1h|24h|7d` (default `24h`), `admin.Monitoring.Show`, behind the admin key and an admin session like every admin route ([admin.md](admin.md#api)). `{page}` is one of the pages built (`monitor.PageNames()`: `overview`, `security`, `limits`, `ips`, `accounts`, `workspaces`, `usage`); any other is 404 "Page not found.", an unknown window 400 "Check the window.", any other parameter (or one given twice) 400. Viewers read every page: nothing here changes data.
- **One recorded read per page:** `admin.monitoring.viewed` (`admin`, `page`, `window`), actor only, written once the page was read, so a page's signals, charts and review lists are one read. A page that wasn't served (busy, too slow, an error) records nothing, since nothing was read. One address's page is its own read, `admin.ip.viewed` ([below](#one-address)).
- **Bounded:** a page is a set of aggregates, so it shares the Events charts' bound (`httpapi.Summaries`, [admin.md](admin.md#the-summary)): **one page or chart summary in flight per admin**, **two at once per server** (waiting at most 1 s for a slot), and a **5 s deadline** for the whole page. Within it, **each signal and each section has a budget of its own** (2 s, `Summaries.Section`; `monitor/sections.go`): one that isn't read in time is sent as not loaded (a signal with `unavailable: true`, a section's key in the answer's `unavailable` list, its data missing) and the rest of the page still answers, so a slow query, or one someone's own events slow down, hides its part and never the page (`TestAdminMonitoringSectionsFailAlone`). Past the slot it answers `503` "Monitoring is busy. Try again in a moment." (with `Retry-After: 1`); only when nothing at all could be read in time, `503` "This page took too long. Try a shorter window." `admin-read:per-user` still counts the request once. Every query is bounded by a window of 7 days at most (the review lists' takeover lookback reads one account's own sign-ins through its index), read through an index, and returns at most 20 rows per review list.
- **Computed when read** (decision 3), never stored. A signal too slow at real volume moves to the hourly rollup ([observability.md](observability.md#metrics-in-the-database)), not to a slower page.
- **SQL:** every query is Bun's builder in `internal/store/monitor_*.go`, values through `?` placeholders. Pages, windows, signals, limits and outcomes come from fixed Go tables; nothing from input becomes SQL text.
- **Emails and IPs only in review lists** (decision 7): the Security page's lists name the addresses and emails behind an attack, Rate limits' top addresses the addresses reaching the most limits, and the IP addresses page and one address's page the addresses events came from, admin-only and recorded; everywhere else is counts (the Accounts, Workspaces and Usage pages name accounts and workspaces by ID and name only, and one address's page counts the emails it tried that have no account, never naming them). An account is named by its `usr_` ID and name, never its email. No response holds a token, hash or internal ID (`decodeAdmin` walks every one in the tests).

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

Each signal is defined once, in Go (decision 2): its name (stable: alerts and these docs use it), its page, label, unit and window, its **warn** and **critical** thresholds (0: no such level), an optional **minimum sample** (below it the level is `ok` whatever the value: a share of a handful means nothing), how it's measured (a store query), and its drill-down. **A value at or over a threshold is at that level**, the higher winning. Thresholds change only by a reviewed commit, and this table with them. **Anyone with an account can raise a level**: their own wrong passwords, codes and password checks, junk emails at sign-in, limits they reach on purpose. A level says something is happening, not that it's an attack on someone else, so the alerts that will read these ([alerts](../../plans/features/admin/alerts.md)) must allow for a level raised on purpose, to cry wolf or to hide something else in the noise. Alerts ([alerts](../../plans/features/admin/alerts.md)) will read the same definitions.

A signal's span is its window before now, its start floored to the window's bucket as the Events page floors the same window (an hour to the minute, a day to 30 minutes), so its drill-down covers the rows it counted (and any recorded since). **Only some drill-downs list exactly the rows counted** (`drillExact: true`): failed sign-ins, the unknown-account share (the attempts it's a share of), limit windows reached and sign-ups. The others list the events the number was counted from, which is more: stuffing and guessing (every attempt in the hour, not only the address's or email's), success after failures, takeovers (every such change), failed codes and password checks (every failure, not only the accounts' at 3+), the reset and shared limits (every `rate_limit.reached`), the sign-up limit, and the admin door (its failures' list holds the refusals of real credentials too, and the refusals' list the wrong passwords: the Events page has no filter for one result). The Events page has no filter that would make them exact, and none was added for it. `TestSignalsAreWellFormed` checks names are unique, every drill-down names only catalogue names or groups and a window the Events page takes, and warn is below critical.

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
| `usage.weekly_active_accounts` | Usage | …in the last 7 days | 7 d | – |
| `usage.inactive_accounts` | Usage | accounts not seen for two years ([below](#usage)) | now | – |
| `ips.sign_up_cluster` | IP addresses | the most accounts made (`auth.registered`) from one address (an IPv6 /64) | 24 h | 5 / 20 |
| `ips.busiest_share` | IP addresses | share of the events from an address (admin reads left out) that came from the busiest one, exactly | 24 h | 80% / –, from 50 events |
| `accounts.workspace_sprees` | Accounts | accounts making **5+ workspaces** (as actor) | 24 h | 1 / – |
| `accounts.invitation_sprees` | Accounts | accounts sending **20+ invitations to 15+ addresses** | 24 h | 1 / – |
| `accounts.low_acceptance` | Accounts | accounts with **20+ invitations sent and under 10% accepted** (an acceptance finds its invitation by ID) | 7 d | 1 / – |
| `accounts.project_sprees` | Accounts | accounts making **200+ projects** | 24 h | 1 / – |
| `accounts.links_in_names` | Accounts | accounts that named something in the window (signed up, renamed themselves, made, renamed or invited from a workspace) whose name, or an owned workspace's, holds a link or markup (`store.LinkInName`: a scheme, `www.`, a domain-looking word, `<` or `>`) | 24 h | 1 / – |
| `accounts.limits_reached` | Accounts | accounts reaching any `*:per-user` limit | 24 h | – |
| `accounts.failed_checks` | Accounts | accounts failing **3+ password or second-factor checks** | 24 h | 1 / – |
| `accounts.new_and_busy` | Accounts | accounts made in the last 24 hours tripping any of the above | 24 h | 1 / – |
| `workspaces.quota_used` | Workspaces | the fullest quota of the workspaces with an event in the window, and their owners: projects of 1,000, members and live invitations of 100, live invitations of 50, workspaces owned of 20 | 24 h | 80% / 100% |
| `workspaces.largest` | Workspaces | the most rows one workspace holds across `store.WorkspaceTables`, of those with an event in the window | 24 h | 50,000 / – |
| `workspaces.throwaway_accounts` | Workspaces | accounts with **3+ workspaces** deleted within a day of making them | 24 h | 1 / – |

The thresholds are starting points sized from the existing limits (`TestThresholdsMatchTheLimits` checks the shared limits they read exist). Metric signals read `request_minutes`; with `METRICS_STORE=off` they read nothing and show zero.

## Overview

A grid of tiles, each an area with one or more signals: its worst level in words, its first signal as the number, the others listed under it with their levels, a sparkline over the page's window, and a link. Tiles: **Servers** (live, versions), **Requests**, **Server errors**, **Latency**, **Mail** (oldest waiting, failed), **Errors** (new, new panics, open), **Failed sign-ins**, **Shared limits**, **Admin door** (refusals, failures), **Sign-ups** (sign-ups, active accounts).

- **Pages not built yet:** a tile whose page is Security, Rate limits or Usage links there, Mail to the Mail page; Performance, Errors and Configuration come with part 3, so their tiles show their numbers now (the data is already there: metrics, grouped errors). A tile whose first signal has Events rows links to them; one with none to link to (Servers, Requests, Server errors, Latency, Errors) has no link and says "Its page comes later." **Integrity and Alerts have no tile** until their pages exist (their checks and rules don't yet). The Database tile (pool waits, ping time) comes with Performance.
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

## IP addresses

| Section | Shows |
| --- | --- |
| Shared address | a warning above the page while `ips.busiest_share` is at its level: the server probably isn't reading the client's address behind its proxy (`CLIENT_IP_HEADER`, `TRUSTED_PROXIES`), so every number by address is the proxy's |
| Top addresses | the addresses events came from in the window (the dashboard's own reads left out: they'd put the admin's address on top), with events, failed sign-ins, sign-ups, limit windows reached and accounts (distinct, as actor or subject). One scan, both ways (`GROUPING SETS`): an IPv6 address by its /64, as the signals count, or each address on its own; the top 10 by each column of each, so the page sorts by any column (`?sort=`) and switches IPv6 (`?ipv6=each`) without asking again |
| Sign-up clusters | addresses (an IPv6 /64) that made 5+ accounts in a day, by day counted back from the window's end, at most 20 |

Each address links to its page.

### One address

`GET /api/admin/ips/{ip}?window=1h|24h|7d` (`admin.IPAddresses.Show`): an IP address (an IPv4-mapped one as IPv4, never with a zone) or an IPv6 /64, its slash escaped (`2001:db8::%2F64`); anything else is 404 "Address not found." (`monitor.ParseAddress`). Bounded like a page (one per admin, two per server, 5 s) and recorded as **`admin.ip.viewed`** (`admin`, `address`, `window`; the address looked at is `address`, apart from the event's `ip` column, which is the admin's own): looking an address up is following someone. A refusal or a busy read records nothing. It answers, over the window and through `events_ip_created_idx` (`ip <<= ?`): the events and when the address was first and last seen in it, its events by name, the accounts seen from it (signed in, made there, failing to sign in; at most 20, by ID and name), **how many** different emails with no account it tried (never which: its events show them, a recorded read of their own), the limits it reached, and for a /64 its addresses (at most 20). An IPv6 address's answer names its /64 (`network`) to open. Everything recorded from the address counts here, admin reads included.

## Accounts

| Section | Shows |
| --- | --- |
| Accounts to review | every account tripping any of the Accounts signals in a day of the window (days counted back from its end, so the last is the whole last 24 hours; acceptance over the 7 days before its end), each with every signal it tripped and its count in its busiest day; a link in a workspace's name names the workspace. Each signal finds its 20 busiest; the list keeps at most 50, most signals first, then new accounts, then the newest |
| Writing the most | top 10 accounts by events about workspaces and projects as actor |
| Owning the most workspaces | of the accounts that made a workspace in the window, the 10 owning the most (`workspace_user_user_id_idx`) |

Accounts are named by ID and name, never email; the row's Activity opens the account's events. Names are read only for the accounts that named something in the window, so the check is bounded by the window, never a scan of every account. The account actions (sign out everywhere, reset MFA, suspend) are on the account's page with [actions](../../plans/features/admin/actions.md).

## Workspaces

A workspace's rows have no time window of their own, so the page counts them only for **the workspaces with an event in the window** (`events_created_idx`), each counted through its tables' `workspace_id` indexes. A workspace grows only by writing, which records events, so one filling up is among them; one nobody touched in the window isn't counted.

| Section | Shows |
| --- | --- |
| Quota headroom | workspaces at 80%+ of a quota (projects 1,000; members and live invitations 100; live invitations 50) and their owners at 80%+ of 20 workspaces owned, each quota a `Meter` with its level in words |
| Rows per workspace | the 10 holding the most rows across **every table with a `workspace_id`** (`store.WorkspaceTables`: projects, members, invitations, events), a column per table and the total |
| Growth | the top 10 by projects and invitations made, and the top 10 by events, in the window |
| Thrown away | accounts whose workspaces were deleted within a day of being made (the deletion finds its creation, and the invitations the workspace sent, by its ID in `events_payload_workspace_idx`), at most 20 |
| Invitations | the 10 workspaces sending the most invitations over the 7 days before the window's end: sent, accepted, revoked (their events, by the workspace's ID, so a deleted one keeps its row), and expired unanswered (`workspace_invitations_expires_at_idx`); under 10% of 20+ accepted is a warning |
| Made and deleted | workspaces made and deleted per bucket, and PostgreSQL's running count of all of them (`pg_stat_user_tables`, close, not exact: no count of the table) |

**The workspace-tables registry** (decision 9): `store.WorkspaceTables` lists every table with a `workspace_id` column. `TestEveryWorkspaceTableIsMonitored` reads the columns from `information_schema` and fails when a table has one and isn't listed (or is listed and hasn't), and when a listed table has no index leading with `workspace_id`; so a future table (tasks, files) is counted once it's added, without anyone remembering.

## Usage

| Section | Shows |
| --- | --- |
| Active accounts | seen (a signed-in request) in the last 24 hours, 7 days and 30 days, from `users.last_seen_at` (`users_last_seen_idx`, a range of the index; the month is the one count past 7 days), and the share of the week's with two-factor on |
| Sign-ups | accounts made per bucket; **activation**: of the accounts made 8 to 1 days ago, those that made or joined a workspace within a day, and of those made 14 to 7 days ago, those that invited someone within a week (each cohort 7 days of sign-ups, each account given its whole time; `events_actor_created_idx` per account) |
| Feature use | projects made and changed, invitations sent and accepted, two-factor turned on, per bucket |
| Workspace sizes | the workspaces with an event in the window by members (1, 2–5, 6–20, 21+) and projects (0, 1–10, 11–100, 101+) |
| Not seen for two years | **inactive accounts** ([compliance](../../plans/features/admin/compliance.md) decision 9): accounts whose last signed-in request, or whose making when they never had one, is over two years ago (`store.InactiveAfter`), the 20 unseen longest and how many in all, through `users_inactive_idx` on `coalesce(last_seen_at, created_at)`. Admins are marked: an admin session doesn't move `last_seen_at`. Listed for a decision, **never deleted automatically**: deleting them is a business policy, not a default |

## Active accounts: `users.last_seen_at`

`users.last_seen_at` is when the account last used a session, to the hour. The session check (`store.FindSession`, behind `RequireSession` and the `/app/*` page gate) moves it **in the same statement** that refreshes the session's idle time, and only when it's null or over an hour old (`seenEvery`), so an active account costs one row write an hour and the check no extra round trip. It never records an event: it isn't something the account did, and an event an hour per account would fill the audit trail. It's null until the account's first signed-in request; an expired or idle session moves nothing. It never waits for the account's row (`FOR NO KEY UPDATE SKIP LOCKED`): changing the password or making a session locks the account and then its sessions, the other way round from the session check, so waiting could deadlock; a locked account's time just moves on its next request (`TestSessionCheckSkipsALockedAccount`). A partial index (`users_last_seen_idx`) serves counting the accounts seen since a time. `TestSessionCheckMovesLastSeenHourly` (`internal/store`) checks it.

## Web app

- **Navigation:** a **Monitoring** heading in the admin sidebar under the other pages (decision 5), with Overview, Security, Rate limits, IP addresses, Accounts, Workspaces and Usage; later pages join it as they're built. IP addresses is current on one address's page too.
- **A page** (`AdminMonitoringOverview`, `AdminMonitoringSecurity`, `AdminMonitoringLimits`, `AdminMonitoringIPs`, `AdminMonitoringAddress`, `AdminMonitoringAccounts`, `AdminMonitoringWorkspaces`, `AdminMonitoringUsage`) is one `useAdminResource` call (`useMonitoringPage`): the window in the URL (`?window=1h`, the default left out; each change a navigation, so Back undoes it), the heading with a **time range** `Listbox` on the right, and a line with the window the server resolved, in UTC. Nothing reloads on its own: each load is a recorded read.
- **States:** a skeleton until the first answer; a new window dims what's shown (`aria-busy`); a failure keeps what was shown with a `Notice` and **Try again**. A section the server couldn't read in time shows its heading and a notice with **Try again** in its place (`SectionGate`), and a signal "Not loaded"; Try again loads the whole page again (one more recorded read).
- **Signals** are `Stat` tiles: the label linking to the rows behind it, the level as a `LevelBadge` (OK, Warning, Critical, with an icon: never colour alone), the value in its unit, and its window and thresholds in words.
- **Charts** are `BarChart`s with **Show as table**, and `Sparkline`s in the Overview's tiles; both keep a summary sentence and a table for screen readers. Shared limits are `Meter`s. Red, amber and green appear only for levels, always with their word ([ui/design.md](../ui/design.md#charts)).

## Demo data: `make seed only=monitoring`

Plants what the Security and Rate limits pages look for, in a development database: a day of ordinary sign-ins, then in the last hour credential stuffing from `203.0.113.66` (40 emails, one guess working), one password guessed from six addresses, an account signed in after six wrong passwords and then failing codes, a reset flood from `198.51.100.23`, `register:global` reached, wrong passwords and a non-admin's right password at the admin door, and a fake account (home `198.51.100.140`) taken over from a new network. Addresses are from the documentation ranges, emails `@example.test`, every payload `"seeded": true`. For part 2's pages (`monitoring_abuse.go`): an invitation spammer made today from `203.0.113.120` ("Visit http://cheap-pills.example", owning "Free money at www.cheap-pills.example" and 16 more workspaces, 30 invitations to 25 addresses with none accepted, 45 live invitation rows that have no link, so no mail can go, and four workspaces made and deleted within the hour), nine accounts made from `198.51.100.77`, "Northwind Archive" at 850 of its 1,000 projects with 210 made today, accounts active today, this week and this month (one turning on MFA), sign-ups over two weeks that made a workspace and invited someone, and two accounts not seen for two years. It never writes `rate_limits` (a counter there would really refuse requests) or metrics and errors (made-up traffic would be misleading). Insert-only; refused in production. `TestMonitoringSeeder` checks each planted pattern reaches its level ([seeding.md](seeding.md)).

## Tests

- **Signals** (`endpoints/admin/monitoring_integration_test.go`): `TestAdminMonitoringSecuritySignals` puts each Security signal just under and just over its threshold (99 and 100 failed sign-ins, 9 and 10 emails from one address with a repeat in other case, then a success; 4 and 5 addresses for one email, an IPv6 /64 counting once; the unknown share at 50% and under the minimum sample; 4 and 5 failures before a success; 2 and 3 failed codes and password checks; the shared limits; the admin door's failure, refusal and code step, and events outside the window), and checks a drill-down lists exactly the rows counted. `TestAdminMonitoringTakeovers`: a new network then a password change within the hour counts; the same /24, no sign-in in 30 days, and a change two hours later don't. `TestAdminMonitoringLimits`: every section, the shared limit critical, subjects at a limit (an ended window and one under it left out, a memory limit null, a shared limit not listed). `TestAdminMonitoringOverview`: servers and versions, requests, 5xx share and p95 from minutes, a week joining the summed hours, the mail queue, error groups, sign-ups and active accounts, each at its level.
- **Budgets:** `TestAdminMonitoringSectionsFailAlone` locks the sessions table: Security answers with its Sessions section listed as unavailable, every signal read and one read recorded. `TestAdminMonitoringTakeoverFlood`: 20,000 of an account's own changes leave the takeover check fast.
- **The endpoint:** `TestAdminMonitoringPages` (every page and window, one `admin.monitoring.viewed` each with the page and window, a viewer reading; an unknown page 404, a bad or repeated window or another parameter 400, no session 401, none recorded) and `TestAdminMonitoringBounds` (busy with the admin's slot taken, too slow under a 1 ns deadline: 503, nothing recorded). The key and session checks cover the three routes (`adminRoutesList`).
- **Unit** (`internal/monitor`): `TestSignalsAreWellFormed`, `TestThresholdsMatchTheLimits`, `TestLevelOf` (at or over, the higher wins, under the minimum sample ok), `TestPercentile`, `TestSignalSpans`.
- **Part 2** (`endpoints/admin/monitoring_abuse_integration_test.go`): `TestAdminMonitoringIPs` (4 and 5 accounts from one address, 20 from one /64 critical; the busiest share under its sample then at 80%; both lists, each row once, the admin's own reads left out), `TestAdminMonitoringAddress` (what one address did, a week reaching further, an IPv6 address's /64 and the /64's page escaped, an unseen address, 404 for a range, a /48 or a zone, 400 for a window or parameter, 401, busy; `admin.ip.viewed` with the address and window, nothing for a refusal), `TestAdminMonitoringAccounts` (each signal just under and at its level: 4 and 5 workspaces, 19 and 20 invitations and 25 to 10 addresses, 2 and 1 of 20 accepted, 199 and 200 projects, links in an account's and a workspace's name, a per-user limit, 2 and 3 failed checks, new and busy; the review list's signals and order, top writers and owners, 25 hours ago left out), `TestAdminMonitoringWorkspaces` (79.9% and 80% of projects, full invitations critical, an untouched full workspace not counted, an owner at 16 of 20, rows per registered table, growth, invitations with an expired one, 2 and 3 throwaways and one kept two days, totals), `TestAdminMonitoringUsage` (active by day, week and month with MFA, both activation cohorts, feature use, sizes, the inactive accounts oldest first). `TestEveryWorkspaceTableIsMonitored` (`store`), `TestLinkInName` (`store`), `TestParseAddress` (`monitor`).
- **Volume** (manual, on a development database with `make seed only=events count=1000000`, 950,000 of them in the last 7 days, and `only=monitoring`): every page answers within its bounds on every window. Served times: Overview 0.05 / 0.25 / 0.45 s (1 h / 24 h / 7 d), Security 0.1 / 1.5 / 3.8 s, Rate limits under 0.2 s, IP addresses 0.7 / 1.8 / 2.7 s, one address under 0.05 s, Accounts 0.3 / 0.4 / 0.9 s, Workspaces under 0.2 s, Usage under 0.1 s. **IP addresses' top list over 7 days of a million events passes its 2 s budget** and shows as not loaded (the rest of the page shows): it groups every event of the week by address, so at that volume it needs the hourly rollup (decision 3). Security over 7 days is under the 5 s deadline but not the plan's 2 s.
- **Vitest** (`pages/admin/monitoring.test.tsx`, `monitoring-pages.test.tsx`): tiles with their levels in words and links (none for a page not built), a sparkline's table, the sidebar's Monitoring heading; a failed reload keeping the data with Try again and the window in the URL; Security's stuffing address and signal links, the shared limit's meter, Show as table; Rate limits' counts and the memory limit.
- **Playwright** (`admin.spec.ts`): a seeded stuffing address appears on Security, reached from the Overview's tile, links to its events, the window in the URL, and each load records one read; on IP addresses, sorted by failed sign-ins, it opens its own page (the emails it tried counted, `admin.ip.viewed` with the address); an IPv6 /64's page with its slash escaped gets through the page gate and the router; Accounts, Workspaces and Usage load.
