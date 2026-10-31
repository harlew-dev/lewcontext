# Admin dashboard

Code: `internal/httpapi/admin.go` (the key, admin sessions, the page gate), `admin_auth.go` (sign-in), `admin_users.go` and `admin_workspaces.go` (the reads), `internal/store/admins.go` (admins and admin sessions), `internal/store/admin_views.go` (what admins see), `internal/config/admin_key.go`, `cmd/admin/admins.go` (the CLI). Web: `pages/admin/*`, `lib/admin.tsx`, `lib/api/admin.ts`. Plan: [plans/features/implemented/admin-access.md](../../plans/features/implemented/admin-access.md).

Operators see every account and every workspace without running SQL against production. That view reads every user's data, so it's guarded by layers, each of which alone gets nobody in:

1. **A secret door:** the dashboard lives at `/admin/<ADMIN_ROUTE_KEY>`. Every other `/admin…` URL is the ordinary 404. The key **hides** the door; it doesn't authenticate anyone (it ends up in browser history, proxy logs and screenshots).
2. **An admin account:** a normal user account the operator granted admin from the command line. There is no API route, page or seeder for granting.
3. **The password and a second factor at every admin sign-in**, separate from the app's sign-in. Admins must have MFA; one without it sets it up at their first admin sign-in, inside a 24-hour window.
4. **A separate admin session:** its own table and cookie, 15 minutes idle and 8 hours at most, tied to the current key.
5. **Read-only.** A stolen admin session can read, never change anything. Every read is recorded.

## The key: `ADMIN_ROUTE_KEY`

- **Unset turns admin off:** every `/admin…` page and `/api/admin/…` route is then the ordinary 404, and no comparison is made.
- **Format:** a UUIDv4 (`uuidgen`), or 32–128 characters of `A–Z a–z 0–9 _ -`. Not one repeated character, and in production not `.env.example`'s throwaway key. `go run ./cmd/admin route-key generate` prints a 256-bit one (`make admin cmd="route-key generate"`). Messages never show the value.
- **Compared in constant time** over equal-length SHA-256 digests (`auth.AdminKeyMatches`).
- **Rotating** (change it and restart) signs every admin out: each admin session stores the digest of the key it was made under, and stops working when that no longer matches. Rotate it if it may have leaked.
- **Never in an API path.** Pages carry it in the URL; every API call sends it in `X-Admin-Key`. A script injected into an **app** page could send the admin cookie (same origin), but can't know the key: it isn't in any app page, the bundle or storage, only in the admin page's URL.
- **Local and tests:** `.env.example` and Docker Compose carry a throwaway key, so `http://localhost:3000/admin/<that key>` works under `make dev`. `.env.testing` holds none: `playwright.config.ts` makes one per run (`E2E_ADMIN_ROUTE_KEY`, kept by `scripts/test-env.sh`), the e2e server uses it and prints the admin URL, and Go tests use `factory.AdminRouteKey`.

## Pages

```
GET /admin, /admin/<anything>     key unset, or <anything> isn't the key
                                  → exactly what GET /nowhere gets: the fallback page, 404, same headers, no query
GET /admin/<key>                  → the app shell, 200: the admin sign-in page
GET /admin/<key>/<page…>          no live admin session → 302 /admin/<key> (an app session doesn't count)
                                  admin session         → the app shell, 200
```

Admin pages and their redirects send `Referrer-Policy: no-referrer` (the key never leaves in a Referer), `X-Robots-Tag: noindex, nofollow` and `Cache-Control: no-store` (`Pages.ServeAdmin`, `adminPageHeaders`). The CSP and other headers are the app's.

**The browser picks the page.** The 404's HTML is the same app bundle as any page's, so the admin routes' loaders first call `GET /api/admin/me` with the key: a 404 throws a 404 and the root renders the ordinary not-found page; a 401 shows the sign-in form (or, inside the layout, redirects to it); a 200 goes on. Until then they show the same blank `HydrateFallback` as every page. The bundle reveals that admin pages exist (`admin/:key`, `/api/admin/*`), never the key.

## API

Everything under `/api/admin/` checks `X-Admin-Key` first (`requireAdminKey`): a missing or wrong key gets exactly what an unknown API route gets, `404 {"error": "Not found."}` (the API's unknown-route answer is that JSON too, `unknownRoutesAsJSON`). So does any other path or method under `/api/admin/`, with the key or without. `security` (Origin, content type) runs before both.

| Method | Route | Requires | Success |
| --- | --- | --- | --- |
| POST | `/api/admin/login` | key, Origin, JSON, `limitAuth`, `auth:per-account` | `{mfaRequired: true}` or `{mfaSetupRequired: true}`, and the challenge cookie |
| POST | `/api/admin/login/mfa/setup` | key, Origin, JSON (`{}`), an `admin_setup` challenge inside the setup window | `{secret, uri}`; replaces a pending secret. 401 otherwise (the challenge is ended) |
| POST | `/api/admin/login/mfa` | key, Origin, JSON, a challenge, `auth:per-ip`, `mfa:per-account` | 200 `{user, csrfToken, expiresAt}` and the admin cookie; for an `admin_setup` challenge MFA is turned on and it's 201 with `recoveryCodes` too. 422 `field: "code"`; 401 when the challenge ended; 409 when the setup expired |
| GET | `/api/admin/me` | key, admin session | `{user, csrfToken, expiresAt}`; 401 without one (the pages' probe) |
| POST | `/api/admin/logout` | key, admin session, CSRF | 204, cookie cleared |
| GET | `/api/admin/users` | key, admin session | A page of accounts: `id`, `name`, `email`, `createdAt`, `mfaEnabled`, `admin`, `workspaceCount`, `lastSignedInAt`. `q` (name or email), `sort` `created` (default `-created`), `name`, `email` |
| GET | `/api/admin/users/{user}` | key, admin session | The account as listed, plus `memberships` (`{workspace: {id, name}, role, addedAt}`, at most 100, newest first) and `sessionCount` (live app sessions) |
| GET | `/api/admin/workspaces` | key, admin session | A page of workspaces: `id`, `name`, `createdAt`, `memberCount`, `projectCount`, `owners` (`{id, name, email}`, at most 3, oldest first). `q` (name), `sort` `created` (default `-created`), `name` |
| GET | `/api/admin/workspaces/{workspace}` | key, admin session | The workspace as listed, plus `members` (`{id, name, email, role, addedAt}`, by role then name) |

- **Pagination** is the app's ([pagination.md](pagination.md)): `page`, `perPage`, and 400 for unknown or malformed parameters. A malformed or unknown ID is 404.
- **Never in a response:** password hashes, MFA secrets, recovery codes, other users' session or CSRF tokens, internal IDs, project names or descriptions (counts only). `decodeAdmin` in the tests walks every admin response for such field names.
- **Not scoped by membership, on purpose:** these queries see across workspaces, which is why they're only reachable behind both middlewares. `TestAdminRoutesStayInTheAdminGroup` fails if an `/api/admin` route is registered outside `adminRoutes`, or there without `requireAdminKey`.
- **Admin status is never in an app response** (`/api/auth/me`). The one place an account learns it is an admin is turning MFA off: 409 "Admins must keep two-factor authentication on."

## Admins (`admins`)

`admins (user_id, mfa_setup_until, created_at)`: one row per admin, deleted with the account.

```sh
make admin cmd=grant email=ada@example.com CONFIRM=1    # an existing account; MFA off → a 24-hour setup window
make admin cmd=revoke email=ada@example.com CONFIRM=1   # deletes the row and their admin sessions
make admin cmd=list                                     # email, name, ID, MFA, admin since
make admin cmd="route-key generate"                     # a new ADMIN_ROUTE_KEY
docker run --rm --env-file prod.env lewcontext admin grant -email ada@example.com -confirm
```

- `grant` and `revoke` preview without `-confirm`. `grant` refuses an `@example.test` account in production (the seeders' fakes).
- **The setup window:** an admin without MFA may set it up at the door only until `mfa_setup_until`, which `grant` (again, to reopen) and `reset-mfa` on an admin set to 24 hours ahead, and turning MFA on clears. Outside it they get the door's ordinary 401, a server log warning and `result: "mfa_off"` in the event. Without the window, anyone with only an admin's password could set up their own authenticator at the door.
- **Losing admin sessions:** revoking, deleting the account, a password change, a password reset and an MFA reset each delete every admin session of the user's, in the same transaction.
- An admin can't turn MFA off (409); revoke admin first.

## Sign-in

```
/admin/<key>   email + password → POST /api/admin/login
  the same 401 "Email or password is incorrect." for a wrong password, an unknown email (dummy hash),
  a non-admin, and an admin without MFA outside the window
  admin, MFA on           → challenge purpose "admin", 5 minutes, cookie lewcontext_admin_mfa → {mfaRequired}
  admin, MFA off, window  → challenge purpose "admin_setup", 15 minutes                   → {mfaSetupRequired}

MFA on: the code step (shared CodeStep) → POST /api/admin/login/mfa → admin session → Users
setup:  POST …/mfa/setup → the QR code (hidden until revealed) → POST …/mfa with the first code
        → MFA on for the whole account, 10 recovery codes, other app sessions signed out, window closed,
          admin session → the recovery codes (saved before going on) → Users
```

- **Always a code.** A live app session that passed MFA a minute ago doesn't skip it.
- **The purpose keeps flows apart:** an app login's challenge can't finish an admin sign-in, nor the other way round, and only `admin_setup` can set MFA up. The admin must still be an admin at the second step (`FindAdminChallenge` joins `admins`), and the window is checked again when the setup's code is confirmed.
- **MFA set up at the door is the account's MFA:** the admin's app sign-ins ask for a code too, and settings show it on.
- The door's password step shares the app login's `auth:per-ip` and `auth:per-account`, so guessing at either draws on one allowance. There is no `admin-login:global`: once the key leaked it would let anyone lock every admin out.

## Admin sessions (`admin_sessions`)

| | App session | Admin session |
| --- | --- | --- |
| Cookie | `__Host-lewcontext_session`, SameSite=Lax | `__Host-lewcontext_admin`, **SameSite=Strict**, HttpOnly, Secure, Path=/ (`lewcontext_admin` in development) |
| Idle / absolute | 30 min / 24 h | **15 min / 8 h** |
| Per user | 5 | **2** (a third sign-in drops the oldest) |
| Created by | login, register, password change | admin sign-in only, after a code |
| Valid while | not expired | not expired, **still an admin, and made under the current key** |

- `requireAdminSession` (after `requireAdminKey`) loads the session joined with `admins`, checks its `key_hash`, checks `X-CSRF-Token` on mutations, puts it in the context (`currentAdmin(r)`, `events.WithActor`) and counts reads against `admin-read:per-user` (1,000 / 5 min, in memory).
- **Independent of the app session.** Signing in to either never signs in to the other; signing out of one leaves the other. One browser can hold both. Only the token's digest is stored. `Store.Cleanup` deletes ended ones.

## Audit trail

| Event | Payload | Scope |
| --- | --- | --- |
| `admin.granted`, `admin.revoked` | `user`; `mfaSetupUntil` / `adminSessionsRevoked` | `user_id` (no actor: the CLI) |
| `admin.login_attempted` | `email`, `ip`, `user`, `result` (`mfa_required`, `mfa_setup_required`, `wrong_password`, `unknown_account`, `not_admin`, `mfa_off`) | none |
| `admin.logged_in`, `admin.logged_out` | `user` (and `ip`) | actor |
| `admin.user.listed`, `admin.workspace.listed` | `admin`, `q`, `sort`, `page` | actor |
| `admin.user.viewed`, `admin.workspace.viewed` | `admin`, `user` / `workspace` | actor |

Every read is recorded, so the trail shows what each admin looked at and searched for. The admin is named in the payload so the record outlives their account; `user_id` and `workspace_id` stay empty, so deleting the account or workspace looked at doesn't erase the record, and it never shows among their own events. Each code check also records `auth.mfa.checked` with step `admin_login` (or `enable` for the setup). See [events.md](events.md).

## Web app

- **Routes** (`routes.ts`): `admin/:key` (`pages/admin/[key]/index.tsx`, `AdminLogin`), and inside `pages/admin/layout.tsx`: `users` (`AdminUserIndex`), `users/:user` (`AdminUserShow`), `workspaces` (`AdminWorkspaceIndex`), `workspaces/:workspace` (`AdminWorkspaceShow`). Not prerendered.
- **The key** is read from `:key` and kept in `AdminProvider` (`lib/admin.tsx`), never in storage, SWR keys or page text. `useAdminApi()` sends it in `X-Admin-Key` with the admin CSRF token; `useAdminResource(path)` loads with SWR key `["admin", adminUserId, path]` and sends a 401 back to `/admin/<key>`. Links come from `routes.admin.*(key)`.
- **Loaders** (`lib/loaders.ts`): `adminLoginLoader` (404 → not-found, signed in → Users) and `adminLoader` (404 → not-found, 401 → sign-in).
- **Chrome:** `AdminLayout` (`components/layout/admin-layout.tsx`), the `SidebarLayout` with Users and Workspaces, the app name with a red "Admin" badge (under the name in the sidebar, beside it in the phone top bar), and an account menu with Back to app (`/app`), Account & security (`/app/settings`) and Sign out.
- **Lists** use `PaginatedTable`, `SearchField` and sortable headers (`components/app/admin/admin-list.tsx`: URL state, `AdminTableHead`, `MfaBadge`, `AdminBadge`); rows link to their page. There are no row menus: there are no actions.
- `react/no-danger` is on for the whole web app: other users' names and emails render only through React's escaping.

## Not built

Events, Mail and Monitoring pages (the admin-dashboard plan), any write action or impersonation, an IP allowlist, a separate admin origin, passkeys for admins, and several keys at once (rotating without signing everyone out).
