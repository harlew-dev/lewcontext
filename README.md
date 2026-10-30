# Forma

A working project workspace with a Go API, PostgreSQL, and a React single-page app (with a prerendered public homepage) built with Tailwind (sidebar layout, auth layout, buttons, fields, listbox, dialog, dropdown, table) on **Uber Base Web (BaseUI)** for behaviour, with light and dark mode.

## Run locally

Requirements: Go **1.26+**, Node **24+**, npm, PostgreSQL **15+**. PostgreSQL must already be running. On this computer, inspect it with `lewagent-services status`.

```sh
cp .env.example .env
make install
make migrate
make seed
```

The example environment uses the supplied test database at `127.0.0.1:5432/lewcontext`. Its credentials are for this local test environment only. Existing tables/data are never dropped by setup. Migrations use Bun's migrator: explicit, transactional per file, versioned, grouped for rollback, and protected by a PostgreSQL advisory lock. `make migrate-status` lists them, `make migration name=add_tasks` creates an up/down pair, and `make migrate-rollback` previews undoing the last group (`CONFIRM=1` runs it and can drop data).

Start the dev server, from the repository root:

```sh
make dev
```

`make dev` (`scripts/dev.sh`) rebuilds the web app the API embeds, then runs the Go API on :8080 and Vite on :3000 in one terminal; Vite forwards `/api` to the API. Ctrl-C stops both. `make serve` builds and runs the production binary instead. See [running and building](docs/testing.md#run-the-dev-server). Open **http://localhost:3000**. Use this exact host (not `127.0.0.1`) because the default allowed origin is `http://localhost:3000`.

Sign in with **test@example.com** and the `DEMO_PASSWORD` in `.env` (default local example: **password**). Or choose **Sign up** for your own empty workspace. The seed runs only when explicitly invoked, refuses production, and leaves an existing demo account unchanged.

More seeders add random fake data (gofakeit) for trying pagination, search and sorting. They only insert, into the `DATABASE_URL` in `.env`:

```sh
make seed only=volume count=500          # projects for the demo account (or email=you@example.com)
make seed only=users count=25 projects=20 # fake accounts, up to 20 projects each, password DEMO_PASSWORD
```

Fake records come from the factories in `apps/api/internal/factory`, one per table; integration tests use the same factories.

The dashboard shows real database data. Create/edit projects, change their status, search/filter them, refresh, manage other sessions, and sign out. Reloading a page preserves data. Every account can access only its own projects.

## Checks

```sh
make test          # Go unit tests (race detector), React/API client tests
make check         # gofmt, go vet, TypeScript, ESLint, Prettier
make build         # web build, embedded into one Go executable (bin/api)
make serve         # build, then run bin/api serving the API and web app on :3000

# Real DB integration tests, each in a fresh temporary schema that is cleaned up:
export TEST_DATABASE_URL='postgresql://lewcontext:password@127.0.0.1:5432/lewcontext?sslmode=disable'
make test-integration

# With make serve running (stop make dev first):
make test-e2e
```

The browser test uses `/usr/bin/chromium` by default; set `CHROMIUM_PATH` for another installed Chromium. Set `TEST_DATABASE_URL` for automatic removal of the uniquely created E2E account after the run; without it, that test account remains. Browser tests exercise registration, login failures, project creation/editing, pending states, filters, the prerendered homepage, the /app session redirect, CSP hashes (no violations), security headers, mobile layout, session revocation, logout, and persistence. See [test-environment instructions](docs/testing.md).

## Architecture

- `apps/api/cmd`: small server, migration, and seed entrypoints.
- `apps/api/internal/httpapi/routes.go`: all API routes and authentication requirements, and the web app's page routes (signed-out `/app/*` requests go to login before any HTML).
- `apps/api/internal/{auth,config,database,httpapi,store}`: cryptography, validated config, Bun connection and migrator, HTTP boundary, Bun models and queries.
- `apps/api/internal/web`: serves the embedded web build: prerendered pages, the single-page-app fallback (404 for unknown URLs), cached assets, security headers and a CSP with each HTML file's inline-script hashes.
- `apps/api/migrations`: embedded Bun SQL migrations (`.tx.up.sql` / `.tx.down.sql` pairs).
- `apps/web/src/routes.ts`: the React Router page registry. `react-router.config.ts` prerenders `/` and builds everything else as a single-page app.
- `apps/web/src/pages`: pages, nested as `home.tsx` (`/`), `auth/` (`/auth/login`, `/auth/register`) and `app/` (`/app/onboarding`, `/app/settings`, `/app/wsp_…/dashboard`, `/app/wsp_…/projects`, …). Each page holds its own unique content; `app/layout.tsx` is the signed-in layout route.
- `apps/web/src/lib/routes.ts`: page URLs and navigation.
- `apps/web/src/components/ui`: Tailwind components in kebab-case files (`button.tsx`, `input.tsx`, `fieldset.tsx`, `listbox.tsx`, `dialog.tsx`, `dropdown.tsx`, `sidebar.tsx`, `navbar.tsx`, `table.tsx`, …), BaseUI underneath where there is behaviour.
- `apps/web/src/components/layout`: `sidebar-layout.tsx`, `auth-layout.tsx`, `application-layout.tsx` (Forma's sidebar, navbar and account menu) and the status page.
- `apps/web/src/components/app/projects`: project UI reused by the dashboard and projects pages.
- `apps/web/src/lib`: typed API client, route loaders, BaseUI theme and `slot()` override helper, interaction-state hook, Styletron setup, domain types.
- `apps/web/src/styles`: Tailwind entry (`globals.css`, also loads Inter), font setting (`theme.css`) and element defaults (`base.css`). Colours and spacing are Tailwind's defaults.
- `docs`: how the system works, indexed in [docs/README.md](docs/README.md): `docs/api/*` (endpoints, auth, pagination, database, seeding, tests), `docs/ui/*` (pages, API use, pagination, design, tests) and `docs/testing.md` (environment and manual checks).

In production one Go process serves both the API and the web app from the same origin, so there is no proxy and no Node runtime. Pages render in the browser: the signed-in layout loads the session once, and pages fetch their data through SWR with skeletons, so moving between pages is instant. The app shell and API responses are not cacheable; hashed assets are cached for a year. BaseUI's Styletron CSS for prerendered pages is collected at build time and hydrated on the client. SWR caches are keyed by user ID to prevent data carrying across account changes.

## Security and deployment

Passwords use Argon2id. Sessions use 256-bit opaque random tokens stored as SHA-256 digests, HttpOnly/SameSite cookies, 30-minute idle expiry, 24-hour absolute expiry, rotation at login, logout revocation, and a five-session account limit. Mutations require an exact trusted Origin, JSON content type, and an authenticated CSRF token. Auth endpoints have persistent IP/account rate limits and bounded hashing concurrency. See [security details and limits](docs/api/auth.md).

For a production-like run, `make serve` (or `make build`, then run `./bin/api` with configuration exported). `bin/api` refuses to start in production without the web build embedded. Before an actual production deployment: set `APP_ENV=production`, an HTTPS `APP_ORIGIN`, and a restricted database role with `sslmode=verify-full`; terminate TLS at a trusted reverse proxy. The production session cookie uses the `__Host-` prefix and Secure flag. Expose only the Go server, behind the TLS proxy. Do not use demo credentials. This demo does not implement password reset, verified email, MFA, or OIDC federation.
