# Forma

A working project workspace with a Go API, PostgreSQL, and a server-rendered React frontend styled with **Tailwind CSS** and using **Uber Base Web (BaseUI)** for interactive controls. The restrained sidebar, typography, tables, forms, and dialogs are inspired by [Catalyst](https://catalyst.tailwindui.com/); no proprietary Catalyst source is included.

## Run locally

Requirements: Go **1.26+**, Node **24+**, npm, PostgreSQL **15+**. PostgreSQL must already be running. On this computer, inspect it with `lewagent-services status`.

```sh
cp .env.example .env
make install
make migrate
make seed
```

The example environment uses the supplied test database at `127.0.0.1:5432/lewcontext`. Its credentials are for this local test environment only. Existing tables/data are never dropped by setup. Migrations use Bun's migrator: explicit, transactional per file, versioned, grouped for rollback, and protected by a PostgreSQL advisory lock. `make migrate-status` lists them, `make migration name=add_tasks` creates an up/down pair, and `make migrate-rollback` previews undoing the last group (`CONFIRM=1` runs it and can drop data).

In two terminals, from the repository root:

```sh
make api
```

```sh
make web
```

Open **http://localhost:3000**. Use this exact host (not `127.0.0.1`) because the default allowed origin is `http://localhost:3000`.

Sign in with **hello@forma.test** and the `DEMO_PASSWORD` in `.env` (default local example: **DemoWorkspace2026!**). Or choose **Create an account** for your own empty workspace. The seed runs only when explicitly invoked, refuses production, and leaves an existing demo account unchanged.

The dashboard shows real database data. Create/edit projects, change their status, search/filter them, refresh, manage other sessions, and sign out. Reloading a page preserves data. Every account can access only its own projects.

## Checks

```sh
make test          # Go unit tests (race detector), React/API client tests
make check         # gofmt, go vet, TypeScript, ESLint, Prettier
make build         # Go executable plus Next production build

# Real DB integration tests, each in a fresh temporary schema that is cleaned up:
export TEST_DATABASE_URL='postgresql://lewcontext:password@127.0.0.1:5432/lewcontext?sslmode=disable'
make test-integration

# API and web servers must already be running:
make test-e2e
```

The browser test uses `/usr/bin/chromium` by default; set `CHROMIUM_PATH` for another installed Chromium. Set `TEST_DATABASE_URL` for automatic removal of the uniquely created E2E account after the run; without it, that test account remains. Browser tests exercise registration, login failures, project creation/editing, pending states, filters, SSR HTML, security headers, mobile layout, session revocation, logout, and persistence. See [test-environment instructions](docs/testing.md).

## Architecture

- `apps/api/cmd`: small server, migration, and seed entrypoints.
- `apps/api/internal/httpapi/routes.go`: all API routes and authentication requirements.
- `apps/api/internal/{auth,config,database,httpapi,store}`: cryptography, validated config, Bun connection and migrator, HTTP boundary, Bun models and queries.
- `apps/api/migrations`: embedded Bun SQL migrations (`.tx.up.sql` / `.tx.down.sql` pairs).
- `apps/web/src/pages`: Next.js Pages Router pages, nested as `auth/` (`/auth/login`) and `app/` (`/app/dashboard`, `/app/projects`, `/app/settings`). Each page holds its own unique content; protected pages use request-time SSR. `/` and the old `/login`, `/projects`, `/settings` URLs redirect (see `next.config.mjs`).
- `apps/web/src/lib/routes.ts`: navigation and API route definitions.
- `apps/web/src/components/{ui,layout,app/projects}`: reused UI only — Tailwind/BaseUI primitives, app chrome, and project UI shared by the dashboard and projects pages.
- `apps/web/src/lib`: typed API client, SSR loader, theme, Styletron setup, domain types.
- `apps/web/src/styles`: Tailwind entry (`globals.css`), design tokens (`theme.css`), element defaults (`base.css`) and custom utilities (`utilities.css`); no copied proprietary kit.
- `docs`: setup, API/security contracts, and test instructions.

Browser requests go to the same-origin `/api/*` proxy; Go listens on loopback `:8080`. SSR forwards only the requesting browser's cookies to that fixed backend URL. Authenticated HTML and API responses are not cacheable. Styletron styles are collected per SSR request and hydrated on the client. SWR caches are keyed by user ID to prevent data carrying across account changes.

## Security and deployment

Passwords use Argon2id. Sessions use 256-bit opaque random tokens stored as SHA-256 digests, HttpOnly/SameSite cookies, 30-minute idle expiry, 24-hour absolute expiry, rotation at login, logout revocation, and a five-session account limit. Mutations require an exact trusted Origin, JSON content type, and an authenticated CSRF token. Auth endpoints have persistent IP/account rate limits and bounded hashing concurrency. See [security details and limits](docs/security.md).

For a production-like run, `make build`, then run `./bin/api` with configuration exported and `npm --prefix apps/web start`. Before an actual production deployment: set `APP_ENV=production`, an HTTPS `APP_ORIGIN`, and a restricted database role with `sslmode=verify-full`; terminate TLS at a trusted reverse proxy. The production session cookie uses the `__Host-` prefix and Secure flag. Keep the API private and deploy only the web entrypoint. Do not use demo credentials. This demo does not implement password reset, verified email, MFA, or OIDC federation.
