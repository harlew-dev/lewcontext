# Admin access (secret door, admin sign-in, Users and Workspaces)

Status: **implemented 2026-10-31**, every decision confirmed 2026-10-31 (decision 8 changed: admin reads are recorded); see [How the build differs](#how-the-build-differs). Docs: [admin.md](../../../docs/api/admin.md) (new), [auth.md](../../../docs/api/auth.md), [endpoints.md](../../../docs/api/endpoints.md), [events.md](../../../docs/api/events.md), [rate-limits.md](../../../docs/api/rate-limits.md), [database.md](../../../docs/api/database.md), [seeding.md](../../../docs/api/seeding.md), [api/testing.md](../../../docs/api/testing.md), [api/README.md](../../../docs/api/README.md), [deployment.md](../../../docs/deployment.md), [testing.md](../../../docs/testing.md), [naming.md](../../../docs/naming.md), [ui/README.md](../../../docs/ui/README.md), [ui/pages.md](../../../docs/ui/pages.md), [ui/api.md](../../../docs/ui/api.md), [ui/design.md](../../../docs/ui/design.md), [ui/testing.md](../../../docs/ui/testing.md), and AGENTS.md (rules 4 and 5, the security invariants, the docs table). Builds on [two-factor authentication](mfa.md). The events, mail and monitoring pages are a later plan (`admin-dashboard.md`), built on this shell.

## Why

Operators need to see who has signed up, which workspaces exist, and later what the event log and mail queue say, without running SQL against production. That view can read every user's data, so it's guarded more tightly than the app:

1. **A secret door.** The dashboard lives at `/admin/<ADMIN_ROUTE_KEY>`; every other `/admin…` URL is the ordinary 404 page. This hides the door; **it doesn't authenticate anyone.** The URL ends up in browser history, proxy and CDN logs, and screenshots.
2. **An admin account:** a normal user account granted admin from the command line only (decision 1).
3. **Password plus a second factor at every admin sign-in**, separate from the app's sign-in.
4. **A separate admin session**: its own cookie, a short lifetime, and tied to the current key.
5. **Read-only** in v1 (decision 4). A stolen admin session can read, but not change anything.

Someone with only the key gets nothing. Someone with only an admin's password, or only their app session, gets nothing either.

## The door: `ADMIN_ROUTE_KEY`

### Configuration (`internal/config`)

| Variable | Value | Notes |
| --- | --- | --- |
| `ADMIN_ROUTE_KEY` | a UUIDv4 (`uuidgen`), or 32–128 characters of `[A-Za-z0-9_-]` (`admin route-key generate` prints one: 32 random bytes, base64url) | **Unset: admin is off.** Every `/admin…` page and `/api/admin/…` route is then the ordinary 404, and no comparison is made |

Startup refuses:

- a value that is neither a UUIDv4 nor 32–128 of those characters;
- a value made of one repeated character;
- in production, the throwaway value committed in `.env.example`.

`.env.example` ships that throwaway key, so `/admin/<key>` works under `make dev` at once. `.env.testing` holds none (`TestEnvTestingHoldsNoSecrets` refuses any `*_KEY` with a value): `playwright.config.ts` makes a random key per run and puts it in the environment, which the server it starts inherits and the tests read.

Messages name the variable, never its value. A UUIDv4 has 122 random bits, enough against guessing. The CLI's 256-bit key is the stronger default the docs suggest.

**Rotating the key** (change the variable and restart) signs every admin out: each admin session stores the digest of the key it was created under, and stops working when that no longer matches (see [Admin sessions](#admin-sessions)). If a key may have leaked, rotate it. Nobody can get in with the key alone.

### Pages (`pageRoutes`, `internal/web`)

```
GET /admin, /admin/<anything>     key unset, or <anything> isn't the key (constant-time compare)
                                  → exactly what GET /nowhere gets: the fallback page, 404, same headers
GET /admin/<key>                  → the app shell, 200: the admin sign-in page
GET /admin/<key>/<page…>          no live admin session → 302 /admin/<key>
                                  admin session           → the app shell, 200
```

- **Same 404 as an unknown URL.** A wrong key falls straight through to `Pages`, which already answers unknown paths with the fallback page and 404 (`appPath` in `internal/web/pages.go` doesn't include `/admin`). No database query runs, so the timing matches too. A test compares the status, headers and body with `/nowhere`'s.
- **A right key gets 200 through a new `Pages.ServeApp`**, the same fallback HTML with status 200. `appPath` stays as it is.
- **Admin pages send stricter headers** than app pages, on every response:
  - `Referrer-Policy: no-referrer`, so the key in the path never reaches any other site, even its origin;
  - `X-Robots-Tag: noindex, nofollow`;
  - `Cache-Control: no-store`.
  
  The CSP and other headers are unchanged.
- **The redirect without an admin session** goes to the sign-in page (`/admin/<key>`), as `/app/*` does to `/auth/login` (AGENTS.md rule 7). An app session doesn't count.

### The catch: the browser picks the page

The fallback HTML for `/admin/wrong` is the same app bundle as for any page, and React Router decides what to render. Without care, a wrong key would get a 404 status **but still show the admin sign-in form**. So:

- The admin routes' `clientLoader` first calls `GET /api/admin/me` with the key from the URL in `X-Admin-Key`.
  - **404:** throws a 404 response, and the route renders the ordinary not-found page. It's the same component as `pages/not-found.tsx`, so it looks identical.
  - **401:** shows the sign-in form.
  - **200:** goes to Users.
- Until the loader answers, the admin routes show the same blank `HydrateFallback` as every other page. They show no "Admin" text.

**What the public bundle still shows:** that admin pages exist (the route pattern `admin/:key` and the `/api/admin/*` paths are in the JavaScript), but never the key. Hiding even that would need a separate admin build, which is left out (decision 12).

## API: `X-Admin-Key` on every call (decision 2)

Everything under `/api/admin/` goes through `requireAdminKey` first, before the session is looked at:

```go
// requireAdminKey answers every /api/admin/ request whose X-Admin-Key isn't
// the configured key (or with admin off) exactly as an unknown API route.
func (s *Server) requireAdminKey(next http.Handler) http.Handler
```

- **The key goes in a header, never in an API path.** AGENTS.md keeps tokens out of API URLs, and proxies log paths far more often than headers.
- **A wrong or missing key gets the unknown-route answer.** The API's unknown-route response becomes a deliberate JSON 404 (`{"error": "Not found."}`, today it's ServeMux's plain-text default), shared by both, and a test compares them for GET and POST. `security` (Origin, content type) runs before both, so a bad Origin is refused the same way for both too.
- **What the header protects against:** admin pages and app pages share one origin, so a script injected into an **app** page (XSS) would send the admin cookie with its requests. It can't send the right `X-Admin-Key`, though: the key isn't in any app page, and scripts can't read browser history. Only an XSS on the admin pages themselves could read it (from `location`), and those render other users' data only through React's escaping.

## Who is an admin (decision 1)

```sql
-- 014_create_admins
CREATE TABLE admins (
 user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 -- Until when this admin may set up MFA at the door (first sign-in). Set by
 -- grant and by an MFA reset, cleared once MFA is on. Null: no setup at the door.
 mfa_setup_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
```

- **Granted only from the command line**, by someone with access to the server's environment and database. There is no API route, UI or seeder for it:

  ```sh
  admin grant -email ada@example.com -confirm   # needs an existing account; MFA off → opens a 24 h setup window
  admin revoke -email ada@example.com -confirm  # deletes the row and the user's admin sessions
  admin list                                    # email, name, MFA on/off, admin since
  admin route-key generate                      # prints a new random ADMIN_ROUTE_KEY (like app-key generate)
  ```

  These are `make admin grant email=… CONFIRM=1` locally, and `docker run … lewcontext admin grant …` in production. They are refused for `@example.test` accounts in production, so a seeded fake can't become an admin.
- **An admin must have MFA on to get in.** If it's already on (set up in account settings), they sign in with a code. If it's off, their first admin sign-in runs the setup stage ([below](#admin-sign-in)), but only inside the **setup window**:
  - **Opened by:** `admin grant`, or `admin reset-mfa` on an admin. The window lasts 24 hours (`mfa_setup_until`), and `grant` prints when it closes. Running `grant` again on an existing admin without MFA opens a new window.
  - **Closed by:** MFA being turned on, at the door or in settings.
  - **Why a window:** without one, anyone holding only an admin's password could set up **their own** authenticator at the door and get in, locking the real admin out of their second factor. The window limits that to the day after the operator deliberately granted admin, when the operator expects the setup.
- An admin can't turn MFA off (409 "Admins must keep two-factor authentication on."): revoke admin first.
- **Losing admin:** revoking, deleting the account, a password change, a password reset or an MFA reset deletes every admin session of the user's, in the same transaction.
- Admin status is **never** in app responses (`/api/auth/me`), so the app doesn't reveal who is an admin.

## Admin sessions

A separate table (decision 5), so an app session token can never be looked up as an admin one:

```sql
-- 015_create_admin_sessions
CREATE TABLE admin_sessions (
 token_hash text PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES admins(user_id) ON DELETE CASCADE,  -- revoking admin ends them
 key_hash text NOT NULL,          -- SHA-256 of the ADMIN_ROUTE_KEY it was created under
 csrf_token text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 last_seen_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL
);
CREATE INDEX admin_sessions_user_id_idx ON admin_sessions(user_id);
```

| | App session | Admin session |
| --- | --- | --- |
| Cookie | `__Host-lewcontext_session`, SameSite=Lax | `__Host-lewcontext_admin`, **SameSite=Strict**, HttpOnly, Secure, Path=/ (`lewcontext_admin` in development) |
| Idle / absolute | 30 min / 24 h | **15 min / 8 h** (decision 6) |
| Per user | 5 | 2 |
| Created by | login, register, password change | admin sign-in only, always after a code |
| Valid while | not expired | not expired, **the user is still an admin, and `key_hash` matches the current key** |

- **`requireAdminSession`** (after `requireAdminKey`) loads the session with a join on `admins`, checks `key_hash` against the current key's digest, and checks `X-CSRF-Token` on mutations. It puts the admin in the context (`currentAdmin(r)`, with `events.WithActor`).
- **The two sessions are independent.** Signing in to the app never makes you an admin. Signing out of the app leaves the admin session, and the other way round. One browser can hold both.
- **Cleanup:** `Store.Cleanup` deletes expired admin sessions.

## Admin sign-in

```
/admin/<key>  email + password
  POST /api/admin/login {email, password}       (X-Admin-Key; limitAuth, auth:per-account)
    dummy hash for unknown emails, as login does
    wrong password, or not an admin, or an admin without MFA outside the setup window
        → 401 "Email or password is incorrect."        (the same for all; decision 7)
    right password, admin, MFA on
        → mfa_challenges row, purpose "admin" (mfa.md), cookie lewcontext_admin_mfa, 5 minutes
        → 200 {mfaRequired: true}
    right password, admin, MFA off, inside the setup window
        → mfa_challenges row, purpose "admin_setup", the same cookie, 15 minutes
        → 200 {mfaSetupRequired: true}
    records admin.login_attempted {email, ip, user?, result}

step 2a, MFA on: "Authentication code" (or a recovery code)
  POST /api/admin/login/mfa {code}              (X-Admin-Key; auth:per-ip, mfa:per-account)
    as /api/auth/login/mfa, but only a challenge with purpose "admin" counts
    and the user must still be an admin
    → admin session cookie, records auth.mfa.checked (step admin_login) and admin.logged_in
    → 200 {user, csrfToken, expiresAt}

step 2b, setup: the shared <MfaSetup> (mfa.md, "One setup, two entry points")
  POST /api/admin/login/mfa/setup               (X-Admin-Key; admin_setup challenge, still in the window)
    → 200 {secret, uri}                           (QR code hidden until "Reveal QR code")
  POST /api/admin/login/mfa {code}              (with the admin_setup challenge)
    confirms the secret: MFA on for the whole account, 10 recovery codes,
    other app sessions signed out, mfa_setup_until cleared, auth.mfa.enabled
    → admin session cookie, admin.logged_in
    → 201 {recoveryCodes, user, csrfToken, expiresAt}
  the recovery codes (hidden until revealed), "I've saved these codes" → Users
```

- **Always a code, every time.** A live app session that passed MFA a minute ago doesn't skip it.
- **MFA set up at the door is the account's MFA.** From then on, the admin's app sign-ins ask for a code too, and it shows as on in their account settings.
- **The same generic 401** for a non-admin with the right password: the admin door doesn't confirm a correct password to someone who can't use it. An admin without MFA outside the window gets it too, plus a server log warning and `result: "mfa_off"` in the event, so the operator can see why. The fix is to run `admin grant` again.
- **The setup step works within the challenge's 15 minutes** (longer than a code's 5, for scanning) and the window. Both are checked again when the code is confirmed. A refresh starts over at the password.

## Endpoints

All under `/api/admin`, all behind `requireAdminKey`. Reads have their own in-memory budget, `admin-read:per-user` (1,000 / 5 min).

| Method | Route | Requires | Success |
| --- | --- | --- | --- |
| GET | `/api/admin/me` | key; admin session | `{user, csrfToken, expiresAt}`. 401 without a session (the sign-in page's probe) |
| POST | `/api/admin/login` | key, Origin, JSON, `limitAuth`, `auth:per-account` | `{mfaRequired: true}` or `{mfaSetupRequired: true}`, and the challenge cookie |
| POST | `/api/admin/login/mfa/setup` | key, Origin, JSON, `admin_setup` challenge inside the setup window | `{secret, uri}`; replaces a pending secret. 401 no live challenge or the window closed |
| POST | `/api/admin/login/mfa` | key, Origin, JSON, challenge, `auth:per-ip`, `mfa:per-account` | admin session; with an `admin_setup` challenge, MFA turned on and `201 {recoveryCodes, …}` |
| POST | `/api/admin/logout` | key, admin session, CSRF | 204, cookie cleared, `admin.logged_out` |
| GET | `/api/admin/users` | key, admin session | One page of users: `id`, `name`, `email`, `createdAt`, `mfaEnabled`, `admin`, `workspaces` (count), `lastSignedInAt`. `q` searches name and email; sort by `createdAt` (default, newest first), `name`, `email` ([pagination.md](../../../docs/api/pagination.md)) |
| GET | `/api/admin/users/{user}` | key, admin session | The user as above, plus their memberships (`{workspace: {id, name}, role, addedAt}`) and live session count |
| GET | `/api/admin/workspaces` | key, admin session | One page: `id`, `name`, `createdAt`, `members`, `projects`, `owners` (`{id, name, email}`, at most 3). `q` searches the name |
| GET | `/api/admin/workspaces/{workspace}` | key, admin session | The workspace, its members (`{id, name, email, role, addedAt}`) and project count |

- **Never in a response:** password hashes, session or CSRF tokens of other users, MFA secrets, recovery codes, or internal IDs. A test walks every admin response for those field names, as `TestPayloadsHoldNothingSensitive` does for events.
- **`lastSignedInAt`** is the newest `auth.logged_in` event, read with a lateral join on an index on `events (user_id, created_at) WHERE name = 'auth.logged_in'`. If that's too slow, add a `users.last_signed_in_at` column, set by `CreateSession`.
- **Project names and descriptions** aren't shown in v1: counts only. Showing the least data that's needed is the safer default for a support view (decision 9).
- **Store:** `IndexAdminUsers`, `FindAdminUser`, `IndexAdminWorkspaces`, `FindAdminWorkspace` in `internal/store/admin.go`. Each is **one scoped query**, with no workspace or membership filter, because these views exist to see across workspaces. That's why they're only reachable through `requireAdminSession`. A `routes_test.go` check fails if any `/api/admin/` route is registered outside the admin group.
- **Naming:** `adminUserHandlers.index`/`.show` and `adminWorkspaceHandlers.index`/`.show`; `/api/admin/login` and `/logout` follow the `/api/auth/*` exception for named session actions.

## Events

| Event | Payload | Recorded by |
| --- | --- | --- |
| `admin.granted` | `user`, `mfaSetupUntil` (null when MFA is already on) (no actor: the CLI) | `admin grant` |
| `admin.revoked` | `user`, `adminSessionsRevoked` | `admin revoke` |
| `admin.login_attempted` | `email`, `ip`, `user` (when it exists), `result`: `mfa_required` / `mfa_setup_required` / `wrong_password` / `unknown_account` / `not_admin` / `mfa_off` (outside the setup window) | `POST /api/admin/login` |
| `admin.logged_in` | `user`, `ip` | admin sign-in, after the code |
| `admin.logged_out` | `user` | `POST /api/admin/logout` |
| `admin.user.listed`, `admin.workspace.listed` | `admin`, `q`, `sort`, `page` | `GET /api/admin/users`, `/workspaces` |
| `admin.user.viewed` | `admin`, `user` (the account looked at) | `GET /api/admin/users/{user}` |
| `admin.workspace.viewed` | `admin`, `workspace` | `GET /api/admin/workspaces/{workspace}` |

- **The admin events keep `actor_id`, not `user_id`,** apart from `granted` and `revoked`. Deleting an admin's account then sets the actor to null and leaves the record that someone signed in to the admin dashboard, readable by the `usr_` ID in the payload, until pruned.
- **Reads are recorded** (decision 8): every admin GET of Users or Workspaces writes `admin.*.listed` or `.viewed`, so the trail shows what each admin looked at and searched for. The admin is the actor and is named in the payload (`admin`), so the record survives their account being deleted. `user_id` and `workspace_id` stay empty: deleting the account or workspace looked at doesn't erase the record of an admin having looked, and the record never shows in anything scoped to that user or workspace.
- **`not_admin`** in a failed sign-in is useful in monitoring (someone with a user's password found the door) and doesn't leak, because events are only readable by admins.

## Rate limits

| Name | Limit | Counted by | Where |
| --- | --- | --- | --- |
| `auth:per-ip`, `auth:per-account` | (existing) | IP, email | `POST /api/admin/login`, through `limitAuth` and `accountLimit`, so guessing at the admin door and at the app's login draws on the same allowance |
| `mfa:per-account` | (from [mfa.md](mfa.md)) | user | `POST /api/admin/login/mfa` |
| `admin-read:per-user` | 1,000 / 5 min, in memory | admin user | every admin GET |

A wrong key costs nothing (no query) and isn't counted: guessing 122 bits is hopeless. There is no shared `admin-login:global` limit, because once the key leaks it would let an attacker lock every admin out.

## Web app

- **A new page area, `src/pages/admin/`** (AGENTS.md rule 4 gains it):

  | URL | File | Export |
  | --- | --- | --- |
  | `/admin/:key` | `pages/admin/[key]/index.tsx` | `AdminLogin` |
  | `/admin/:key/users` | `pages/admin/[key]/users.tsx` | `AdminUserIndex` |
  | `/admin/:key/users/:user` | `pages/admin/[key]/users/[user].tsx` | `AdminUserShow` |
  | `/admin/:key/workspaces` | `pages/admin/[key]/workspaces.tsx` | `AdminWorkspaceIndex` |
  | `/admin/:key/workspaces/:workspace` | `pages/admin/[key]/workspaces/[workspace].tsx` | `AdminWorkspaceShow` |

  The four signed-in pages sit under `pages/admin/layout.tsx`. It loads `/api/admin/me` in its `clientLoader` and wraps them in `AdminLayout` (`components/layout/admin-layout.tsx`): the existing `SidebarLayout` with Users and Workspaces (Events, Mail and Monitoring come later), a zinc "Admin" badge beside the app name so it's never mistaken for the app, and "Signed in as … · Sign out". Not prerendered.
- **The key in the web app:** read from the route's `:key` param and passed to the admin API client, which sends it as `X-Admin-Key`. It is never put in `localStorage`, `sessionStorage`, SWR keys or logs. SWR keys are `["admin", adminUserId, path]`.
- **API client:** `lib/api/admin.ts`, `useAdminApi()` → `api.users().index(params)`, `.show(id)`, `api.workspaces().index(params)`, `.show(id)`, `api.login(…)`, `api.loginMfa(code)`, `api.logout()`. Typed paths in `paths.admin…`.
- **Route constants:** `routes.admin.login(key)`, `routes.admin.users.index(key)`, `.show(key, usr)`, `routes.admin.workspaces.index(key)`, `.show(key, wsp)`.
- **Sign-in page:** `AuthLayout`, with full-width buttons and the heading "Admin sign-in". It doesn't link to register or forgot-password. The steps are:
  - email and password;
  - then **either** the code (as the app's login) **or**, on `mfaSetupRequired`, the shared `MfaSetup`, under the heading "Set up two-factor authentication" and the line "Admins need two-factor authentication. Scan this with your authenticator app." `MfaSetup` gets the door's `start` and `confirm` calls, so the QR code, setup key and recovery codes are hidden until revealed, exactly as in account settings;
  - then Users.
- **Lists** use the existing `Table`, `Pagination`, search and `ListSkeleton` ([ui/pagination.md](../../../docs/ui/pagination.md)). Rows link to their detail page; there are no row menus, because there are no actions. Badges: "MFA" (green when on), "Admin".
- **An expired admin session** (401 from any admin call) goes back to `/admin/<key>`, as the app does with login.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| The key leaks (proxy or Cloudflare logs, browser history or sync, a screenshot) | The key isn't authentication: an admin's password and second factor are still needed. Rotating it signs every admin out. [deployment.md](../../../docs/deployment.md) advises keeping `/admin/` out of access logs or redacting it |
| Finding that the door exists | Wrong keys get the byte-identical 404 for pages and API, with no query. The bundle shows that admin pages exist, but never the key |
| Brute-forcing the key | 122–256 bits; nothing to count |
| Guessing an admin's password at the door | The same `auth:*` limits as login, a dummy hash for unknown emails, and a code still needed |
| An app session or app XSS used to reach admin | A separate table and cookie; `X-Admin-Key` isn't known to app pages; Strict cookie; the admin CSRF token is held only by admin pages |
| XSS on the admin pages (showing other users' names, emails, workspace names) | React escaping only (no `dangerouslySetInnerHTML`: this plan turns on ESLint's `react/no-danger` for the whole web app, since nothing uses it today); the strict script CSP; `frame-ancestors 'none'` |
| A stolen admin session | 15 minutes idle and 8 hours maximum; read-only; ends on key rotation, revocation, password change or MFA reset |
| Becoming an admin | CLI only (server and database access); `@example.test` accounts refused in production; `admin.granted` recorded |
| An admin turning off MFA to weaken their own account | Refused while they're an admin |
| Learning that a password is right at the door | The same 401 for non-admins and for admins without MFA outside the setup window |
| An attacker with only an admin's password sets up their own authenticator at the door | Only possible inside the 24-hour window after `grant` or an operator's MFA reset, which the operator starts deliberately. Outside it, the ordinary 401. Setting up MFA is recorded (`auth.mfa.enabled`, `admin.logged_in`), and the window closes the moment MFA is on |
| The QR code, setup key or recovery codes seen over a shared screen | Hidden until revealed, in the shared `MfaSetup` ([mfa.md](mfa.md#web-app)) |
| Admin pages cached or indexed | `no-store`, `noindex`, `no-referrer` |
| Admin reads not scoped like app reads | They're meant to cross workspaces, so they're only reachable behind both admin middlewares, and a test fails if an `/api/admin/` route is registered elsewhere |

## Tests

**Go unit:**
- Config: unset turns admin off; a UUID and a 32+ character key are accepted; short keys, bad characters, one repeated character and the committed values in production are refused; no message contains the value.
- The key comparison is constant time (`subtle.ConstantTimeCompare` over equal-length digests).

**Go integration** (`admin_integration_test.go`, `admin_pages_integration_test.go`):
- **Pages:**
  - With admin off, and with a wrong key, `/admin`, `/admin/x` and `/admin/x/users` are byte-identical to `/nowhere` (status, headers, body).
  - The right key gives 200 with `no-referrer` and `noindex`.
  - An inner page without an admin session gives 302, and an app session doesn't count.
- **API key:**
  - A missing or wrong `X-Admin-Key` on every admin route (GET and POST) matches an unknown `/api/` route.
  - An admin session with a wrong key gives 404.
  - After rotation (a server built with another key), the old admin session gives 401.
- **Sign-in:**
  - A non-admin with the right password, an admin without MFA, a wrong password and an unknown email all give the same 401, with the right `result`.
  - An admin gets `mfaRequired` and no session; the code starts an admin session.
  - **The first sign-in without MFA:**
    - inside the window, `mfaSetupRequired`; `login/mfa/setup` returns a secret; a wrong code is 422; the right code turns MFA on for the account, returns recovery codes, starts an admin session and clears the window;
    - the admin's next app login asks for a code;
    - outside the window (or with the window cleared), the same 401 as a wrong password, with `result: "mfa_off"`;
    - `login/mfa/setup` with a `login` or `admin` challenge, or after the window closes mid-setup, is refused;
    - `grant` again reopens it, and `reset-mfa` on an admin reopens it.
  - A `login`-purpose challenge can't finish an admin sign-in, and an admin challenge can't finish an app login.
  - A user who stops being an admin between the two steps is refused.
- **Sessions:**
  - An app cookie used as the admin cookie is refused, and the other way round.
  - Idle and absolute expiry.
  - A third admin session drops the oldest.
  - Revoking admin, a password change, a password reset, an MFA reset and account deletion each end admin sessions.
  - An admin turning MFA off gets 409.
- **Data:**
  - Users and workspaces index with pagination, search and sort; the shows.
  - A missing or malformed ID gives 404.
  - No sensitive field names in any admin response.
  - Admin routes refuse a normal app session (401), even with the key.
- **Events and limits:** one event per action with the right payload; `auth:per-account` reached at the admin door gives 429.
- **CLI:** `grant` and `revoke` (and the `@example.test` refusal in production mode), `list`, and `key` output that passes config validation.

**Vitest:**
- A wrong key renders the not-found page, not the form.
- The sign-in's two steps.
- The layout redirects on 401.
- The users list states (loading, empty, error with Retry, data).

**Playwright** (`e2e/admin.spec.ts`, using the run's random key from `playwright.config.ts`):
- A wrong key shows the 404 page with status 404.
- An admin (granted with the factory in the test database, MFA on) signs in with a code, sees Users, opens a user, sees Workspaces, and signs out.
- A newly granted admin without MFA:
  - signs in and is taken to setup, where the QR code is hidden;
  - reveals the setup key, enters a code and acknowledges the recovery codes;
  - lands on Users;
  - signs out and signs in again with a code.
- A non-admin's correct password is refused.

## Docs

- **New:** `docs/api/admin.md`, covering the door, admins, sessions, sign-in, endpoints and the CLI. It gets a row in [docs/README.md](../../../docs/README.md) and in the AGENTS.md table.
- [auth.md](../../../docs/api/auth.md): the admin session and key, in short, with a link.
- [endpoints.md](../../../docs/api/endpoints.md): the admin routes, and the JSON 404 for unknown routes.
- [events.md](../../../docs/api/events.md), [rate-limits.md](../../../docs/api/rate-limits.md), [database.md](../../../docs/api/database.md): their rows.
- [api/README.md](../../../docs/api/README.md): `ADMIN_ROUTE_KEY`.
- [deployment.md](../../../docs/deployment.md): the variable, the `admin` binary, granting the first admin, rotating the key, keeping `/admin/` out of access logs, and the checklist.
- [ui/README.md](../../../docs/ui/README.md) and [ui/pages.md](../../../docs/ui/pages.md): the `pages/admin` area.
- **AGENTS.md:** rule 4 gains `src/pages/admin/*`; rule 5 gains `cmd/admin`; the security invariants gain the admin rules (CLI-only grants, the key header, MFA required).

## Decisions

1. **Admins are user accounts granted admin from the CLI.** **Confirmed 2026-10-31.**
2. **The key travels in `X-Admin-Key` on every admin API call.** **Confirmed 2026-10-31.**
3. **MFA required for admins, TOTP plus recovery codes.** **Confirmed 2026-10-31** ([mfa.md](mfa.md)).
4. **Read-only v1.** **Confirmed 2026-10-31.** Actions (sign a user out everywhere, reset MFA, retry a failed delivery) come with step-up codes in a later plan.
5. **A separate `admin_sessions` table** rather than a `kind` column on `sessions`. **Confirmed 2026-10-31:** an app token can't become an admin one through a missed `WHERE kind = …`, and the app's session code stays untouched. The cost is a little duplicated session code.
6. **Admin sessions: 15 minutes idle, 8 hours maximum, 2 per admin.** **Confirmed 2026-10-31:** short enough that a forgotten tab expires, long enough for a working session.
7. **An admin without MFA sets it up at their first admin sign-in.** **Confirmed 2026-10-31.** It uses the same setup component as account settings, with the QR code hidden until revealed.
   - *Confirmed within that:* setup at the door only inside a 24-hour window opened by `admin grant` or an operator's MFA reset; outside it, the same 401 as a wrong password.
   - *Also confirmed:* the same 401 for non-admins. The operator sees `not_admin` or `mfa_off` in the event and a log warning.
8. **Admin reads are recorded.** **Confirmed 2026-10-31, changed from the recommendation:** every list and detail read writes an event, as an audit trail of what admins have done.
9. **Counts only for projects** (no names or descriptions) in v1. **Confirmed 2026-10-31:** the least data that's needed.
10. **Signed-out `/admin/<key>/<page>` redirects to `/admin/<key>` without `?next=`**, and sign-in always lands on Users. **Confirmed 2026-10-31:** fewer patterns to validate, and the admin area is small.
11. **Key format:** a UUIDv4 or 32–128 URL-safe characters. **Confirmed 2026-10-31,** with `admin route-key generate` producing 256-bit keys as the documented default.
12. **The admin pages ship in the main bundle.** **Confirmed 2026-10-31,** on the condition that the key is never stored in the built views: not in the bundle, prerendered HTML, storage, SWR keys or page text. It lives only in the URL, and in the links and `X-Admin-Key` header built from it at runtime.

## Not included

- The **Events, Mail and Monitoring pages**: login attempts, rate limits reached, failed second factors and sign-ups in the last 24 hours; filtering events by email; delivery statuses. They're the next plan, `admin-dashboard.md`. Notes for it:
  - `rate_limit.reached` is written once per limit per window, so it counts windows reached, not requests refused;
  - `payload->>'email'` needs an expression index to filter by email;
  - `event_deliveries` already has the statuses.
- **Any write action** in the dashboard, and impersonating a user.
- An **IP allowlist** for admin (`ADMIN_ALLOWED_IPS`, read through `clientIP`). It's easy to add later as a further layer.
- A **separate admin origin or subdomain**, and passkeys for admins.
- **Several keys at once** (to rotate without signing everyone out).

## How the build differs

- **Admin reads are recorded** (decision 8, changed at confirmation): `admin.user.listed`, `admin.workspace.listed` (`admin`, `q`, `sort`, `page`) and `admin.user.viewed`, `admin.workspace.viewed` (`admin`, and what was looked at), with the admin as actor and no `user_id` or `workspace_id`, so the record outlives what was looked at. The `admin` area joins the event name pattern (`TestCatalogueNames`, naming.md).
- **The key in tests:** `.env.testing` holds no key (`TestEnvTestingHoldsNoSecrets` refuses any `*_KEY`), so `playwright.config.ts` makes `E2E_ADMIN_ROUTE_KEY` per run, `scripts/test-env.sh` keeps it, and `scripts/serve-test.sh` uses it (or a fresh one) and prints the admin URL. Go tests use `factory.AdminRouteKey`, random per process. `.env.example` and Docker Compose carry a throwaway key, refused in production; the plan's "values committed in `.env.example` and `.env.testing`" became that one.
- **Key format:** one rule, 32–128 characters of `[A-Za-z0-9_-]`, which every UUID also meets; not one repeated character; not the template in production.
- **Field names say what they count:** `workspaceCount`, `sessionCount`, `memberCount`, `projectCount`, beside the lists `memberships` and `members`, instead of the plan's `workspaces`, `members` and `projects` (a workspace's page has both a count and a list of members). Sort keys follow projects: `created`, `name`, `email`.
- **Store files:** `internal/store/admins.go` (admins, admin sessions, the door's challenges) and `admin_views.go` (`IndexAdminUsers`, `FindAdminUser`, `IndexAdminWorkspaces`, `FindAdminWorkspace`), instead of one `admin.go`. Handlers: `adminAuthHandlers` (`admin_auth.go`), `adminUserHandlers`, `adminWorkspaceHandlers`.
- **Wrong methods and unknown paths under `/api/admin/`** go through a catch-all behind `requireAdminKey` too, so they answer as an unknown route with or without the key, rather than 405. Elsewhere the API's 405 stays; only ServeMux's plain-text 404 became JSON (`unknownRoutesAsJSON`).
- **The shared code step:** login's `CodeStep` moved to `components/app/mfa/code-step.tsx`, taking its `verify` call and destination, as the MFA plan foresaw. The setup at the door composes `MfaScanStep` and `RecoveryCodeList` like settings (there's no single `MfaSetup` component), on full-width sign-in page buttons. The setup's secret is fetched right after the password step, so the scan step opens at once.
- **The CLI** is `make admin cmd=grant|revoke|list email=… CONFIRM=1` and `make admin cmd="route-key generate"`, the existing `make admin` target, rather than `make admin grant …`. `grantable` holds the `@example.test` refusal so it's testable.
- **The admin chrome:** "Signed in as …" and Sign out live in an account menu in the sidebar footer, as the app's account menu does.
- **A DB outage at the page gate** serves the admin page anyway (its loader then shows the outage page), as `requirePageSession` does.
- **The Playwright admin** is granted through `psql`, as other specs set up memberships: e2e can't use Go factories.
