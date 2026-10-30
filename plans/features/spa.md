# Single-page app served by Go (replacing Next.js)

Status: **planned, on hold (2026-10-30). All questions are answered (decisions 1–10).** Not scheduled yet. Starts after [workspaces-and-public-ids.md](workspaces-and-public-ids.md) has landed, because it moves every app route. Don't write code until you're told to start.

## Goal

1. **One process in production.** The Go server serves the API and the web app from the same origin. Node isn't needed at runtime.
2. **Public pages get SEO.** The homepage (and later pages such as pricing) are prerendered to full HTML at build time, with their title and meta tags in the page source.
3. **Signed-in pages are a single-page app with instant page swaps.** Navigating under `/app` renders the next page immediately and shows skeletons while its data loads.

## Decisions

| # | Question | Decision |
| --- | --- | --- |
| 1 | Replace Next.js now? | **Not yet.** Plan only |
| 2 | Where Go gets the built files | **Embedded** in the Go binary with `//go:embed` ([why](#2-embedding-the-built-files)) |
| 3 | CSP for prerendered HTML | **Hashes** of each file's inline scripts, computed at startup. The AGENTS.md nonce invariant is reworded ([nonces vs hashes](#3-csp-nonces-vs-hashes)) |
| 4 | Signed-out `/app/*` | **Go redirects** to `/auth/login?next=…` when the request has no valid session, before serving any HTML |
| 5 | `/` | **A placeholder homepage**, prerendered |
| 6 | BaseUI on prerendered pages | **Collect Styletron CSS at build time** in a custom `entry.server.tsx` ([how](#6-collecting-baseui-css-at-build-time)) |
| 7 | Homepage content | **Existing components only:** a title, a description, and a "Log in" or "Dashboard" button |
| 8 | Route module folder | **`src/pages/`**, registered in an explicit `src/routes.ts` |
| 9 | Page data | **Instant page swaps.** No page loaders: pages render at once and fetch through SWR (`useResource`), with skeletons while loading |
| 10 | Playwright target | **`make serve`** (Go serving the production build) |

## Why

Next.js server-renders every request today. That buys:

- no loading state on the first visit;
- the session check before rendering;
- real 404/503 status codes.

Every page except login and register is behind a session, so it isn't buying SEO. The cost is a second runtime and the code that keeps a multi-user render server safe:

| Next.js job | Where today | After |
| --- | --- | --- |
| Session check, workspace loading, login/onboarding redirects, 404/503 | `lib/server.ts` (`appProps`, `guestProps`, `workspaceHomeProps`, `onboardingProps`) | Go redirects signed-out `/app/*` requests. The app layout's `clientLoader` loads the session once; pages handle their own 404 and errors |
| First page of data | `getServerSideProps` + `initial()` | Deleted. Pages fetch through `useResource(path)` and show skeletons |
| Forwards only the request cookie to the private API URL (AGENTS rule 7) | `serverHttp`, `serverApi()` | Deleted: the server makes no authenticated fetches |
| Styletron CSS per request | `_document.tsx`, `lib/styletron.ts` | Collected at build time for prerendered HTML; the browser client takes over after load |
| Per-request CSP nonce, HSTS | `proxy.ts` | Go ([CSP](#3-csp-nonces-vs-hashes)) |
| Proxies `/api/*` to Go | `next.config.mjs` rewrites | Gone in production (same process). Vite's dev server proxies in development |
| `/` → `/app` redirect | `next.config.mjs` | `/` becomes the placeholder homepage |
| Layout stays mounted between pages | `getLayout` + `_app` | A layout route with `<Outlet/>` |
| Route progress, `useRouter`, `Link`, `Head`, 404 page | `next/router` (11 files), `next/link`, `next/head`, `pages/404.tsx` | `useNavigation()`, `useNavigate`/`useParams`/`useLocation`, React Router `Link`, `meta` exports, a catch-all route |

## Stack

React Router v7 in **framework mode** with `ssr: false` and a `prerender` list, built by Vite ([docs](https://reactrouter.com/how-to/pre-rendering)). Framework mode is needed for prerendering, and it also generates types for route params. React 18, TypeScript, Tailwind v4 (`@tailwindcss/vite`), BaseUI, SWR, `motion`, Heroicons and Vitest stay. `next` and `eslint-config-next` go.

```ts
// apps/web/react-router.config.ts
export default {
  appDirectory: "src",
  ssr: false,
  prerender: ["/"],
} satisfies Config;
```

`react-router build` writes `build/client/`:

- `index.html`: the prerendered homepage.
- `__spa-fallback.html`: renders only the root route; it serves every other path and loads the right page in the browser.
- `assets/*`: hashed JS and CSS.

## Request flow (production)

```
GET /api/...                   → API handlers (unchanged); unknown paths → JSON 404
GET /assets/*, /favicon.svg    → embedded file, Cache-Control: public, max-age=31536000, immutable
GET /  (and other prerendered) → prerendered HTML, Cache-Control: no-cache
GET /app/*   no valid session  → 302 /auth/login?next=<path>
GET /app/*, /auth/*            → __spa-fallback.html, Cache-Control: no-store
anything else                  → __spa-fallback.html with status 404 (the app renders the not-found page)
```

- Go's patterns in `internal/httpapi/routes.go` stay the registry. `/api/` keeps the existing `security` middleware: no-store, Origin/CSRF/content type for mutations, 10 s timeout. Pages get their own header middleware, because assets must be cacheable.
- **The `/app/*` gate (decision 4)** uses the same session lookup as `requireSession`:
  - no cookie, an unknown session, or an expired one → 302 to login with `next`;
  - signed-out visitors never download the app shell.

  `next` is only a hint; the client still validates it with `returnPath()`.
- Everything else stays client-side, so Go doesn't duplicate those rules:
  - the `/app` → newest-workspace redirect;
  - onboarding when the account has no workspace;
  - a workspace that isn't the member's (404);
  - signed-in visitors on login/register.

## Front-end structure

```
apps/web/
  react-router.config.ts  ssr: false, prerender
  vite.config.ts          reactRouter(), tailwind, dev proxy /api → 127.0.0.1:8080
  src/
    root.tsx              <html>, providers (Styletron, BaseProvider, SWR, Motion), <Scripts>, ErrorBoundary
    entry.server.tsx      build-time render: collects Styletron CSS into prerendered HTML
    entry.client.tsx      hydrates with the Styletron client engine
    routes.ts             the page registry (replaces file-system routing)
    pages/
      home.tsx            "/" (prerendered, public, meta tags)
      auth/login.tsx, auth/register.tsx
      app/layout.tsx      clientLoader: session + first page of workspaces; renders ApplicationLayout + <Outlet/>
      app/index.tsx       redirects to the newest workspace or onboarding
      app/onboarding.tsx, app/settings.tsx
      app/workspace/dashboard.tsx, projects.tsx, project.tsx, settings.tsx
      not-found.tsx       catch-all "*"
    components/, lib/, types/, styles/   unchanged layout
```

```ts
// src/routes.ts
export default [
  index("pages/home.tsx"),
  route("auth/login", "pages/auth/login.tsx"),
  route("auth/register", "pages/auth/register.tsx"),
  route("app", "pages/app/layout.tsx", [
    index("pages/app/index.tsx"),
    route("onboarding", "pages/app/onboarding.tsx"),
    route("settings", "pages/app/settings.tsx"),
    route(":workspace/dashboard", "pages/app/workspace/dashboard.tsx"),
    route(":workspace/projects", "pages/app/workspace/projects.tsx"),
    route(":workspace/projects/:project", "pages/app/workspace/project.tsx"),
    route(":workspace/settings", "pages/app/workspace/settings.tsx"),
  ]),
  route("*", "pages/not-found.tsx"),
] satisfies RouteConfig;
```

Onboarding renders in the auth layout, so it may sit outside the app layout route. That's settled when it's built. AGENTS rules 3 and 4 keep saying `src/pages/...`; the only addition is that a new page is also listed in `src/routes.ts`.

### Placeholder homepage (decisions 5 and 7)

`src/pages/home.tsx`, prerendered, built only from existing components:

- **Layout:** `AuthLayout` (the centred signed-out layout) with `Logo`, `Heading` ("Forma"), and `Text` with the description from today's `<meta name="description">`.
- **Meta:** a `meta` export sets `<title>` and the meta description, so they're in the raw HTML.
- **Button:** the prerendered HTML shows `<Button href={routes.login}>Log in</Button>`, because it was built without a session. After load, the page asks `GET /api/auth/me`. If signed in, the button becomes `<Button href={routes.app}>Dashboard</Button>`.
  - Signed-in visitors see "Log in" briefly before it switches.
  - If the API can't be reached, "Log in" stays, which still works once it's back.

Real homepage content is its own plan later.

### Loading data (decision 9)

- **App layout `clientLoader`** runs once, on the first load of an `/app` page:
  - It loads `me` and the first page of workspaces in parallel.
  - With no workspaces it redirects to onboarding. A 401 means the session expired after the Go gate, so it redirects to login. A network failure renders `ServiceUnavailable`.
  - It feeds `SessionProvider` and `WorkspaceProvider`, so `useSession()` and `useWorkspace()` don't change.
  - While it runs, `HydrateFallback` shows the layout skeleton.
  - `shouldRevalidate` returns `false`, so moving between `/app` pages never waits on it. Logout and workspace creation update the SWR cache as they do today.
- **Pages have no loaders.** They render on the first frame and fetch through `useResource(path)`:
  - `ListSkeleton` for lists;
  - skeletons for records and dashboard stats;
  - `Notice` with Retry on failure;
  - 401 → login, which `useResource` already does.

  Revisiting a page shows SWR's cached data at once and refreshes it in the background.
- **The current workspace** comes from `useResource(paths.workspaces.show(id))` keyed by the `:workspace` param, so switching workspace is instant too. The sidebar shows the name from the cached workspace list meanwhile.
- **Missing records.** A workspace that isn't the member's, or a missing project, renders the not-found page inside the layout when its fetch returns 404.
- **Deleted or simplified.** `Initial<T>`, `initial()` and `useResource`'s `initial` argument are deleted. The browser client in `lib/api/http.ts` stays. `serverHttp`, `serverApi` and `lib/server.ts` are deleted.

## Decisions in detail

### 2. Embedding the built files

| | Embedded (`//go:embed`) | `WEB_DIR` on disk | CDN / static host |
| --- | --- | --- | --- |
| Deploy | One binary | Binary + a folder that must match it | Binary + a separate upload |
| API and UI versions | Always match: they ship together | Can drift if one is updated without the other | Can drift; old cached HTML can call a newer API |
| Config | None | A path per environment | CDN routing, cache invalidation, maybe a second origin |
| Front-end-only change | Rebuild and redeploy the binary | Copy files | Upload |
| Global latency | Served from wherever Go runs | Same | Edge-cached |

**Decided: embedded.** The typed client in `lib/api/*` and the Go handlers change together, so shipping them as one unit removes a whole class of version mismatches. The app is small (a few MB of assets). A CDN can still sit in front later and cache the immutable `/assets/*` without changing this design.

Mechanics: `//go:embed` can only see files inside the Go module. So `make build` copies `apps/web/build/client` into `apps/api/internal/web/dist/`, which is gitignored. A committed placeholder `index.html` keeps `go build` and `go test` working without a front-end build. In development Go doesn't serve the web app at all; Vite does.

### 3. CSP: nonces vs hashes

Both let our own inline `<script>`s run while blocking injected ones. They differ in how the browser recognises "ours":

| | Nonce placeholder | Hashes |
| --- | --- | --- |
| How it works | The build writes `nonce="__CSP_NONCE__"`; Go swaps in a fresh random value on every response and sends `script-src 'nonce-<value>'` | Go reads each embedded HTML file once at startup, hashes every inline script (SHA-256) and sends `script-src 'sha256-<hash>' …` for that file |
| What's allowed | Any inline script carrying today's value | Only those exact script bytes |
| Per-request work | Generate a value, rewrite the HTML | None (headers are precomputed per file) |
| Fits | HTML rendered per request (Next.js today) | HTML built ahead of time (this plan) |
| Failure mode | Forgetting the placeholder on a new script tag blocks it | None at runtime: hashes are recomputed from whatever was built |
| AGENTS invariant | Unchanged ("per-request script CSP nonces") | Reworded to "script CSP hashes for prerendered HTML" |

**Decided: hashes.** The HTML never changes after the build, which is the case hashes were designed for. They're at least as strict, because a script must match byte for byte. They also need no string rewriting on each response.

How Go does it:

- At startup, `internal/web` walks the embedded HTML files and parses each with `golang.org/x/net/html`. For every inline `<script>` (one without `src`), it takes the base64 SHA-256 of the exact text between the tags.
- It stores one precomputed header per file, e.g. `script-src 'self' 'sha256-…' 'sha256-…'`, and sends that file's header with it. The fallback page and each prerendered page get their own.
- A build with no inline scripts gets plain `script-src 'self'`.
- Startup fails if an embedded HTML file can't be parsed, rather than serving a policy that would block the app.

AGENTS.md's "Keep per-request script CSP nonces" becomes "Keep script CSP hashes for the prerendered HTML (computed by Go at startup) and other security headers; never allow `'unsafe-inline'` scripts."

`style-src 'unsafe-inline'` stays for Styletron. `frame-ancestors 'none'`, `nosniff`, `Referrer-Policy`, `Permissions-Policy` and HSTS (production) move from `proxy.ts` and `next.config.mjs` into Go's page middleware.

### 6. Collecting BaseUI CSS at build time

BaseUI styles itself with **Styletron**, a CSS-in-JS engine:

1. When a BaseUI component renders, it asks the engine for its styles, e.g. `{ position: "relative" }`.
2. The engine turns each declaration into an atomic class (`.ae { position: relative }`) and returns the class names for the element.
3. Where the CSS goes depends on the engine:
   - **Client engine (browser):** inserts the rules into `<style>` tags as components mount.
   - **Server engine:** keeps them in memory. After rendering, `engine.getStylesheets()` returns them to write into `<head>`.
4. **Hydration.** The browser's client engine is created with `hydrate: document.getElementsByClassName("_styletron_hydrate_")`. It reads the existing `<style>` tags, learns which classes already exist, and continues from there without duplicating them.

Today `_document.tsx` does steps 1–3 with a server engine on every request. With prerendering, the same thing happens once per prerendered file during `react-router build`:

- **`src/entry.server.tsx`.** React Router uses it to render the prerendered HTML at build time. It:
  - creates a fresh `Server` engine for each file;
  - wraps the app in Styletron's `Provider`;
  - waits for the full render (`onAllReady`);
  - inserts `<style class="_styletron_hydrate_">` tags from `getStylesheets()` before `</head>`.
- **`src/entry.client.tsx`.** It creates the `Client` engine with `hydrate`, as `lib/styletron.ts` does now, and wraps `<HydratedRouter/>` in the same `Provider`.

The homepage is styled from its first paint, and the fallback page carries the styles of its shell. Most of our components already replace BaseUI's slots with Tailwind classes via `slot()`, so the collected CSS is small.

**Spike before building:** confirm that `ssr: false` prerendering runs a custom `entry.server.tsx`. If it doesn't, fall back to a post-build step that renders the homepage with `renderToString` and the server engine.

## Go side

- **New `internal/web` package.** It owns serving the built app:
  - the embedded file system;
  - the fallback and 404 rules;
  - cache headers;
  - page security headers and precomputed CSP per file.

  It takes an `fs.FS`, so tests use `fstest.MapFS`. The `/app/*` gate is registered in `routes.go` with the session lookup from `httpapi`. `cmd/server` composes them (AGENTS rule 5).
- `APP_ORIGIN` becomes the Go server's own public origin in production. In development it stays `http://localhost:3000`, because the browser uses Vite, which proxies `/api` and passes `Origin` through unchanged.

## Development

- `make web` runs `react-router dev` (Vite, HMR) on :3000. `make api` is unchanged. Still two terminals in development; one process only in production.
- The `/app/*` gate and CSP only run when Go serves the build. In development the layout `clientLoader` redirects to login instead.
- A new `make serve` builds the web app, copies it and runs the API serving both on one port.
- The Vitest config moves into Vite's config.

## Testing

- **Go unit tests (`internal/web`):**
  - fallback for `/app/*` and `/auth/*`;
  - prerendered `/`;
  - asset cache headers;
  - unknown `/api/...` stays a JSON 404;
  - unknown paths get 404 plus the fallback;
  - the CSP hashes match the inline scripts of each embedded file;
  - a file whose inline script changes gets a different hash, and one with none gets `script-src 'self'`;
  - every HTML response carries its file's policy.
- **Go integration tests:**
  - no cookie, an unknown session or an expired session on `/app/...` → 302 to login with `next`;
  - a valid session gets the shell.
- **Vitest:**
  - `lib/session`, `use-resource`, `routes`, `ids`, `api/*` and component tests carry over;
  - `lib/server` tests become layout-loader tests: 401 → login, no workspace → onboarding, network failure → unavailable;
  - the homepage button switches to "Dashboard" when signed in.
- **Playwright (decision 10), against `make serve`:**
  - the existing flows;
  - `/` has its title, meta description and Styletron `<style>` tags in the raw HTML;
  - a signed-out `/app/...` request lands on login and returns there after signing in;
  - no CSP violations in the console.

## What changes

| Area | Change |
| --- | --- |
| `apps/web/package.json` | Remove `next`, `eslint-config-next`. Add `react-router`, `@react-router/dev`, `vite`, `@tailwindcss/vite` (and the React Router ESLint setup). `npm audit` afterwards |
| `next.config.mjs`, `proxy.ts`, `_document.tsx`, `_app.tsx`, `lib/server.ts`, `lib/page.ts`, `next-env.d.ts` | Deleted. Their jobs move to `root.tsx`, `entry.*.tsx`, `routes.ts`, the layout loader and Go |
| 19 files importing `next/*` | `useRouter` → `useNavigate`/`useParams`/`useLocation`/`useSearchParams`; `next/link` in `components/ui/link.tsx` → React Router `Link`; `next/head` → `meta`; `GetServerSideProps*` types → generated route types |
| `lib/use-resource.ts`, `lib/types.ts` | `initial` argument and `Initial<T>` removed |
| `components/ui/route-progress.tsx` | Driven by `useNavigation().state` instead of router events |
| `apps/api` | New `internal/web`, page routes and gate in `routes.go`, composition in `cmd/server`, `.gitignore` for `dist/`. New dependency `golang.org/x/net/html` for finding inline scripts (`govulncheck` afterwards) |
| `Makefile` | `web` runs Vite, `build` copies the client build into the API, new `serve` |
| `lewagent-services` web process | Command changes to the Vite dev server |
| Docs | `docs/ui/README.md`, `pages.md`, `api.md`, `testing.md`; `docs/api/README.md` (serving the web app); README; AGENTS.md product/stack, rules 4, 6, 7 and the CSP invariant |

## Is it better?

**Yes, for this app.**

- **Fewer moving parts:** one production process and one origin, with no proxy and no Node runtime.
- **Smaller attack surface:** no server-side authenticated fetches, so the rule-7 class of mistakes is gone.
- **Instant navigation** between signed-in pages.
- **Layouts are a first-class route feature**, not the `getLayout` workaround.
- **Public pages still get full HTML** for search engines.

**Trade-offs:**

- **Skeletons on every new page**, not just a hard refresh. Data arrives a moment after the page instead of with it. Cached pages show their last data at once.
- **Status codes.** Missing records render "not found" with HTTP 200, because the fallback has already been sent. Unknown paths still get a 404.
- **Build-time content.** Prerendered pages change only on rebuild. A page that needs per-request server rendering (e.g. a database-backed blog) would bring Node back.
- **The homepage button.** Signed-in visitors briefly see "Log in" before it switches to "Dashboard".
- **The migration itself:** about 12 page files, 19 `next/*` imports, the Makefile, tests and docs. It's one change; the two routers can't share an origin page by page.
