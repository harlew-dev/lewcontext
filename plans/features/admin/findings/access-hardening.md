# Findings: A2, admin access hardening

Plan: [access-hardening](../../implemented/admin/access-hardening.md), implemented 2026-11-01. Built by a sub-agent, checked by the orchestrator, then reviewed by a second agent that saw only the diff and the plan.

## What shipped

- **`ADMIN_ALLOWED_IPS`:**
  - a right key from outside the list gets a 404 byte-identical to a wrong key's, on pages, the API and `/admin-assets/`;
  - one `admin.access_refused` event per address per 15 minutes per server, with IPv6 counted by its /64;
  - a production startup warning when `CLIENT_IP_HEADER` isn't set.
- **`ADMIN_ROUTE_KEY_PREVIOUS`:**
  - only sessions made under the previous key keep working, and they get `X-Admin-Key-Status: previous` and a banner;
  - it never opens the sign-in;
  - both digests are compared every time in constant time (`subtle.ConstantTimeSelect`, no early exit).
- **Roles:**
  - `viewer` (the default) and `operator`, granted only through `admin grant -role`, which records `admin.updated`;
  - `RequireAdminRole`;
  - the router panics at startup on an admin mutation that names no role, with `DELETE /api/admin/sessions` as the one written-down exception.
- **Admin sessions** store their sign-in IP and user agent. `admin.session.moved` is recorded once per session when one is used from another /24 or /48.
- **The Admins page:** role, MFA, grant date, last sign-in, live sessions, a CSV for access reviews, a 90-day stale warning, and "sign out my other admin sessions".

## Where the build differs from the plan

- **The allowlist also covers `/admin-assets/`.** This was the orchestrator's choice, prompted by A8's open question: a stolen assets pass shouldn't work from outside the list either.
- **The refusal set is capped at 10,000 addresses** (also the orchestrator's choice). When full, it records nothing more rather than grow. The event is written after the response, so a right key from outside the list takes as long as a wrong one. The log line names the address but never the path, because page paths hold the key.
- **The previous key looks exactly like a wrong key to anyone without a live session under it,** on every page and route, not only the sign-in steps.
  - A session made under the current key and sent with the previous key gets 401 from the API and 404 on pages, since there's no sign-in page to redirect to.
  - Sign-out works under the previous key.
- **New column `admin_sessions.moved_at`,** so "once per session" holds across several servers.
- **`grant` without `-role` keeps an existing admin's role,** so granting again never demotes an operator.
- **New index on events,** `events_admin_logged_in_idx`, partial on `name = 'admin.logged_in'`, for `lastSignedInAt`, per the admin plans' rule 4 (bounded queries).
- **`GET /api/admin/admins`:**
  - answers `{data: [...]}`, at most 500 rows, and 400 for any query parameter;
  - the CSV defuses cells a spreadsheet would run as formulas.
- **The banner can appear mid-visit,** when any admin answer carries the header.
- **Not built, because the pages don't exist yet:** the Configuration page warning (A4) and the alert on a refusal (A5). Code comments mark where they hook in.
- **A9's plan updated:** Unblock now needs `operator`, and its decision 3's waiver is gone, since A2 shipped first.

## For you to confirm

1. **The event name `admin.session.moved`, against `docs/naming.md`.**
   - The naming doc said facts about admin sessions are a single segment; the plan names this event with two.
   - The builder kept the plan's name, which you confirmed, and amended `naming.md` to allow it, citing the sub-resource rule the `auth` area already uses.
   - The alternative is renaming it `admin.session_moved`. Say if you prefer that.

## Risks that remain

- **A timing signal under the previous key.** A request carrying an admin cookie of the right length triggers a session lookup, which a wrong key never does. Someone who already holds a candidate key could learn from timing that it's the previous one. Requests without a cookie make no query and can't be told apart.
- **The idle timer.** A session sent with the other key still has its idle timer refreshed before the 401. The holder already has the cookie, so this is judged harmless.
- **Per-server memory.** The refusal set and the "once" checks are in memory on each server. Every server must share the same admin settings, as the deployment docs ask.
- **Pruned sign-in events.** Once `admin.logged_in` events are pruned (after 90 days), the Admins page shows "Never" for last sign-in. The stale warning still works, because it falls back to the grant date.

## For later plans

- **A9:**
  - register Unblock with `signedIn.Role("DELETE", "/email-blocks", auth.AdminOperator, …)`;
  - hide the button for viewers with `adminCan(role, "operator")`;
  - signed-in admin routes now sit behind `RequireAdminKeyOrPrevious`;
  - A9 decides whether step-up is allowed for a session under the previous key. The orchestrator's view is yes: the session is live and the code is still required.
- **A4:** the Configuration page shows how many sessions still use the previous key (`AdminKeys()`), and the `CLIENT_IP_HEADER` warning.
- **A5:** an alert on the first `admin.access_refused`.

## Environment notes

- **Dev database rebuilt:** the local dev database was rebuilt (`make migrate-fresh CONFIRM=1 && make seed`) for the migration edits. Other checkouts using it need the same.
- **Another session is working in this checkout:** an untracked `plans/features/page-folders.md` (a proposal to move page files into folders) appeared while A2 was being built. It isn't part of this work and was left out of the commit. If it's implemented while these plans are being built, its page moves will collide with the admin pages being added here.

## Security review

The independent reviewer confirmed sound:
- key comparison (no early exit; unset keys never match);
- the previous key gets the 404 on the sign-in steps, and no route under it can create a session;
- the allowlist's parsing and placement (after the key, before any database work, on every admin route, page and asset);
- roles read from the database on every request;
- the Admins response holds the least data needed, and every read is recorded;
- `DELETE /api/admin/sessions` stays scoped to the caller;
- the CSV's formula defusing.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | **Medium** | **The previous key failed open when the database was slow or down.** A request with the previous key and any well-formed cookie got the admin shell, `X-Admin-Key-Status: previous` and a fresh assets pass, confirmed by a probe with an unreachable database. A retired key that leaked could confirm it's still configured and get a pass that outlives its removal | Under the previous key, any lookup error answers exactly as a wrong key, with no pass; a test with an unreachable database. _Result below_ |
| 2 | Low | **The docs claimed timing reveals nothing about which key matched.** True for the comparison, but a previous-key request carrying a cookie costs a database round trip that a wrong key doesn't | Doc reworded to state the gap |
| 3 | Low | **A refused session still had its idle timer refreshed** before the refusal, so refused requests could keep a stolen session alive | The lookup uses only the matched key's digest |
| 4 | Low | **The role registry could be bypassed:** a non-literal method in `OwnSessions`, a call split over lines, or the lowercase `handle` escaped both the startup panic and the source test | `OwnSessions` accepts a fixed list; the test fails on any registration it can't read |
| 5 | Low | **Each refused address started its own goroutine** holding a database connection, so up to 10,000 could burst against real traffic | One worker behind a bounded queue, drained at shutdown |
| 6 | Nit | `adminCan` (web) allowed two unknown roles | Fixed |
| 7 | Nit | The `grant` preview promised an MFA setup window for a role change, which opens none | Fixed |
| 8 | Nit | A client address that isn't an IP (a unix socket) made admin sign-in answer 500 | Refused clearly and documented. TCP deployments are unaffected |

**Fix results.** All eight are fixed. These tests prove the main ones:
- `TestPreviousAdminKeyInAnOutage` (an unreachable database: the previous key's pages and API calls are byte-identical to a wrong key's, and no cookie is set);
- `TestRefusedAdminSessionsAreLeftAlone` (`last_seen_at` unchanged);
- `TestAdminRefusalQueueDropsWhenFull`;
- `TestAdminSignInWithoutAnIPAddress` (503, nothing recorded);
- `TestPreviousAdminKeyOutsideTheAllowlist`;
- `TestAdminSessionMovedOnceUnderConcurrency`.

The builder reproduced each of the three role-registry bypasses and confirmed the test catches it.

**One judgement call kept:** under the *current* key, an inner page still gets its assets pass when the session lookup fails. Anyone holding the current key already gets the shell and a pass from the sign-in page, so it grants nothing new, and without it the admin page couldn't load to show its outage screen.

All checks passed after the fixes: `make check`, `make test` (20 Go packages, 276 Vitest tests), `make build`, and e2e (22 of 22).
