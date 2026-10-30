# API overview

Go 1.26, standard-library `net/http` with method-aware routing, Bun over pgx, PostgreSQL 15+. The same binary serves the web app in production.

## Packages (`apps/api`)

| Package | Owns |
| --- | --- |
| `cmd/server` | Startup (with the embedded web build when there is one), HTTP server timeouts, 15-minute cleanup of expired sessions and limits, graceful shutdown |
| `cmd/migrate` | `make migrate`, `migrate-status`, `migrate-rollback`, `migrate-fresh`, `migration` |
| `cmd/seed` | `make seed` flags only; refuses production |
| `internal/config` | Environment loading and validation |
| `internal/database` | Connection (`Open`) and the Bun migrator |
| `internal/httpapi` | Routes (`routes.go`, API and pages), middleware, input validation, JSON responses |
| `internal/web` | Serving the embedded web build: prerendered pages, the SPA fallback, assets, page security headers and CSP hashes |
| `internal/store` | Bun models and every query, scoped by the user's workspace membership |
| `internal/auth` | Argon2id passwords, random tokens, digests, cookies |
| `internal/factory` | Fake records for every table (seeders and tests only) |
| `internal/seed` | Seeders for `make seed` |
| `migrations` | SQL migration pairs, embedded into the binaries |

`cmd/server` never imports `factory` or `seed`.

## Request flow

`Routes()` sends `/api` and `/api/*` to the API; every other request goes to the pages ([below](#serving-the-web-app)).

```
security (headers, no-store, origin + JSON checks on mutations, 10 s timeout)
  → mux (routes.go)
  → requireSession (cookie → session, CSRF on mutations) for private routes
  → requireWorkspace ({workspace} → the member's workspace and role, else 404) for workspace routes
  → requireRole (403 below the needed role) where only some roles may act
  → handler (parse + validate) → store (query scoped by workspace ID) → jsonResponse
```

- Responses are JSON. Errors are `{"error": "message"}` with a generic message; details go to `slog` only (`internalError`).
- Request bodies: `decode()` caps them at 16 KiB, rejects unknown fields and requires exactly one JSON object.
- The signed-in session is `current(r)` inside handlers, and the workspace in the path `currentWorkspace(r)`.
- Routes are grouped with `group(mux, prefix, middleware…)` in `routes.go`; `net/http` has no groups, so the helper prefixes each pattern and wraps its handler.

## Serving the web app

`make build` (and `make dev`, through `scripts/build-web.sh`) builds `apps/web` (`react-router build`), copies `build/client` into `internal/web/dist/` (gitignored; only `.gitkeep` is committed) and compiles it into `bin/api` with `//go:embed`. `make serve` runs that binary on :3000.

| Request | Answer |
| --- | --- |
| `GET /api/...` | The API, as above. Unknown API paths are the API's 404, never the app shell |
| `GET /assets/*` | The hashed file, `Cache-Control: public, max-age=31536000, immutable` |
| `GET /favicon.svg` (other built files) | The file, `no-cache` |
| `GET /` (prerendered pages) | Their HTML, `no-cache` |
| `GET /app`, `/app/*` without a valid session | `302` to `/auth/login?next=<path and query>` (`requirePageSession`) |
| `GET /app/*`, `/auth/*` | `__spa-fallback.html`, `no-store`; the browser renders the page |
| any other `GET` | `__spa-fallback.html` with status 404 (the app shows its not-found page) |
| other methods outside `/api` | 405 |

`internal/web` hashes each HTML file's inline scripts once, in `web.New`, and sends that file's CSP with it ([auth.md](auth.md#transport-and-headers)). Dot-files (`.vite/`) and `.html` paths aren't served directly. If the session check's database query fails, the page is served anyway and its own session check shows the outage page.

Without a web build, the server serves the API only in development (Vite serves the pages) and refuses to start with `APP_ENV=production`. `make dev` rebuilds `dist/` before starting, so the API on :8080 serves the pages as they were at startup. `make api` alone compiles whatever `dist/` holds from the last build. In development, browse :3000 either way.

## Environment

| Variable | Default | Notes |
| --- | --- | --- |
| `DATABASE_URL` | (required) | Production requires `sslmode=verify-full` |
| `APP_ORIGIN` | `http://localhost:3000` | Exact origin allowed for mutations; HTTPS in production |
| `API_ADDR` | `127.0.0.1:8080` | Listen address (loopback by default); `make serve` uses `127.0.0.1:3000` |
| `API_URL` | `http://127.0.0.1:8080` | Only for Vite's dev proxy (`make dev`, `make web`) |
| `APP_ENV` | (development) | `production` enables Secure `__Host-` cookies, HSTS and stricter config |
| `DEMO_PASSWORD` | | Only for `make seed` seeders that create accounts (1–128 bytes; `password` locally) |

`make` loads the root `.env` and exports it, and its values win over variables set on the command line. To point a command at another database, run the Go command directly.

## More

[endpoints.md](endpoints.md) · [auth.md](auth.md) · [pagination.md](pagination.md) · [database.md](database.md) · [seeding.md](seeding.md) · [testing.md](testing.md)
