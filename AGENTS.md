# Project guide: Forma

## Product and stack

This project replaces the previous project completely. The previous README was stale and was not used as project context. The current README documents the new application.

- Backend: Go 1.26+, standard-library `net/http` method-aware routing, Bun (`uptrace/bun`, pgdialect) over the pgx `database/sql` driver, PostgreSQL 15+.
- Frontend: React 18, TypeScript in strict mode, Next.js Pages Router with request-time SSR, Tailwind CSS v4 utilities for layout and styling, Uber Base Web (`baseui`) for interactive controls (styled by Styletron), SWR, Lucide icons.
- Visual reference: Catalyst at https://catalyst.tailwindui.com. Reproduce its restrained layout/typography/spacing principles using our own styles; do not copy proprietary component source.
- Testing: Go testing + httptest + race detector + real PostgreSQL integration tests; Vitest + React Testing Library; Playwright end-to-end tests. ESLint, TypeScript, go vet, gofmt, Prettier.
- This is a working demo of authenticated Go API ↔ React communication with durable PostgreSQL projects, not a static mockup.

## Non-negotiable architecture conventions

1. Keep source files focused and readable. Split business rules, persistence, HTTP boundaries, and presentation into named functions and small modules. Avoid large single-file apps and dense one-line functions.
2. Components are only for UI that is reused. Reuse shared buttons, form controls, headings, descriptions, badges, notices, layout, loading, empty states, and dialogs. Extend an existing primitive before duplicating markup/styles. Use BaseUI for interactive controls and accessible overlays; do not build a second component library beside it.
3. Content unique to one page lives in that page file (`src/pages/...`), split into named local functions; do not create `components/` files such as `DashboardPage` or a section used by a single page. `src/components` holds only reused UI: generic primitives in `components/ui`, page chrome in `components/layout`, and feature UI shared by several app pages in `components/app/<feature>` (e.g. `components/app/projects`). When page-local UI becomes needed by a second page, move it into `components/`. Use typed props and shared domain types.
4. Pages are nested by area: `src/pages/auth/*` (signed-out, e.g. `/auth/login`) and `src/pages/app/*` (signed-in, e.g. `/app/dashboard`, `/app/projects`, `/app/settings`). Route constants live in `apps/web/src/lib/routes.ts`; redirects from `/` and pre-nesting URLs live in `next.config.mjs`; the API registry is `apps/api/internal/httpapi/routes.go`. Do not put route declarations in `main.go` or `_app.tsx`.
5. Go `cmd` contains composition, process startup/shutdown, migration and seed entrypoints only. `internal/httpapi` owns transport/input validation; `internal/store` owns Bun models and queries; `internal/auth` owns auth primitives; `internal/config` owns environment validation; `internal/database` owns connection and migration setup.
6. Style with Tailwind utilities in markup; do not add per-component CSS files. Design tokens (colors, type scale, breakpoints, shadows, animations) live in `src/styles/theme.css`, element defaults in `src/styles/base.css`, custom `@utility` rules in `src/styles/utilities.css`, all imported by `src/styles/globals.css`. Use theme tokens instead of arbitrary values where a token exists. Breakpoints are desktop-first: step down with `max-lg:`/`max-md:`/`max-sm:`, up with `xl:`. BaseUI controls keep the central BaseUI theme (`src/lib/theme.ts`) and `overrides`, not Tailwind classes.
   - Style alternatives on our own components are chosen with a `variant` prop (e.g. `<Badge variant="completed">`, `<Notice variant="error">`); do not use `tone`, `type`, or one component per alternative (e.g. no `ErrorNotice`/`SuccessNotice`). BaseUI wrappers keep BaseUI's prop names (e.g. Button `kind`).
   - Form controls come from `components/ui/Form.tsx` (`Input`, `Textarea`), which wrap BaseUI with a label.
   - A component's `className` prop is for outer spacing/placement only; never pass utilities that compete with its own (font size, color, display). Map variants to complete class strings so Tailwind can detect them.
   Keep layouts responsive and retain keyboard navigation, focus indicators, accessible names, labels, error/status announcements, and reduced-motion support.
7. Preserve SSR for protected pages. Forward only the incoming request cookie to the fixed private API URL; never globally cache user data or reuse a server-side authenticated fetch context. Styletron collection must be per request. SWR keys include user ID. Private HTML and API responses must remain no-store.
8. Every async interaction must handle pending, success, failure, and empty results. Disable duplicate mutation submission; use BaseUI spinners for buttons, skeletons for fetched lists, and route progress. Preserve available data on a refresh failure and provide Retry. Redirect expired sessions to login.
9. Persist real application data through the Go API. Derive dashboard counts from actual project data. Demo records belong in the explicit seed command, never hardcoded in UI. New registrations start with no projects.

## Authentication/security invariants

- Keep Argon2id with fresh random salts and constant-time comparison. Never log/store plaintext passwords. Password minimum is 12 Unicode characters, max 128 UTF-8 bytes.
- Opaque 256-bit server sessions, digests in DB, independent CSRF tokens, HttpOnly/SameSite cookies; production Secure `__Host-` cookie. No session/auth tokens in localStorage or query strings.
- Enforce exact Origin, content type, and CSRF for mutations; keep auth rate limits, hashing concurrency bound, session rotation, idle/absolute expiry, logout revocation, and other-session revocation.
- Require session middleware on every private endpoint and scope SQL by the authenticated user ID. Never trust a request-supplied owner ID. Test cross-user access before adding resources.
- Pass every value through Bun `?` placeholders (Bun escapes them into the query) and never build SQL text from input. Use bounded bodies/fields/headers, HTTP/query timeouts, generic public errors, and graceful shutdown. Do not enable wildcard credentialed CORS or trust forwarding headers blindly.
- Keep per-request script CSP nonces and other security headers; inline CSS is required by Styletron. Production requires HTTPS and verified DB TLS. No demo seed in production.
- See `docs/security.md` for limits. Do not claim email verification, password recovery, MFA, or OIDC exists until it is implemented and tested.

## Database and migrations

- Read `.env` (untracked); `.env.example` is a local test template. Do not commit production secrets.
- The supplied local test DB is `postgresql://lewcontext:password@127.0.0.1:5432/lewcontext` (local setup adds `sslmode=disable`). Use only for development/testing.
- Forward/reverse migrations are Bun SQL pairs under `apps/api/migrations` named `<version>_<name>.tx.up.sql` / `.tx.down.sql`; create them with `make migration name=...`. Never rewrite applied migrations. The Bun migrator (`internal/database/migrate.go`) runs each file in a transaction, records it in `bun_migrations` only after success, groups each run for rollback, and holds a PostgreSQL advisory lock. Databases from the previous runner are adopted once: `schema_migrations` is copied into `bun_migrations` and renamed `schema_migrations_legacy`. No auto-migrate on API startup.
- Do not drop/reset databases or existing app data. Integration tests create and remove only their unique temporary schema. Down migration SQL is destructive and requires explicit user authorization; `make migrate-rollback` only previews unless `CONFIRM=1`.
- The idempotent demo seed is explicit. It creates only its demo user/projects and leaves an existing account unchanged.

## Commands and validation

Use `make install`, `make migrate` (`make migrate-status` to inspect), `make seed`, then `make api` and `make web` in separate terminals. Make loads root `.env`. Browse `http://localhost:3000` with the default origin configuration.

Before claiming completion, run relevant checks and report failures:

- `make test` and `make check`.
- Set `TEST_DATABASE_URL` and run `make test-integration` for auth, SQL, migrations, middleware, or authorization changes. An unset variable skips integration tests in `make test`; do not report that as DB verification.
- `make test-e2e` with the servers running for user flows; set the test DB URL so the unique test user is cleaned up. Default installed Chromium path can be changed with `CHROMIUM_PATH`.
- `make build` when changing frontend/build integration or backend composition.
- Check dependency audits for dependency/auth changes. Preserve lockfiles. Explain compatibility overrides in `docs/security.md`; do not suppress type errors with `any`/unsafe casts instead of fixing dependencies.
- Do not add tests that merely mirror trivial styling. Prefer behavior, security boundaries, and real persistence coverage.

## Working on this computer

- Check `lewagent-services status` before starting services. Reuse the running PostgreSQL instance; never install a second DB server. Restart a registered process after config changes instead of duplicating it.
- Use the using-the-browser skill and Playwright tools for web flows; desktop tools for other applications/visual artifact handoff. Do not touch windows you did not open.
- Show the result with a saved screenshot or published file in the user's Artifacts panel.
- Keep scope to the request. Do not push, merge, force-push, delete branches, or perform destructive operations unless explicitly asked. No external messages without authorization.
- Finish with a concise summary of changes, checks, and remaining limitations.
