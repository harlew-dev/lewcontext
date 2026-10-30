# API overview

Go 1.26, standard-library `net/http` with method-aware routing, Bun over pgx, PostgreSQL 15+.

## Packages (`apps/api`)

| Package | Owns |
| --- | --- |
| `cmd/server` | Startup, HTTP server timeouts, 15-minute cleanup of expired sessions and limits, graceful shutdown |
| `cmd/migrate` | `make migrate`, `migrate-status`, `migrate-rollback`, `migration` |
| `cmd/seed` | `make seed` flags only; refuses production |
| `internal/config` | Environment loading and validation |
| `internal/database` | Connection (`Open`) and the Bun migrator |
| `internal/httpapi` | Routes (`routes.go`), middleware, input validation, JSON responses |
| `internal/store` | Bun models and every query, always scoped by user |
| `internal/auth` | Argon2id passwords, random tokens, digests, cookies |
| `internal/factory` | Fake records for every table (seeders and tests only) |
| `internal/seed` | Seeders for `make seed` |
| `migrations` | SQL migration pairs, embedded into the binaries |

`cmd/server` never imports `factory` or `seed`.

## Request flow

```
security (headers, no-store, origin + JSON checks on mutations, 10 s timeout)
  → mux (routes.go)
  → requireSession (cookie → session, CSRF on mutations) for private routes
  → handler (parse + validate) → store (query scoped by user ID) → jsonResponse
```

- Responses are JSON. Errors are `{"error": "message"}` with a generic message; details go to `slog` only (`internalError`).
- Request bodies: `decode()` caps them at 16 KiB, rejects unknown fields and requires exactly one JSON object.
- The signed-in session is `current(r)` inside handlers.

## Environment

| Variable | Default | Notes |
| --- | --- | --- |
| `DATABASE_URL` | (required) | Production requires `sslmode=verify-full` |
| `APP_ORIGIN` | `http://localhost:3000` | Exact origin allowed for mutations; HTTPS in production |
| `API_ADDR` | `127.0.0.1:8080` | Listen address (loopback by default) |
| `APP_ENV` | (development) | `production` enables Secure `__Host-` cookies, HSTS and stricter config |
| `DEMO_PASSWORD` | | Only for `make seed` seeders that create accounts |

`make` loads the root `.env` and exports it, and its values win over variables set on the command line. To point a command at another database, run the Go command directly.

## More

[endpoints.md](endpoints.md) · [auth.md](auth.md) · [pagination.md](pagination.md) · [database.md](database.md) · [seeding.md](seeding.md) · [testing.md](testing.md)
