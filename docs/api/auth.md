# Authentication and security

Code: `internal/auth` (primitives), `internal/httpapi/auth.go` (handlers), `internal/httpapi/middleware.go` (security, sessions, limits), `internal/store/sessions.go`.

## Passwords

- Argon2id: 19 MiB, 2 iterations, 1 lane, random 16-byte salt, 32-byte digest, constant-time comparison.
- Sign-up requires 12+ Unicode characters; every password is at most 128 UTF-8 bytes. Login only checks that cap, so seeded development accounts with a short `DEMO_PASSWORD` (`password`) can sign in. Emails are trimmed and lowercased; names are 1–80 characters.
- Login against an unknown email still checks a dummy hash, and every failure gives the same message.
- At most 4 password hashes run at once per process (503 when busy).
- Plaintext passwords are never logged or stored.

## Sessions

- A session token is 32 random bytes (base64url, 43 characters). Only its SHA-256 digest is stored in `sessions`.
- **Cookie:** `forma_session` locally, `__Host-forma_session` in production. HttpOnly, SameSite=Lax, Path=/, Secure in production.
- **Expiry:** 30 minutes idle (every request refreshes it) and 24 hours absolute. Expired sessions answer 401.
- **Login and register rotate the session.** The previous one is deleted, and at most 5 sessions exist per user, enforced in a transaction with a row lock on the user.
- Logout deletes the session. `DELETE /api/auth/sessions` deletes all others.
- Session tokens never go in localStorage or URLs.

## CSRF and origin checks

For every mutation (anything except GET, HEAD and OPTIONS), the `security` middleware requires:

- an `Origin` exactly equal to `APP_ORIGIN`,
- no `Sec-Fetch-Site: cross-site`,
- `Content-Type: application/json` (except DELETE).

Signed-in mutations also need `X-CSRF-Token`: an independent random token returned by login, register and `/api/auth/me`, compared in constant time. CORS is not enabled.

## Rate limits

Stored in `auth_attempts`, in 15-minute fixed windows. Successful attempts count too.

| Key | Limit | Where |
| --- | --- | --- |
| `ip:<digest of connection IP>` | 60 | `limitAuth` on register and login |
| `account:<digest of email>` | 10 | `accountLimit` in register and login |

Forwarded-IP headers are ignored, so everyone behind a reverse proxy (or Vite's dev proxy) shares one IP quota. A scaled deployment needs a trusted edge limiter. Account lockout abuse is possible with this fixed-window design.

## Transport and headers

- **API:** `Cache-Control: no-store`, `nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`.
- **Limits:** request bodies 16 KiB, headers 16 KiB, 10 s per request context, and server read/write timeouts.
- **Web** (`internal/web`): a CSP per HTML file whose `script-src` allows only this origin plus the SHA-256 hashes of that file's own inline scripts (computed at startup; never `'unsafe-inline'` scripts), inline CSS allowed because Styletron needs it, self-hosted fonts, `frame-ancestors 'none'`, `nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: strict-origin-when-cross-origin`, `Permissions-Policy`, and HSTS when `APP_ENV=production`. The app shell is `no-store`; hashed assets are cached for a year.
- **Signed-in pages:** `GET /app` and `/app/*` without a valid session (missing, unknown, idle or expired) get a `302` to `/auth/login?next=<path>` before any HTML is sent.
- **Production:** HTTPS (`APP_ORIGIN`) and verified database TLS (`sslmode=verify-full`) are required.

## Workspaces and roles

Data belongs to workspaces; users reach a workspace through a `workspace_user` row with a role.

| Action | owner | admin | member |
| --- | --- | --- | --- |
| View the workspace and its projects | ✓ | ✓ | ✓ |
| Create and edit projects | ✓ | ✓ | ✓ |
| View workspace settings | ✓ | ✓ | ✓ (read-only) |
| Rename the workspace | ✓ | ✓ | |

- **`requireWorkspace`** (after `requireSession`) checks the `{workspace}` ID's shape, then loads it through the session user's membership in one query. A non-member, a missing workspace and a malformed ID all get the same 404, so workspace IDs can't be probed. The workspace and role go in the request context (`currentWorkspace(r)`).
- **`requireRole(min, …)`** refuses lower roles with 403 (`store.RoleAtLeast`). The web app hides controls by role, but the API is what enforces it.
- Creating a workspace makes the creator its `owner`, in the same transaction. Invites, member management, ownership transfer and deleting workspaces are not implemented.
- Public IDs are unguessable (about 83 random bits), but access never depends on that: every workspace route checks membership.

## Protecting a new endpoint

Register workspace routes on the `ws` group in `routes.go`, which applies `requireSession` and `requireWorkspace`; add `requireRole` when only some roles may act. Account routes use `requireSession`. Read the user from `current(r).User.ID` and the workspace from `currentWorkspace(r).ID`, never from the request, and pass them into every store query.

## Not implemented

Email verification, password recovery, MFA/passkeys, OIDC and workspace invites. Don't describe them as existing. Before public use you'd also need email delivery, observability, backups, abuse monitoring and load testing.

References: [OWASP password storage](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html), [OWASP session management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html).
