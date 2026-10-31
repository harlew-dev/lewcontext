# Admin dashboard

Code: `internal/httpapi/admin.go` (the key, admin sessions, the page gate), `admin_auth.go` (sign-in), `admin_users.go`, `admin_workspaces.go`, `admin_events.go` and `admin_mail.go` (the reads), `internal/store/admins.go` (admins and admin sessions), `internal/store/admin_views.go`, `admin_events.go` and `admin_mail.go` (what admins see), `internal/config/admin_key.go`, `cmd/admin/admins.go` (the CLI). Web: `pages/admin/*`, `lib/admin.tsx`, `lib/api/admin.ts`. Plans: [admin access](../../plans/features/implemented/admin-access.md), [events and mail](../../plans/features/implemented/admin/events-and-mail.md).

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
| GET | `/api/admin/users` | key, admin session | A page of accounts: `id`, `name`, `email`, `createdAt`, `emailVerifiedAt` (null: never verified), `mfaEnabled`, `admin`, `workspaceCount`, `lastSignedInAt`. `q` (name or email), `sort` `created` (default `-created`), `name`, `email` |
| GET | `/api/admin/users/{user}` | key, admin session | The account as listed, plus `memberships` (`{workspace: {id, name}, role, addedAt}`, at most 100, newest first) and `sessionCount` (live app sessions) |
| GET | `/api/admin/workspaces` | key, admin session | A page of workspaces: `id`, `name`, `createdAt`, `memberCount`, `projectCount`, `owners` (`{id, name, email}`, at most 3, oldest first). `q` (name), `sort` `created` (default `-created`), `name` |
| GET | `/api/admin/workspaces/{workspace}` | key, admin session | The workspace as listed, plus `members` (`{id, name, email, role, addedAt}`, by role then name) |
| GET | `/api/admin/workspaces/{workspace}/projects` | key, admin session | A page of the workspace's projects (`id`, `name`, `description`, `status`, `color`, `createdAt`, `updatedAt`), with the app's `q`, `status` and `sort` ([pagination.md](pagination.md)); 404 for a missing workspace |
| GET | `/api/admin/events` | key, admin session | The audit trail, newest first, a cursor page: `{data, next}` ([Events](#events)) |
| GET | `/api/admin/events/{event}` | key, admin session | One event with `userAgent`, `deliveries` and `related` events |
| GET | `/api/admin/catalogue` | key, admin session | `{events, limits, listeners}`: the names the filters offer. **Not recorded**: it holds no user data |
| GET | `/api/admin/mail` | key, admin session | The Mail summary for `window` `24h` (default) or `7d` ([Mail](#mail)) |
| GET | `/api/admin/deliveries` | key, admin session | Every delivery, newest event first, a cursor page: `status`, `listener`, `from`, `to`, `before` (`evt_…:listener`), `perPage` |

- **Pagination** is the app's ([pagination.md](pagination.md)): `page`, `perPage`, and 400 for unknown or malformed parameters. Events and deliveries page by a cursor instead (log tables: [pagination.md](pagination.md#cursor-pages-log-tables)). A malformed or unknown ID is 404.
- **Never in a response:** password hashes, MFA secrets, recovery codes, other users' session or CSRF tokens, internal IDs, the SMTP username and password. Project names and descriptions **are** shown (the workspace's projects, and event payloads as stored). `decodeAdmin` in the tests walks every admin response for such field names.
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
| `admin.event.listed`, `admin.delivery.listed` | `admin`, `filters` (those used, the cursor included) | actor |
| `admin.event.viewed` | `admin`, `event` | actor |
| `admin.mail.viewed` | `admin`, `window` | actor |
| `admin.project.listed` | `admin`, `workspace`, `q`, `status`, `sort`, `page` | actor |

Every read is recorded, so the trail shows what each admin looked at and searched for (who searched for an email, an IP, a workspace). The one exception is `GET /api/admin/catalogue`, which holds only names. The admin is named in the payload so the record outlives their account; `user_id` and `workspace_id` stay empty, so deleting the account or workspace looked at doesn't erase the record, and it never shows among their own events. Each code check also records `auth.mfa.checked` with step `admin_login` (or `enable` for the setup). See [events.md](events.md).

## Events

`GET /api/admin/events` reads the `events` table across every account and workspace:

| Param | Rule | Default |
| --- | --- | --- |
| `name` | A catalogue name (`auth.login_attempted`) or a prefix ending in `.` (`auth.`, `workspace.member.`), expanded in Go to the catalogue names under it, so the query is `name IN (…)`, never `LIKE`. Anything else is 400 | all |
| `account` | `usr_…`: what the account did (`actor_id`) or is about (`user_id`); an unknown account matches nothing | all |
| `workspace` | `wsp_…`, matched on the payload's `workspace`, so a deleted workspace's events and admins' reads of it are found | all |
| `email` | An address, lower-cased, matched with the payload's `email` | all |
| `ip` | An address, or a CIDR no wider than /16 (IPv4) or /32 (IPv6), matched with `events.ip` (`<<=`) | all |
| `from`, `to` | RFC 3339 times (`from` inclusive, `to` exclusive) | none |
| `adminReads` | `include` shows `admin.*.listed` and `admin.*.viewed`, hidden otherwise (named exactly, one shows anyway) | hidden |
| `before`, `perPage` | an `evt_` cursor (400 when that event is gone) and 1–100 | newest, 50 |

- **Rows:** `id`, `name`, `createdAt`, `actor`, `user` and `workspace` (`{id, name, email}` / `{id, name}`, read from the column or, once that's empty, from the payload's `admin`, `user` or `workspace`: a deleted one is `{id}` alone), `ip` (null outside a request), `payload` as stored. The detail adds `userAgent`, `deliveries` (`listener`, `status`, `attempts`, `nextAttemptAt`, `deliveredAt`, `failedAt`, `lastError`) and `related`: the event its `request` field names, and the events naming it (a `.sent` and its `.requested`).
- **Every filter has an index** (migration 007): `ip`, the payload's `email` and `workspace`, `actor_id`, `request`, beside `name` and `user_id`. Cursor pages count nothing.

## Mail

`GET /api/admin/mail` (`window` `24h` or `7d`):

| Field | Holds | From |
| --- | --- | --- |
| `queue` | Per listener (every registered one, plus any renamed one still holding deliveries): `due` now, `retrying` (waiting, failed at least once), `oldestPendingAt` (the oldest waiting delivery's event: the worker's lag), and `failed` / `delivered` among the window's events | `event_deliveries` (the pending index), and the window's events |
| `volume` | Emails sent per hour and kind: `password_reset`, `invitation`, `registration_code`, `account_exists`, `email_verification` | the listeners' `.sent` events |
| `requests` | Asked for vs sent, for `password_reset`, `registration` and `email_verification`. Resets for an address with no account send nothing (a wide gap is someone trying addresses); sign-ups and verification always send | the `.requested` and `.sent` events |
| `topRecipients` | Addresses asked for 3 or more times in the window (at most 20), with `resets`, `invitations` and `registrations`: enough to spot an inbox being flooded, not a log of who got mail | `email` on `auth.password_reset.requested`, `workspace.invitation.created`, `auth.registration.requested` |
| `driver`, `lastDeliveredAt`, `lastFailure` | `MAIL_DRIVER`, SMTP host, port and TLS mode, the sender (never the credentials); the newest delivery done and the newest failure's listener, time and error, in the window | config, `event_deliveries` |

`GET /api/admin/deliveries` lists `event` (`{id, name, createdAt}`), `listener`, `to` (the payload's `email`, if any), `status` (`pending`, `retrying`, `failed`, `delivered`), `attempts`, `nextAttemptAt` (while waiting), `deliveredAt`, `failedAt`, `lastError`. A failure's `last_error` may name the recipient (an SMTP reply), never the password.

## Web app

- **Routes** (`routes.ts`): `admin/:key` (`pages/admin/[key]/index.tsx`, `AdminLogin`), and inside `pages/admin/layout.tsx`: `users` (`AdminUserIndex`), `users/:user` (`AdminUserShow`, with **Activity**: Events for `?account=`), `workspaces` (`AdminWorkspaceIndex`), `workspaces/:workspace` (`AdminWorkspaceShow`, with **Activity** and its projects in a `PaginatedTable`, rows without links), `events` (`AdminEventIndex`), `events/:event` (`AdminEventShow`), `mail` (`AdminDeliveryIndex`). Not prerendered.
- **The key** is read from `:key` and kept in `AdminProvider` (`lib/admin.tsx`), never in storage, SWR keys or page text. `useAdminApi()` sends it in `X-Admin-Key` with the admin CSRF token; `useAdminResource(path)` loads with SWR key `["admin", adminUserId, path]` and sends a 401 back to `/admin/<key>`. Links come from `routes.admin.*(key)`.
- **Nothing reloads on its own.** Every load is a recorded read, so `useAdminResource` doesn't revalidate on focus or reconnect: only opening a page, a new query, or a Refresh button (Events, Mail) loads again.
- **Events** (`pages/admin/[key]/events.tsx`): the filters in a form (Event, a `Listbox` of the catalogue grouped by area, each area's prefix an option; Account, Workspace, Email, IP; From and To as UTC days; Admin reads), applied together with Apply filters, kept in the URL. The table (`components/app/admin/events.tsx`: `EventTable`, `eventSummary`) shows the time (opening the event), the name and a one-line summary from the payload, the actor, what it's about, and the IP; every account, workspace and IP links to its page or filter. **Older / Newer** (`CursorNav`) follow the cursor; the way back travels in navigation state, so the URL holds only `before`. An event's page shows the payload as indented JSON in a `<pre>`, through React's escaping.
- **Mail** (`pages/admin/[key]/mail.tsx`): the window and Refresh in the header, `Stat` tiles for the queue, emails per hour as a stacked `BarChart` (`components/ui/chart.tsx`, Recharts; its numbers in a table for screen readers), asked for vs sent, frequent addresses (each linking to Events `?email=`), the queue per listener, how mail is sent, then the deliveries with status tabs (Failed first while the window has any).
- **Loaders** (`lib/loaders.ts`): `adminLoginLoader` (404 → not-found, signed in → Users) and `adminLoader` (404 → not-found, 401 → sign-in).
- **Chrome:** `AdminLayout` (`components/layout/admin-layout.tsx`), the `SidebarLayout` with Users, Workspaces, Events and Mail, the app name with a red "Admin" badge (under the name in the sidebar, beside it in the phone top bar), and an account menu with Back to app (`/app`), Account & security (`/app/settings`) and Sign out.
- **Lists** use `PaginatedTable`, `SearchField` and sortable headers (`components/app/admin/admin-list.tsx`: URL state, `AdminTableHead`, `MfaBadge`, `EmailBadge` (zinc "Not verified"; a user's page also shows lime "Verified", the list only the unverified), `AdminBadge`); rows link to their page. There are no row menus: there are no actions.
- `react/no-danger` is on for the whole web app: other users' names and emails render only through React's escaping.

## Not built

Monitoring pages, alerts, write actions (retrying a delivery among them), an IP allowlist, admin roles and several keys at once (rotating without signing everyone out) are planned in [plans/features/admin/](../../plans/features/admin/README.md). Email bodies are never stored, so the dashboard can't show them. Not planned: impersonation, a separate admin origin, passkeys for admins.
