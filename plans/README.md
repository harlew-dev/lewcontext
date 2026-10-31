# Plans

Every feature that changes the architecture gets a plan in `features/` before code is written (AGENTS.md, "Plan before changing the architecture"). Once a plan ships it moves to `features/implemented/`, so `features/` holds only the open ones. Implemented plans stay as a record of what was decided and how the build differed: their old names and details are history, so don't update them to match later code.

## Open plans, in build order

Build them in this order. Each one assumes the ones above it are done, and later plans are written with the names and tables earlier ones introduce.

| # | Plan | Needs | Status |
| --- | --- | --- | --- |
| 1 | [event-names](features/implemented/event-names.md): `<resource>.<fact>` (`workspace.member.removed`, `auth.password_reset.sent`); the `events.name` CHECK | CRUD naming (done) | implemented 2026-10-31 |
| 2 | [convention-checks](features/convention-checks.md): tests that enforce `naming.md` (store methods, handlers, routes, permissions, events, client, page routes and exports, file names), a docs link check in `make check`, and `docs/checks.md` for people and agents | 1 | decisions to confirm |
| 3 | [deletion](features/implemented/deletion.md): delete a project, a workspace, your account; `ConfirmationDialog`, danger zones | 1, 2 | implemented 2026-10-31 |
| 4 | [rate-limits](features/implemented/rate-limits.md): budgets on every signed-in request, per-feature limits, quotas; `auth_attempts` → `rate_limits`, `rate_limit.reached`; adds `TestRateLimitNames` | 1, 2 | implemented 2026-10-31, before plan 2 (`TestRateLimitNames` is a test of its own) |
| 5 | [client-ip](features/implemented/client-ip.md): real client IPs behind a proxy (`CLIENT_IP_HEADER`, `TRUSTED_PROXIES`), IPv6 /64 limit keys | 4 (limit keys) | implemented 2026-10-31 |
| 6 | [captcha-challenges](features/captcha-challenges.md): `:challenge` / `:block` levels, Turnstile, `ChallengeDialog`, `PER_IP_LIMITS` | 4, and 5 before any proxied deploy | decisions 1–3, 9, 10, 12–15, 18 agreed; 4–8, 11, 16, 17 to confirm |
| 7 | [workspace-invitations](features/implemented/workspace-invitations.md): invite by email, pending list, accept by link, callout on sign-in | 1 (its event names), 3 (`ConfirmationDialog`), 4 (quota, limits); 6 optional (until then, `invitation:global` has only its block level) | implemented 2026-10-31 |
| 8 | [test-environment](features/implemented/test-environment.md): a committed `.env.testing` for every test run, `lewcontext_test`, a port and database per e2e run (`E2E_PORT`, `auto`), Playwright starting the server | none | implemented 2026-10-31 |
| 9 | [email-delivery](features/email-delivery.md): SMTP driver (`go-mail`), Mailpit, plain-text parts, `MAIL_DRIVER=smtp\|log\|off` | 8 | decisions confirmed |

Implementing a plan:

1. Confirm its open decisions with the user first.
2. Read the docs for every area it touches, and [`docs/naming.md`](../docs/naming.md). Once plan 2 ships, also [`docs/checks.md`](../docs/checks.md); don't work around a failing check.
3. When it ships, set `Status:` to implemented with the date, note where the build differs, link the docs it updated, update this table, and `git mv` it to `features/implemented/` (fixing the relative links to and from it).

## How the open plans fit together

- **Names** come from [crud-routing-convention](features/implemented/crud-routing-convention.md) (implemented), plan 1 for events, and [`docs/naming.md`](../docs/naming.md): `projects.destroy`, `IndexWorkspaces`, `routes.workspaces.edit`, `api.invitations().accept`. From plan 2 on, `make test` checks them; a later plan that adds a named verb (invitations' `accept`) adds it to the check's list with the user's agreement.
- **The workspace row lock** (members, implemented) serialises every membership change. Deletion (account and workspace) and invitations (accept) take the same lock, always locking workspaces in `id` order.
- **Rate limits:** plan 4 renames the table and the event, adds budgets and quotas, and checks limit names. Plan 6 replaces its limit struct with `threshold` levels, adds challenge levels (there is no `login:global`), and owns the `rate_limit.reached` payload. Plan 5 changes how per-IP keys are counted.
- **Shared dialogs:** plan 3 adds `ConfirmationDialog`, which the members page, invitations' Revoke and every later confirmation use. Plan 6 adds `ChallengeDialog`, opened by the HTTP client, which no page handles itself.
- **Deployment notes** for all of these are in [`docs/deployment.md`](../docs/deployment.md), with unbuilt parts marked Planned.
