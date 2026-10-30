# Project guide: Forma

## Product and stack

This project replaces the previous project completely. The previous README was stale and was not used as project context. The current README documents the new application.

Short summaries of how each part works (API, auth, pagination, database, seeding, pages, components, testing) are indexed in [`docs/README.md`](docs/README.md). Read the relevant doc before changing an area, and update it when what it summarises changes.

- Backend: Go 1.26+, standard-library `net/http` method-aware routing, Bun (`uptrace/bun`, pgdialect) over the pgx `database/sql` driver, PostgreSQL 15+. In production the same Go binary serves the web app (`internal/web`, the build embedded with `//go:embed`): one process, one origin.
- Frontend: React 18, TypeScript in strict mode, a single-page app on React Router v7 framework mode (`ssr: false`; public pages such as `/` prerendered at build time for SEO), built by Vite, Tailwind CSS v4 on top of Uber Base Web (`baseui`) behaviour, SWR, Heroicons, `motion` (sliding sidebar indicator and hover background), Inter (self-hosted via Fontsource).
- Component standard: `components/ui` holds the app's Tailwind components (restrained zinc styling, sidebar and auth layouts). Match the existing components' APIs, class patterns and data attributes when adding or changing one.
- Testing: Go testing + httptest + race detector + real PostgreSQL integration tests; Vitest + React Testing Library; Playwright end-to-end tests. ESLint, TypeScript, go vet, gofmt, Prettier.
- This is a working demo of authenticated Go API ↔ React communication with durable PostgreSQL projects, not a static mockup.

## Non-negotiable architecture conventions

1. Keep source files focused and readable. Split business rules, persistence, HTTP boundaries, and presentation into named functions and small modules. Avoid large single-file apps and dense one-line functions.
2. Components are only for UI that is reused. Reuse shared buttons, form controls, headings, descriptions, badges, notices, layout, loading, empty states, and dialogs. Extend an existing primitive before duplicating markup/styles. Use BaseUI for interactive controls and accessible overlays; do not build a second component library beside it.
3. Content unique to one page lives in that page file (`src/pages/...`), split into named local functions; do not create `components/` files such as `DashboardPage` or a section used by a single page. `src/components` holds only reused UI: generic primitives in `components/ui`, page chrome in `components/layout`, and feature UI shared by several app pages in `components/app/<feature>` (e.g. `components/app/projects`). When page-local UI becomes needed by a second page, move it into `components/`. Use typed props and shared domain types.
4. Pages are nested by area: `src/pages/home.tsx` (the public, prerendered `/`), `src/pages/auth/*` (signed-out, e.g. `/auth/login`) and `src/pages/app/*` (signed-in; workspace pages under `src/pages/app/[workspace]/*`, e.g. `/app/wsp_…/dashboard`, `/app/wsp_…/projects`, plus `/app/onboarding` and the account's `/app/settings`). Every page is registered with its URL in `apps/web/src/routes.ts`. Signed-in pages render only their content inside the `pages/app/layout.tsx` layout route, which wraps them in `ApplicationLayout` and stays mounted between pages (sidebar state, the sliding `layoutId` indicator). Page route constants live in `apps/web/src/lib/routes.ts`; API paths and calls live only in the typed client `apps/web/src/lib/api/<resource>.ts` (`const api = useApi()` then `api.projects(workspace.id).create()`; typed paths via `paths.projects(workspace.id).index()`); components read the session with `useSession()` (`apps/web/src/lib/session.tsx`) and the current workspace with `useWorkspace()` (`apps/web/src/lib/workspace.tsx`); shared route loaders live in `apps/web/src/lib/loaders.ts`; the API and page registry is `apps/api/internal/httpapi/routes.go`. Do not put route declarations in `main.go` or `root.tsx`.
5. Go `cmd` contains composition, process startup/shutdown, migration and seed entrypoints only. `internal/httpapi` owns transport/input validation; `internal/store` owns Bun models and queries; `internal/auth` owns auth primitives; `internal/config` owns environment validation; `internal/database` owns connection and migration setup; `internal/web` owns serving the embedded web build (prerendered pages, the SPA fallback, assets, page security headers and CSP).
6. Build UI with Tailwind utilities in markup, Tailwind's default palette (zinc), spacing and mobile-first breakpoints (`sm:`, `lg:`), and `dark:` variants for the system colour scheme. Do not add per-component CSS files or custom colour tokens; `src/styles/theme.css` only sets the font, `src/styles/base.css` holds element defaults, both imported by `src/styles/globals.css`.
   - Component files are kebab-case and group a family of named exports: `button.tsx`, `input.tsx` (`Input`, `InputGroup`), `fieldset.tsx` (`Fieldset`, `Legend`, `FieldGroup`, `Field`, `Label`, `Description`, `ErrorMessage`), `dialog.tsx`, `dropdown.tsx`, `listbox.tsx`, `sidebar.tsx`, `navbar.tsx`, `table.tsx`, `text.tsx`, `heading.tsx`. Layouts are `components/layout/sidebar-layout.tsx`, `auth-layout.tsx` and `application-layout.tsx`.
   - Components in `components/ui` keep their established props (`<Button color|outline|plain href>`, `<Badge color>`, `<Dialog open onClose size>`, `<SidebarItem href current>`). Other components with alternatives use a `variant` prop (`<Notice variant="error">`); never `tone` or one component per alternative.
   - BaseUI stays underneath for behaviour (Button, Input, Textarea, Select→`Listbox`, Modal→`Dialog`, Popover→`Dropdown`, Drawer→mobile sidebar). Style it by replacing its override slots with `slot()` from `src/lib/baseui.tsx`, which renders Tailwind classes and maps BaseUI `$` state props to the `data-hover`/`data-active`/`data-focus`/`data-disabled`/`data-invalid`/`data-closed` attributes our classes target. Plain links and buttons get the same attributes from `useInteractive` (`src/lib/interactive.ts`). Do not style BaseUI with `style` overrides except to neutralise a surface (`transparentSurface`).
   - Icons are Heroicons (`@heroicons/react/16|20/solid`), which carry `data-slot="icon"` for the Tailwind icon styles.
   - Pass `className` for placement and spacing; components merge it before their own classes, so don't pass utilities that compete with them. Map variants/colours to complete class strings so Tailwind can detect them.
   Keep layouts responsive and retain keyboard navigation, focus indicators, accessible names, labels, error/status announcements, and reduced-motion support.
7. Keep the server free of user data: there is no server rendering, and prerendered pages are built without a session. Go sends signed-out requests for `/app/*` to login before serving any HTML; signed-in pages load the session in the layout route's `clientLoader` and their data through SWR (instant page swaps with skeletons). Never cache user data in module-level variables. Styletron CSS for prerendered pages is collected per file at build time (`entry.server.tsx`). SWR keys include user ID. The app shell and API responses must remain no-store.
8. Every async interaction must handle pending, success, failure, and empty results. Disable duplicate mutation submission; use `<Button loading>` for pending actions, `ListSkeleton` for fetched lists, and route progress. Preserve available data on a refresh failure and provide Retry. Redirect expired sessions to login.
9. Persist real application data through the Go API. Derive dashboard counts from actual project data. Demo records belong in the explicit seed command, never hardcoded in UI. New registrations start with no workspace (onboarding creates the first) and no projects.

## Authentication/security invariants

- Keep Argon2id with fresh random salts and constant-time comparison. Never log/store plaintext passwords. Password minimum at sign-up is 12 Unicode characters, max 128 UTF-8 bytes; login checks only the max, so the development seed's `DEMO_PASSWORD` may be short (`password`).
- Opaque 256-bit server sessions, digests in DB, independent CSRF tokens, HttpOnly/SameSite cookies; production Secure `__Host-` cookie. No session/auth tokens in localStorage or query strings.
- Enforce exact Origin, content type, and CSRF for mutations; keep auth rate limits, hashing concurrency bound, session rotation, idle/absolute expiry, logout revocation, and other-session revocation.
- Require session middleware on every private endpoint. Workspace data goes through `requireWorkspace` (membership via `workspace_user`, same 404 for non-members and missing workspaces) and `requireRole` where only some roles may act; scope SQL by the member's workspace ID, never a request-supplied owner, workspace or role. Test non-members, other workspaces and refused roles before adding resources.
- Identify records outside the API only by their public IDs (`usr_`/`wsp_`/`prj_` + 14 base62, generated by the PostgreSQL `public_id()` column default); never expose internal bigint IDs in responses or URLs.
- Pass every value through Bun `?` placeholders (Bun escapes them into the query) and never build SQL text from input. Use bounded bodies/fields/headers, HTTP/query timeouts, generic public errors, and graceful shutdown. Do not enable wildcard credentialed CORS or trust forwarding headers blindly.
- Keep script CSP hashes for the built HTML (computed by `internal/web` at startup from each file's inline scripts; never `'unsafe-inline'` scripts) and the other security headers; inline CSS is required by Styletron. Production requires HTTPS and verified DB TLS. No demo seed in production.
- See `docs/api/auth.md` for limits. Do not claim email verification, password recovery, MFA, or OIDC exists until it is implemented and tested.

## Database and migrations

- Read `.env` (untracked); `.env.example` is a local test template. Do not commit production secrets.
- The supplied local test DB is `postgresql://lewcontext:password@127.0.0.1:5432/lewcontext` (local setup adds `sslmode=disable`). Use only for development/testing.
- Forward/reverse migrations are Bun SQL pairs under `apps/api/migrations` named `<version>_<name>.tx.up.sql` / `.tx.down.sql`; one create migration per table. **While in development, migrations are squashed:** change a table by editing its create migration, then rebuild the local database with `make migrate-fresh CONFIRM=1 && make seed`. Once a database holds data that must be kept, migrations become append-only again (never rewrite applied ones). The Bun migrator (`internal/database/migrate.go`) runs each file in a transaction, records it in `bun_migrations` only after success, groups each run for rollback, and holds a PostgreSQL advisory lock. No auto-migrate on API startup.
- Do not drop/reset databases or existing app data, except the local development database with `make migrate-fresh` (previews without `CONFIRM=1`, refused when `APP_ENV=production`). Integration tests create and remove only their unique temporary schema. Down migration SQL is destructive and requires explicit user authorization; `make migrate-rollback` only previews unless `CONFIRM=1`.
- The idempotent demo seed is explicit. It creates only its demo user, the "Acme" workspace and its projects, and leaves an existing account unchanged. Other seeders (`make seed only=volume|users`) only insert fake data; seeders live in `internal/seed`.
- Every table has a factory in `apps/api/internal/factory` with its own Bun model. A migration that adds a table or a required column updates the factory (and seeders) in the same change; `TestEveryTableHasAFactory` enforces this. Integration tests build data with factories rather than hand-written rows or setup through API calls, except where the endpoint itself is under test.

## Commands and validation

Use `make install`, `make migrate` (`make migrate-status` to inspect), `make seed`, then `make dev` (`scripts/dev.sh`: the API and Vite in one terminal, Vite on :3000 proxying `/api` to Go; or `make api` and `make web` in separate terminals). `make serve` instead builds the single binary with the web app embedded and runs it on :3000, as in production. Make loads root `.env`. Browse `http://localhost:3000` with the default origin configuration.

Before claiming completion, run relevant checks and report failures:

- `make test` and `make check`.
- Set `TEST_DATABASE_URL` and run `make test-integration` for auth, SQL, migrations, middleware, or authorization changes. An unset variable skips integration tests in `make test`; do not report that as DB verification.
- `make test-e2e` with `make serve` running for user flows (it covers the Go page gate and CSP, which the Vite dev server doesn't have); set the test DB URL so the unique test user is cleaned up. Default installed Chromium path can be changed with `CHROMIUM_PATH`.
- `make build` when changing frontend/build integration or backend composition.
- Check dependency audits for dependency/auth changes. Preserve lockfiles. Explain compatibility overrides in `docs/ui/README.md` (Dependencies); do not suppress type errors with `any`/unsafe casts instead of fixing dependencies.
- Do not add tests that merely mirror trivial styling. Prefer behavior, security boundaries, and real persistence coverage.

## Working on this computer

- Check `lewagent-services status` before starting services. Reuse the running PostgreSQL instance; never install a second DB server. Restart a registered process after config changes instead of duplicating it.
- Use the using-the-browser skill and Playwright tools for web flows; desktop tools for other applications/visual artifact handoff. Do not touch windows you did not open.
- Show the result with a saved screenshot or published file in the user's Artifacts panel.
- Keep scope to the request. Do not push, merge, force-push, delete branches, or perform destructive operations unless explicitly asked. No external messages without authorization.
- Finish with a concise summary of changes, checks, and remaining limitations.
