# Plans

Every feature that changes the architecture gets a plan in `features/` before code is written (AGENTS.md, "Plan before changing the architecture"). Once a plan ships it moves to `features/implemented/`, so `features/` holds only the open ones. Implemented plans stay as a record of what was decided and how the build differed: their old names and details are history, so don't update them to match later code.

## Open plans, in build order

Build them in this order. Each one assumes the ones above it are done, and later plans are written with the names and tables earlier ones introduce.

| # | Plan | Needs | Status |
| --- | --- | --- | --- |
| 1 | [event-names](features/implemented/event-names.md): `<resource>.<fact>` (`workspace.member.removed`, `auth.password_reset.sent`); the `events.name` CHECK | CRUD naming (done) | implemented 2026-10-31 |
| 2 | [convention-checks](features/implemented/convention-checks.md): tests that enforce `naming.md` (store methods, handlers, routes, permissions, events, client, page routes and exports, file names), a docs link check in `make check`, and `docs/checks.md` for people and agents | 1 | implemented 2026-11-01 |
| 3 | [deletion](features/implemented/deletion.md): delete a project, a workspace, your account; `ConfirmationDialog`, danger zones | 1, 2 | implemented 2026-10-31 |
| 4 | [rate-limits](features/implemented/rate-limits.md): budgets on every signed-in request, per-feature limits, quotas; `auth_attempts` → `rate_limits`, `rate_limit.reached`; adds `TestRateLimitNames` | 1, 2 | implemented 2026-10-31, before plan 2 (`TestRateLimitNames` is a test of its own) |
| 5 | [client-ip](features/implemented/client-ip.md): real client IPs behind a proxy (`CLIENT_IP_HEADER`, `TRUSTED_PROXIES`), IPv6 /64 limit keys | 4 (limit keys) | implemented 2026-10-31 |
| 6 | [captcha-challenges](features/captcha-challenges.md): `:challenge` / `:block` levels, Turnstile, `ChallengeDialog`, `PER_IP_LIMITS` | 4, and 5 before any proxied deploy | decisions 1–3, 9, 10, 12–15, 18 agreed; 4–8, 11, 16, 17 to confirm |
| 7 | [workspace-invitations](features/implemented/workspace-invitations.md): invite by email, pending list, accept by link, callout on sign-in | 1 (its event names), 3 (`ConfirmationDialog`), 4 (quota, limits); 6 optional (until then, `invitation:global` has only its block level) | implemented 2026-10-31 |
| 8 | [test-environment](features/implemented/test-environment.md): a committed `.env.testing` for every test run, `lewcontext_test`, a port and database per e2e run (`E2E_PORT`, `auto`), Playwright starting the server | none | implemented 2026-10-31 |
| 9 | [email-delivery](features/implemented/email-delivery.md): SMTP driver (`go-mail`), Mailpit, plain-text parts, `MAIL_DRIVER=smtp\|log\|off`, `invitation:per-email` | 8 | implemented 2026-10-31 |
| 10 | [email-blocks](features/implemented/email-blocks.md): a "Block this address" link on blockable emails (invitations), `/auth/email-blocks/<token>`, `email_blocks`, `admin unblock`, `List-Unsubscribe`; blocked addresses get no email and can't sign up; addresses with an account are told to delete it first | 9 | implemented 2026-11-01; the admin page moved to plan 21 |
| 11 | [mfa](features/implemented/mfa.md): two-factor authentication with an authenticator app (TOTP) and recovery codes, optional per account; `APP_KEY`, `mfa:per-account`, `cmd/admin reset-mfa`, `Reveal` | none | implemented 2026-10-31 |
| 12 | [admin-access](features/implemented/admin-access.md): the secret admin door (`ADMIN_ROUTE_KEY`, `X-Admin-Key`), CLI-granted admins with required MFA (set up at the first admin sign-in, inside a 24-hour window), separate admin sessions, read-only Users and Workspaces | 11 | implemented 2026-10-31 |
| 13 | [admin/events-and-mail](features/implemented/admin/events-and-mail.md): the Events page (audit trail filters, cursor pagination, project content shown), a projects list on the workspace page, and the Mail page (delivery queue, volume, top recipients); `events.ip` | 12 | implemented 2026-10-31 |
| 14 | [admin/access-hardening](features/implemented/admin/access-hardening.md): `ADMIN_ALLOWED_IPS`, `ADMIN_ROUTE_KEY_PREVIOUS`, admin roles (`viewer`, `operator`), the Admins page | 12, 5 | implemented 2026-11-01 |
| 15 | [admin/observability](features/implemented/admin/observability.md): JSON logs with request IDs, panic recovery, `app_errors`, per-minute sums per route (never a row per request), `server_instances`, slow queries, opt-in `/metrics`, liveness/readiness | none (13 for `events.ip`) | implemented 2026-11-01 |
| 16 | [admin/monitoring](features/admin/monitoring.md): twelve Monitoring pages (Overview, Security, Rate limits, IPs, Accounts, Workspaces, Usage, Data, Integrity, Performance, Errors, Configuration) built on shared signals in `internal/monitor` | 13, 15 | decisions to confirm |
| 17 | [admin/alerts](features/admin/alerts.md): alert rules from those signals, one evaluating server, email to admins, a signed webhook, a heartbeat URL | 16 | decisions to confirm |
| 18 | [admin/actions](features/implemented/admin/actions.md): step-up codes, reasons and the `operator` role; sign out, reset MFA, suspend, retry deliveries, IP blocks, revoke a workspace's invitations, acknowledge alerts | 14, 13, 16, 17 | **implemented 2026-11-01**, apart from acknowledging alerts (with alerts) |
| 19 | [admin/compliance](features/admin/compliance.md): retention per kind of data, pseudonymisation, erasure of traces, "Download my data", audit lines shipped off the host, the data map and incident runbook | 13, 15, 18 | decisions to confirm |
| 20 | [email-verification](features/implemented/email-verification.md): `EMAIL_VERIFICATION` (on by default), a 6-digit code before a sign-up's account exists (`pending_registrations`, tied to the browser by a cookie), "You already have an account" instead of a code for a taken email (sign-up no longer reveals accounts), `email_verified_at`, verifying older accounts from settings or at sign-in (`EMAIL_VERIFICATION_EXISTING=required`) | 9, 11 | implemented 2026-10-31 |
| 21 | [admin/email-blocks](features/implemented/admin/email-blocks.md): the Email blocks page (the block list) and Unblock, the dashboard's first action, with A6's step-up, reasons, `admin-action:per-user` and `AdminActionDialog` built for it | 10, 13; 14 for the `operator` role | implemented 2026-11-01 |

## Future plans

Plans in `future/` are bigger directions, not in the build order above. Each says when it can start; once its decisions are confirmed and it's next, it moves to `features/` and gets a row in the table.

| Plan | Starts after | Status |
| --- | --- | --- |
| [framework](future/framework.md): `kit`, a Laravel-style Go framework (`github.com/harlew-dev/kit`, `@harlew-dev/kit-ui`) built from this app's core; Buffalo-style routes and resources, single-action controllers, requests, Eloquent-style models over Bun, typed events and listeners, nested folders enforced by `kit check` | the open admin plans (A8, A2, A9, A3) | decisions to confirm |

Implementing a plan:

1. Confirm its open decisions with the user first.
2. Read the docs for every area it touches, and [`docs/naming.md`](../docs/naming.md). Also [`docs/checks.md`](../docs/checks.md); don't work around a failing check.
3. When it ships, set `Status:` to implemented with the date, note where the build differs, link the docs it updated, update this table, and `git mv` it to `features/implemented/` (fixing the relative links to and from it).

## How the open plans fit together

- **Names** come from [crud-routing-convention](features/implemented/crud-routing-convention.md) (implemented), plan 1 for events, and [`docs/naming.md`](../docs/naming.md): `projects.destroy`, `IndexWorkspaces`, `routes.workspaces.edit`, `api.invitations().accept`. From plan 2 on, `make test` checks them; a later plan that adds a named verb (invitations' `accept`) adds it to the check's list with the user's agreement.
- **The workspace row lock** (members, implemented) serialises every membership change. Deletion (account and workspace) and invitations (accept) take the same lock, always locking workspaces in `id` order.
- **Rate limits:** plan 4 renames the table and the event, adds budgets and quotas, and checks limit names. Plan 6 replaces its limit struct with `threshold` levels, adds challenge levels (there is no `login:global`), and owns the `rate_limit.reached` payload. Plan 5 changes how per-IP keys are counted.
- **Shared dialogs:** plan 3 adds `ConfirmationDialog`, which the members page, invitations' Revoke and every later confirmation use. Plan 6 adds `ChallengeDialog`, opened by the HTTP client, which no page handles itself.
- **The admin plans** (13–19) live in [`features/admin/`](features/admin/README.md), whose README holds their shared rules (every read recorded, one endpoint per page, actions behind a code and a reason) and what the dashboard can and can't answer. They move to `features/implemented/admin/` as each ships. Email blocks (10) adds a Blocked addresses tab to their Mail page.
- **Deployment notes** for all of these are in [`docs/deployment.md`](../docs/deployment.md), with unbuilt parts marked Planned.
