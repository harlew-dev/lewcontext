# Admin dashboard

Code: `internal/httpapi/admin.go` (the keys, admin sessions, roles, the page gate, the assets pass), `admin_access.go` (`ADMIN_ALLOWED_IPS`, a session that moved), `internal/auth/admin.go` (comparing keys, roles) and `admin_assets.go` (the pass itself), `internal/web` (the admin build, apart from the app's), `internal/httpapi/endpoints/admin`: `sessions.go` (sign-in, sign-out, your other sessions), `users.go`, `workspaces.go`, `workspace_projects.go`, `events.go` (with `event_window.go`), `mail.go`, `deliveries.go`, `admins.go`, `monitoring.go` and `ip_addresses.go` (the reads), `email_blocks.go` (the block list and Unblock) and `step_up.go` (a fresh code for the actions), `internal/store/admins.go` (admins and admin sessions, the step-up window), `internal/store/admin_views.go`, `admin_events.go` (with `admin_event_summary.go` and `admin_event_failures.go`), `admin_mail.go`, `admin_admins.go` and `admin_email_blocks.go` (what admins see), `internal/config/admin_key.go`, `internal/httpapi/summaries.go` (two summaries or Monitoring pages at a time), `internal/monitor` and `internal/store/monitor_*.go` (the Monitoring pages, [monitoring.md](monitoring.md)), `cmd/admin/admins.go` (the CLI). Web: `pages/admin/*`, `lib/admin.tsx`, `lib/admin-routes.ts`, `lib/api/admin.ts`, built on their own (`WEB_BUILD=admin`). Plans: [admin access](../../plans/features/implemented/admin-access.md), [events and mail](../../plans/features/implemented/admin/events-and-mail.md), [separate build](../../plans/features/implemented/admin/separate-build.md), [access hardening](../../plans/features/implemented/admin/access-hardening.md), [email blocks](../../plans/features/implemented/admin/email-blocks.md), [the Events page](../../plans/features/implemented/admin/events-page.md), [monitoring](../../plans/features/admin/monitoring.md) (parts 1 and 2 of 3).

Operators see every account and every workspace without running SQL against production. That view reads every user's data, so it's guarded by layers, each of which alone gets nobody in:

1. **A secret door:** the dashboard lives at `/admin/<ADMIN_ROUTE_KEY>`. Every other `/admin…` URL is the ordinary 404. The key **hides** the door; it doesn't authenticate anyone (it ends up in browser history, proxy logs and screenshots). With `ADMIN_ALLOWED_IPS`, the door is a 404 from anywhere else too, key or not.
2. **An admin account:** a normal user account the operator granted admin, and a role (`viewer` or `operator`), from the command line. There is no API route, page or seeder for granting or for changing a role.
3. **The password and a second factor at every admin sign-in**, separate from the app's sign-in. Admins must have MFA; one without it sets it up at their first admin sign-in, inside a 24-hour window.
4. **A separate admin session:** its own table and cookie, 15 minutes idle and 8 hours at most, tied to the key it was made under (the current one, or `ADMIN_ROUTE_KEY_PREVIOUS` while a rotation finishes).
5. **Read-only apart from unblocking an address, behind a fresh code and a reason.** Every read is recorded. The only writes are signing out, signing out your own other admin sessions, and the one action, Unblock on the Email blocks page: it needs the `operator` role, a code from the last 5 minutes ([step-up](#step-up)) and a written reason, and records who did it and why. A stolen viewer's session can only read; a stolen operator's can unblock only with the admin's authenticator too.

## The key: `ADMIN_ROUTE_KEY`

- **Unset turns admin off:** every `/admin…` page and `/api/admin/…` route is then the ordinary 404, and no comparison is made.
- **Format:** a UUIDv4 (`uuidgen`), or 32–128 characters of `A–Z a–z 0–9 _ -`. Not one repeated character, and in production not `.env.example`'s throwaway key. `go run ./cmd/admin route-key generate` prints a 256-bit one (`make admin cmd="route-key generate"`). Messages never show the value.
- **Compared in constant time** over equal-length SHA-256 digests, with the current key and the previous one **both, every time**, with no early exit, so the comparison's timing shows neither which one matched nor whether one did (`auth.MatchAdminKey`). That is the comparison only: what follows differs. A request under the previous key that carries an admin cookie costs a database round trip a wrong key never makes (one with no cookie costs none), so with a cookie of any shape the previous key can be told from a wrong one by timing. It opens nothing: no sign-in, and no session without that session's cookie. A session is looked up only under the key the request matched, and its stored digest checked again against both in constant time (`auth.MatchAdminKeyHash`).
- **Rotating** (change it and restart) signs every admin out: each admin session stores the digest of the key it was made under, and stops working when that no longer matches. Rotate it this way if it may have leaked. A routine rotation can keep sessions working instead ([below](#rotating-without-signing-out-admin_route_key_previous)).
- **Never in an API path.** Pages carry it in the URL; every API call sends it in `X-Admin-Key`. A script injected into an **app** page could send the admin cookie (same origin), but can't know the key: it isn't in any app page, the bundle or storage, only in the admin page's URL.
- **Local and tests:** `.env.example` and Docker Compose carry a throwaway key, so `http://localhost:3000/admin/<that key>` works under `make dev`. `.env.testing` holds none: `playwright.config.ts` makes one per run (`E2E_ADMIN_ROUTE_KEY`, kept by `scripts/test-env.sh`), the e2e server uses it and prints the admin URL, and Go tests use `factory.AdminRouteKey`.

## Rotating without signing out: `ADMIN_ROUTE_KEY_PREVIOUS`

The key being retired, same format as `ADMIN_ROUTE_KEY`; refused without a current key or equal to it, and never shown in a message or log (startup says only that it is set). **It keeps live sessions made under it working; it never opens the door:**

| Request | Under the current key | Under the previous key |
| --- | --- | --- |
| `GET /admin/<key>` (the sign-in page), `POST /api/admin/login`, `/login/mfa/setup`, `/login/mfa` | as before | exactly a wrong key's 404 |
| An inner page or `/api/admin/*` with a live session made under that key | 200 | 200, with `X-Admin-Key-Status: previous` |
| … with a live session made under the other key | 401 (inner pages: 302 to sign in) | 401 (inner pages: the 404) |
| … with no admin cookie, or one that's no live session | 401 (inner pages: 302 to sign in) | exactly a wrong key's 404, with no query when there's no cookie |
| … when the session can't be checked (the database is down or slow) | inner pages are served anyway, with their pass (the sign-in page gives both to anyone with the key, and the page shows the outage); the API answers 500 | exactly a wrong key's 404, pages and API, and no pass: it fails closed, logging the error |

- **A session sent with the other key is never touched:** the lookup is by the matched key's digest, so it isn't refreshed, and a request with the wrong key can't keep someone's session from idling out. Telling "made under the other key" (401) from "none" under the previous key is a second, read-only check.

- To anyone without a live admin session, the previous key looks like a wrong one. Its sessions' pages say so: the header goes only to a live session made under it, and the web app shows "This admin address is being retired. Ask your operator for the new one before this session ends." above every page.
- **Signing out** works under it (204); the sign-in page it would return to is then the 404.
- **Step by step:** `admin route-key generate`; set it as `ADMIN_ROUTE_KEY` and the old one as `ADMIN_ROUTE_KEY_PREVIOUS`; restart; hand out the new URL; after 8 hours at most (a session's longest) remove the previous key and restart. Removing it ends its sessions at once.
- **A key that may have leaked is never set as previous:** replace it outright, and every session under it ends.
- The assets pass is always signed for the current key, so pages under the previous key hand out passes that keep working after it's removed ([The assets pass](#the-assets-pass)).

## The address allowlist: `ADMIN_ALLOWED_IPS`

Comma-separated addresses or CIDR ranges (`203.0.113.10,198.51.100.0/24,2001:db8:1::/48`), parsed like `TRUSTED_PROXIES`: a range that doesn't parse, an IPv4-mapped range, `0.0.0.0/0` and `::/0` are refused. **Unset: any address.**

- **Checked once a key matched** (current or previous) on every admin page and API call, and once an assets pass is valid on `/admin-assets/…`, against `httpapi.ClientIP(r)`. From any other address the answer is exactly a wrong key's (a missing pass's for files): nobody outside the list learns that the key, or a pass, is right. A wrong key is never checked, so it costs nothing.
- **The operator learns it:** a server log warning (the address, never the path, which holds the key) and `admin.access_refused` (`reason: "ip_not_allowed"`, the address in `events.ip`), **at most once per address per 15 minutes per server**, an IPv6 address counting by its /64 as per-IP limits do. The request only queues the refusal; one worker per server logs it and writes the event, so the request never waits on the database and a right key from outside takes about as long as a wrong one. The queue holds 64: past that, refusals are dropped and counted in a log line at most once a minute, so a burst can't hold database connections real requests need. Shutdown records what's queued (within the server's 10-second grace). The set of recent addresses holds at most 10,000 per server: when it's full it drops ended ones (at most once a minute) and otherwise records nothing more rather than grow, so someone with the key changing address can't grow it, or the events table, without bound. A refusal means the key (or a pass) is somewhere it shouldn't be: rotate it. (The alerts plan raises an alert on the first one.)
- **Behind a proxy it needs the real client address** (`CLIENT_IP_HEADER`, `TRUSTED_PROXIES`): otherwise every request comes from the proxy, and an allowlist naming it lets everyone in. Production startup warns when `ADMIN_ALLOWED_IPS` is set without `CLIENT_IP_HEADER` ([deployment.md](../deployment.md#admin-dashboard)).
- **Stronger, outside the app:** Cloudflare Access, a VPN or Tailscale in front of `/admin/` and `/admin-assets/` authenticate people before they reach the server. The allowlist is the layer that works everywhere.

## Pages

```
GET /admin, /admin/<anything>     key unset, or <anything> isn't the key, or an address outside ADMIN_ALLOWED_IPS
                                  → exactly what GET /nowhere gets: the fallback page, 404, same headers, no query
GET /admin/<key>                  → the admin build's shell, 200: the admin sign-in page, + the assets pass
GET /admin/<key>/<page…>          no live admin session → 302 /admin/<key> (an app session doesn't count), no pass
                                  admin session         → the admin build's shell, 200, + the assets pass
GET /admin/<previous key>         → exactly what GET /nowhere gets
GET /admin/<previous key>/<page…> a live session made under it → the shell, 200, X-Admin-Key-Status: previous, + the pass
                                  anything else                → exactly what GET /nowhere gets
GET /admin-assets/<file>          a valid assets pass, from an allowed address, and the file exists → the file, Cache-Control: private
                                  anything else                                                    → exactly what GET /nowhere gets
```

Admin pages and their redirects send `Referrer-Policy: no-referrer` (the key never leaves in a Referer), `X-Robots-Tag: noindex, nofollow` and `Cache-Control: no-store` (`Pages.ServeAdmin`, `adminPageHeaders`). The CSP and other headers are the app's, with the admin shell's own script hashes.

**The browser picks the page.** The admin build's loaders first call `GET /api/admin/me` with the key: a 404 throws a 404 and the root renders the ordinary not-found page; a 401 shows the sign-in form (or, inside the layout, redirects to it); a 200 goes on. Until then they show the same blank `HydrateFallback` as every page. A wrong key never gets that far: Go answers it with the app's 404 page, which holds no admin code.

## The admin build

The admin pages are a build of their own, so nobody without the key can download them: the app build (every page anyone can load) holds no admin route, no `/api/admin` path and no `X-Admin-Key`. Plan: [separate build](../../plans/features/implemented/admin/separate-build.md).

- **Two builds from one web app.** `scripts/build-web.sh` runs `react-router build` for the app (`build/client` → `internal/web/dist`) and again with `WEB_BUILD=admin` (`build/admin/client` → `internal/web/admin`). `routes.ts` holds both page lists and `react-router build` takes one; the admin build's files live under `/admin-assets/` (Vite's `base`) and its one HTML file is `index.html`. Its every-other-URL page (`pages/admin/not-found.tsx`) loads anything outside `/admin/` as a new page, so links to the app (Back to app, the logo) reach the app build.
- **Kept apart, three times over:**
  - **The app build fails on any admin module** it bundles (`no-admin-modules` in `vite.config.ts`, the modules listed by `isAdminModule` in `web-build.ts`): `pages/admin`, `components/app/admin`, `components/layout/admin-layout.tsx`, `lib/admin.tsx`, `lib/admin-routes.ts`, `lib/use-admin-resource.ts`, `lib/api/admin.ts`, `types/admin.ts`. It reads Rollup's module ids, so it holds however the import was written (an alias, a relative path, `..`, an extension, a dynamic `import()`). This is the main guard.
  - **ESLint** stops such imports in the editor: `no-restricted-imports` for any path naming an admin module, and `no-restricted-syntax` for `import()` of one (and for `import()` of a template literal, which it can't read). Only the admin modules themselves may import them.
  - **`scripts/check-app-build.sh` reads the output** for what isn't a module, such as a pasted string: it fails `make build`, the e2e server and the Docker image when any app build file contains `/admin/`, `/api/admin`, `X-Admin-Key`, `pages/admin` or `admin-assets`, in any case. `make check` runs its self-test (`scripts/check-app-build.test.sh`).
- **Served apart.** `internal/web` embeds the admin build in its own directory (`//go:embed all:admin`), never in `dist`: `/admin-assets/…` on the app's side is the 404, and only `Pages.ServeAdmin` (the shell, after the key) and `Pages.ServeAdminFile` (a file, after the pass) read it. With `ADMIN_ROUTE_KEY` set, a server whose web build has no admin build refuses to start (`web.ErrAdminNotBuilt`); unset, nothing of it is loaded.

### The assets pass

The admin shell's scripts, styles and fonts need the key's protection too, but the key can't be in their URLs (Vite fixes them at build time, and the key would end up in every stack trace and HAR file). So every 200 admin page sets a pass, and `GET /admin-assets/{file...}` (`Server.AdminAssets`) checks it:

| | |
| --- | --- |
| Cookie | `__Secure-lewcontext_admin_assets` in production (`lewcontext_admin_assets` in development); **Path=/admin-assets/**, HttpOnly, SameSite=Strict, Secure in production, Max-Age 8 hours (an admin session's longest) |
| Value | `<expiry, unix seconds>.<base64url HMAC-SHA256(k, "admin-assets" ‖ key digest ‖ expiry)>`; `k` is derived from `APP_KEY` for this alone (HKDF, `lewcontext admin-assets-pass v1`), not the key that seals MFA secrets |
| Set | by `issueAdminAssetsPass` on every 200 admin page (the sign-in page and each inner page), renewing it; never on a redirect or a 404 |
| Signed with | **always the current `ADMIN_ROUTE_KEY`'s digest** (`AdminKeyHash`), whichever key the page was served under: pages under `ADMIN_ROUTE_KEY_PREVIOUS` hand out passes for the current key, and a pass signed with any other digest is invalid |
| Checked | before any file is looked up: the MAC in constant time (`hmac.Equal`), the expiry a positive number of seconds, in the future and at most 8 hours and a minute ahead (the minute for another server's clock). Any of the first 4 cookies of the pass's name may be the pass: the name can't be `__Host-` (its path isn't `/`), so a sibling subdomain could plant one that the browser sends first, but no request makes the server check more. Then, with `ADMIN_ALLOWED_IPS`, the address: a valid pass from outside the list opens nothing (and is recorded as `admin.access_refused`), so a stolen pass works only from where admins work |
| Invalid when | it expires, the key is rotated, `APP_KEY` changes, or admin is off |

- **Stateless:** no table and no query, so servers sharing `APP_KEY` and `ADMIN_ROUTE_KEY` accept each other's passes.
- **It opens code, nothing else.** The API never reads it, and a pass without an admin session only gets the sign-in page's code, which anyone with the key gets anyway.
- **Anything else is the ordinary 404:** no pass, an expired, tampered or other key's pass, an unknown file, HTML (the shell itself), dot-files and directories all get exactly what `GET /nowhere` gets (status, headers, body), and so does the bare `/admin-assets`. Whether a file exists isn't checked until the pass is valid.
- **Caching:** `Cache-Control: private, max-age=28800, immutable`: browsers keep a file only as long as the pass that opened it lasts (`AdminAssetsPassLifetime`), and never `public`, or a shared cache would hand the file to anyone. The names are content-hashed, so a cached file is always the right one.
- **A pass that ends mid-visit:** the next page's code fails to load and React Router reloads the page, which goes through the gate again and renews it (or, without an admin session, back to sign-in).
- **A script injected into an app page** (same origin, and the cookie's path matches) could read admin code in a browser that opened the dashboard in the last 8 hours (the pass, and the browser's cached copies, last that long): still no key and no API access. The app's CSP and `react/no-danger` are the defence against the injection itself.
- **In development** `make dev`'s second Vite (`WEB_BUILD=admin` on `127.0.0.1:3002`, which `:3000` forwards `/admin` and `/admin-assets` to; [testing.md](../testing.md#run-the-dev-server)) serves the admin pages and checks neither the key nor the pass, so anyone who can reach `:3000` can load the admin code (never the data: the API still checks the key). `make serve` and the e2e server run the real gate.

## API

Everything under `/api/admin/` checks `X-Admin-Key` first: a missing or wrong key, or an address outside `ADMIN_ALLOWED_IPS`, gets exactly what an unknown API route gets, `404 {"error": "Not found."}` (the API's unknown-route answer is that JSON too, `unknownRoutesAsJSON`). So does any other path or method under `/api/admin/`, with the key or without. The sign-in steps take the current key only (`RequireAdminKey`); the routes behind a session take the previous key too (`RequireAdminKeyOrPrevious`), and `RequireAdminSession` then lets through only a session made under the key the request came with. `security` (Origin, content type) runs before both.

| Method | Route | Requires | Success |
| --- | --- | --- | --- |
| POST | `/api/admin/login` | key, Origin, JSON, `LimitAuth`, `auth:per-account` | `{mfaRequired: true}` or `{mfaSetupRequired: true}`, and the challenge cookie |
| POST | `/api/admin/login/mfa/setup` | key, Origin, JSON (`{}`), an `admin_setup` challenge inside the setup window | `{secret, uri}`; replaces a pending secret. 401 otherwise (the challenge is ended) |
| POST | `/api/admin/login/mfa` | key, Origin, JSON, a challenge, `auth:per-ip`, `mfa:per-account` | 200 `{user, role, csrfToken, expiresAt}` and the admin cookie; for an `admin_setup` challenge MFA is turned on and it's 201 with `recoveryCodes` too. 422 `field: "code"`; 401 when the challenge ended; 409 when the setup expired |
| GET | `/api/admin/me` | key, admin session | `{user, role, csrfToken, expiresAt}`; 401 without one (the pages' probe) |
| POST | `/api/admin/logout` | key, admin session, CSRF | 204, cookie cleared |
| DELETE | `/api/admin/sessions` | key, admin session, CSRF | 204: every other admin session of yours signed out, this one kept; `admin.sessions_revoked`. No role and no code: it only takes your own access away |
| GET | `/api/admin/users` | key, admin session | A page of accounts: `id`, `name`, `email`, `createdAt`, `emailVerifiedAt` (null: never verified), `mfaEnabled`, `admin`, `workspaceCount`, `lastSignedInAt`. `q` (name or email), `sort` `created` (default `-created`), `name`, `email` |
| GET | `/api/admin/users/{user}` | key, admin session | The account as listed, plus `memberships` (`{workspace: {id, name}, role, addedAt}`, at most 100, newest first) and `sessionCount` (live app sessions) |
| GET | `/api/admin/workspaces` | key, admin session | A page of workspaces: `id`, `name`, `createdAt`, `memberCount`, `projectCount`, `owners` (`{id, name, email}`, at most 3, oldest first). `q` (name), `sort` `created` (default `-created`), `name` |
| GET | `/api/admin/workspaces/{workspace}` | key, admin session | The workspace as listed, plus `members` (`{id, name, email, role, addedAt}`, by role then name) |
| GET | `/api/admin/workspaces/{workspace}/projects` | key, admin session | A page of the workspace's projects (`id`, `name`, `description`, `status`, `color`, `createdAt`, `updatedAt`), with the app's `q`, `status` and `sort` ([pagination.md](pagination.md)); 404 for a missing workspace |
| GET | `/api/admin/events` | key, admin session | The audit trail, newest first, a cursor page: `{data, next, window, refs, summary?}`; the last 24 hours by default; `summary=include` adds the charts' counts on the newest page ([Events](#events)) |
| GET | `/api/admin/events/{event}` | key, admin session | One event with `userAgent`, `deliveries` and `related` events |
| GET | `/api/admin/catalogue` | key, admin session | `{events, eventGroups, limits, listeners}`: the names the filters offer, and the event names' groups ([Event groups](events.md#groups)). **Not recorded**: it holds no user data |
| GET | `/api/admin/mail` | key, admin session | The Mail summary for `window` `24h` (default) or `7d` ([Mail](#mail)) |
| GET | `/api/admin/deliveries` | key, admin session | Every delivery, newest event first, a cursor page: `status`, `listener`, `from`, `to`, `before` (`evt_…:listener`), `perPage` |
| GET | `/api/admin/monitoring/{page}` | key, admin session | A Monitoring page (`overview`, `security`, `limits`, `ips`, `accounts`, `workspaces`, `usage`) for `window` `1h`, `24h` (default) or `7d`: `{page, window, signals, sections, unavailable}`, read within the summaries' bound (one per admin, two per server, 5 s, each signal and section 2 s of it: one past its budget is listed as not loaded; 503 when busy, or when nothing could be read) and recorded once ([monitoring.md](monitoring.md)) |
| GET | `/api/admin/ips/{ip}` | key, admin session | One address's page: an IP address or an IPv6 /64 (its slash escaped), any other 404, over `window` `1h`, `24h` (default) or `7d`: what it did (events by name, accounts seen, a count of emails with no account, limits, a /64's addresses), bounded like a Monitoring page and recorded as `admin.ip.viewed` ([monitoring.md](monitoring.md#one-address)) |
| GET | `/api/admin/admins` | key, admin session | `{data}`: every admin, oldest grant first ([Admins](#the-admins-page)); no parameters (400 for any) |
| GET | `/api/admin/email-blocks` | key, admin session | A page of the block list ([Email blocks](#email-blocks)): `email`, `createdAt`, `linkExpiresAt`. `q` (part of the address, any case), `sort` `created` (default `-created`), `email` |
| POST | `/api/admin/step-up` | key, admin session, CSRF, **`operator`**, `mfa:per-account` | 204: this session's [step-up](#step-up) window is open for 5 minutes. Body `{code}`, an authenticator or recovery code. 422 `field: "code"`; the session's fifth wrong code ends it: 401, cookie cleared |
| DELETE | `/api/admin/email-blocks` | key, admin session, CSRF, **`operator`**, step-up, `admin-action:per-user` | 204: the address is unblocked. Body `{email, reason}`. 403 `{error: "Enter a code to continue.", stepUp: true}` outside the window; 422 `field` `email` or `reason`; 404 "This address isn't blocked." |

- **Pagination** is the app's ([pagination.md](pagination.md)): `page`, `perPage`, and 400 for unknown or malformed parameters. Events and deliveries page by a cursor instead (log tables: [pagination.md](pagination.md#cursor-pages-log-tables)). A malformed or unknown ID is 404.
- **Never in a response:** password hashes, MFA secrets, recovery codes, other users' session or CSRF tokens, internal IDs, the SMTP username and password. Project names and descriptions **are** shown (the workspace's projects, and event payloads as stored). `decodeAdmin` in the tests walks every admin response for such field names.
- **Not scoped by membership, on purpose:** these queries see across workspaces, which is why they're only reachable behind both middlewares. `TestAdminRoutesStayInTheAdminGroup` fails if an `/api/admin` route is registered outside `adminRoutes`, or there without `RequireAdminKey` (or `RequireAdminKeyOrPrevious`).
- **Roles:** every admin route that changes anything is registered with `Role(method, path, role, handler)`, behind `RequireAdminRole` (403 for a role without it), as workspace mutations name a permission with `ws.Can`. The signed-in group panics at startup on any other mutation, and the same test reads `routes.go` for it. The exceptions, each with its reason in the test: the door's three sign-in steps (no session yet) and `OwnSessions` for signing out and signing out your other sessions (they only take your own access away). `OwnSessions` takes only those two (`ownSessionRoutes`) and panics on anything else, and the test fails on any registration in `adminRoutes` it can't read (a method that isn't a string literal, a call split across lines) and on the unchecked `handle`. **Actions** are registered with `Action(method, path, handler)`, which composes `RequireAdminRole(operator)`, `RequireAdminStepUp` and `LimitAdmin(admin-action:per-user)` in that order; Unblock is one (`Action("DELETE", "/email-blocks", …)`). `Role` with `operator` on a mutation panics at startup for anything but the step-up (`operatorRoutes`: it opens the window, so it can't need one), and the source test fails on such a `Role` line too, so an action can't ship with only its role.
- **Admin status is never in an app response** (`/api/auth/me`). The one place an account learns it is an admin is turning MFA off: 409 "Admins must keep two-factor authentication on."

## Admins (`admins`)

`admins (user_id, mfa_setup_until, role, created_at)`: one row per admin, deleted with the account.

| Role | Can |
| --- | --- |
| `viewer` (the default) | every admin page and read, and signing out its own sessions |
| `operator` | that, plus the actions, each still behind its own checks: today, unblocking an address (a fresh code and a reason) |

```sh
make admin cmd=grant email=ada@example.com CONFIRM=1    # an existing account, a viewer; MFA off → a 24-hour setup window
make admin cmd=grant email=ada@example.com role=operator CONFIRM=1  # a new admin as an operator, or change an admin's role
make admin cmd=revoke email=ada@example.com CONFIRM=1   # deletes the row and their admin sessions
make admin cmd=list                                     # email, name, ID, role, MFA, admin since
make admin cmd="route-key generate"                     # a new ADMIN_ROUTE_KEY
make admin cmd=blocks                                   # every blocked email address, oldest first
make admin cmd=unblock email=bob@example.com CONFIRM=1  # take one off the block list
docker run --rm --env-file prod.env lewcontext admin grant -email ada@example.com -role operator -confirm
```

- `grant` and `revoke` preview without `-confirm` (the preview says which role it would give or change). `grant` refuses an `@example.test` account in production (the seeders' fakes).
- **Roles change only here.** `grant -role` on an existing admin with another role changes it and records `admin.updated` (`changes.role`), and nothing else (the setup window stays as it was); the admin's next request has the new role. Without `-role`, a new admin is a viewer and an existing one keeps their role, so granting again to reopen the setup window never changes it. `admin.granted` names the role.
- **The setup window:** an admin without MFA may set it up at the door only until `mfa_setup_until`, which `grant` (again, to reopen) and `reset-mfa` on an admin set to 24 hours ahead, and turning MFA on clears. Outside it they get the door's ordinary 401, a server log warning and `result: "mfa_off"` in the event. Without the window, anyone with only an admin's password could set up their own authenticator at the door.
- **Losing admin sessions:** revoking, deleting the account, a password change, a password reset and an MFA reset each delete every admin session of the user's, in the same transaction.
- An admin can't turn MFA off (409); revoke admin first.
- **The block list** ([auth.md](auth.md#blocking-an-email-address)): `blocks` lists it, and `unblock` previews ("Would unblock … (blocked …)") unless `-confirm`, then records `email.unblocked` with `by: "cli"` and no actor; an address that isn't blocked says so and changes nothing. There is no `block`: only the inbox's own link blocks. The dashboard's [Email blocks](#email-blocks) page lists them too, and an operator can unblock one there.

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
| Valid while | not expired | not expired, **still an admin, and made under the key the request came with** (the current one, or the previous one while it's set) |

- `RequireAdminSession` (after `RequireAdminKeyOrPrevious`) loads the session joined with `admins` (its role included), compares its `key_hash` with both keys' digests and requires the one the request's key matched, checks `X-CSRF-Token` on mutations, puts it in the context (`httpapi.Admin(r)`, `events.WithActor`), notices a move (below) and counts reads against `admin-read:per-user` (1,000 / 5 min, in memory).
- **Where it signed in:** each session stores the client address (`ip`, `httpapi.ClientIP`) and `User-Agent` (cut to 256 characters) of its sign-in, shown on the Admins page. A server whose client address isn't an IP address (reached through a unix socket) can't store one: the door's password and code steps answer 503 "Admin sign-in isn't available on this server." and log why; serve the API over TCP. It isn't bound to them (mobile networks change address), but the first request from another network (another IPv4 /24 or IPv6 /48) records `admin.session.moved` (`from` and `to` as networks, `sessionAge` in seconds) and sets `moved_at`, so it's recorded once per session. A stolen admin cookie used elsewhere shows up in the trail.
- **Independent of the app session.** Signing in to either never signs in to the other; signing out of one leaves the other. One browser can hold both. Only the token's digest is stored. `Store.Cleanup` deletes ended ones.

## Audit trail

| Event | Payload | Scope |
| --- | --- | --- |
| `admin.granted`, `admin.revoked` | `user`; `role` and `mfaSetupUntil` / `adminSessionsRevoked` | `user_id` (no actor: the CLI) |
| `admin.updated` | `user`, `changes.role` (`from`, `to`) | `user_id` (no actor: the CLI) |
| `admin.access_refused` | `reason`: `ip_not_allowed` | none; the address in `ip` |
| `admin.session.moved` | `admin`, `from`, `to` (networks), `sessionAge` | actor |
| `admin.session.ended` | `admin`, `reason`: `step_up_failures` (the session's fifth wrong step-up code) | actor |
| `admin.sessions_revoked` | `admin`, `count` | actor |
| `admin.login_attempted` | `email`, `ip`, `user`, `result` (`mfa_required`, `mfa_setup_required`, `wrong_password`, `unknown_account`, `not_admin`, `mfa_off`) | none |
| `admin.logged_in`, `admin.logged_out` | `user` (and `ip`) | actor |
| `admin.user.listed`, `admin.workspace.listed` | `admin`, `q`, `sort`, `page` | actor |
| `admin.user.viewed`, `admin.workspace.viewed` | `admin`, `user` / `workspace` | actor |
| `admin.event.listed`, `admin.delivery.listed` | `admin`, `filters` (those used, the cursor included; for events, `names` as sent, the window as resolved and `summary`, the charts' status when they were asked for) | actor |
| `admin.event.viewed` | `admin`, `event` | actor |
| `admin.mail.viewed` | `admin`, `window` | actor |
| `admin.monitoring.viewed` | `admin`, `page`, `window`: one per page served, its signals, charts and review lists together | actor |
| `admin.ip.viewed` | `admin`, `address` (the address or IPv6 /64 looked at), `window` | actor |
| `admin.project.listed` | `admin`, `workspace`, `q`, `status`, `sort`, `page` | actor |
| `admin.admin.listed` | `admin` | actor |
| `admin.email_block.listed` | `admin`, `q`, `sort`, `page` | actor |
| `admin.email.unblocked` | `admin`, `email`, `reason`; with `email.unblocked` (`by: "admin"`, the admin as actor too), in one transaction | actor |

Every read is recorded, so the trail shows what each admin looked at and searched for (who searched for an email, an IP, a workspace). The one exception is `GET /api/admin/catalogue`, which holds only names. The admin is named in the payload so the record outlives their account; `user_id` and `workspace_id` stay empty, so deleting the account or workspace looked at doesn't erase the record, and it never shows among their own events. Each code check also records `auth.mfa.checked` with step `admin_login` (or `enable` for the setup, `admin_step_up` for a [step-up](#step-up)). See [events.md](events.md).

## Events

`GET /api/admin/events` reads the `events` table across every account and workspace:

| Param | Rule | Default |
| --- | --- | --- |
| `name` | Comma-separated tokens: catalogue names (`auth.login_attempted`) and group IDs from `events.Groups()` (`auth.*`, `auth.mfa.*`, `admin-reads`; [events.md](events.md#groups)), expanded in Go to the union of their names, so the query is `name IN (…)` on at most 60 known names, never `LIKE`. 400 for an unknown, empty (`a,,b`) or repeated token, more than 64, or a value over 4,096 bytes | every name but the admin reads |
| `outcome` | `failed`: only events recording a failed check ([Failed checks](#failed-checks)) | any |
| `account` | `usr_…`: what the account did (`actor_id`) or is about (`user_id`); an unknown account matches nothing | all |
| `workspace` | `wsp_…`, matched on the payload's `workspace`, so a deleted workspace's events and admins' reads of it are found | all |
| `email` | An address, lower-cased, matched with the payload's `email` | all |
| `ip` | An address, or a CIDR no wider than /16 (IPv4) or /32 (IPv6), matched with `events.ip` (`<<=`) | all |
| `window` | `1h`, `24h`, `today`, `yesterday` (UTC days), `7d`, `all`, resolved from the server's clock. A relative window starts on its first chart bucket's boundary ("the last 24 hours" at 14:47 starts at 14:30) and runs to now; the list has no upper bound but `yesterday`'s midnight | **`24h`** |
| `from`, `to` | RFC 3339 times in years 1970–9999 (`from` inclusive, `to` exclusive, `from` < `to`, at most 366 days apart: a year back to the end of today, as the page's calendar offers), instead of `window` (400 with it); one alone is open-ended | none |
| `summary` | `include`: add the [summary](#the-summary). 400 with `before` | none |
| `before`, `perPage` | an `evt_` cursor (400 when that event is gone) and 1–100 | newest, 50 |

- **Admin reads** (`admin.*.listed`, `.viewed`) are their own group, `admin-reads`: picked (or named), they show; `admin.*` leaves them out; with no `name` they're hidden.
- **Response:** `{data, next, window, refs, summary?}`. `window` is `{from, to}` as resolved (`from` null for a range with no start), or `null` for `all`. `refs` names the account and workspace filters, `{account: {id, name} | null, workspace: {id, name} | null}` (`{id}` alone once deleted, or for an ID nothing has), so the page's badges read as names. It names the filtered account or workspace whether or not any rows match (the ID and name only, never the email), and the read records the filter.
- **Rows:** `id`, `name`, `createdAt`, `failed` (it records a failed check), `actor`, `user` and `workspace` (`{id, name, email}` / `{id, name}`, read from the column or, once that's empty, from the payload's `admin`, `user` or `workspace`: a deleted one is `{id}` alone), `ip` (null outside a request), `payload` as stored. The detail adds `userAgent`, `deliveries` (`listener`, `status`, `attempts`, `nextAttemptAt`, `deliveredAt`, `failedAt`, `lastError`) and `related`: the event its `request` field names, and the events naming it (a `.sent` and its `.requested`).
- **Every filter has an index** (migration 007): `ip`, the payload's `email` and `workspace`, `actor_id`, `request`, beside `name` and `user_id`; `events_created_idx` includes `name`, so the default view's counts read the index alone. Cursor pages count nothing.
- **One recorded read per request**, the summary included: `admin.event.listed` with `filters` (`names` as sent, `outcome`, `account`, `workspace`, `email`, `ip`, `window`, `from` and `to` as resolved, `before`, and `summary`: the charts' status when they were asked for (`ok`, `busy`, `too_slow`, `too_wide`), left out when they weren't).

### Failed checks

One fixed table in the store, `adminEventFailures` (`internal/store/admin_event_failures.go`), is behind the rows' `failed`, `outcome=failed` and the failures chart, so the three never disagree. Each row is a name and fixed SQL over its payload; nothing in it comes from input.

| Kind | Event | Failed when |
| --- | --- | --- |
| `sign_in` | `auth.login_attempted` | `result` is `wrong_password` or `unknown_account` |
| `second_factor` | `auth.mfa.checked` | `ok` is false |
| `password` | `auth.password_checked` | `ok` is false |
| `sign_up_code` | `auth.registration.checked` | `ok` is false |
| `admin_door` | `admin.login_attempted`; `admin.access_refused` | `result` is `wrong_password`, `unknown_account`, `not_admin` or `mfa_off`; always |

### The summary

`summary=include` on the newest page (no `before`) adds the charts' numbers, computed after the list under the same filters (`SummarizeAdminEvents`, `internal/store/admin_event_summary.go`): two grouped counts (per bucket and name; per bucket and failure kind, reading only those names' payloads), folded in Go.

```json
{ "status": "ok", "bucketSeconds": 1800, "total": 12345, "stackedBy": "group",
  "series": ["auth.*", "workspace.*", "admin.*", "project.*", "other"],
  "buckets": [{ "start": "2026-10-31T14:30:00Z", "counts": [12, 3, 0, 1, 0] }],
  "topNames": [{ "name": "auth.login_attempted", "count": 4021 }],
  "failures": { "total": 312, "series": ["sign_in", "second_factor", "password", "sign_up_code", "admin_door"], "buckets": [ … ] },
  "limitsReached": 4, "signInAttempts": 4021 }
```

- **Buckets** by the window's length, aligned to UTC (`date_bin`): up to 1 hour, 1 minute; up to 6 hours, 5 minutes; up to 2 days (and `24h`, `today`, `yesterday`), 30 minutes; up to 7 days, 3 hours. Every bucket is listed, empty ones as zeros; `counts` line up with `series`.
- **Series:** stacked by top-level group, or by name when every picked name is in one group; the four largest and `other` when there are more than five. `topNames` are the ten busiest names; `failures` is null when no picked name can record a failed check.
- **Bounds**, so the list always answers: `status` is `too_wide` for a window over 7 days, `all` or a range with no start; `too_slow` past a **5 s deadline**; `busy` at once when **the same admin** already has one in flight, and after waiting at most 1 s when **two summaries** are already running on this server (`httpapi.Summaries`). None is a rate limit: `admin-read:per-user` counts the request once. Only `status` is sent when it isn't `ok`.
- **How long a request can take:** the list first, then the summary, which waits up to 1 s for a slot and then runs up to 5 s. The list itself is bounded only by the request's 10 s timeout: its worst case is rare names over all time, where the planner may walk the time index; at a million events it took 30 ms, but on a much larger table it could take seconds.
- At a million events (`make seed only=events`), the default view's counts take about 0.1 s (an index-only scan), 7 days' about 1 s ([testing.md](../testing.md#volume-check-for-the-events-page)).

## Mail

`GET /api/admin/mail` (`window` `24h` or `7d`):

| Field | Holds | From |
| --- | --- | --- |
| `queue` | Per listener (every registered one, plus any renamed one still holding deliveries): `due` now, `retrying` (waiting, failed at least once), `oldestPendingAt` (the oldest waiting delivery's event: the worker's lag), and `failed` / `delivered` among the window's events | `event_deliveries` (the pending index), and the window's events |
| `volume` | Emails sent per hour and kind: `password_reset`, `invitation`, `registration_code`, `account_exists`, `email_verification` | the listeners' `.sent` events |
| `requests` | Asked for vs sent, for `password_reset`, `registration` and `email_verification`. Resets for an address with no account send nothing (a wide gap is someone trying addresses); sign-ups and verification send, except to a blocked address. An invitation to a blocked address is counted as asked for and never sent | the `.requested` and `.sent` events |
| `topRecipients` | Addresses asked for 3 or more times in the window (at most 20), with `resets`, `invitations` and `registrations`: enough to spot an inbox being flooded, not a log of who got mail | `email` on `auth.password_reset.requested`, `workspace.invitation.created`, `auth.registration.requested` |
| `driver`, `lastDeliveredAt`, `lastFailure` | `MAIL_DRIVER`, SMTP host, port and TLS mode, the sender (never the credentials); the newest delivery done and the newest failure's listener, time and error, in the window | config, `event_deliveries` |

`GET /api/admin/deliveries` lists `event` (`{id, name, createdAt}`), `listener`, `to` (the payload's `email`, if any), `status` (`pending`, `retrying`, `failed`, `delivered`), `attempts`, `nextAttemptAt` (while waiting), `deliveredAt`, `failedAt`, `lastError`. A failure's `last_error` is scrubbed before it's stored (an SMTP reply's recipient is `<email>`, [observability.md](observability.md#scrubbing)), never the password.

## Email blocks

`GET /api/admin/email-blocks` (recorded as `admin.email_block.listed`, searches included) lists the block list ([auth.md](auth.md#blocking-an-email-address)): the addresses whose owners asked, from the link in an invitation, for no email. Each row is `email`, `createdAt` and `linkExpiresAt`: when the newest live block link sent to it stops working (a subquery on `email_block_links (email, expires_at)`), or null once none does, when only an admin or the operator's `admin unblock` can lift it. Never a link's token or digest. `q` matches part of the address, ignoring case, with the users list's `LIKE` escaping (`%` and `_` match themselves).

**Unblock** (`DELETE /api/admin/email-blocks {email, reason}`) is the dashboard's one action:

- **The address travels in the body, never the path**, so it stays out of access logs and browser history (blocks have no public ID). It is normalised as sign-up does (`httpapi.ValidEmail`: trimmed, lower-cased; 422 `field: "email"` for a malformed one).
- **Guards, in order:** the key and the admin session (with its CSRF token), `RequireAdminRole(operator)` (403 for a viewer), `RequireAdminStepUp` (403 `stepUp` outside the window), then `admin-action:per-user` (60 an hour): all four from `Action`. Every attempt past the step-up counts, a 404 included, so a stolen session can't act faster than the limit. (The list itself, each read recorded, is how admins see which addresses are blocked.)
- **The reason** is 10 to 500 characters once trimmed, counted as characters, not bytes; line breaks and tabs are allowed; other control characters (C0, DEL, C1), format characters (bidi overrides such as U+202E, zero-width characters, U+FEFF) and U+2028/U+2029 aren't, so a reason can't hide or rearrange what the record says (422 `field: "reason"`).
- **In one transaction:** the block is deleted, and `admin.email.unblocked` (`admin`, `email`, `reason`) and `email.unblocked` (`by: "admin"`) are recorded, both with the admin as actor and the request's IP. An address that isn't blocked (its owner may have unblocked it meanwhile) is 404 "This address isn't blocked." and records nothing.
- **There is no Block here, nor anywhere but the owner's link:** an admin block would let a stolen session stop someone signing up. An unblock can't be undone from the dashboard; only its owner can block it again, from the link in one of our emails (a still-live link from before, or the next invitation's).

## Step-up

An account-and-access action (today Unblock; the actions plan adds more) needs a code from the last 5 minutes, not just a live session:

- **`POST /api/admin/step-up {code}`** (operators only: nothing else needs it) checks an authenticator or recovery code exactly as the door's second step does: the same `useConfirmedFactor` under the authenticator's row lock, so a code's 30-second step works once across the door and the step-up, and a recovery code once. It counts against `mfa:per-account` with every other code check of the account, and records `auth.mfa.checked` (step `admin_step_up`) right or wrong. The right code sets `admin_sessions.stepped_up_until` to the database's `now()` plus 5 minutes (`auth.AdminStepUpWindow`): 204. A wrong one is 422 `field: "code"` and adds one to the session's `step_up_failures`; **the fifth ends the session** (`store.MaxStepUpFailures`, as five wrong codes end a sign-in's challenge): it is deleted, `admin.session.ended` (`reason: "step_up_failures"`) is recorded, and the answer is 401 with the cookie cleared, so the admin signs in again with the password and a code. A right code doesn't reset the count: a session gets five wrong codes in its whole life.
- **What a stolen operator cookie can still do here:** `mfa:per-account` (5 per 15 minutes) is counted with the admin session alone, where every other code check needs the password first, so one stolen session can spend the account's allowance once, keeping its owner at 429 on the app's and the door's code steps and the MFA settings for up to 15 minutes. Its fifth wrong code ends it, so it can't do so again without another stolen session. The shared limit was a confirmed decision (a stolen session guesses no faster than the door).
- **`RequireAdminStepUp`** wraps each action, after the role. It reads the window from the session `RequireAdminSession` already loaded, where `FindAdminSession` compared it with the database's clock as it does the session's expiry, so it costs no query. Outside the window: 403 `{"error": "Enter a code to continue.", "stepUp": true}`.
- **It belongs to one session:** a new admin session (or another of the same admin's) starts without it; signing out, its fifth wrong step-up code, revoking admin, a password change or reset and an MFA reset delete the session and its window with it, and a role change (`admin grant -role`) closes the windows of all the admin's sessions. A window rather than a code per action, since a code works once per 30 seconds and incident response often takes several actions in a row.
- **Under the previous key** a live session made under it may step up and act too; it still needs a fresh code.

## The Admins page

`GET /api/admin/admins` (recorded as `admin.admin.listed`) lists every admin, oldest grant first (at most 500; admins are granted one by one): `user` (`{id, name, email}`), `role`, `mfaEnabled`, `mfaSetupUntil`, `grantedAt`, `lastSignedInAt` and `sessions`.

- **`lastSignedInAt`** is their newest `admin.logged_in` (the admin is its actor), read through a partial index on `events (actor_id, created_at) WHERE name = 'admin.logged_in'`, so it doesn't walk every read the admin made. Events are pruned after 90 days by default, so an older sign-in reads as none.
- **`sessions`** are their live admin sessions (not expired or idle, made under the current or previous key), newest first: `createdAt`, `lastSeenAt`, `ip`, `userAgent`, `current` (the session asking) and `previousKey`. Never a token or CSRF token.
- **The page** (`pages/admin/[key]/admins.tsx`, `AdminAdminIndex`): each admin with badges for the role, MFA and "Setup window open", and a warning on anyone with no admin sign-in for 90 days ("consider revoking"; granted that long ago and never signed in counts too; nothing is revoked automatically); every live session ("This session", "Retiring address"); **Download CSV** (email, role, MFA, granted, last sign-in), built in the browser from the same answer so it records nothing more, cells quoted and any a spreadsheet would run as a formula started with an apostrophe; and **Sign out my other sessions** (`DELETE /api/admin/sessions`), disabled while you have none. Roles change only from the command line.

## Web app

- **Routes** (`routes.ts`): `admin/:key` (`pages/admin/[key]/index.tsx`, `AdminLogin`), and inside `pages/admin/layout.tsx`: `users` (`AdminUserIndex`), `users/:user` (`AdminUserShow`, with **Activity**: Events for `?account=…&window=all`), `workspaces` (`AdminWorkspaceIndex`), `workspaces/:workspace` (`AdminWorkspaceShow`, with **Activity** and its projects in a `PaginatedTable`, rows without links), `events` (`AdminEventIndex`), `events/:event` (`AdminEventShow`), `mail` (`AdminDeliveryIndex`), `email-blocks` (`AdminEmailBlockIndex`), `admins` (`AdminAdminIndex`), `monitoring` (`AdminMonitoringOverview`), `monitoring/security` (`AdminMonitoringSecurity`), `monitoring/limits` (`AdminMonitoringLimits`), `monitoring/ips` (`AdminMonitoringIPs`), `monitoring/ips/:ip` (`AdminMonitoringAddress`), `monitoring/accounts` (`AdminMonitoringAccounts`), `monitoring/workspaces` (`AdminMonitoringWorkspaces`), `monitoring/usage` (`AdminMonitoringUsage`) ([monitoring.md](monitoring.md#web-app)). Not prerendered.
- **The key** is read from `:key` and kept in `AdminProvider` (`lib/admin.tsx`), never in storage, SWR keys or page text. The provider also holds the session (its `role` included: `adminCan(useAdminSession().role, "operator")` hides what a viewer can't do; the API refuses it anyway) and whether the key is being retired: `adminLoader` reads `X-Admin-Key-Status` from `/api/admin/me`, and every admin call after it can turn it on (`adminHttp`'s `onPreviousKey`), so a rotation mid-visit shows too. `useAdminApi()` (`lib/api/admin.ts`) sends it in `X-Admin-Key` with the admin CSRF token; `useAdminResource(path)` loads an `adminPaths` path with SWR key `["admin", adminUserId, path]` and sends a 401 back to `/admin/<key>`. Links come from `adminRoutes.*(key)` (`lib/admin-routes.ts`).
- **Nothing reloads on its own.** Every load is a recorded read, so `useAdminResource` doesn't revalidate on focus or reconnect: only opening a page, a new query, picking Events' or a Monitoring page's time range again, or Mail's Refresh button loads again.
- **Events** (`pages/admin/[key]/events.tsx`): the heading with the window the API resolved (UTC), and on the right the **time range** (`Listbox`: Last hour, Last 24 hours, Today, Yesterday, Last 7 days, All time, Custom range…: days on a `Calendar` up to a year back, with UTC start and end times, so any chart column is reachable from the keyboard) and **Filters** with a count. The Filters dialog holds Events (a `Combobox` of the catalogue's groups and names: a group picks all its names, a partly picked one shows a dash), Outcome (Failed checks only), Account, Workspace, Email and IP, applied together with **Apply filters**, which waits for the catalogue (Clear all and Cancel apply nothing). Applied filters show as removable badges under the heading (accounts and workspaces by name, from `refs`), with Clear all from two; removing one moves focus to the next badge, or the Filters button. Then the summary: four tiles (Events, Failed checks with the share of failed sign-ins, Limits reached, Busiest), Events over time (stacked by group or name; clicking a column zooms to its bucket), Failed checks over time and Top events (`BarList`, each linking to the list filtered to that name), or a line saying why there are none (over 7 days, too slow or busy with Retry (on an older page, Retry opens the newest), or "Charts show with the newest events" on an older page). Then the table (`components/app/admin/events.tsx`: `EventTable`), times to the second, a rose **Failed** badge, each row opening in place to its payload (nothing more loaded or recorded), every account, workspace and IP linking to its page or filter (an IP keeps the current filters). Everything lives in the URL (`components/app/admin/event-filters.ts`: names written canonically, groups first), so back and forward walk through filter changes. **Older / Newer** (`CursorNav`) follow the cursor, the summary kept from the newest page. An event's page shows the payload as indented JSON in a `<pre>`, through React's escaping, and **More like this**: the same name, IP and account over the last 7 days. A user's and a workspace's **Activity** and Email blocks' **Show activity** open `window=all`; Mail's frequent addresses pass Mail's window.
- **Mail** (`pages/admin/[key]/mail.tsx`): the window and Refresh in the header, `Stat` tiles for the queue, emails per hour as a stacked `BarChart` (`components/ui/chart.tsx`, Recharts; its numbers in a table for screen readers), asked for vs sent, frequent addresses (each linking to Events `?email=`), the queue per listener, how mail is sent, then the deliveries with status tabs (Failed first while the window has any).
- **Loaders** (`lib/admin.tsx`, so they stay out of the app build): `adminLoginLoader` (404 → not-found, signed in → Users) and `adminLoader` (404 → not-found, 401 → sign-in).
- **Email blocks** (`pages/admin/[key]/email-blocks.tsx`): what blocking means, a search, and the list (Address, Blocked, the owner's link: "Works until …" or a zinc Expired), each row with a ⋯ menu: **Show activity** (Events `?email=`, where `email.blocked` and `email.unblocked` show who blocked, from where, and every unblock's reason) and, for operators only (`adminCan(role, "operator")`; the API refuses a viewer anyway), **Unblock** (`danger`). Unblock opens `AdminActionDialog` (`components/app/admin/admin-action-dialog.tsx`): "Unblock ada@example.com?", what it does, a required Reason (counted, 10 to 500 characters), then, when the API answers `stepUp`, a code (authenticator, or "Lost your device? Use a recovery code"), which steps up and retries the action. A failure stays in the dialog (a wrong code empties the code; a 404 also reloads the list behind it; a 401, the session ended, goes back to the sign-in); success toasts "ada@example.com unblocked." and reloads the list (one more recorded read).
- **Chrome:** `AdminLayout` (`components/layout/admin-layout.tsx`), the `SidebarLayout` with Users, Workspaces, Events, Mail, Email blocks and Admins, then a **Monitoring** heading with Overview, Security, Rate limits, IP addresses, Accounts, Workspaces and Usage, a warning `Notice` above every page while the key is being retired, the app name with a red "Admin" badge (under the name in the sidebar, beside it in the phone top bar), and an account menu with Back to app (`/app`), Account & security (`/app/settings`) and Sign out. The app's pages are the other build, so those two load as a new page (`pages/admin/not-found.tsx`).
- **Lists** use `PaginatedTable`, `SearchField` and sortable headers (`components/app/admin/admin-list.tsx`: URL state, `AdminTableHead`, `MfaBadge`, `EmailBadge` (zinc "Not verified"; a user's page also shows lime "Verified", the list only the unverified), `AdminBadge`); rows link to their page. Only Email blocks has a ⋯ menu per row, as its rows have actions and no page.
- `react/no-danger` is on for the whole web app: other users' names and emails render only through React's escaping.

## Not built

Monitoring pages beyond Overview, Security, Rate limits, IP addresses, Accounts, Workspaces and Usage ([monitoring.md](monitoring.md)), alerts, write actions beyond Unblock (retrying a delivery among them, and signing out another admin; they will reuse the step-up, reasons, `admin-action:per-user` and `AdminActionDialog`) are planned in [plans/features/admin/](../../plans/features/admin/README.md). The Configuration page's warnings (an allowlist without `CLIENT_IP_HEADER`, sessions still on the previous key) come with monitoring, and an alert on `admin.access_refused` with alerts. Email bodies are never stored, so the dashboard can't show them. An event's `request_id` (which finds its request's log lines, [observability.md](observability.md#request-ids)) is stored but not shown on its page yet. Not planned: impersonation, a separate admin origin, passkeys for admins.
