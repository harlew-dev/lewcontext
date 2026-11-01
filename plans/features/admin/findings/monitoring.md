# Findings: A4, monitoring

Plan: [monitoring](../monitoring.md), built on main in three parts, with every recommendation in its Decisions table taken as accepted. Screenshots are in [monitoring/](monitoring/).

| Part | Pages | State |
| --- | --- | --- |
| 1 | Overview, Security, Rate limits | **committed**, under review |
| 2 | IP addresses, Accounts, Workspaces, Usage (plus A7's inactive accounts) | _building_ |
| 3 | Data, Integrity, Performance, Errors, Configuration | next |

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

_Running._
