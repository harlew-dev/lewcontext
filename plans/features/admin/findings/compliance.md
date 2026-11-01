# Findings: A7, compliance

Plan: [compliance](../compliance.md) (on the events branch it has moved to `implemented/admin/compliance.md`). Built on the **events** worktree (`../lewcontext-events`, branch `admin-run/events`), with every recommendation in its Decisions table taken as accepted. Screenshots are in [compliance/](compliance/).

## What shipped

| Commit | What |
| --- | --- |
| `a9e3f6a` | Merge of main (A3 phases 2–3, A4 part 1) into the events branch |
| `ff23f75` | A6's IP blocks on `/metrics`: `ip_block_refused_total`, `ip_blocks_loaded_timestamp_seconds` |
| `2a47ce4` | `admin.access_refused` keeps the user agent when recorded from a page or an IP block |
| `0906b2d` | The compliance API: retention classes, minimising old events, erasure, Download my data, audit lines, Events CSV |
| `4eff309` | Web: **Download your data** in Account & security, **Download CSV** on the admin Events page |
| `84bfdbe` | `docs/compliance.md` (the data map and control mapping), `docs/incident-response.md`, and a test that every table is in the data map |
| `f86de86` | Fix: the Download button's height |

**Screenshots:**
- [the Download my data dialog](compliance/download-my-data-dialog.png), and [after downloading](compliance/download-my-data-done.png);
- [Download CSV on Events](compliance/events-download-csv.png).

### What each part does

- **Retention:**
  - audit events (all of `auth.*`, `admin.*`, `server.*`, `prune.*`) are kept a year;
  - other events 90 days;
  - through `make prune` targets `audit_events` and `events`;
  - every confirmed prune run records `prune.completed` (built here rather than in A4).
- **Minimising old events** (`make prune target=pseudonymise`): after 30 days, IP addresses are cut to their /24 or /48, and emails are replaced by `hmac:` and a keyed hash, including in admins' Events searches and list searches. The key comes from `APP_KEY` with its own label, so the hash can't be reversed without it. Rows are rewritten, never deleted.
- **Erasure:** deleting an account removes its email and name from every other event, in the same transaction, and keeps IPs. `admin erase` on the CLI handles staff requests: it refuses admins and sole owners of workspaces with other members, and deletes workspaces only that account belongs to.
- **Download my data:**
  - after the password;
  - five a day, counted only once the password is right;
  - synchronous JSON;
  - events capped at 10,000;
  - **no secrets and no one else's data:** a test walks the JSON for every secret's real value and for a co-owner's email, name, ID and IPs;
  - `admin export` on the CLI for people who can't sign in.
- **Audit lines:** every audit event is also written as a JSON log line without emails, for you to ship to storage nobody can alter. The server warns if `LOG_LEVEL` would hide them.
- **Events CSV:** at most 10,000 rows, the read recorded with its row count, and formula cells defused.

## Where the build differs from the plan (recorded in the plan)

- **`prune.completed` is built here,** not in A4.
- **A new column, `events.minimised_at`** (an edit to `007`). Events whose delivery hasn't run yet aren't minimised. Retrying a minimised event's delivery answers 409.
- **Audit lines are written as the row is inserted,** in the same transaction. A rolled-back event can leave a line with no row; the docs explain how to tell.
- **The Events CSV is generated on the server** (`GET /api/admin/events/export`).
- **Decision 9 (inactive accounts) is built with A4's Usage page.**

## For you to decide

1. **`admin export` for a suspended account.** _Built: allowed,_ since the right of access doesn't end with a suspension.
2. **Download my data asks for the password only, not a code.** _As planned,_ matching account deletion.
3. **Retention classes.** All of `auth.*` (sign-ups and verification included) is kept a year; `email.*` (the block-list history) 90 days. _Confirm, or move `email.*` to a year so who blocked what stays provable longer._
4. **Audit lines at insert time.** Lines written strictly after commit would mean wrapping about 60 store transactions. _Accepted as documented._

## Checks

All passed except one end-to-end test:
- `make check`: link check 89 files, 0 broken;
- `make test`: 24 Go packages, 365 Vitest tests;
- `make build`;
- e2e: 25 passed, 1 failed.

The failure is the Monitoring Security chart crash, which main had already fixed (`ebcacd8`) after this branch's merge point. It clears when main is merged in.

## Security review

_Running._
