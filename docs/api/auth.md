# Authentication and security

Code: `internal/auth` (primitives), `internal/httpapi/auth.go` (handlers), `internal/httpapi/middleware.go` (security, sessions, limits), `internal/store/sessions.go`.

## Passwords

- Argon2id: 19 MiB, 2 iterations, 1 lane, random 16-byte salt, 32-byte digest, constant-time comparison.
- Sign-up requires 12+ Unicode characters; every password is at most 128 UTF-8 bytes. Login only checks that cap, so seeded development accounts with a short `DEMO_PASSWORD` (`password`) can sign in. Emails are trimmed and lowercased; names are 1–80 characters.
- Login against an unknown email still checks a dummy hash, and every failure gives the same message.
- At most 4 password hashes run at once per process (503 when busy).
- **Changing the password** (signed in) takes the current password twice: `POST /api/auth/password/verify` checks it so the web app can move on to the new one, and `PUT /api/auth/password` checks it again, because nothing remembers that the first check passed. The new password follows the sign-up rule and must differ from the current one. Verifying, saving the new hash and deleting **every** session of the user's run in one transaction with the user row locked; then this browser gets a new session (cookie and CSRF token), like login. A wrong current password is 422 with `field`, not 401, which the web app treats as an expired session.
- Plaintext passwords are never logged or stored.

## Password reset by link

Code: `internal/httpapi/password_reset.go`, `internal/store/password_resets.go`, `internal/mail` (listener and mailbox). Plan: [plans/features/forgot-password.md](../../plans/features/forgot-password.md).

1. **Request** (`POST /api/auth/password-reset {email}`): after the rate limits below, the API records `auth.password_reset_requested` with the email and IP and answers `202`. It doesn't look the account up, so the status, body and timing are the same for every email.
2. **Listener** (`mail.password_reset`, run by the worker about a second later): for an email with an account, it makes a 256-bit token (`auth.Token`), stores only its SHA-256 digest in `password_resets` (one row per user, so a newer link replaces the older one), sends the link `APP_ORIGIN + /auth/forgot-password/<token>` with `mailer.Send`, and records `auth.password_reset_sent`. An email without an account gets nothing. Requests older than the link's 30-minute lifetime are dropped, since retries can run an hour later. The token is never in the database, an event or the server log.
3. **Page** `/auth/forgot-password/<token>`: checks the link (`POST …/check {token}`) before asking for a password.
4. **Reset** (`PUT /api/auth/password-reset {token, newPassword}`): sign-up's password rule; a cheap validity check before hashing, then one transaction locks the user, then the link (the same order as a password change), checks the link again, saves the hash, deletes the link and **every** session, and records `auth.password_reset_completed`. Nobody is signed in afterwards: the page sends the user to login with the email filled in.

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
- `Content-Type: application/json` (except DELETE).

Signed-in mutations also need `X-CSRF-Token`: an independent random token returned by login, register and `/api/auth/me`, compared in constant time. CORS is not enabled.

## Rate limits

Stored in `auth_attempts` in fixed windows; successful attempts count too. Each limit is a named `rateLimit` in `httpapi/limits.go`. Its name, `<feature>:<counted by>`, prefixes the key (`<name>:<sha256 of IP or email>`, or the bare name for a limit shared by everyone) and is the `scope` of `auth.rate_limited`.

| Name | Limit | Where | Refusal |
| --- | --- | --- | --- |
| `auth:per-ip` | 60 / 15 min | `limitAuth` on register, login, both signed-in password endpoints and the reset itself; directly on reset requests and checks | 429 |
| `auth:per-account` | 10 / 15 min | `accountLimit` in register, login and both signed-in password endpoints (the session user's email), so a stolen session can't guess the password faster than a login could. Password reset never touches it | 429 |
| `password-reset:per-ip` | 10 / hour | reset requests: one IP can't mail many people | 429 |
| `password-reset:per-email` | 3 / hour | reset requests, from any IP, known email or not: caps mail to one inbox | 429 |
| `password-reset:all-accounts` | 100 / hour | reset requests from everyone: caps mail in a distributed flood. Also logs a warning | 503 |

A reset request is counted against those four in the order listed, stopping at the first refusal, so one client can't use up the limit shared by everyone.

Forwarded-IP headers are ignored, so everyone behind a reverse proxy (or Vite's dev proxy) shares one IP quota. A scaled deployment needs a trusted edge limiter. Account lockout abuse is possible with this fixed-window design.

## Audit trail

Sign-ups, login attempts (with the email typed and the connection IP), sign-ins, sign-outs, other sessions revoked, password checks and changes, reset links requested, sent and used, and the first refusal of each rate-limit window are recorded in the `events` table; never passwords, hashes or tokens. See [events.md](events.md).

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
| `workspace.destroy` | ✓ | | | | no endpoint yet |
| `workspace.invitation.index` / `.store` / `.destroy` | ✓ | ✓ | | | no endpoints yet (invitations) |
| `workspace.member.index` / `.update` / `.destroy` | ✓ | ✓ | | | no endpoints yet (members) |
| `project.store` | ✓ | ✓ | ✓ | | `POST …/projects` |
| `project.update` | ✓ | ✓ | ✓ | | `PATCH …/projects/{prj}` |
| `project.destroy` | ✓ | ✓ | | | no endpoint yet |

A viewer reads everything except the member and invitation lists (which hold other people's emails) and changes nothing. For actions on another person's role, `Role.AssignableRoles()` also says which roles may be targeted: an owner any role, an admin only `member` and `viewer` (so only owners create admins or touch owners), others none. An action on a member will need the permission and the target's current (and new) role in that list.

- **`requireWorkspace`** (after `requireSession`) checks the `{workspace}` ID's shape, then loads it through the session user's membership in one query. A non-member, a missing workspace and a malformed ID all get the same 404, so workspace IDs can't be probed. The workspace and role go in the request context (`currentWorkspace(r)`).
- **`ws.Can(method, path, permission, handler)`** registers a workspace route behind `requirePermission`, which answers 403 when `currentWorkspace(r).Role.Can(permission)` is false. The `ws` group panics at startup on any other mutation (`Handle` with a method other than GET or HEAD), so a mutation can't ship without naming its permission; one every member may do names `auth.AnyMember`. Unknown roles grant nothing.
- The workspace response carries `permissions` and `assignableRoles`, computed from the role, and the web app hides controls with `can(workspace, permission)` instead of keeping its own copy of the rules. The API is what enforces them.
- Creating a workspace makes the creator its `owner`, in the same transaction. Invites, member management, ownership transfer and deleting workspaces are not implemented.
- Public IDs are unguessable (about 83 random bits), but access never depends on that: every workspace route checks membership.

## Protecting a new endpoint

Register workspace routes in `workspaceRoutes` in `routes.go` (the `ws` group applies `requireSession` and `requireWorkspace`): reads with `ws.HandleFunc("GET", …)`, every mutation with `ws.Can(…, auth.<Permission>, …)`. A new permission goes in `internal/auth/permissions.go` (constant and `grants`) and in `permissions` in `apps/web/src/types/workspaces.ts`; tests check both. Add the route to the permission matrix (`workspaceRoutesMatrix`). Account routes use `requireSession`. Read the user from `current(r).User.ID` and the workspace from `currentWorkspace(r).ID`, never from the request, and pass them into every store query.

## Not implemented

Email verification, email delivery (password reset links go to a development mailbox file only, so reset is off in production), MFA/passkeys, OIDC and workspace invites. Don't describe them as existing. Before public use you'd also need email delivery, observability, backups, abuse monitoring and load testing.

References: [OWASP password storage](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html), [OWASP session management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html).
