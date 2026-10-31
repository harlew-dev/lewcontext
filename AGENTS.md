# Project guide: lewcontext

## Product and stack

This project replaces the previous project completely. The previous README was stale and was not used as project context. The current README documents the new application.

- Backend: Go 1.26+, standard-library `net/http` method-aware routing, Bun (`uptrace/bun`, pgdialect) over the pgx `database/sql` driver, PostgreSQL 15+. In production the same Go binary serves the web app (`internal/web`, the build embedded with `//go:embed`): one process, one origin.
- Frontend: React 18, TypeScript in strict mode, a single-page app on React Router v7 framework mode (`ssr: false`; public pages such as `/` prerendered at build time for SEO), built by Vite, Tailwind CSS v4 on top of Uber Base Web (`baseui`) behaviour, SWR, Heroicons, `motion` (sliding sidebar indicator and the hover/focus highlight in the sidebar, tabs, menus and listboxes), Inter (self-hosted via Fontsource).
- Component standard: `components/ui` holds the app's Tailwind components (restrained zinc styling, sidebar and auth layouts). Match the existing components' APIs, class patterns and data attributes when adding or changing one.
- Testing: Go testing + httptest + race detector + real PostgreSQL integration tests; Vitest + React Testing Library; Playwright end-to-end tests. ESLint, TypeScript, go vet, gofmt, Prettier.
- This is a working demo of authenticated Go API ↔ React communication with durable PostgreSQL projects, not a static mockup.

## Docs and plans

Short summaries of how each part works live in `docs/`, indexed in [`docs/README.md`](docs/README.md). They hold the project's standards: naming, patterns, security rules and known pitfalls that the code alone doesn't show.

**Always read the docs for an area before writing code that affects it.** This isn't optional, even for small changes, and it applies to subagents too:

1. Before the first edit, find every area the change touches in the table below, and read each of those docs in full. Most changes touch more than one: a new endpoint touches `endpoints.md`, `auth.md`, `rate-limits.md`, `database.md` and `api/testing.md`, plus `ui/api.md` if the web app calls it. A new component touches `ui/design.md` and `ui/base-ui.md`.
2. If the work has a plan in `plans/features/` (open) or `plans/features/implemented/` (shipped), read it too. Its confirmed decisions override your defaults.
3. Follow what the docs say over what you'd otherwise do. If a doc and the code disagree, or a doc's rule doesn't fit the task, stop and tell the user; don't silently pick one.
4. Update every doc whose summary the change makes untrue, in the same change. A new doc gets a row in `docs/README.md` and in the table below.

| Doc | Covers |
| --- | --- |
| `docs/testing.md` | Local setup, `make dev`, `make build`/`make serve`, manual demo and failure checks |
| `docs/naming.md` | Naming for routes, handlers, store methods, permissions, events, rate limits, client methods, page routes and components. **Applies to every change** |
| `docs/deployment.md` | Production: image and commands, required config, migrating and releasing, several servers, proxies and client IPs, Cloudflare, email, security checklist |
| `docs/api/README.md` | Go package layout, request flow, serving the web app, environment variables |
| `docs/api/endpoints.md` | Every API endpoint, its protection and responses |
| `docs/api/auth.md` | Passwords, password reset, sessions, CSRF, headers, workspace roles |
| `docs/api/rate-limits.md` | Budgets on every signed-in request, per-feature and sign-in limits, quotas, `RATE_LIMITS`, limiting a new endpoint |
| `docs/api/events.md` | The `events` audit trail, the event catalogue, listeners and the delivery worker |
| `docs/api/mail.md` | `mailer.Send`, mail templates and their safety rules, the development mailbox |
| `docs/api/pagination.md` | Listing, searching and sorting a resource |
| `docs/api/database.md` | Tables, queries, migrations, adding a table, pruning |
| `docs/api/seeding.md` | Factories, seeders, `make seed` |
| `docs/api/testing.md` | Go unit and integration tests, helpers, notable tests |
| `docs/ui/README.md` | Web app folder layout and where new code goes |
| `docs/ui/pages.md` | Pages, routes, loaders, prerendering, `?next=` |
| `docs/ui/api.md` | Fetching and mutating API data from components and loaders |
| `docs/ui/pagination.md` | Paginated, searchable, sortable lists |
| `docs/ui/design.md` | Building and styling components |
| `docs/ui/forms-and-actions.md` | Where buttons and actions go (settings forms, one-off actions, dialogs, danger zones, page headers, table rows, sign-in pages), their order and style. **Read it before adding any button** |
| `docs/ui/base-ui.md` | Using and customising BaseUI, and its known issues |
| `docs/ui/strict-mode.md` | Behaviour seen only under `make dev` (double renders, effects twice) |
| `docs/ui/testing.md` | Vitest and Playwright tests |

**Plan before changing the architecture.** A new feature, a new table, package, event, listener or endpoint family, a new dependency, or a change to auth, security, rate limits or how layers fit together needs a completed plan in `plans/features/<name>.md` before any code is written. Follow the existing plans: Why, the design or flow, API and data changes, security risks and how they're avoided, tests, Decisions (each open question with a recommendation), and Not included. Write the plan, have the user confirm its decisions, then implement. When it ships, set its `Status:` line to implemented with the date, note where the build differs from the plan, link the docs it updated, and move it to `plans/features/implemented/` (fixing relative links to and from it). Small fixes and changes within an existing design don't need a plan.

## Non-negotiable architecture conventions

1. Keep source files focused and readable. Split business rules, persistence, HTTP boundaries, and presentation into named functions and small modules. Avoid large single-file apps and dense one-line functions.
2. Components are only for UI that is reused. Reuse shared buttons, form controls, headings, descriptions, badges, notices, layout, loading, empty states, and dialogs. Extend an existing primitive before duplicating markup/styles. Use BaseUI for interactive controls and accessible overlays; do not build a second component library beside it.
3. Content unique to one page lives in that page file (`src/pages/...`), split into named local functions; do not create `components/` files such as `DashboardPage` or a section used by a single page. `src/components` holds only reused UI: generic primitives in `components/ui`, page chrome in `components/layout`, and feature UI shared by several app pages in `components/app/<feature>` (e.g. `components/app/projects`). When page-local UI becomes needed by a second page, move it into `components/`. Use typed props and shared domain types.
4. Pages are nested by area: `src/pages/home.tsx` (the public, prerendered `/`), `src/pages/auth/*` (signed-out, e.g. `/auth/login`) and `src/pages/app/*` (signed-in; workspace pages under `src/pages/app/[workspace]/*`, e.g. `/app/wsp_…/dashboard`, `/app/wsp_…/projects`, plus `/app/onboarding` and the account's `/app/settings`). Every page is registered with its URL in `apps/web/src/routes.ts`. Signed-in pages render only their content inside the `pages/app/layout.tsx` layout route, which wraps them in `ApplicationLayout` and stays mounted between pages (sidebar state, the sliding `layoutId` indicator). Page route constants live in `apps/web/src/lib/routes.ts`; API paths and calls live only in the typed client `apps/web/src/lib/api/<resource>.ts` (`const api = useApi()` then `api.projects(workspace.id).store()`; typed paths via `paths.projects(workspace.id).index()`). Names follow [`docs/naming.md`](docs/naming.md): API client methods use the API's actions (`index`, `show`, `store`, `update`, `destroy`), page route constants are grouped by resource with page actions (`routes.projects.show(ws, prj)`, `routes.workspaces.edit(ws)`), and resource pages export `<Resource><Action>` (`ProjectIndex`); components read the session with `useSession()` (`apps/web/src/lib/session.tsx`) and the current workspace with `useWorkspace()` (`apps/web/src/lib/workspace.tsx`); shared route loaders live in `apps/web/src/lib/loaders.ts`; the API and page registry is `apps/api/internal/httpapi/routes.go`. Do not put route declarations in `main.go` or `root.tsx`.
5. Go `cmd` contains composition, process startup/shutdown, migration, seed and prune entrypoints only. `internal/httpapi` owns transport/input validation; `internal/store` owns Bun models and queries; `internal/auth` owns auth primitives; `internal/config` owns environment validation; `internal/database` owns connection and migration setup; `internal/web` owns serving the embedded web build (prerendered pages, the SPA fallback, assets, page security headers and CSP).
6. Build UI with Tailwind utilities in markup, Tailwind's default palette (zinc), spacing and mobile-first breakpoints (`sm:`, `lg:`), and `dark:` variants for the system colour scheme. Do not add per-component CSS files or custom colour tokens; `src/styles/theme.css` only sets the font, `src/styles/base.css` holds element defaults, both imported by `src/styles/globals.css`.
   - Component files are kebab-case and group a family of named exports: `button.tsx`, `input.tsx` (`Input`, `InputGroup`), `fieldset.tsx` (`Fieldset`, `Legend`, `FieldGroup`, `Field`, `Label`, `Description`, `ErrorMessage`), `dialog.tsx`, `confirmation-dialog.tsx` (`ConfirmationDialog`, for every "are you sure?"), `dropdown.tsx`, `listbox.tsx`, `sidebar.tsx`, `navbar.tsx`, `table.tsx`, `text.tsx`, `heading.tsx`. Layouts are `components/layout/sidebar-layout.tsx`, `auth-layout.tsx` and `application-layout.tsx`.
   - Components in `components/ui` keep their established props (`<Button color|outline|plain href>`, `<Badge color>`, `<Dialog open onClose size>`, `<SidebarItem href current>`). Other components with alternatives use a `variant` prop (`<Notice variant="error">`); never `tone` or one component per alternative.
   - BaseUI stays underneath for behaviour (Button, Input, Textarea, Select→`Listbox`, Modal→`Dialog`, Popover→`Dropdown`, Drawer→mobile sidebar). Style it by replacing its override slots with `slot()` from `src/lib/baseui.tsx`, which renders Tailwind classes and maps BaseUI `$` state props to the `data-hover`/`data-active`/`data-focus`/`data-disabled`/`data-invalid`/`data-closed` attributes our classes target. Plain links and buttons get the same attributes from `useInteractive` (`src/lib/interactive.ts`). Do not style BaseUI with `style` overrides except to neutralise a surface (`transparentSurface`).
   - Icons are Heroicons (`@heroicons/react/16|20/solid`), which carry `data-slot="icon"` for the Tailwind icon styles.
   - Pass `className` for placement and spacing; components merge it before their own classes, so don't pass utilities that compete with them. Map variants/colours to complete class strings so Tailwind can detect them.
   Keep layouts responsive and retain keyboard navigation, focus indicators, accessible names, labels, error/status announcements, and reduced-motion support.
7. Keep the server free of user data: there is no server rendering, and prerendered pages are built without a session. Go sends signed-out requests for `/app/*` to login before serving any HTML; signed-in pages load the session in the layout route's `clientLoader` and their data through SWR (instant page swaps with skeletons). Never cache user data in module-level variables. Styletron CSS for prerendered pages is collected per file at build time (`entry.server.tsx`). SWR keys include user ID. The app shell and API responses must remain no-store.
8. Every async interaction must handle pending, success, failure, and empty results. Disable duplicate mutation submission; use `<Button loading>` for pending actions, `ListSkeleton` for fetched lists, and route progress. Preserve available data on a refresh failure and provide Retry. Redirect expired sessions to login.
9. Persist real application data through the Go API. Derive dashboard counts from actual project data. Demo records belong in the explicit seed command, never hardcoded in UI. Registration never creates or joins a workspace, and new accounts have no projects. A new account goes to onboarding to create its first workspace, unless it signed up through an invitation link; it then returns to that link, which accepts the invitation. Joining a workspace only ever happens by accepting an invitation link: never look up invitations by email at sign-up or sign-in (there is no email verification).

## Authentication/security invariants

- Keep Argon2id with fresh random salts and constant-time comparison. Never log/store plaintext passwords. Password minimum at sign-up is 12 Unicode characters, max 128 UTF-8 bytes; login checks only the max, so the development seed's `DEMO_PASSWORD` may be short (`password`).
- Opaque 256-bit server sessions, digests in DB, independent CSRF tokens, HttpOnly/SameSite cookies; production Secure `__Host-` cookie. No session or CSRF tokens in localStorage, URLs or query strings. The one exception is single-use emailed link tokens (password reset, invitations): they may appear in their page's path, and in `?next=` pointing at that page (`returnPath` matches the exact token shape). They are never put in API URLs; they go in JSON bodies.
- Enforce exact Origin, content type, and CSRF for mutations; keep rate limits (budgets in `requireSession`, feature limits, auth limits) and quotas, hashing concurrency bound, session rotation, idle/absolute expiry, logout revocation, and other-session revocation.
- Require session middleware on every private endpoint. Workspace data goes through `requireWorkspace` (membership via `workspace_user`, same 404 for non-members and missing workspaces) and every workspace mutation names its permission with `ws.Can` (`internal/auth/permissions.go`; the `ws` group panics on an unguarded one); scope SQL by the member's workspace ID, never a request-supplied owner, workspace or role. Test non-members, other workspaces and refused roles before adding resources.
- Identify records outside the API only by their public IDs (`usr_`/`wsp_`/`prj_`/`inv_` + 14 base62, generated by the PostgreSQL `public_id()` column default); never expose internal bigint IDs in responses or URLs.
- Pass every value through Bun `?` placeholders (Bun escapes them into the query) and never build SQL text from input. Use bounded bodies/fields/headers, HTTP/query timeouts, generic public errors, and graceful shutdown. Do not enable wildcard credentialed CORS. Believe a forwarded client address only through `clientIP(r)`, which reads `CLIENT_IP_HEADER` only on connections from `TRUSTED_PROXIES`.
- Keep script CSP hashes for the built HTML (computed by `internal/web` at startup from each file's inline scripts; never `'unsafe-inline'` scripts) and the other security headers; inline CSS is required by Styletron. Production requires HTTPS and verified DB TLS. No demo seed in production.
- See `docs/api/auth.md` for limits. Password reset by link and workspace invitations exist, but there is no email delivery: links go to a development mailbox file (`MAIL_DRIVER=file`, refused in production), so both are off in production. Do not claim email delivery, email verification, MFA, or OIDC exists until it is implemented and tested.
- Email goes through `mailer.Send` (`internal/mail`) from a listener, never a handler, rendered from embedded `html/template` files; never `text/template` for HTML, never wrap data in `template.HTML`/`template.URL`, and never put user data in mail headers.

## Database and migrations

- Read `.env` (untracked); `.env.example` is a local test template. Do not commit production secrets.
- The supplied local test DB is `postgresql://lewcontext:password@127.0.0.1:5432/lewcontext` (local setup adds `sslmode=disable`). Use only for development/testing.
- Forward/reverse migrations are Bun SQL pairs under `apps/api/migrations` named `<version>_<name>.tx.up.sql` / `.tx.down.sql`; one create migration per table. **While in development, migrations are squashed:** change a table by editing its create migration, then rebuild the local database with `make migrate-fresh CONFIRM=1 && make seed`. Once a database holds data that must be kept, migrations become append-only again (never rewrite applied ones). The Bun migrator (`internal/database/migrate.go`) runs each file in a transaction, records it in `bun_migrations` only after success, groups each run for rollback, and holds a PostgreSQL advisory lock. No auto-migrate on API startup.
- Do not drop/reset databases or existing app data, except the local development database with `make migrate-fresh` (previews without `CONFIRM=1`, refused when `APP_ENV=production`). Integration tests create and remove only their unique temporary schema. Down migration SQL is destructive and requires explicit user authorization; `make migrate-rollback` only previews unless `CONFIRM=1`. `make prune` deletes old rows from log tables (the `events` audit trail) only with a target and `CONFIRM=1`, never under an hour old, and keeps events whose deliveries are pending; it is allowed in production (cron).
- The idempotent demo seed is explicit. It creates only its demo user, the "Acme" workspace, its projects and four fake `@example.test` members with unusable passwords, and leaves an existing account unchanged. Other seeders (`make seed only=volume|users`) only insert fake data; seeders live in `internal/seed`.
- Write every query with Bun's query builder (`NewSelect`, `NewInsert`, `NewUpdate`, `NewDelete`, `With`, `Where`/`Set`/`ColumnExpr` with `?` placeholders), in application code and tests alike. Don't write SQL strings with `NewRaw`, `QueryContext`, `QueryRowContext` or `ExecContext`. The only exceptions are statements Bun has no builder for in `internal/database` (creating and dropping schemas, advisory locks) and the migration `.sql` files.
- Every table has a factory in `apps/api/internal/factory` with its own Bun model. A migration that adds a table or a required column updates the factory (and seeders) in the same change; `TestEveryTableHasAFactory` enforces this. Integration tests build data with factories rather than hand-written rows or setup through API calls, except where the endpoint itself is under test.

## Commands and validation

Use `make db` (PostgreSQL with pgvector in Docker), `make install`, `make migrate` (`make migrate-status` to inspect), `make seed`, then `make dev` (`scripts/dev.sh`: the API and Vite in one terminal, Vite on :3000 proxying `/api` to Go; or `make api` and `make web` in separate terminals). `make serve` instead builds the single binary with the web app embedded and runs it on :3000, as in production. `make docker-dev` runs the whole app in Docker from the `Dockerfile` instead (database, image build, migrate, seed, then the app); after a code change, `make docker-app` rebuilds and restarts only the app container. Make loads root `.env`. Browse `http://localhost:3000` with the default origin configuration.

Before claiming completion, run relevant checks and report failures:

- `make test` and `make check`.
- Set `TEST_DATABASE_URL` and run `make test-integration` for auth, SQL, migrations, middleware, or authorization changes. An unset variable skips integration tests in `make test`; do not report that as DB verification.
- `make test-e2e` with `RATE_LIMITS=off make serve` running for user flows (it covers the Go page gate and CSP, which the Vite dev server doesn't have); set the test DB URL so the unique test user is cleaned up. Default installed Chromium path can be changed with `CHROMIUM_PATH`.
- `make build` when changing frontend/build integration or backend composition.
- Check dependency audits for dependency/auth changes. Preserve lockfiles. Explain compatibility overrides in `docs/ui/README.md` (Dependencies); do not suppress type errors with `any`/unsafe casts instead of fixing dependencies.
- Do not add tests that merely mirror trivial styling. Prefer behavior, security boundaries, and real persistence coverage.

## Working on this computer

- PostgreSQL runs in Docker (`compose.yaml`, `make db`; `docker compose ps` shows it). Reuse that container; never install a second DB server. Restart a running process after config changes instead of duplicating it.
- **Other agents may be running tests on the same machine**, in this checkout or another worktree. Be aware of that before starting a heavy run (`make test`, `make test-e2e`, `make build`):
  - Check what's already running: test processes (`go test`, Playwright, Vitest, a test server) and the ports you plan to use.
  - Take the machine's CPU and memory limits into account to avoid overloading it. Running several suites at once can make every run slow and flaky, so if the machine is already busy, wait for a run to finish before starting yours. A single focused test (`go test ./internal/mail -run TestX`) is fine at any time.
  - Start long runs in the background and wait for the result. Don't start a second copy of a run you already have going.
  - Never stop, restart, reuse or kill a server, container or test run you didn't start.
- Use the using-the-browser skill and Playwright tools for web flows; desktop tools for other applications/visual artifact handoff. Do not touch windows you did not open.
- Show the result with a saved screenshot or published file in the user's Artifacts panel.
- Keep scope to the request. Do not push, merge, force-push, delete branches, or perform destructive operations unless explicitly asked. No external messages without authorization.
- Finish with a concise summary of changes, checks, and remaining limitations.
