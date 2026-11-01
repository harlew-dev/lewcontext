# Admin: a separate build, served only behind the key

Status: **implemented 2026-11-01** (decisions confirmed as written). Plan A8 of the [admin plans](../../admin/README.md). Builds on [admin access](../admin-access.md) and the [single-page app](../spa.md). Docs updated: [admin.md](../../../../docs/api/admin.md) (The admin build, The assets pass), [api/README.md](../../../../docs/api/README.md) (serving the web app, `WEB_BUILD`), [auth.md](../../../../docs/api/auth.md) (transport and headers), [api/testing.md](../../../../docs/api/testing.md), [deployment.md](../../../../docs/deployment.md), [testing.md](../../../../docs/testing.md), [naming.md](../../../../docs/naming.md), [ui/README.md](../../../../docs/ui/README.md), [ui/pages.md](../../../../docs/ui/pages.md), [ui/api.md](../../../../docs/ui/api.md), [ui/testing.md](../../../../docs/ui/testing.md), [ui/strict-mode.md](../../../../docs/ui/strict-mode.md), and the root README.

## How the build differs from the plan

- **Routes in development and typegen** ([spike](#development), 2): `routes.ts` keeps `appRoutes` and `adminRoutes`, but only `react-router build` takes one list. `react-router typegen` and `react-router dev` take both (typegen deletes `.react-router/types` on every run and registers one global set of route types, and each dev server runs it), so in development each dev server knows the other's pages. The proxy still sends `/admin` and `/admin-assets` to the admin dev server; the split is checked on real builds (the output check, the e2e suite).
- **The admin dev server needs a plugin** ([spike](#development), 1): Vite's dev server refuses every URL outside `base`, so `adminPagesUnderBase` (`vite.config.ts`, admin dev server only) puts `/admin-assets` in front of `/admin…` page URLs. Its hot-reload socket goes through `:3000` (`hmr.clientPort`).
- **Paths:** the admin build writes `build/admin/client` (`buildDirectory: "build/admin"`), and its files are at `/admin-assets/assets/<file>` (Vite's `assetsDir` stays `assets`). The route takes any path under `/admin-assets/`.
- **Links from the admin build to the app** (Back to app, Account & security, the logo, the not-found page's button) would have landed on the admin build's not-found route. Its catch-all is its own, `pages/admin/not-found.tsx`: a URL outside `/admin/` loads as a new page (`redirectDocument`), which Go answers from the app build; Go never serves the admin build there, so it can't loop. The app build keeps `pages/not-found.tsx`.
- **Names:** the admin API's typed paths are `adminPaths` (`lib/api/admin.ts`, was `paths.admin`), so they don't clash with the page URLs' `adminRoutes` (`lib/admin-routes.ts`). `lib/use-admin-resource.ts` counts as an admin module in the ESLint rule. `adminHttp` is built on `httpWith` (`lib/api/http.ts`, exported for it).
- **The guards, after the security review:**
  - **The main guard is a third one, in the build itself:** `no-admin-modules` (`vite.config.ts`, app build only) fails the build if Rollup bundled any admin module (`isAdminModule`, `web-build.ts`). ESLint and a string check can both be talked around (a dynamic `import()`, `@/lib/../lib/api/admin`, `./api/admin.js`, `/src/lib/api/admin`; admin modules such as `lib/admin-routes.ts` whose output holds none of the four strings); module ids can't.
  - **ESLint** matches any import path naming an admin module, and `no-restricted-syntax` covers `import()` (and refuses `import()` of a template literal).
  - **The output check** is its own script, `scripts/check-app-build.sh` (POSIX `sh`, for the image's Alpine stage), run by `scripts/build-web.sh` before anything is copied, and by the `Dockerfile`. It also looks for `/admin/` (the admin pages' URLs) and ignores case. `make check` runs its self-test, `scripts/check-app-build.test.sh`.
- **The pass:** its primitives are in `internal/auth/admin_assets.go` (`Keys.AdminAssetsPass`, `AdminAssetsPassValid`, HKDF label `lewcontext admin-assets-pass v1`); `Server.AdminAssets` and `issueAdminAssetsPass` in `admin.go`. It is **always signed with the current key's digest**, whichever key served the page, so [access-hardening](access-hardening.md)'s `ADMIN_ROUTE_KEY_PREVIOUS` needs no change to it. The expiry may be up to 8 hours **and a minute** ahead, for another server's clock running ahead. Only the canonical form is accepted (a positive expiry, no leading zero or sign, strict base64url). Any of the first 4 cookies named as the pass may be it, so a same-named cookie planted by a sibling subdomain and sent first can't lock an admin out. The pass is set only when the admin shell is served (`Pages.AdminBuilt`), never on the 404 a server without an admin build gives.
- **`GET /admin-assets`** (no slash) is registered as an unknown page: without it, the mux redirects it to `/admin-assets/` (307), which shows the route exists.
- **Admin files** are cached `private, max-age=28800, immutable`, the pass's lifetime, not a year: a browser keeps admin code no longer than the pass that opened it. They also send `Referrer-Policy: no-referrer` and `X-Robots-Tag: noindex, nofollow`, like admin pages.
- **In development** anyone who can reach the app's dev server (`0.0.0.0:3000`) can load the admin code through its proxy, never the data; before this plan the same code was in the app's dev bundle.
- **A missing admin build** refuses to start wherever the app build is embedded and `ADMIN_ROUTE_KEY` is set (development too, so a stale build isn't served half). Without any web build, development still serves the API only, as before.
- **Development commands:** `npm run dev:admin` and `make web-admin` start the admin dev server on its own; `scripts/dev.sh` runs it as `[admin]`.
- `tsconfig.json` allows `.ts` extensions in imports, so the configs import `./web-build.ts` as Vite's coming native config loader wants.

## Why

The admin pages ship inside the public app bundle. Every page, the home page included, loads the React Router manifest (`assets/manifest-<hash>.js`), which lists `pages/admin/layout`, `pages/admin/[key]/users`, `…/workspaces/[workspace]`, `…/events`, `…/mail` and the URL of each one's chunk. The shared `api` chunk holds every `/api/admin/*` path and the `X-Admin-Key` header. Anyone can download and read the admin UI without the key.

That isn't a hole: the API checks the key, an admin account, the password and a code, and a separate session, and the code reveals none of them. But it hands out a map. It shows that an admin area exists, every admin endpoint and its parameters, what each response holds, and from [events-and-mail](events-and-mail.md) and [monitoring](monitoring.md) on, which signals we watch: rate-limit thresholds, alert rules and the abuse checks that someone probing us would most like to read. Today's [admin.md](../../../../docs/api/admin.md) accepts this ("The bundle reveals that admin pages exist"). This plan stops it, before the monitoring pages make the map more useful.

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

**What the spike found (2026-11-01, React Router 7.18.5, Vite 8.3.5):**

1. **Builds: yes. The dev server: not as it stands, fixed with a few lines.** `WEB_BUILD=admin react-router build` with `base: "/admin-assets/"` and `basename: "/"` writes `build/admin/client/index.html` (SPA mode, `isSpaMode: true`, `basename: "/"`) whose scripts, preloads, the manifest and the CSS's fonts all load from `/admin-assets/assets/<file>` (Vite's `assetsDir` stays `assets`, so the files sit one folder down from the plan's `/admin-assets/<file>`; the route takes any path under `/admin-assets/`). React Router's own check (`basename` must start with `base`) applies only when `basename` isn't `/`. But Vite's dev server answers **every URL outside `base`** with a 404 ("The server is configured with a public base URL of /admin-assets/ …"), so the admin dev server refused `/admin/<key>`. The plan's fallback doesn't help there: in development Vite serves modules at `/src/…`, `/@vite/client` and `/node_modules/.vite/…` whatever `assetsDir` says, and with `base: "/"` those collide with the app's dev server behind the one proxy. **The fix:** a small plugin, only on the admin dev server (`adminPagesUnderBase` in `vite.config.ts`), puts `/admin-assets` in front of `/admin…` page URLs; Vite's base check takes it off again and React Router renders the page for the original URL (`req.originalUrl`). Checked: `/admin/<key>/users` → 200 with `import("/admin-assets/src/entry.client.tsx")`.
2. **No.** Typegen always deletes the whole `.react-router/types` first (`run` and `watch` both `rm` it), the directory is fixed (`typesDirectory` is `<root>/.react-router/types`, not configurable), and each run writes one `+routes.ts` that registers **one** global `Register { pages, routeFiles, routeModules }` for the TypeScript program. So the plan's fallback fails too: two directories in `rootDirs` would declare `Register`'s properties twice, with different types. And every `react-router dev` runs its own typegen watcher, so two dev servers with different route lists would keep replacing each other's types. **Instead:** typegen and the dev servers register **every** page (the two lists together); only `react-router build` takes one list (`routes.ts` reads the command: `build`, or anything it doesn't know, gets `WEB_BUILD`'s list). So `npm run typecheck` covers both builds in one program, and both dev servers write the same types. In development, then, each dev server knows the other's routes; the proxy still sends `/admin` and `/admin-assets` to the admin one, and the build output check and the e2e suite run on real builds.
3. **Yes.** `loadRouteModule` (`react-router/dist/production`) catches a failed `import()` of a route module, logs "Error loading route module …, reloading page..." and calls `window.location.reload()` (it rethrows only in SPA mode under Vite's dev server). The e2e suite checks it end to end: a pass deleted mid-visit, then a page whose chunk wasn't loaded yet.

## API and data changes

- **No database changes, no new endpoint under `/api`.** One new page route, `GET /admin-assets/{file...}` in `routes.go`, beside the admin page routes.
- **No new environment variable.** The pass derives from `APP_KEY` and the admin key.
- **The image and `make build`** copy two builds; `Dockerfile` gains the second `COPY`. `scripts/build-web.sh` builds both and runs the output check.
- **Event:** none. Fetching code isn't a read of anyone's data, and recording every asset request would flood the trail.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| Admin code reachable without the key | Not in the app build (ESLint, output check); the admin build is served only with a pass, which only the page gate gives out, and only for the right key |
| A shared cache (Cloudflare, a proxy) serving admin files to anyone | `Cache-Control: private` on every admin file; a Go test fails on anything else; [deployment.md](../../../../docs/deployment.md) says not to add a "cache everything" rule on `/admin-assets/` |
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
