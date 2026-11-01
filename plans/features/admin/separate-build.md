# Admin: a separate build, served only behind the key

Status: **draft 2026-10-31**, decisions to confirm. Plan A8 of the [admin plans](README.md). Builds on [admin access](../implemented/admin-access.md) and the [single-page app](../implemented/spa.md). Docs it will change: [admin.md](../../../docs/api/admin.md), [api/README.md](../../../docs/api/README.md) (serving the web app), [deployment.md](../../../docs/deployment.md), [testing.md](../../../docs/testing.md), [ui/README.md](../../../docs/ui/README.md), [ui/pages.md](../../../docs/ui/pages.md), [ui/testing.md](../../../docs/ui/testing.md).

## Why

The admin pages ship inside the public app bundle. Every page, the home page included, loads the React Router manifest (`assets/manifest-<hash>.js`), which lists `pages/admin/layout`, `pages/admin/[key]/users`, `…/workspaces/[workspace]`, `…/events`, `…/mail` and the URL of each one's chunk. The shared `api` chunk holds every `/api/admin/*` path and the `X-Admin-Key` header. Anyone can download and read the admin UI without the key.

That isn't a hole: the API checks the key, an admin account, the password and a code, and a separate session, and the code reveals none of them. But it hands out a map. It shows that an admin area exists, every admin endpoint and its parameters, what each response holds, and from [events-and-mail](../implemented/admin/events-and-mail.md) and [monitoring](monitoring.md) on, which signals we watch: rate-limit thresholds, alert rules and the abuse checks that someone probing us would most like to read. Today's [admin.md](../../../docs/api/admin.md) accepts this ("The bundle reveals that admin pages exist"). This plan stops it, before the monitoring pages make the map more useful.

**Server rendering isn't the fix.** The home page is prerendered at build time and still ships its JavaScript for hydration. Rendering admin pages on each request would need a Node renderer next to the Go binary and would break "there is no server rendering" (AGENTS.md). What hides code is not sending it, so the admin pages become their own build, which the server hands out only to someone who already holds the key.

## The design

```
make build
  react-router build                 → build/client   → internal/web/dist   (public, as today; no admin routes)
  WEB_BUILD=admin react-router build → build/admin    → internal/web/admin  (embedded separately, never served from dist)

GET /admin/<key>[/<page…>]   right key (+ session for inner pages, as today)
                             → the admin build's index.html, 200
                             + Set-Cookie: the assets pass (Path=/admin-assets/, 8 h)
GET /admin-assets/<file>     valid assets pass, file exists → the file, Cache-Control: private
                             anything else                  → exactly what GET /nowhere gets (the fallback page, 404)
```

### Two builds from one web app (decision 1)

The web app stays one package (`apps/web`) with one `routes.ts`, one `root.tsx` and one set of dependencies. It is built twice:

| | App build | Admin build |
| --- | --- | --- |
| Command | `react-router build` | `WEB_BUILD=admin react-router build` |
| Routes | everything except admin, plus `*` → not-found | `admin/:key`, the admin layout and its pages, plus `*` → not-found |
| Vite `base` | `/` | `/admin-assets/` |
| React Router `basename` | `/` | `/` (pages stay at `/admin/<key>/…`) |
| Prerender | `["/"]` | none (SPA mode: `index.html` only) |
| Output | `build/client` → `internal/web/dist` | `build/admin` → `internal/web/admin` |

- **`routes.ts` keeps both lists**, as `appRoutes` and `adminRoutes`, and exports the one `WEB_BUILD` asks for. Every page is still registered in `routes.ts` (AGENTS.md).
- **`react-router.config.ts` and `vite.config.ts` read `WEB_BUILD`** (`app` by default) for `buildDirectory`, `prerender` and `base`. An unknown value fails the build.
- **Typegen runs for both route lists**, so admin pages keep `./+types/…` and `clientLoader`, and `npm run typecheck` covers both.
- **Pages, components and tests stay where they are** (`pages/admin/*`, `components/app/admin/*`, `components/layout/admin-layout.tsx`).

### Nothing admin left in the app build

Removing the routes isn't enough: shared modules pull admin code in today. They move:

| Today | Moves to | Why |
| --- | --- | --- |
| `lib/api/index.ts` re-exports `./admin` and holds `useAdminApi` | `lib/api/admin.ts` only; admin pages import from there | `index.ts` is in every page's chunk |
| `adminHttp` (the `X-Admin-Key` header) in `lib/api/http.ts` | `lib/api/admin.ts` | Same |
| `routes.admin.*` and `adminNavigation` in `lib/routes.ts` (with `types/admin` imports) | `lib/admin-routes.ts` (`adminRoutes.users.index(key)`, …) | `lib/routes.ts` is imported everywhere |
| `adminLoginLoader`, `adminLoader`, `adminProbe` in `lib/loaders.ts` | `lib/admin.tsx`, beside `AdminProvider` | Same |

Two guards keep it that way:

1. **ESLint** `no-restricted-imports`: only files under `pages/admin/`, `components/app/admin/`, `components/layout/admin-layout.tsx` and the admin modules themselves may import `@/lib/admin*`, `@/lib/api/admin`, `@/types/admin` or `@/components/app/admin/*`.
2. **The build checks its output.** `scripts/build-web.sh` fails if any file in the app build contains `/api/admin`, `X-Admin-Key`, `pages/admin` or `admin-assets` (decision 5). ESLint stops the import that caused it; this catches anything that slips past, such as a string pasted somewhere.

### The assets pass (decision 2)

The admin page's scripts and chunks need the key's protection too. Without it anyone could fetch `/admin-assets/manifest-<hash>.js` and every file it names. The key can't be in their URLs, because Vite fixes the asset base at build time and the key is a runtime secret. So the page gate hands out a pass:

| | |
| --- | --- |
| Cookie | `__Secure-lewcontext_admin_assets` in production (`lewcontext_admin_assets` in development); **Path=/admin-assets/**, HttpOnly, SameSite=Strict, Secure in production, Max-Age 8 hours |
| Value | `<expiry unix seconds>.<base64url HMAC-SHA256(k, "admin-assets" ‖ adminKeyHash ‖ expiry)>`, with `k` derived from `APP_KEY` for this purpose only (not the MFA sealing key itself) |
| Set | on every 200 admin page (`/admin/<key>` and each inner page), renewing it. Never on a 404 or a redirect |
| Checked | by `GET /admin-assets/{file...}` (`adminAssets` in `internal/httpapi/admin.go`), in constant time; the expiry must be in the future and at most 8 hours ahead |
| Invalid when | it expires, the key is rotated (the HMAC covers the key's digest), or `APP_KEY` changes |

- **No database.** It's stateless, so several servers accept each other's passes, and it costs no query.
- **It opens files, nothing else.** The API never reads it. A pass with no admin session gets the sign-in page's code, which is what anyone with the key gets anyway.
- **A missing or bad pass, or an unknown file, gets the ordinary 404**, the same bytes and headers as `GET /nowhere`, so `/admin-assets/` looks like any other missing path. The answer doesn't depend on whether the file exists.
- **Caching:** `Cache-Control: private, max-age=31536000, immutable`. **Never `public`**: Cloudflare or another shared cache would keep the file and serve it to anyone without a pass. The names are content-hashed, so a browser keeps them across deploys as the app's assets do.
- **A pass that ends mid-visit:** the next lazily loaded page chunk fails, and React Router reloads the page. The reload goes through the page gate, which renews the pass, or sends a visitor without an admin session to sign in. Eight hours matches the admin session's maximum, so this rarely happens.

### Serving: `internal/web`

- `internal/web/admin/` is a second embedded directory (`//go:embed all:admin`, with a committed `.gitkeep`), never part of `dist`. `serveFile` can't reach it, and `/admin-assets/…` on the public side is the fallback 404.
- `Pages` gains the admin shell (`admin/index.html`, its CSP hashes computed at startup like every other HTML file) and `ServeAdminFile(w, r, name)`. `ServeAdmin` serves the admin shell instead of the app's fallback page, with today's headers (`no-referrer`, `noindex`, `no-store`).
- **Missing admin build:** with `ADMIN_ROUTE_KEY` set, startup fails with `ErrAdminNotBuilt`, as a missing app build fails today. Unset, admin is off and nothing is loaded.
- `requireAdminKey`, `requireAdminSession` and the API don't change. The admin pages' `GET /api/admin/me` probe stays: it still decides between sign-in and Users.

### Development

`make dev` runs a second Vite dev server for the admin build (`WEB_BUILD=admin`, on `127.0.0.1:3002`, not exposed). The main one on `:3000` proxies `/admin` and `/admin-assets` to it, websockets included for hot reload, so the browser still sees one origin and `APP_ORIGIN` stays `http://localhost:3000`. The development server doesn't check the key or the pass: the code is on the developer's disk anyway, and the API still checks the key. `make serve` and the e2e server run the real gate.

**Spike first.** Before the rest, confirm on React Router 7.18 that:

1. a framework-mode SPA builds with Vite `base: "/admin-assets/"` and `basename: "/"`, and the dev server serves `/admin/<key>` documents with that pair;
2. typegen for both route lists can share `.react-router/types` without one run deleting the other's types;
3. a failed lazy route import reloads the page.

If (1) fails, the fallback is to keep `base` and `basename` both `/` and point the admin build's `assetsDir` at `admin-assets` (files at `/admin-assets/<file>` either way). If (2) fails, typegen writes into one directory per build and `tsconfig.json` lists both in `rootDirs`. The plan's records are written up as the spike finds them, before the build changes.

## API and data changes

- **No database changes, no new endpoint under `/api`.** One new page route, `GET /admin-assets/{file...}` in `routes.go`, beside the admin page routes.
- **No new environment variable.** The pass derives from `APP_KEY` and the admin key.
- **The image and `make build`** copy two builds; `Dockerfile` gains the second `COPY`. `scripts/build-web.sh` builds both and runs the output check.
- **Event:** none. Fetching code isn't a read of anyone's data, and recording every asset request would flood the trail.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| Admin code reachable without the key | Not in the app build (ESLint, output check); the admin build is served only with a pass, which only the page gate gives out, and only for the right key |
| A shared cache (Cloudflare, a proxy) serving admin files to anyone | `Cache-Control: private` on every admin file; a Go test fails on anything else; [deployment.md](../../../docs/deployment.md) says not to add a "cache everything" rule on `/admin-assets/` |
| The pass outliving the key | The HMAC covers the key's digest, so rotating the key ends every pass |
| A stolen pass | It opens only code, for 8 hours at most; it gives no API access and doesn't reveal the key |
| A script injected into an app page fetching `/admin-assets/…` with the cookie (same origin, the path matches) | It can read admin code in a browser that visited admin in the last 8 hours. That is today's exposure, narrowed to admins' own browsers, and it still learns no key and gets no API access. The app's CSP and `react/no-danger` are the defence against the injection itself |
| Probing to learn which admin files exist | A missing or bad pass gets the same 404 whether the file exists or not |
| The key leaking through asset URLs (devtools, HAR files, error reports' stack traces) | The key is never in an asset URL; files live at a fixed `/admin-assets/` |
| Admin code drifting back into the app build | The output check fails `make build`, the e2e suite and the Docker image build |
| Admin turned on without its build | Startup refuses to run |

## Tests

**Go unit (`internal/web`):** the admin shell's CSP hashes; `ServeAdminFile` sends `private` caching and never serves HTML or dot-files; `New` fails without the admin build when admin is on.

**Go integration (`internal/httpapi`):**
- `GET /admin/<key>` sets the pass; a wrong key, a redirect and a 404 don't. An inner page with a session renews it.
- `/admin-assets/<file>` with a valid pass gets the file and `Cache-Control: private, …`. With no pass, an expired one, one made under the previous key, a tampered one, or an unknown file, the response is byte-identical to `GET /nowhere`'s (status, headers, body).
- The admin page serves the admin shell, not the app's fallback.

**Build check (`scripts/build-web.sh`):** no app build file contains `/api/admin`, `X-Admin-Key`, `pages/admin` or `admin-assets`.

**Playwright:**
- The home page's manifest and every script it loads name no admin route.
- Signing in to admin and opening Users, a user, Workspaces, Events and Mail still works, through the real gate and the pass.
- `/admin-assets/<a real file from the admin build>` fetched from a fresh context (no pass) is the not-found page.

**Vitest:** the moved modules' existing tests move with them; nothing new is needed.

## Decisions (to confirm)

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | How to make the second build | One package, one `routes.ts`, built twice with `WEB_BUILD=admin`. Rejected: a second package (`apps/admin`), which duplicates dependencies and the lockfile; and a hand-written Vite SPA in React Router's data mode, which drops `clientLoader` and `+types` for the admin pages only |
| 2 | How to protect the admin files | A stateless HMAC "assets pass" cookie, scoped to `/admin-assets/`, set by the page gate. Rejected: the key in asset URLs (`/admin/<key>/assets/…`), which needs a relative Vite base plus rewriting the HTML at serve time, and puts the key in every chunk URL, stack trace and HAR file |
| 3 | Pass lifetime | 8 hours, the admin session's maximum, renewed on every admin page load |
| 4 | Admin turned on without an admin build | Refuse to start, as for a missing app build |
| 5 | Guarding the app build | Both ESLint import rules and a string check on the build output, which fails the build |
| 6 | Development | A second Vite dev server, proxied by the main one under `/admin` and `/admin-assets`, with no key or pass check (the API still checks the key) |

## Not included

- **Server rendering or prerendering admin pages.** Neither hides code, and request-time rendering breaks the no-server-rendering rule.
- **Obfuscating or minifying further.** The admin build is minified as the app is; obfuscation slows a reader but hides nothing.
- **A separate admin origin or subdomain** (as in [access-hardening](access-hardening.md)): it would isolate cookies and scripts more strongly, at the cost of a second hostname, certificate and CORS.
- **Splitting the sign-in page's code from the dashboard's**, so only signed-in admins could fetch the dashboard pages. Anyone holding the key can already reach the sign-in, and the key alone gets no data.
- **Gating the API's shape further.** The API is protected by the key, the admin account, MFA and the session, as today.
