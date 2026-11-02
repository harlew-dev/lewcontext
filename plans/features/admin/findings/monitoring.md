# Findings: A4, monitoring

Plan: [monitoring](../../implemented/admin/monitoring.md), built on main in three parts, with every recommendation in its Decisions table taken as accepted. Screenshots are in [monitoring/](../../screenshots/monitoring/).

| Part | Pages | State |
| --- | --- | --- |
| 1 | Overview, Security, Rate limits | **done** (reviewed, fixed) |
| 2 | IP addresses (and a page per address), Accounts, Workspaces, Usage (plus A7's inactive accounts) | **done** (reviewed, fixed) |
| 3 | Integrity, Performance, Errors (and one group), Configuration | **committed**, under review |
| 3b | Data | **committed** (A4 complete, plan moved to `implemented/`), under review |

## Part 1

### What shipped

| Commit | What |
| --- | --- |
| `f4cb2bc` | `users.last_seen_at`, moved at most hourly inside the session lookup itself, so no request pays an extra query |
| `fdfa038` | `internal/monitor`: every signal and threshold in one place in code (shared with A5's alerts later); the Overview, Security and Rate limits endpoints |
| `3660dc0` | `make seed only=monitoring` plants fake attack patterns for the pages to find |
| `7434b6c` | The pages, under a new **Monitoring** heading in the admin sidebar |
| `6235f32` | `docs/api/monitoring.md` (new) and an e2e flow |
| `ebcacd8` | Fix: the admin door chart stacked 6 series and crashed the Security page (caught by e2e); a test now checks every chart |

**Screenshots:**
- [Overview](../../screenshots/monitoring/overview-light.png), [Overview, dark](../../screenshots/monitoring/overview-dark.png);
- [Security](../../screenshots/monitoring/security-light.png), [Security, dark](../../screenshots/monitoring/security-dark.png), [Security on a phone](../../screenshots/monitoring/security-phone.png);
- [Rate limits](../../screenshots/monitoring/limits-light.png).

**What each page answers:**
- **Overview:** is everything all right? Tiles with a level (OK, Warning, Critical, always in words as well as colour), a sparkline, and the threshold written out. Tiles whose page isn't built yet say "Its page comes later".
- **Security:** failed sign-ins, password guessing per account and per address, possible takeovers, the MFA, password-reset and sign-up signals, and the admin door.
- **Rate limits:** which limits are hit, by route, and whether any shared limit is shutting everyone out.

Each page is one endpoint and one recorded read (`admin.monitoring.viewed`). Queries are limited to 7 days, an index and 5 s, with one page in flight per admin.

### Where the build differs from the plan (recorded in the plan)

- **Each signal keeps its own window** (an hour, a day, or "now"); the page's window drives only the charts. Each signal links to exactly the Events rows it counted.
- **A level applies at or over its threshold.**
- **The Servers tile warns when more than one version is live,** not when a server has been silent for 2 minutes, because a stopped server's row stays a day and every release would warn.
- **"By route" on Rate limits shows 429s and 5xx together,** because the minute tables don't keep 503s apart.
- **Limits counted only in one server's memory show as "can't be seen".**
- **`LevelBadge` takes a `level` prop, not `variant`.** AGENTS.md rule 6 asks for `variant`, but the level is the signal's value, not a style choice. _Your call: rename to `variant` for consistency?_

### Not verified yet

- **Speed at scale.** The plan's check (every page under 2 s with a million events) hasn't been run. The Events page's equivalent check passed, and every query here has an index and a 7-day limit, but this isn't proven.

### A3 phase 3 review fixes (made before part 1)

`a4967cb`, `d08fd66`, `15b2dbc`, `db4d9ce`:
- **`/metrics` without a token** is allowed only on loopback or private addresses, and zoned addresses are refused.
- **Metric labels** must be valid UTF-8.
- **The rollup** sums hour by hour on the database's clock, and its 7-day ceiling holds even on a timeout.
- **Silent servers** are deleted at startup.
- **The link check** survives a deleted file.

These are recorded in [observability](observability.md#security-review).

### Security review

The independent reviewer confirmed these sound:
- **Access:** every route is behind the key and an admin session.
- **Recording:** exactly one recorded read per page served, and nothing recorded when nothing was served.
- **Data shown:**
  - emails appear only in the guessing list;
  - full IPs appear only in review and top lists;
  - the admin door list never shows the typed email or key;
  - "Blocked right now" is counts only.
- **Queries:** builder-only SQL; queries are bounded and indexed; the takeover lookback is tied to one account.
- **Hourly sums:** minute and hour tables never double count; two servers can't sum an hour twice.
- **The seeder** is refused in production.
- **The page:** everything renders as text, levels are always in words, and charts have text alternatives.
- **Privacy:** `last_seen_at` isn't in any response.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | **Medium** | **Any signed-in account could make the Security page time out.** "Sign out my other sessions" had no limit and recorded an event even when nothing was revoked; each such event made the possible-takeover query slower. Measured: 1M events (about 12 requests a second for a day) took the query to 5.4 s, past the page's 5 s limit, which hid the whole page during an attack | No event when nothing was revoked; the route gets its own per-account limit; the takeover query is driven by sign-ins, not by changes; each section of the page fails on its own |
| 2 | Low | **A deadlock** between the hourly last-seen update and a password change or a new sign-in (reproduced; about once an hour per account, and only during one of those) | The last-seen update never waits: it skips the hour instead |
| 3 | Low | The admin door's drill-down links listed every failure kind, not just what they counted | Exact drills where cheap, otherwise the docs say which are broader |
| 4 | Low | `/metrics` without a token accepted the name `localhost`, which a container may resolve elsewhere | Literal addresses only |
| 5 | Nits | Silent-server cleanup on the server's clock; a minute could arrive after its hour was summed; totals and latency read the cutoff twice; an admin disconnecting caused a 500; doc drift; seeder IPs | Fixed |

**Worth knowing for alerts (A5):** any account can raise a Critical level on purpose, with a few wrong passwords on its own account or a handful of junk emails. That's fine for pages, but alerts must allow for it.

_These fixes are being made by the A4 part 2 builder first._


## Part 1 fixes (`f017cfe`..`d82d331`)

- **The Security-page flood is closed.** "Sign out other sessions" records nothing when there's nothing to revoke, and gets its own limit, `sessions-revoke:per-user` (30 an hour). The takeover check starts from sign-ins, so the page took 51 ms with 20,000 of an account's own changes.
- **Every section of every Monitoring page has its own 2 s budget.** A slow section shows "Not loaded" with Try again; the rest of the page renders.
- **The session check never waits for the account's row** (SKIP LOCKED), so there's no deadlock.
- **Each signal says whether its drill-down is exact.** The admin door's two are broader; the docs say so.
- **`/metrics` without a token** needs a literal address (no `localhost`).

## Part 2

| Commit | What |
| --- | --- |
| `f5557cc` | Endpoints for IP addresses, Accounts, Workspaces and Usage; a page for one address (`GET /api/admin/ips/{ip}`, recorded as `admin.ip.viewed`); the workspace-tables registry; inactive accounts (A7's decision 9) |
| `7f46861` | Seeder patterns (an invitation spammer, a near-full quota, …) |
| `7f3c87c` | The pages |
| `0baeef0`, `a6986fd`, `c27460d` | Fixes: whole days in clusters, UI polish, a faster IP list query |
| `ce21590` | Docs, plan, e2e |

**Screenshots:**
- [IP addresses](../../screenshots/monitoring/part2/ips-light.png), [one address](../../screenshots/monitoring/part2/ip-light.png), [on a phone](../../screenshots/monitoring/part2/ips-phone.png);
- [Accounts](../../screenshots/monitoring/part2/accounts-light.png), [Accounts, dark](../../screenshots/monitoring/part2/accounts-dark.png);
- [Workspaces](../../screenshots/monitoring/part2/workspaces-light.png), [Workspaces, dark](../../screenshots/monitoring/part2/workspaces-dark.png);
- [Usage](../../screenshots/monitoring/part2/usage-light.png);
- [Overview](../../screenshots/monitoring/part2/overview-light.png).

**How the pages stay bounded:**
- Rows per workspace and quota headroom are counted only for workspaces with an event in the window (a workspace only grows by writing).
- Inactive accounts are a top 20 plus a count, through a new index.
- The workspace total is PostgreSQL's estimate.

**Read-only for now:** A6's "Block this address" and the account actions plug in when the events branch merges; the places are marked.

**Measured at a million events:**
- every page answers;
- **IP addresses over 7 days** misses its 2 s budget and shows "took too long" for that section;
- **Security over 7 days** takes about 3.8 s (under the 5 s limit, over the plan's 2 s).

Both need an hourly rollup (decision 3) if real traffic reaches that volume.

**Merge task:** `admin.ip.viewed` keeps the looked-up address in its payload. A7's minimiser (on the events branch) only shortens the IP column, so it needs extending to this field when the branches merge. Erasure doesn't apply here, since IPs are kept.

### Security review (part 2)

The independent reviewer found **no data exposure or injection**. It confirmed sound:
- **The one-address page:** parsing (a host, mapped, or a /64 only; zones and wider ranges refused), the address only as a placeholder, an indexed query.
- **Recording:** exactly one recorded read per page served.
- **Logs:** they carry only the route pattern, never the address.
- **What's shown:** names as text only; at most 50 accounts in review lists; `decodeAdmin` on every response.
- **SQL:** the top-10 query is correct; the builder is used throughout.
- **Timeouts:** a timed-out section cancels its query and leaks no connection.
- **Workspaces:** every workspace write records an event, so none grows unseen.
- **The seeder:** fake data only.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | **Medium** | **An attacker could blind the takeover check.** Signing in from a new network each time made the check's cost grow quadratically: two accounts kept the 24-hour view from loading, exactly the view that would show their real takeover. About 20 cheap fake takeovers also pushed a real one off the list. Measured | Only accounts that changed something are checked, against their last 50 sign-ins at most; one row per account in the list |
| 2 | Low–medium | **A spam link in a workspace's name went unflagged** if a workspace *admin* (not the owner) renamed it | Names are checked for the workspaces the window's events are about, credited to whoever acted |
| 3 | Low | A stolen session could use up the owner's "sign out other sessions" limit | The limit is per session, not per user |
| 4 | Low | One workspace with millions of events could stop "rows per workspace" from loading | Each count stops at what the threshold needs |
| 5–6 | Nits | Inactive accounts need a `last_seen_at` backfill once migrations stop being squashed; the registry test accepted partial indexes | Documented; test tightened |

**Merge task (for A7's minimiser):** `admin.ip.viewed`'s `address` and `admin.session.moved`'s `from`/`to` hold addresses or networks outside the fields the minimiser covers.

**Fix results:**
- **`8446617`, takeovers:** only accounts with a change are checked, each against its last 50 sign-ins, with `INCLUDE (ip)` on the sign-in index; one row per account. Two accounts each signing in 3,360 times from new networks: the 7-day page answers in 0.33 s, and the real victim is still listed.
- **`9900a2f`, workspace names:** a linked name is credited to whoever acted.
- **`ab59173`, `sessions-revoke`:** each session gets its own budget. The limit keeps the name `sessions-revoke:per-user`, because a `per-session` kind needs your agreement under the naming checks (question 18).
- **`c5d3c5b`, rows per workspace:** counts stop at 50,001 ("Over 50,000").
- **`bb4171c`, `dea74ad`:** the registry test is tightened; the backfill note is added.


## Part 3

| Commit | What |
| --- | --- |
| `1c32c0f` | Endpoints for Integrity, Performance, Errors and Configuration; one error group's page (`GET /api/admin/errors/{error}`, recorded as `admin.error.viewed`); `app_errors.public_id` (`err_`); Database and Integrity tiles on the Overview |
| `bd7781b` | Seeder: a stuck delivery, a project created twice, a reset sent twice, three error groups (one user-reported). Nothing is ever sent |
| `e17f6b6` | The pages |
| `3c53ba4`, `64f5cd4`, `5f08fb2`, `2cb68a5` | Fixes, e2e, docs |

**Screenshots:**
- [Overview](../../screenshots/monitoring/part3/overview-light.png), [Overview, dark](../../screenshots/monitoring/part3/overview-dark.png);
- [Integrity](../../screenshots/monitoring/part3/integrity-light.png), [on a phone](../../screenshots/monitoring/part3/integrity-phone.png);
- [Performance](../../screenshots/monitoring/part3/performance-light.png);
- [Errors](../../screenshots/monitoring/part3/errors-light.png), [one error group](../../screenshots/monitoring/part3/error-group-light.png);
- [Configuration](../../screenshots/monitoring/part3/configuration-light.png).

**What each page answers:**
- **Integrity:** checks that should never fail. Each is its own section and catches its planted row by ID.
- **Performance:** route latency (p50, p95, p99), the database pool, delivery lag, and `pg_stat_statements` (normalised statements only, never one with a literal left in it).
- **Errors:** groups by source and route. **Browser-reported groups are labelled user-reported and never turned into links.** The request ID ties a group to the logs.
- **Configuration:** live servers and versions, and the warnings the earlier plans asked for:
  - `ADMIN_ALLOWED_IPS` without `CLIENT_IP_HEADER`;
  - sessions still on the previous admin key;
  - more than one version live;
  - migrations pending;
  - mail off;
  - no admin with MFA.

  **Never a secret.** A test seeds a summary full of secrets and checks none reaches the answer.

**Speed at a million events:** Integrity about 0.5 s, Configuration 0.6 s, Performance and Errors under 0.03 s. The Overview is now 0.7–1.2 s, since it runs every integrity check.

**Where the build differs (recorded in the plan):**
- **Hashing wait** has no histogram, so busy-slot refusals are shown instead.
- **Delivery lag** tops out at the histogram's 10 s, so the oldest-waiting delivery covers stalls.
- **Pool waits** are averages.
- **Error charts** are by source and route, not per group.
- **Resolving an error group** is A6's housekeeping action, still to come with A5.

### Security review (part 3)

The independent reviewer found **nothing high-severity**. It confirmed sound:
- **Configuration:** only listed fields reach the answer; secrets are shown as set or not set; the test plants secrets and unknown keys.
- **Errors:**
  - everything is rendered as text;
  - browser groups are labelled and never linked;
  - releases are shape-checked;
  - stacks are scrubbed and capped;
  - `err_` IDs are regex-checked;
  - one read is recorded per view.
- **`pg_stat_statements`:** read through the builder; this database and role only; quotes, comments and dollar quotes rejected.
- **Integrity:** capped samples of public IDs only; duplicates shown as counts and `evt_` IDs, never emails.
- **The part 2 fixes:** the per-session limit key can't collide with others; the takeover check's limit of 50 opens no new way to hide.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | **Medium** | **During credential stuffing, the Overview could hide the security tiles.** The Integrity tile was read first and could use up the page's time on a day with a million sign-in attempts, so Failed sign-ins and Admin door showed "not loaded" during the attack | The Integrity tile is read last; checks run cheapest first and report partially; the active-workspace list is computed once |
| 2 | Medium/low | An unexpected error from the optional `pg_stat_statements` extension (a revoked grant, an old version) gave the whole Performance page a 500 | Any such error shows "not available" with a reason |
| 3 | Low | The statement filter let through numbers and double-quoted names (`WHERE id = 42`, `AS "ada@example.test"`, `SELECT … INTO`), so a statement PostgreSQL didn't normalise could show an internal ID | Rejected |
| 4 | Low | The takeover signal can be dodged by waiting over an hour after the new-network sign-in; enrolling MFA isn't counted as a change | `auth.mfa.enabled` added; the timing gap documented for alerts |
| 5–6 | Low | The "New" badge compared timestamps as strings; a timed-out count still showed its warning with 0 | Fixed |
| — | Nits | An unencoded ID in one page link; duplicate React keys; a comment; `SMTP_HOST` accepting `user:pass@` | Fixed |

**For the ui branch's merge:** the captcha settings need rows on the Configuration page (provider, whether the secret is set, never its value) and a place in the no-secrets test. The ui integration agent has been told.

**Fix results:** all made first, as separate commits (`1ec1c80`..`92c980d`). The Overview reads Integrity last and shows partial results; any `pg_stat_statements` error now shows "unreadable" rather than a 500; `SafeStatement` refuses numbers, odd quoted names and `SELECT … INTO`; `auth.mfa.enabled` counts as a takeover change (the "wait over an hour" gap is documented, since no session network is recorded); error groups carry a server-computed `new` flag; the previous-key warning appears only when its count succeeded; `SMTP_HOST` refuses `@`.


## Part 4: the Data page (A4 complete)

| Commit | What |
| --- | --- |
| `adaf000` | Migration 026 `table_stats_daily`, its factory, a daily writer (one server, once a day, insert-only), a `table_stats_daily` prune target (400 days), the Data queries and signals |
| `125ec20`, `0972b4c`, `a221c64` | Tests and seeder, the page, docs and e2e |
| `26b365b` | The plan moved to `plans/features/implemented/admin/monitoring.md` |

**Screenshots:** [Data](../../screenshots/monitoring/data/data-light.png), [dark](../../screenshots/monitoring/data/data-dark.png), [phone](../../screenshots/monitoring/data/data-phone.png) (taken before a width fix), [Overview with the Data tile](../../screenshots/monitoring/data/overview-light.png), [Configuration](../../screenshots/monitoring/data/configuration-light.png).

- **What it shows:** database and table sizes, connections (counts only), lock waits, growth (a rate and a 90-day projection once two days of sizes exist), and retention: the oldest event per class against its limit and the last `prune.completed` per target, with a warning on Configuration when prune isn't running.
- **Never shown:** query text, role names or client addresses from `pg_stat_activity`; a test holds a lock with a marker in its query and checks none of it appears. Only tables the app knows are listed.
- **Fix beyond the brief:** hidden chart tables widened the phone page to 684 px; they're now clipped.
- **Not done:** the million-event volume check for this page (its queries are catalog reads and index probes), and disk space (not visible from SQL).

### Security review (Data page and part 3 fixes)

**No high or medium findings, and no leak** of query text, role names, client addresses or catalog names (a test holds a lock with a marker in its query and checks none of it appears). Probes of `SafeStatement` (hex, `1_000`, `x'..'`, `U&"…"`, `$tag$`, unicode digits, doubled quotes) were all rejected. Sound: catalog reads through the builder, known tables only, the daily writer (one server, insert-only, at most 28 rows a day), retention probes through indexes, one recorded read per page, text-only rendering, and the sr-only chart fix.

| # | Severity | Finding | Plan |
| --- | --- | --- | --- |
| 1 | Low | A partial Integrity summary that read only the cheap checks can show an OK badge for checks it never ran | Show "unknown" (or "OK so far") when partial and zero. **Queued for the final polish** |
| 2 | Low | Docs say "oldest row a run would prune"; the probe takes the table's oldest row | Reword docs. Queued |
| 3 | Low | The retention reading isn't memoised (up to 3 runs per Data page, 2 per Overview) | Memoise like the integrity checks. Queued |
| 4 | Low | The daily row's "day" uses the session time zone, not UTC; a crash can record 0 rows for a day | `(now() AT TIME ZONE 'UTC')::date`. Queued |
| 5–6 | Low | Plain quoted names of any length pass the statement filter; the active-workspace list is materialised in Go | Optional hardening; bounded today |
