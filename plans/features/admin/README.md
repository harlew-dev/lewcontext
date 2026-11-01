# Admin dashboard: the plans after admin access

Status: **draft 2026-10-31**. Each plan's state is in the table's Status column. Builds on [admin access](../implemented/admin-access.md) (the secret door, CLI-granted admins with required MFA, admin sessions, read-only Users and Workspaces, every read recorded).

Admin access left out the Events, Mail and Monitoring pages, any write action, an IP allowlist and holding several keys at once. These plans cover all of them, plus the observability and compliance groundwork that production monitoring needs. Each is its own plan with its own decisions, so they can be confirmed and shipped one at a time.

## The plans, in build order

| # | Plan | Adds | Needs | Status |
| --- | --- | --- | --- | --- |
| A1 | [events-and-mail](../implemented/admin/events-and-mail.md) | The **Events** page (the audit trail, filtered by name, account, workspace, email, IP and time) and the **Mail** page (the delivery queue, failures, volume per template and recipient). An `events.ip` column; project names and descriptions shown, with a projects list on the workspace page | admin access | **implemented 2026-10-31** |
| A2 | [access-hardening](../implemented/admin/access-hardening.md) | `ADMIN_ALLOWED_IPS` (an IP allowlist), `ADMIN_ROUTE_KEY_PREVIOUS` (rotating the key without signing everyone out), an Admins page, admin roles (`viewer`, `operator`) | admin access | **implemented 2026-11-01** |
| A3 | [observability](../implemented/admin/observability.md) | The groundwork: JSON logs with request IDs, panic recovery, grouped errors (`app_errors`), per-minute sums per route shared across servers (`request_minutes`, `metric_minutes`; never a row per request), server heartbeats (`server_instances`), slow-query logging, an optional Prometheus `/metrics` on a private port | none (A1 for the IP column it logs alongside) | **implemented 2026-11-01** |
| A4 | [monitoring](monitoring.md) | The **Monitoring** pages: Overview, Security, Rate limits, IP addresses, Accounts, Workspaces, Usage, Data, Integrity, Performance, Errors, Configuration | A1, A3 | **part 1 of 3 implemented 2026-11-01** (Overview, Security, Rate limits); parts 2 and 3 to build |
| A5 | [alerts](alerts.md) | Alert rules checked every minute by one server, the Alerts page, email to admins and an optional webhook | A4 | decisions to confirm |
| A6 | [actions](actions.md) | The first write actions, behind a fresh code (step-up), a written reason and the `operator` role: sign out everywhere, reset MFA, suspend an account, retry a delivery, block an IP, revoke a workspace's invitations, acknowledge an alert | A2 (roles), A1 (retry), A5 (acknowledge) | decisions to confirm |
| A7 | [compliance](compliance.md) | Retention per kind of data, scrubbing a deleted account's email from the log, "Download my data", audit events shipped to the log, a data map (`docs/compliance.md`) and an incident runbook | A1, A3, A6 | decisions to confirm |
| A8 | [separate-build](../implemented/admin/separate-build.md) | The admin pages as their own build (`WEB_BUILD=admin`), out of the public bundle and served only behind the key: its scripts at `/admin-assets/` need an "assets pass" cookie that only the admin page gate sets. A build check that no admin code is left in the app build | admin access | **implemented 2026-11-01** |
| A9 | [email-blocks](../implemented/admin/email-blocks.md) | The **Email blocks** page (the block list, from [email blocks](../implemented/email-blocks.md)) and Unblock, the first write: it builds A6's step-up, reasons, `admin-action:per-user` and `AdminActionDialog` early, and waits for A2 only for the `operator` role | admin access, A1 | **implemented 2026-11-01** |
| A10 | [events-page](../implemented/admin/events-page.md) | The **Events** page redesigned: one time range (windows and a custom range on a calendar), a Filters dialog with a grouped, searchable event picker and removable badges, failed checks (`outcome=failed`), and tiles and charts served with the newest page (`summary=include`: 7 days or less, a 5 s deadline, two at a time, the same recorded read); `make seed only=events` for volume | A1 | **implemented 2026-11-01** |

**What can start now:** A1, A2, A8 and A9 are done, so A3 has everything it needs; A6 can reuse A9's step-up, reasons, `admin-action:per-user` and `AdminActionDialog` once its other prerequisites ship. A3 is the groundwork A4, A5 and A7 wait for. A8 keeps A4's pages (rate-limit thresholds, abuse checks, alert rules) out of public view, as rule 7 says. A4 is large and can ship page by page in the order listed in it.

## What the dashboard answers, and what it can't

The dashboard runs inside the app it watches, so it can't tell anyone that the app is down: when the server or database is gone, so is the page. Production monitoring needs both:

| Question | Answered by | Plan |
| --- | --- | --- |
| Is it up? Can people sign in? | **An external uptime check** on `GET /api/health` and the home page, from outside our network, paging someone when it fails | operator's own tool ([deployment.md](../../../docs/deployment.md)), noted in A3 |
| Is it slow, or erroring? On which route? | Request metrics and grouped errors, in the dashboard and through `/metrics` to an external system | A3, A4 |
| Who is attacking it, and how? | The audit trail and rate-limit events, read as abuse signals | A1, A4 |
| Is anyone filling the database? | Row counts per workspace and account, quota headroom, table growth | A4 |
| Is anything broken that tests didn't catch? | Integrity checks: invariants that should never fail | A4 |
| Who should know right now? | Alerts by email and webhook; for "the app is down", the external check | A5 |
| What did admins see and do? | The audit trail, with every admin read and action recorded | admin access, A6 |
| Can we prove it to an auditor? | Retention, erasure, exports, the data map, logs shipped off the server | A7 |

## Rules every admin plan keeps

These come from admin access and AGENTS.md. A plan that needs an exception says so in its Decisions.

1. **Behind both middlewares.** Every route is registered in `adminRoutes`, behind `requireAdminKey` and `requireAdminSession` (`TestAdminRoutesStayInTheAdminGroup`). The key travels only in the page URL and `X-Admin-Key`.
2. **Every read is recorded**, as `admin.<resource>.listed` or `.viewed` with the admin as actor and no `user_id` or `workspace_id`. **One endpoint per page**, so opening a page writes one event, not one per chart.
3. **The least data that's needed.** Counts before names where a count answers the question. Project names and descriptions are shown from A1 on (admin access decision 9 was for the first phase only). Secrets, tokens, hashes and internal IDs never leave the store.
4. **Bounded queries.** Every aggregate has a time window (at most 7 days, 24 hours by default) and an index behind it, and runs inside the request's 10 s timeout. A query that can't meet that becomes a rollup, not a slower page.
5. **Actions are deliberate.** A write needs the `operator` role, a fresh code and a reason, and records who did what and why (A6). Granting admin stays CLI-only.
6. **No third parties by default.** Logs, metrics and errors stay on our servers unless the operator ships them somewhere. Nothing loads a tracking or analytics script.
7. **Admin code stays in the admin build** (from A8 on). New admin pages go in `adminRoutes`; their API calls, route helpers and types live in the admin modules (`lib/api/admin.ts`, `lib/admin-routes.ts`, `types/admin.ts`), never in modules the app imports. The build fails if admin code reaches the app build.

## Admin sidebar, once all ship

```
Users          (admin access)
Workspaces     (admin access)
Events         A1
Mail           A1
Email blocks   A9
Monitoring     A4: Overview · Security · Rate limits · IPs · Accounts · Workspaces · Usage · Data · Integrity · Performance · Errors · Configuration
Alerts         A5   (a count badge while any is open)
Admins         A2
```

## Later, not planned yet

- **Workspace audit logs for owners.** Owners reading their own workspace's history, once we've decided, event by event, which data is available and safe for them to read (other members' IPs and emails, admins' reads). Until then, events are admin-only. Noted in [compliance](compliance.md#known-gaps-written-down).
- **An `Idempotency-Key` for creates**, if [monitoring](monitoring.md#9-integrity)'s duplicate checks show it's needed.

## When these ship

Each moves to `plans/features/implemented/admin/` (fixing relative links), with its `Status:` line, how the build differs, and the docs it updated. `docs/api/admin.md` grows a section per page; monitoring and alerts get `docs/api/monitoring.md`, and compliance gets `docs/compliance.md`, each with a row in `docs/README.md` and the AGENTS.md table.
