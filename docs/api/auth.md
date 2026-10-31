# Authentication and security

Code: `internal/auth` (primitives), `internal/httpapi/auth.go` (handlers), `internal/httpapi/middleware.go` (security, sessions, limits), `internal/store/sessions.go`.

## Passwords

- Argon2id: 19 MiB, 2 iterations, 1 lane, random 16-byte salt, 32-byte digest, constant-time comparison.
- Sign-up requires 12+ Unicode characters; every password is at most 128 UTF-8 bytes. Login only checks that cap, so seeded development accounts with a short `DEMO_PASSWORD` (`password`) can sign in. Emails are trimmed and lowercased; names are 1–80 characters.
- Login against an unknown email still checks a dummy hash, and every failure gives the same message.
- At most 4 password hashes run at once per process (503 when busy).
- **Deleting a workspace or the account** also checks the current password, in the same request, the same way (`passwordConfirmed`: `auth:per-account`, behind `limitAuth`), so a stolen session can't destroy either ([Deleting](#deleting)).
- **Changing the password** (signed in) takes the current password twice: `POST /api/auth/password/verify` checks it so the web app can move on to the new one, and `PATCH /api/auth/password` checks it again, because nothing remembers that the first check passed. The new password follows the sign-up rule and must differ from the current one. Verifying, saving the new hash and deleting **every** session of the user's run in one transaction with the user row locked; then this browser gets a new session (cookie and CSRF token), like login. A wrong current password is 422 with `field`, not 401, which the web app treats as an expired session.
- Plaintext passwords are never logged or stored.

## Password reset by link

Code: `internal/httpapi/password_reset.go`, `internal/store/password_resets.go`, `internal/mail` (listener and mailbox). Plan: [plans/features/implemented/forgot-password.md](../../plans/features/implemented/forgot-password.md).

1. **Request** (`POST /api/auth/password-reset {email}`): after the rate limits below, the API records `auth.password_reset.requested` with the email and IP and answers `202`. It doesn't look the account up, so the status, body and timing are the same for every email.
2. **Listener** (`mail.password_reset`, run by the worker about a second later): for an email with an account, it makes a 256-bit token (`auth.Token`), stores only its SHA-256 digest in `password_resets` (one row per user, so a newer link replaces the older one), sends the link `APP_ORIGIN + /auth/forgot-password/<token>` with `mailer.Send`, and records `auth.password_reset.sent`. An email without an account gets nothing. Requests older than the link's 30-minute lifetime are dropped, since retries can run an hour later. The token is never in the database, an event or the server log.
3. **Page** `/auth/forgot-password/<token>`: checks the link (`POST …/check {token}`) before asking for a password.
4. **Reset** (`PATCH /api/auth/password-reset {token, newPassword}`): sign-up's password rule; a cheap validity check before hashing, then one transaction locks the user, then the link (the same order as a password change), checks the link again, saves the hash, deletes the link and **every** session, and records `auth.password_reset.completed`. Nobody is signed in afterwards: the page sends the user to login with the email filled in.

- A link works once, for 30 minutes. A newer request, a reset or a signed-in password change ends it.
- The link is built from `APP_ORIGIN`, never the request's `Host`. Tokens travel to the API only in JSON bodies. The page URL holds the token, so the 30-minute lifetime and single use limit what browser history can leak; pages send `strict-origin-when-cross-origin` referrers and load nothing from other sites, and Go keeps no access log.
- **No email delivery yet.** `MAIL_DRIVER=file` (development) writes each email to `MAIL_FILE` ([mail.md](mail.md)). That file holds working links, so production refuses it; with `MAIL_DRIVER=off` (the production default) the request endpoint answers 503.

## Sessions

- A session token is 32 random bytes (base64url, 43 characters). Only its SHA-256 digest is stored in `sessions`.
- **Cookie:** `lewcontext_session` locally, `__Host-lewcontext_session` in production. HttpOnly, SameSite=Lax, Path=/, Secure in production.
- **Expiry:** 30 minutes idle (every request refreshes it) and 24 hours absolute. Expired sessions answer 401.
- **Login, register and a password change rotate the session.** The previous one is deleted, and at most 5 sessions exist per user, enforced in a transaction with a row lock on the user.
- Logout deletes the session. `DELETE /api/auth/sessions` deletes all others.
- Session tokens never go in localStorage or URLs.

## CSRF and origin checks

For every mutation (anything except GET, HEAD and OPTIONS), the `security` middleware requires:

- an `Origin` exactly equal to `APP_ORIGIN`,
- no `Sec-Fetch-Site: cross-site`,
- `Content-Type: application/json`, except on a DELETE without a body (a DELETE that has one, such as deleting a workspace with its name and password, needs it too).

Signed-in mutations also need `X-CSRF-Token`: an independent random token returned by login, register and `/api/auth/me`, compared in constant time. CORS is not enabled.

## Rate limits

Sign-in, sign-up and password reset are limited per IP, per account or email, and for everyone, and every signed-in request has a budget per user, with tighter limits on creating workspaces and projects and quotas on how many can exist. All of it is in [rate-limits.md](rate-limits.md). What matters here:

- `auth:per-ip` (60 / 15 min) and `auth:per-account` (10 / 15 min) guard every password check: `limitAuth` (the IP limit and a hashing slot) and `accountLimit`. Both deletions and the signed-in password endpoints use the session user's email, so a stolen session can't guess the password faster than a login could.
- Logout, revoking other sessions and the password endpoints skip the signed-in budget, so a stolen session can't use it up to stop its owner.
- **The client IP** is `clientIP(r)` (`httpapi/client_ip.go`), worked out once per request by `clientAddress`. It is the connection's address, unless `CLIENT_IP_HEADER` and `TRUSTED_PROXIES` are set **and** the connection comes from a trusted proxy: then it is the header's address (`X-Forwarded-For` read from the right, skipping trusted proxies; the single-value headers only when they hold exactly one address). A header from any other connection is ignored, so a client can't forge one for fresh limits or fake audit IPs. Without the variables, everyone behind a proxy (and Vite's dev proxy) shares one IP. Per-IP limits count IPv6 clients by their /64. A scaled deployment still needs an edge limiter. Account lockout abuse is possible with this fixed-window design.

## Audit trail

Sign-ups, login attempts (with the email typed and the client IP), sign-ins, sign-outs, other sessions revoked, password checks and changes, deleted accounts, reset links requested, sent and used, and the first refusal of each rate-limit window (`rate_limit.reached`) are recorded in the `events` table; never passwords, hashes or tokens. See [events.md](events.md).

## Transport and headers

- **API:** `Cache-Control: no-store`, `nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`.
- **Limits:** request bodies 16 KiB, headers 16 KiB, 10 s per request context, and server read/write timeouts.
- **Web** (`internal/web`): a CSP per HTML file whose `script-src` allows only this origin plus the SHA-256 hashes of that file's own inline scripts (computed at startup; never `'unsafe-inline'` scripts), inline CSS allowed because Styletron needs it, self-hosted fonts, `frame-ancestors 'none'`, `nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: strict-origin-when-cross-origin`, `Permissions-Policy`, and HSTS when `APP_ENV=production`. The app shell is `no-store`; hashed assets are cached for a year.
- **Signed-in pages:** `GET /app` and `/app/*` without a valid session (missing, unknown, idle or expired) get a `302` to `/auth/login?next=<path>` before any HTML is sent.
- **Production:** HTTPS (`APP_ORIGIN`) and verified database TLS (`sslmode=verify-full`) are required.

## Workspaces and roles

Data belongs to workspaces; users reach a workspace through a `workspace_user` row with a role: `owner`, `admin`, `member` or `viewer`. What each role may do is a list of named permissions, `<resource>.<action>` after the route's CRUD action, granted in one table (`grants` in `internal/auth/permissions.go`):

| Permission | owner | admin | member | viewer | Guards |
| --- | --- | --- | --- | --- | --- |
| *(membership)* | ✓ | ✓ | ✓ | ✓ | `GET` the workspace and its projects |
| `workspace.update` | ✓ | ✓ | | | `PATCH /workspaces/{ws}` |
| `workspace.destroy` | ✓ | | | | `DELETE /workspaces/{ws}` |
| `workspace.invitation.index` / `.store` / `.destroy` | ✓ | ✓ | | | `GET`, `POST …/invitations`, `DELETE …/invitations/{inv}` ([Invitations](#invitations)) |
| `workspace.member.index` | ✓ | ✓ | | | `GET …/members` |
| `workspace.member.update` | ✓ | ✓ | | | `PATCH …/members/{usr}` |
| `workspace.member.destroy` | ✓ | ✓ | | | `DELETE …/members/{usr}` |
| `auth.AnyMember` | ✓ | ✓ | ✓ | ✓ | `DELETE …/membership` (leave) |
| `project.store` | ✓ | ✓ | ✓ | | `POST …/projects` |
| `project.update` | ✓ | ✓ | ✓ | | `PATCH …/projects/{prj}` |
| `project.destroy` | ✓ | ✓ | | | `DELETE …/projects/{prj}` |

A viewer reads everything except the member and invitation lists (which hold other people's emails) and changes nothing. For actions on another person's role, `Role.AssignableRoles()` also says which roles may be targeted: an owner any role, an admin only `member` and `viewer` (so only owners create admins or touch owners), others none. An action on a member needs the permission, and the target's current role (and, for a role change, the new one) in that list; otherwise 403. Acting on yourself goes through leaving, not `members/{usr}` (422).

- **`requireWorkspace`** (after `requireSession`) checks the `{workspace}` ID's shape, then loads it through the session user's membership in one query. A non-member, a missing workspace and a malformed ID all get the same 404, so workspace IDs can't be probed. The workspace and role go in the request context (`currentWorkspace(r)`).
- **`ws.Can(method, path, permission, handler)`** registers a workspace route behind `requirePermission`, which answers 403 when `currentWorkspace(r).Role.Can(permission)` is false. The `ws` group panics at startup on any other mutation (`Handle` with a method other than GET or HEAD), so a mutation can't ship without naming its permission; one every member may do names `auth.AnyMember`. Unknown roles grant nothing.
- The workspace response carries `permissions` and `assignableRoles`, computed from the role, and the web app hides controls with `can(workspace, permission)` instead of keeping its own copy of the rules. The API is what enforces them.
- Creating a workspace makes the creator its `owner`, in the same transaction.
- **Members** (`internal/store/members.go`): changing a role, removing someone and leaving each run in one transaction that first locks the workspace row (`FOR UPDATE`), then re-reads the actor's role and the target's membership, so concurrent changes are serialised. A change that would leave no owner (demoting, removing or leaving as the only owner) is refused with 409 (`ErrLastOwner`); two owners demoting each other at once leave one. Promoting someone to owner, then stepping down, is how ownership moves. Workspace responses carry `canLeave` (false for the only owner) so the web app can say so before you try.
- **Removal** deletes only the `workspace_user` row: the user's next request to that workspace gets 404, their sessions and account stay, and the workspace's projects stay (projects have no owner). Only owners and admins can list members, so members and viewers never see other members' emails.
- **Transferring ownership** is promoting someone to owner, then leaving or stepping down; there is no separate transfer flow.
- Public IDs are unguessable (about 83 random bits), but access never depends on that: every workspace route checks membership.

## Invitations

Code: `internal/httpapi/invitations.go`, `internal/store/invitations.go`, `internal/mail/workspace_invitation.go` (the listener). Plan: [plans/features/implemented/workspace-invitations.md](../../plans/features/implemented/workspace-invitations.md).

1. **Invite** (`POST …/invitations {email, role}`, `workspace.invitation.store`): the role must be in your `assignableRoles`, and never `owner` (invite as admin, then promote, so a link can't grant ownership). Under the workspace lock the store reads your role again, then refuses an existing member, a live invitation to the same email (an expired one is replaced), the 51st live one and the member quota (100 members plus live invitations). It records `workspace.invitation.created`, which queues the email. The answer is the same whether or not the email has an account.
2. **Listener** (`mail.workspace_invitation`, about a second later): makes a 256-bit token (`auth.Token`), stores only its digest and `sent_at` on the row (replacing any earlier link), emails `APP_ORIGIN + /auth/invitations/<token>`, and records `workspace.invitation.sent`. An invitation revoked, accepted or expired meanwhile gets nothing.
3. **Page** `/auth/invitations/<token>`: `POST /api/invitations/check {token}` (works signed out, `auth:per-ip`) returns the workspace's name, who invited you and the role.
4. **Accept** (`POST /api/invitations/accept {token}`, signed in): one transaction locks the workspace, then the invitation, checks the signed-in email is the invited one (403 otherwise, and the invitation is left as it was), adds the membership (an existing member keeps their role), deletes the invitation and records `workspace.invitation.accepted`. A second accept, or a revoke, waits and then finds nothing.
5. **Revoke** (`DELETE …/invitations/{inv}`, `workspace.invitation.destroy`, and the invitation's role in your `assignableRoles`): locks in the same order (workspace, then invitation; the other way round the two deadlock), deletes the row and records `workspace.invitation.revoked`. The link stops working.

- **Joining is always deliberate.** Registering or signing in never looks up invitations: accepting needs the token, which only the invited inbox has, and the signed-in email must match. There is no email verification, so someone with a leaked link who registers the invited address first could still join; single use, the 7-day lifetime and revoking bound that.
- **The token** works once, for 7 days, and only its digest is stored. It is in the page's path and in `?next=` for login and register (AGENTS.md names this exception), never in an API URL. A crafted `?next=` can't make anyone join: the page joins at once only with navigation state from its own buttons, and otherwise shows **Join**.
- **The email** has a fixed subject, escapes every name ([mail.md](mail.md)), and shows the inviter's address so a misleading workspace name can be judged. With `MAIL_DRIVER=off` (production) inviting answers 503.
- **Limits:** `invitation:per-user` (20 an hour), `invitation:global` (200 an hour, 503), 50 live invitations and the member quota ([rate-limits.md](rate-limits.md)).
- Expired invitations stay listed, as Expired, for 30 days; then `Store.Cleanup` deletes them. Accepted and revoked ones are deleted; their events keep the history.

## Deleting

Plan: [plans/features/implemented/deletion.md](../../plans/features/implemented/deletion.md). What each deletion removes is in [endpoints.md](endpoints.md#deleting).

- **A project** needs only `project.destroy` (owners and admins) and the confirmation dialog: its blast radius is one project.
- **A workspace** needs `workspace.destroy` (owners only), its current name typed, and the owner's password, checked as sign-in checks one. `DeleteWorkspace` takes the workspace row lock, so a membership change in flight finishes first or finds the workspace gone. Other members' next request gets 404.
- **The account** needs the password. `DeleteAccount` locks every workspace the user owns, in `id` order, and refuses (409, `LastOwnerError`, which matches `ErrLastOwner`) while they're the only owner of any, so two co-owners deleting their accounts at once leave one owner, and two deletions can't deadlock. On success every session goes (other tabs get 401), the cookie is cleared, and the email can register again.
- **Events:** `project.deleted`, `workspace.deleted` and `auth.account_deleted` ([events.md](events.md)); each password check records `auth.password_checked` with step `destroy_workspace` or `destroy_account`.
- Emails in event payloads that aren't linked to the account (sign-in attempts and reset requests typed before it existed) aren't scrubbed; the 90-day prune removes them.

## Protecting a new endpoint

Register workspace routes in `workspaceRoutes` in `routes.go` (the `ws` group applies `requireSession` and `requireWorkspace`): reads with `ws.HandleFunc("GET", …)`, every mutation with `ws.Can(…, auth.<Permission>, …)`. A new permission goes in `internal/auth/permissions.go` (constant and `grants`) and in `permissions` in `apps/web/src/types/workspaces.ts`; tests check both. Add the route to the permission matrix (`workspaceRoutesMatrix`). Account routes use `requireSession`. Read the user from `current(r).User.ID` and the workspace from `currentWorkspace(r).ID`, never from the request, and pass them into every store query.

## Not implemented

Email verification, email delivery (password reset links and invitations go to a development mailbox file only, so both are off in production), MFA/passkeys and OIDC. Don't describe them as existing. Before public use you'd also need email delivery, observability, backups, abuse monitoring and load testing.

References: [OWASP password storage](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html), [OWASP session management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html).
