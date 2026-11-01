# Findings: A4, monitoring

Plan: [monitoring](../monitoring.md), built on main in three parts, with every recommendation in its Decisions table taken as accepted. Screenshots are in [monitoring/](monitoring/).

| Part | Pages | State |
| --- | --- | --- |
| 1 | Overview, Security, Rate limits | **done** (reviewed, fixed) |
| 2 | IP addresses (and a page per address), Accounts, Workspaces, Usage (plus A7's inactive accounts) | **committed**, under review |
| 3 | Integrity, Performance, Errors, Configuration | _building_ |
| 3b | Data (needs A7's `prune.completed`, after the events branch merges) | after the merge |

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
- [Overview](monitoring/overview-light.png), [Overview, dark](monitoring/overview-dark.png);
- [Security](monitoring/security-light.png), [Security, dark](monitoring/security-dark.png), [Security on a phone](monitoring/security-phone.png);
- [Rate limits](monitoring/limits-light.png).

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
- [IP addresses](monitoring/part2/ips-light.png), [one address](monitoring/part2/ip-light.png), [on a phone](monitoring/part2/ips-phone.png);
- [Accounts](monitoring/part2/accounts-light.png), [Accounts, dark](monitoring/part2/accounts-dark.png);
- [Workspaces](monitoring/part2/workspaces-light.png), [Workspaces, dark](monitoring/part2/workspaces-dark.png);
- [Usage](monitoring/part2/usage-light.png);
- [Overview](monitoring/part2/overview-light.png).

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

_Running._
