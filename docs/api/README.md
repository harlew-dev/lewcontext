# API overview

Go 1.26, standard-library `net/http` with method-aware routing, Bun over pgx, PostgreSQL 15+. The same binary serves the web app in production.

## Packages (`apps/api`)

| Package | Owns |
| --- | --- |
| `cmd/server` | Startup (the log handler first, then the embedded web build when there is one, and the admin build beside it when `ADMIN_ROUTE_KEY` is set), event listeners and the delivery worker, the error collector (`observe.SetReporter`, flushed to `app_errors` every 10 s), the slow-query hook, this server's `server_instances` row and `server.started`, each minute's metrics with its heartbeat, the hourly rollup, `/metrics` on `METRICS_ADDR` (`metrics.go`), HTTP server timeouts, 15-minute cleanup of expired sessions, limits and reset links, graceful shutdown (readiness answers 503 first, `Server.Drain`, for `SHUTDOWN_DELAY`; then `server.stopped`, the last minute's metrics and the error collector's last flush once everything else has stopped) |
| `cmd/migrate` | `make migrate`, `migrate-status`, `migrate-rollback`, `migrate-fresh`, `migration` |
| `cmd/seed` | `make seed` flags only; refuses production |
| `cmd/prune` | `make prune` flags only; previews unless confirmed ([database.md](database.md#pruning)) |
| `cmd/admin` | The operator's commands, previewing unless confirmed: `grant` (with a role, `-role viewer|operator`), `revoke` and `list` admins ([admin.md](admin.md)); `reset-mfa` turns off a locked-out user's two-factor authentication ([auth.md](auth.md#two-factor-authentication)); `blocks` and `unblock` list and unblock blocked email addresses ([auth.md](auth.md#blocking-an-email-address); the dashboard's Email blocks page does both too); `app-key generate` makes an `APP_KEY` (`make app-key`) and `route-key generate` an `ADMIN_ROUTE_KEY` |
| `internal/config` | Environment loading and validation; `LoadLogging` reads the log's settings on their own, which every command does first |
| `internal/observe` | The one log handler every command installs (`observe.NewLogger`: JSON or text, the request's ID on each line, redaction), request IDs, what a request's access line reports (`observe.Request`), the scrubber every error's text goes through before it's logged or stored (`observe.Scrub`), the error collector that groups what `observe.Report` is handed and flushes it to `app_errors` (`observe.Collector`), and the metric registry (`observe.Metrics`: counters, gauges, histograms and the request table), its minute's flush (`observe.MinuteFlush`) and `/metrics` (`observe.MetricsHandler`) ([observability.md](observability.md)). It imports nothing else of the app's |
| `internal/database` | Connection (`Open`), the Bun migrator, the advisory locks (the email lock, the metrics rollup's), and the query hook that times every query and logs slow ones (`QueryHook`) |
| `internal/httpapi` | The core every controller uses: `Server`, middleware, JSON answers (`respond.go`), what middleware puts on a request (`context.go`), the request's ID, access line, panic recovery and readiness (`observability.go`), rate limits (`limits.go`), sessions, credentials, second factors, the admin keys, sessions, roles, step-up, page gate and assets pass (`admin.go`), and the admin address allowlist (`admin_access.go`) |
| `internal/httpapi/endpoints/<area>` | The controllers, one package per area: `account` (`/api/auth/*`, `/api/email-blocks/*`), `workspace` (`/api/workspaces/*`, `/api/invitations/*`), `admin` (`/api/admin/*`); one controller per file ([naming](../naming.md#controllers-internalhttpapi)) |
| `internal/httpapi/router` | `routes.go`, the one registry of API routes and pages, with their middleware and limits (`router.New`) |
| `internal/httpapi/{clientip,input,ratelimit}` | Leaves the core imports: resolving the client's address; query parameters, paging, public IDs, search terms and link tokens; what a rate limit is and the in-memory counter |
| `internal/httpapi/apitest` | Helpers the integration tests share ([testing.md](testing.md)); only tests import it |
| `internal/web` | Serving the embedded web build: prerendered pages, the SPA fallback, assets, page security headers and CSP hashes; and the admin build, embedded apart, only through `ServeAdmin` and `ServeAdminFile` |
| `internal/store` | Bun models and every query, scoped by the user's workspace membership |
| `internal/auth` | Argon2id passwords, random tokens, digests, cookies, TOTP codes, recovery codes, emailed codes, sealing secrets with `APP_KEY` (`Keys`), and the admin assets pass (`admin_assets.go`) |
| `internal/mail` | Email from embedded templates (`mailer.Send`: an `html/template` part and a plain-text part), the SMTP driver and the development mailbox driver, and the reset and invitation listeners ([mail.md](mail.md)) |
| `internal/events` | Event types, the request's actor and user agent, listeners (`Bus`, `Listen`) and the delivery `Worker` ([events.md](events.md)); `internal/store` records events and is the worker's queue |
| `internal/factory` | Fake records for every table (seeders and tests only) |
| `internal/seed` | Seeders for `make seed` |
| `internal/prune` | `make prune`: one file per target (`events.go`, `app_errors.go`, `metric_hours.go`) registering itself, plus the shared list, `-older-than` parsing and batched run |
| `migrations` | SQL migration pairs, embedded into the binaries |

`cmd/server` never imports `factory`, `seed` or `httpapi/apitest`. Areas never import each other, and the core imports none of them (`TestHTTPLayout`).

## Request flow

`router.New(s)` sends `/api` and `/api/*` to the API; every other request goes to the pages ([below](#serving-the-web-app)).

```
ClientAddress (the client IP: the connection's, or CLIENT_IP_HEADER from TRUSTED_PROXIES; the request's ID and X-Request-ID; once answered, the request counted in the metrics and its access line), for pages too
  → Security (headers, no-store, origin + JSON checks on mutations, 10 s timeout, flagged when it ends a request)
  → RecoverPanics (a panic → the generic 500 with the request's ID; around the pages too)
  → mux (router/routes.go; the matched pattern names the route for the access line)
  → RequireSession (cookie → session, CSRF on mutations) for private routes
  → RequireWorkspace ({workspace} → the member's workspace and role, else 404) for workspace routes
  (or, under /api/admin: RequireAdminKey for the sign-in, RequireAdminKeyOrPrevious → RequireAdminSession → RequireAdminRole on an action → RequireAdminStepUp → LimitAdmin on an account-and-access action; admin.md)
  → RequirePermission (403 unless the role grants it) on workspace mutations (ws.Can)
  → controller, e.g. workspace.Projects.Index (parse + validate) → store, e.g. h.Repo.IndexProjects (query scoped by workspace ID) → httpapi.JSON
```

- Responses are JSON. Errors are `{"error": "message"}` with a generic message; a 500 adds the request's ID, `{"error": "Something went wrong. Please try again.", "requestId": "req_…"}`, and its details go to the log only, under that ID (`httpapi.InternalError(w, r, err)`). Log during a request with `slog.*Context(r.Context(), …)`, never the path, query, headers or body ([observability.md](observability.md)).
- Request bodies: `httpapi.Decode` caps them at 16 KiB, rejects unknown fields and requires exactly one JSON object. `httpapi.DecodeValid(w, r, validProject, message)` decodes and validates in one step (400, or 422 with the message).
- The session cookie: `h.SessionDigest(r)` is what the `sessions` table stores for it (logout, revoking other sessions, a password change); `s.sessionToken(r)` (core only) is the raw, well-formed cookie.
- Controllers embed `*httpapi.Server`, so they reach the store as `h.Repo` and the config as `h.Config`. The signed-in session is `httpapi.Session(r)`, the workspace in the path `httpapi.Workspace(r)`, and the admin's session `httpapi.Admin(r)`. The context keys are unexported, so only the core's middleware can set them.
- Routes are grouped with `group(mux, prefix, middleware…)` in `router/routes.go`; `net/http` has no groups, so the helper prefixes each pattern and wraps its handler.

## Serving the web app

`make build` (and `make dev`, through `scripts/build-web.sh`) builds `apps/web` twice: the app (`react-router build`), copied from `build/client` into `internal/web/dist/`, and the admin dashboard (`WEB_BUILD=admin`), copied from `build/admin/client` into `internal/web/admin/` (both gitignored; only `.gitkeep` is committed). It fails if any app build file mentions admin code (`scripts/check-app-build.sh`; [admin.md](admin.md#the-admin-build)). Both are compiled into `bin/api` with `//go:embed`, apart. `make serve` runs that binary on :3000.

| Request | Answer |
| --- | --- |
| `GET /api/...` | The API, as above. Unknown API paths are the API's 404, never the app shell |
| `GET /assets/*` | The hashed file, `Cache-Control: public, max-age=31536000, immutable` |
| `GET /favicon.svg` (other built files) | The file, `no-cache` |
| `GET /` (prerendered pages) | Their HTML, `no-cache` |
| `GET /app`, `/app/*` without a valid session | `302` to `/auth/login?next=<path and query>` (`RequirePageSession`) |
| `GET /app/*`, `/auth/*` | `__spa-fallback.html`, `no-store`; the browser renders the page |
| `GET /admin/<ADMIN_ROUTE_KEY>`, and `/admin/<key>/*` with an admin session (else `302` to `/admin/<key>`) | The admin build's `index.html` (never the app's fallback) with 200, `no-store`, `no-referrer`, `noindex` (`Pages.ServeAdmin`), and the assets pass cookie. A wrong key, admin off, or an address outside `ADMIN_ALLOWED_IPS` is the 404 below; under `ADMIN_ROUTE_KEY_PREVIOUS` only inner pages, with a session made under it ([admin.md](admin.md#pages)) |
| `GET /admin-assets/*` with a valid assets pass, from an allowed address | The admin build's file, `Cache-Control: private, max-age=28800, immutable`, as long as the pass lasts (`Pages.ServeAdminFile`). Without one, an unknown file, HTML or a dot-file: the 404 below ([admin.md](admin.md#the-assets-pass)) |
| any other `GET` | `__spa-fallback.html` with status 404 (the app shows its not-found page) |
| other methods outside `/api` | 405 |

`internal/web` hashes each HTML file's inline scripts once, in `web.New`, and sends that file's CSP with it ([auth.md](auth.md#transport-and-headers)). Dot-files (`.vite/`) and `.html` paths aren't served directly. If the session check's database query fails, the page is served anyway and its own session check shows the outage page.

Without a web build, the server serves the API only in development (Vite serves the pages) and refuses to start with `APP_ENV=production`. With a web build but no admin build, and `ADMIN_ROUTE_KEY` set, it refuses to start everywhere (`web.ErrAdminNotBuilt`). `make dev` rebuilds `dist/` before starting, so the API on :8080 serves the pages as they were at startup. `make api` alone compiles whatever `dist/` holds from the last build. In development, browse :3000 either way.

## Environment

| Variable | Default | Notes |
| --- | --- | --- |
| `DATABASE_URL` | (required) | Production requires `sslmode=verify-full` |
| `APP_ORIGIN` | `http://localhost:3000` | Exact origin allowed for mutations; HTTPS in production |
| `API_ADDR` | `127.0.0.1:8080` | Listen address (loopback by default); `make serve` uses `127.0.0.1:3000` |
| `API_URL` | `http://127.0.0.1:8080` | Only for Vite's dev proxy (`make dev`, `make web`) |
| `WEB_BUILD` | `app` | Only for building the web app: `admin` builds the admin dashboard instead (`scripts/build-web.sh` runs both). Anything else fails the build |
| `MAIL_DRIVER` | `smtp` | `smtp` sends through `SMTP_HOST` (Mailpit locally); `log` writes email to `MAIL_FILE`; `off` sends nothing, so password reset and invitations answer 503. Production refuses `log` ([mail.md](mail.md)) |
| `MAIL_FROM_ADDRESS`, `MAIL_FROM_NAME` | (required for `smtp`), `APP_NAME` | The sender: a bare address, and its display name |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_TLS` | (required), 587/465, `starttls` | `SMTP_TLS` is `starttls`, `tls` or `none` (Mailpit only; refused in production and with credentials). `SMTP_PORT` is required with `none` ([mail.md](mail.md#smtp)) |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | (empty) | Both or neither; only over TLS. Never logged |
| `EMAIL_VERIFICATION` | `on` | `on`: a sign-up enters a 6-digit code emailed to its address before the account exists, and unverified accounts can verify from Account settings. `off`: sign-up as before. Refused with `MAIL_DRIVER=off` (it sends codes) ([auth.md](auth.md#email-verification)) |
| `EMAIL_VERIFICATION_EXISTING` | `optional` | `required`: an account that never verified its email (made while verification was off) must verify after signing in before anything else; the API refuses it every other route. Needs `EMAIL_VERIFICATION=on` |
| `RATE_LIMITS` | `on` | `off` switches every rate limit off, for the end-to-end tests; quotas stay. Production refuses `off` ([rate-limits.md](rate-limits.md#switching-limits-off-for-tests-rate_limits)) |
| `CLIENT_IP_HEADER` | (unset) | The header a proxy puts the client's address in: `X-Forwarded-For`, `X-Real-IP`, `CF-Connecting-IP` or `True-Client-IP`. Believed only on connections from `TRUSTED_PROXIES`; unset uses the connection's address ([deployment](../deployment.md#behind-a-proxy-or-load-balancer)) |
| `TRUSTED_PROXIES` | (unset) | Comma-separated addresses or CIDR ranges the proxy connects from. Set with `CLIENT_IP_HEADER` or not at all; `0.0.0.0/0` and `::/0` are refused |
| `MAIL_FILE` | `tmp/mail.log` | The `log` driver's mailbox. `make` sets it to `<repo>/tmp/mail.log`, so every target uses one file |
| `APP_KEY` | (required) | 32 random bytes in base64. `make app-key` writes one into `.env` when it has none (never replacing one); `go run ./cmd/admin app-key generate` only prints one. An `.env` still saying `ENCRYPTION_KEY` gets a message to rename it. Seals two-factor secrets, keys recovery-code and emailed-code digests and signs the admin assets pass, so every server on one database needs the same one. Production refuses `.env.example`'s. **Changing it locks out every account with MFA on** (and ends codes in flight). The e2e servers share one per run (`E2E_APP_KEY`) |
| `ADMIN_ROUTE_KEY` | (unset: admin off) | The admin dashboard's secret path segment and `X-Admin-Key`: a UUIDv4 or 32–128 characters of `A–Z a–z 0–9 _ -`; `go run ./cmd/admin route-key generate` prints one. Production refuses `.env.example`'s. Rotating it signs every admin out, unless the old one is set as `ADMIN_ROUTE_KEY_PREVIOUS`. It hides the door; admins still need their password and a code ([admin.md](admin.md)) |
| `ADMIN_ROUTE_KEY_PREVIOUS` | (unset) | After a rotation, the old key: admin sessions made under it keep working until they end (8 hours at most), but it never opens the sign-in. Same format; refused without `ADMIN_ROUTE_KEY` or equal to it. Never set a key that may have leaked ([admin.md](admin.md#rotating-without-signing-out-admin_route_key_previous)) |
| `ADMIN_ALLOWED_IPS` | (unset: any) | Comma-separated addresses or CIDR ranges the admin dashboard answers; the right key from anywhere else is a 404, recorded as `admin.access_refused`. Parsed like `TRUSTED_PROXIES` (`0.0.0.0/0` and `::/0` refused). Behind a proxy it needs `CLIENT_IP_HEADER`; production warns at startup without it ([admin.md](admin.md#the-address-allowlist-admin_allowed_ips)) |
| `LOG_FORMAT` | `json` in production, `text` otherwise | One JSON object per line, or slog's `key=value` text ([observability.md](observability.md#logs)) |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `ACCESS_LOG` | `all` | A line per request (`all`), only failures and requests over a second (`errors`), or none (`off`) |
| `SHUTDOWN_DELAY` | `5s` in production, `0s` otherwise | How long a stopping server keeps serving with readiness already 503 before it drains; 0 to 60 s ([observability.md](observability.md#health)) |
| `METRICS_STORE` | `on` | `off` writes no metrics to the database (`request_minutes`, `metric_minutes`); `/metrics`, logs, grouped errors and the heartbeat still work ([observability.md](observability.md#metrics-in-the-database)) |
| `METRICS_ADDR` | (unset: off) | A separate listener serving only `GET /metrics` in the Prometheus text format, such as `127.0.0.1:9090`; never the API's address. Production refuses one on every interface (`:9090`, `0.0.0.0`, `::`, a host name) without `METRICS_TOKEN` ([observability.md](observability.md#metrics-for-an-external-system)) |
| `METRICS_TOKEN` | (unset) | When set (32 characters at least), `/metrics` requires `Authorization: Bearer <token>`, compared in constant time. Never logged or stored |
| `SLOW_QUERY_MS` | `500` | A query slower than this is logged at warn, without its text or values; `0` logs none; at most 60000 ([observability.md](observability.md#slow-queries)) |
| `APP_NAME` | `lewcontext` | The product name: baked into the web build, and read by the server for emails, which open with it ([mail.md](mail.md#templates)) |
| `APP_ENV` | (development) | `production` enables Secure `__Host-` cookies, HSTS and stricter config |
| `DEMO_PASSWORD` | | Only for `make seed` seeders that create accounts (1–128 bytes; `password` locally) |

`make` loads the root `.env` and exports it, and its values win over variables set on the command line. To point a command at another database, run the Go command directly. Tests don't use `.env`: they use the committed `.env.testing` ([../testing.md](../testing.md#configuration)).

## More

[endpoints.md](endpoints.md) · [auth.md](auth.md) · [admin.md](admin.md) · [rate-limits.md](rate-limits.md) · [observability.md](observability.md) · [mail.md](mail.md) · [pagination.md](pagination.md) · [database.md](database.md) · [seeding.md](seeding.md) · [testing.md](testing.md)
