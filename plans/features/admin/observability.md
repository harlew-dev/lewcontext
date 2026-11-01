# Observability: logs, request IDs, errors, metrics

Status: **phase 1 of 3 implemented 2026-11-01**: logs, request IDs, panic recovery and health (§1, 2, 3, 8; see [How the build differs](#how-the-build-differs)). Phase 2 (grouped errors, §4 and §9) and phase 3 (metrics, server instances, slow queries and `/metrics`, §5 to 7) are still to build. Every decision below was confirmed by the user. Agreed 2026-10-31: build it, keep it fast, and never store a row per request (the two rules below). Plan A3 of the [admin plans](README.md). Docs it will change: [api/README.md](../../../docs/api/README.md) (packages, request flow, environment), [deployment.md](../../../docs/deployment.md) (logging, metrics, uptime checks), [database.md](../../../docs/api/database.md), [events.md](../../../docs/api/events.md), [endpoints.md](../../../docs/api/endpoints.md), [rate-limits.md](../../../docs/api/rate-limits.md), [ui/api.md](../../../docs/ui/api.md), [api/testing.md](../../../docs/api/testing.md); a new `docs/api/observability.md`.

## Why

Today the server writes `slog`'s default text to stderr, a handful of lines at startup and on failures. There's no line per request, no way to tie a user's "something went wrong" to a log line, no count of errors or of slow routes, and a panic in a handler is caught only by `net/http`'s default (a stack on stderr, the connection dropped). The events table answers *who did what*; nothing answers *is the server healthy and fast*.

This plan adds the signals every production service needs, kept on our own servers and in our own database, so that the [monitoring](monitoring.md) pages can show them and [alerts](alerts.md) can watch them. Everything is also exposed in standard forms (JSON logs, Prometheus text) so an operator can send it to whatever they already use.

## Two rules: fast, and bounded

1. **No request is ever written to the database.** A request's line goes to the log (stdout), never to a table. The database gets only per-minute **sums per route** and grouped errors, so what it stores depends on how many routes we have, not how much traffic we get: ten requests a minute or ten million write the same number of rows ([what reaches the database](#what-reaches-the-database-and-how-much)).
2. **Nothing slows a request.** Per request the cost is one 16-byte random read (the request ID), a few atomic adds (the metrics) and one log line. Database writes happen off the request path, batched once a minute (metrics) or every 10 seconds (errors, only when there are any). `BenchmarkRequestOverhead` measures the middleware; the budget is **20 µs and 3 allocations per request**, written in `docs/api/observability.md`, and a change that exceeds it needs a reason in review.

## 1. Structured logs

| Variable | Default | Notes |
| --- | --- | --- |
| `LOG_FORMAT` | `json` in production, `text` otherwise | `json` is one object per line, for any log collector |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `ACCESS_LOG` | `all` | `all` (a line per request), `errors` (only 4xx, 5xx and requests over 1 s), or `off`. Cuts log volume where the log store charges by size; metrics are counted either way (decision 10) |

- **One handler**, built in `internal/observe` (new package, below) and installed by every `cmd/*` with `slog.SetDefault`.
- **The request's ID on every line logged during a request:** the handler reads it from the context, so request code uses `slog.InfoContext(r.Context(), …)` and friends. `internalError(w, err)` becomes `internalError(w, r, err)`.
- **One access line per request** (`msg: "request"`): `requestId`, `method`, `route`, `status`, `durationMs`, `bytes`, `user` (the `usr_` ID when signed in, `admin` for admin sessions), `ip`.
  - **`route` is the matched pattern** (`r.Pattern`: `GET /api/workspaces/{workspace}/projects/{project}`), or a fixed label for pages (`page /app/*`, `page /admin/*`, `asset`, `page 404`). **Never the raw path or query:** reset and invitation pages carry tokens in their paths, admin pages carry the key, and `q` carries what people searched for.
  - `GET /api/health*` logs at `debug`, so probes don't drown the log.
- **Redaction as a backstop:** a `ReplaceAttr` drops the value of any attribute whose key contains `password`, `token`, `secret`, `cookie`, `authorization`, `code` or `key` (except `requestId` and the known-safe keys), replacing it with `[redacted]`. Code shouldn't log those at all; this catches a mistake.

## 2. Request IDs (decision 3)

- **Made in `clientAddress`** (the outermost middleware, so pages get one too): `req_` + 14 base62 from `crypto/rand`.
- **An inbound `X-Request-ID` is never our ID.** A trusted proxy's, matching `^[A-Za-z0-9._-]{1,64}$`, is logged on the access line as `proxyRequestId`, so a proxy's log still ties to ours, and a client can't choose our ID. (The plan first used a trusted proxy's ID as ours; changed after the phase-1 security review: see [How the build differs](#how-the-build-differs).)
- **Sent back** as `X-Request-ID` on every response.
- **In 500 responses:** `{"error": "Something went wrong.", "requestId": "req_…"}`. The web app's error notice and outage page show "Reference: req_…", so a user can quote it and support can find the log line.
- **On events:** a new `events.request_id text NOT NULL DEFAULT ''` column (edited into `007`), set like `user_agent`. An audit row then leads straight to its log lines.

## 3. Panic recovery

`recoverPanics`, just inside `security`: recovers a handler panic, logs it at `error` with the request ID and the stack, hands it to the error collector as `source: panic`, and answers the generic 500 if nothing was written yet. `http.ErrAbortHandler` is re-panicked, as `net/http` expects. The event worker already recovers listener panics; they go to the collector too.

## 4. Grouped errors: `app_errors` (decision 4)

Every `internalError`, panic, final listener failure, failed cleanup run and failed page-gate query is reported to an in-memory **collector**, grouped by fingerprint, and flushed to the database every 10 seconds.

```sql
-- create_app_errors (a new migration)
CREATE TABLE app_errors (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 fingerprint text NOT NULL UNIQUE,     -- sha256 of source, route and the scrubbed message's shape
 source text NOT NULL CHECK (source IN ('request', 'panic', 'worker', 'background', 'client')),
 route text NOT NULL DEFAULT '',       -- the route pattern, listener name or job name
 message text NOT NULL CHECK (length(message) <= 1000),  -- scrubbed
 stack text NOT NULL DEFAULT '' CHECK (length(stack) <= 8000),  -- function names and lines only
 count bigint NOT NULL DEFAULT 1,
 first_seen_at timestamptz NOT NULL,
 last_seen_at timestamptz NOT NULL,
 last_request_id text NOT NULL DEFAULT '',
 last_version text NOT NULL DEFAULT '',
 resolved_at timestamptz               -- set by an admin (actions plan); a new occurrence clears it
);
CREATE INDEX app_errors_last_seen_idx ON app_errors(last_seen_at DESC);
```

- **Scrubbed before grouping and storing:** email addresses, IP addresses, public IDs (`usr_…`), numbers, UUIDs and quoted strings become placeholders (`<email>`, `<ip>`, `<id>`, `<n>`, `'…'`). Two failures differing only in which user they hit are one group, and the stored message holds no personal data. Constraint and table names survive, since they say what broke.
- **Never stored:** request bodies, headers, query strings, cookies, SQL with values.
- **Never in the request's way:** reporting is a map update under a mutex. The flush is one upsert per fingerprint (`count = count + n`). If the database is what's failing, the flush fails, the collector keeps at most 1,000 groups (dropping the stalest), and the log is still complete.
- **Regressions:** a new occurrence of a resolved group clears `resolved_at`, so it shows as open again.
- **Pruned:** a `make prune` target, `app_errors`, deletes groups not seen for 30 days.

## 5. Metrics (decisions 5, 6)

An in-memory registry in `internal/observe`: counters, gauges and fixed-bucket histograms, lock-free on the hot path (atomic adds). **Labels are bounded by construction**: route patterns, limit names, listener names, status classes. Never paths, IDs, emails or IPs, which would leak data and let anyone grow the series count.

| Metric | Kind | Labels | Where |
| --- | --- | --- | --- |
| `http_requests_total` | counter | `route`, `status` (`2xx`…`5xx`) | the access-log middleware; stored as `request_minutes` |
| `http_request_duration_ms` | histogram (5 … 10,000 ms) | `route` | same |
| `http_timeouts_total` | counter | `route` | requests ended by the 10 s timeout: on a write, the change may have committed even though the client saw an error. Stored in `request_minutes.timeouts` |
| `errors_total` | counter | `source`, `route` | the error collector, per occurrence (the per-window counts on the Errors page) |
| `rate_limit_refused_total` | counter | `limit` (`auth:per-ip:block`) | `allow`, **every** refusal (the event is written only for the first in a window) |
| `quota_refused_total` | counter | `quota` | handlers mapping `QuotaError` |
| `db_query_duration_ms` | histogram | `operation` (`select`, `insert`, `update`, `delete`) | a Bun query hook |
| `db_slow_queries_total` | counter | `table` | same, over `SLOW_QUERY_MS` |
| `db_pool_*` | gauges | | `sql.DBStats`: open, in use, idle, wait count, wait time |
| `events_recorded_total` | counter | `name` | `record` |
| `deliveries_total` | counter | `listener`, `outcome` (`delivered`, `retried`, `failed`) | the worker |
| `delivery_lag_ms` | histogram | `listener` | event created → claimed |
| `deliveries_pending` | gauge | | the worker's claim loop, once a minute |
| `password_hash_wait_ms` | histogram | | waiting for one of the 4 hashing slots |
| `go_*` | gauges | | `runtime/metrics`: goroutines, heap, GC pauses |
| `build_info` | gauge (1) | `version`, `go` | startup |

### Shared across servers: per-minute sums

Each server's registry lives in its own memory. For a dashboard that covers every server, each one writes **the last minute's sums** to the database once a minute, in one multi-row `INSERT`:

```sql
-- create_request_minutes (a new migration, with request_hours)
CREATE TABLE request_minutes (
 minute timestamptz NOT NULL,          -- truncated to the minute
 server text NOT NULL,                 -- server_instances.id
 route text NOT NULL,                  -- the route pattern or page label
 requests integer NOT NULL,
 status_4xx integer NOT NULL DEFAULT 0,
 status_429 integer NOT NULL DEFAULT 0,
 status_5xx integer NOT NULL DEFAULT 0,
 timeouts integer NOT NULL DEFAULT 0,
 duration_sum_ms double precision NOT NULL,
 duration_buckets integer[] NOT NULL,  -- counts per fixed boundary: 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000 ms, more
 PRIMARY KEY (minute, route, server)
);
CREATE TABLE request_hours (LIKE request_minutes INCLUDING ALL);  -- per hour, all servers summed (server = '')

-- create_metric_minutes (a new migration, with metric_hours): every other series in the table above
CREATE TABLE metric_minutes (
 minute timestamptz NOT NULL,
 server text NOT NULL,
 metric text NOT NULL,
 labels text NOT NULL DEFAULT '',      -- canonical, sorted: "limit=auth:per-ip:block"
 count bigint NOT NULL,                -- counter delta; histogram observations; 1 for a gauge
 sum double precision NOT NULL DEFAULT 0,  -- histogram sum; a gauge's value
 buckets integer[],                    -- histogram bucket counts
 PRIMARY KEY (minute, metric, labels, server)
);
CREATE TABLE metric_hours (LIKE metric_minutes INCLUDING ALL);
```

- **One row per route per minute,** carrying its request count, status counts and latency buckets together, rather than a row per metric and status. Routes that had no traffic that minute write nothing.
- **Insert only:** a server writes each minute once, so there are no updates and no dead rows to vacuum. If the write fails (the database is down), that minute is dropped from the dashboard; the counters stay in memory for `/metrics`, and nothing retries or queues.
- **Kept short by the server itself:** once an hour, one server (holding an advisory lock) sums the past hour into `request_hours` and `metric_hours` and deletes minute rows older than 48 hours, in batches. The minute tables can't grow without bound even if nobody schedules `prune`. The hour tables are a prune target (90 days).
- Percentiles (p50, p95, p99) are read from the summed buckets.

### What reaches the database, and how much

| Write | How often | Rows | Kept |
| --- | --- | --- | --- |
| `request_minutes` | one statement per server per minute | one per route with traffic: **at most ~50** (the number of route patterns and page labels) | 48 hours |
| `metric_minutes` | the same statement batch | one per other series that changed: **at most ~40** | 48 hours |
| `request_hours`, `metric_hours` | once an hour, one server | at most ~90 an hour in all | 90 days |
| `app_errors` | every 10 s, **only when something failed** | one upsert per distinct error group | 30 days after last seen |
| `server_instances` | once a minute | one update per server | a day after it stops |
| `events.request_id` | no new rows: a column on events already being written | 0 | the event's |

**Ceiling per server:** about 90 rows a minute, so at most ~130,000 a day and ~260,000 kept (roughly 50 MB) at any time, **whatever the traffic**. The hour tables stay under ~200,000 rows in all. A test pins the first part: a flush after 10,000 requests to one route writes one `request_minutes` row.

**Switching it off:** `METRICS_STORE=off` (decision 11) stops every metric write. The Performance page and the latency and 5xx tiles then say "Metric storage is off", and the alerts built on them can't fire; logs, grouped errors and `/metrics` still work.

### `/metrics` for an external system

| Variable | Default | Notes |
| --- | --- | --- |
| `METRICS_ADDR` | (unset: off) | A separate listener serving only `GET /metrics`, in Prometheus text format, from this process's registry. E.g. `127.0.0.1:9090` |
| `METRICS_TOKEN` | (unset) | When set, `/metrics` requires `Authorization: Bearer <token>`. Production refuses `METRICS_ADDR` on `0.0.0.0` or `::` without it |

- **Its own server,** never on the public port, so the public site can't be scraped and the listener can be firewalled on its own.
- **Hand-written exposition** (the text format is small); a golden-file test pins it. No new dependency (decision 6).

## 6. Servers and versions: `server_instances`

```sql
-- create_server_instances (a new migration)
CREATE TABLE server_instances (
 id text PRIMARY KEY,                  -- srv_ + 14 base62, new at every start
 hostname text NOT NULL,
 version text NOT NULL,                -- the VCS revision from debug.ReadBuildInfo, or "dev"
 started_at timestamptz NOT NULL,
 last_seen_at timestamptz NOT NULL,
 config jsonb NOT NULL                 -- the non-secret configuration summary, below
);
```

- **A heartbeat** with each minute's metric flush. The cleanup loop deletes rows silent for a day. A server silent for 2 minutes is shown as gone.
- **The configuration summary:** `APP_ENV`, `MAIL_DRIVER` and SMTP host/port/TLS, `RATE_LIMITS`, whether `CLIENT_IP_HEADER`, `TRUSTED_PROXIES` (count), `ADMIN_ROUTE_KEY`, `ADMIN_ROUTE_KEY_PREVIOUS`, `ADMIN_ALLOWED_IPS` (count) are set, `LOG_FORMAT`, `METRICS_ADDR` set, the migrations applied vs embedded. **Never a value of a secret,** not even a digest of one: a test builds the summary from a config full of known secrets and checks none appear.
- **Events:** `server.started` (`server`, `version`) and `server.stopped` (`server`, `reason`), recorded at startup and graceful shutdown. A release, a crash loop (starts without stops) and a configuration change are all visible in the trail.

## 7. Slow queries

A Bun `QueryHook` times every query. Over `SLOW_QUERY_MS` (default 500) it logs at `warn`: `requestId`, `operation`, `table`, `durationMs`, and the calling store method (from the stack, only when slow). **It never logs `event.Query`,** which Bun formats with the values inlined (emails, digests). `pg_stat_statements`, where the database has it, is read by the [monitoring](monitoring.md) Performance page instead.

## 8. Health

| Route | Answers | For |
| --- | --- | --- |
| `GET /api/health/live` (new) | 200 while the process runs; no database | the orchestrator's liveness probe: a database outage shouldn't restart every server |
| `GET /api/health` | 200 when the database answers; **503 once shutdown has begun** (new), so the load balancer stops sending traffic during the 10 s drain | readiness and the load balancer |

**External uptime checks** aren't built here, but [deployment.md](../../../docs/deployment.md) will say what to set up, because nothing inside the app can report the app being down: `GET /api/health` and `/` every minute from two or more regions, a page to someone after two failures, and a check on the TLS certificate's expiry.

## 9. Errors in the browser (decision 7)

`POST /api/client-errors` (signed in only, `requireSession`'s budget, `client-error:per-user` 30 / hour, body ≤ 4 KiB: `message`, `stack`, `route` pattern, `release`). The web app's root error boundary and `window.onerror` / `unhandledrejection` send one per distinct error per page load. Scrubbed and grouped like server errors, as `source: client`. Signed out, nothing is sent: an anonymous write endpoint would be a free way to fill a table.

## Package: `internal/observe`

The log handler, request IDs, the metric registry, the error collector and scrubber, the minute flush and the hourly rollup. `httpapi` calls into it; `cmd/server` starts its loops; `store` owns its queries (`FlushMetrics`, `RollupMetrics`, `ReportErrors`, `TouchServerInstance`).

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| Tokens or the admin key in logs | `route` is the pattern, never the path; no query strings; the redacting `ReplaceAttr`; a test sends requests to a reset page, an invitation page and `/admin/<key>` and checks no log line contains the token or key |
| Personal data in error groups | The scrubber, with tests feeding errors that contain emails, IPs, IDs and tokens |
| SQL values in logs | The slow-query hook logs operation and table only |
| Metric labels as a memory or privacy leak | Labels come only from fixed sets; an unmatched route is `unmatched` |
| `/metrics` reachable from the internet | A separate listener, off by default, a token required on a public bind in production |
| A spoofed request ID | Believed only from trusted proxies, with a strict format |
| The error table flooded by one failing route | Grouping: one row per fingerprint; browser errors per-user limited and signed-in only |
| Monitoring slowing requests or the database | Atomic counters; one batched write per server per 10 s (errors) and per minute (metrics); bounded memory |
| Config secrets on the dashboard | The summary lists which settings are set, never values; a test checks |

## Tests

- **Benchmark:** `BenchmarkRequestOverhead` (request ID, metrics, access line to a discarding writer) against the 20 µs / 3 allocations budget.
- **Unit:** JSON and text handlers carry `requestId`; redaction; request ID format and the trusted-proxy rule; the scrubber; fingerprints equal for errors differing only in IDs; histogram buckets and percentiles; Prometheus output against a golden file; the config summary holds no secret.
- **Integration:** one access line per request with the pattern as `route`, for API and pages, never a token or key; a 500 carries `requestId` and the same ID is on the log line and on any event the request wrote; a handler panic gives a 500 JSON and one `app_errors` row; repeated errors increment one row; a flush after 10,000 requests to one route writes one `request_minutes` row; a failed flush drops the minute without retrying; hourly rollup (two simulated servers summed; minutes older than 48 h deleted); `METRICS_STORE=off` writes nothing; `ACCESS_LOG=errors` logs only failures and slow requests; heartbeat rows; `server.started`; readiness 503 during shutdown; `/metrics` with and without the token; `client-errors` limits and refuses signed-out requests.
- **Vitest:** the error notice shows the reference; the error boundary reports once.

## How the build differs

### Phase 1 (2026-11-01): logs, request IDs, panic recovery, health

Docs updated: the new [observability.md](../../../docs/api/observability.md), [api/README.md](../../../docs/api/README.md), [endpoints.md](../../../docs/api/endpoints.md), [events.md](../../../docs/api/events.md), [database.md](../../../docs/api/database.md), [auth.md](../../../docs/api/auth.md), [admin.md](../../../docs/api/admin.md), [rate-limits.md](../../../docs/api/rate-limits.md), [api/testing.md](../../../docs/api/testing.md), [deployment.md](../../../docs/deployment.md) (logs, health checks, uptime checks), [testing.md](../../../docs/testing.md), [ui/api.md](../../../docs/ui/api.md), [ui/design.md](../../../docs/ui/design.md), [ui/pages.md](../../../docs/ui/pages.md), [ui/testing.md](../../../docs/ui/testing.md), [docs/README.md](../../../docs/README.md), `.env.example` and AGENTS.md (the docs table, `internal/observe` in rule 5, and a logging invariant).

- **Names:** the middleware were already exported, so it's `ClientAddress`, `InternalError(w, r, err)` and `RecoverPanics` (the router is another package). The phase-2 hook is `observe.Report(ctx, observe.Problem)` with a process-wide `observe.SetReporter` (like `slog.SetDefault`), since `InternalError` is a function, not a method: sources `request`, `panic` and `worker`. Nothing is set yet.
- **`durationMs` is whole milliseconds,** rounded. slog's JSON handler writes a float through `encoding/json`, two more allocations per request. For the same reason the handler writes the level as a string.
- **The budget is met:** `ClientAddress` went from 6 allocations and 0.54 µs to 8 and 4.0–5.7 µs on this machine (an i5-6400T, with other work running): 3 new (the request's record, its ID, the access line's attributes) and 1 gone (the client address is held in the record, no longer boxed into the context). The request's ID and IP reach events in one context layer, `events.WithRequest` (which replaced `events.WithIP`), set in `ClientAddress` rather than in `security` beside the user agent.
- **Route labels:** besides the plan's, `page /auth/*` (the sign-in and link pages, which carry tokens), `admin asset` (`/admin-assets/…` with a pass), `page /` for the prerendered home page (`page <path>` from the build), and `unmatched` for API and page 404s and 405s. A wrong admin key is `page 404`, as it answers. The API's label is the pattern `ServeMux` matched (read in `unknownRoutesAsJSON`), except for a `CONNECT`, whose "pattern" `ServeMux` can make from the path it sent.
- **`ACCESS_LOG=errors`** also keeps aborted requests (`aborted: true`).
- **`method`** is one of the methods HTTP defines, or `OTHER` (`observe.Method`), so a client can't fill a line with an invented one; phase 3 can use it as a label.
- **Redaction's key parts** grew after the review: `pass`, `session`, `csrf`, `otp`, `mfa`, `digest`, `hash` and `email` joined the plan's list. It is broad on purpose (`passed`, `bypass` are redacted too); every key the code logs is checked in `TestSensitiveKeys`, and the safe ones allow-listed.
- **Not covered:** `net/http`'s own 400 and 431 answers (a request line or headers it can't read, or over 16 KiB) come before any handler, so they have no `X-Request-ID` and no access line. Lines are written synchronously to stdout, so a blocking log driver slows requests; deployment.md recommends a non-blocking one.
- **Redaction** also checks the names of the groups around an attribute, and drops a value that is a request, its headers or cookies, a URL or a query, whatever its key; the access line's keys and `requestId` are allow-listed.
- **Request IDs are always ours: a deliberate change to decision 3**, made after the security review. Cloudflare, cloud load balancers and nginx by default pass a client's own `X-Request-ID` on unchanged, so "believed only from `TRUSTED_PROXIES`" would in practice let a client choose its ID, and put a victim's on its own access lines and on `events.request_id`, muddying the audit trail. A trusted proxy's well-formed ID (sent once) is logged only on the access line, as `proxyRequestId`, which keeps the correlation the decision wanted; it is never `requestId`, returned, or stored. The `events.request_id` column's `CHECK` is therefore our shape only: empty or `req_` and 14 base62. `admin.access_refused`, written later by its worker, carries the refused request's ID, and so does the worker's log line. The admin dashboard's event page doesn't show the column yet (A4).
- **Panic values** are logged, reported and (for listeners) stored only through `observe.PanicValue`: a request, headers, cookies or URL are `[redacted]`, a string or error cut to 512 bytes, anything else only its type. A value formatted before the handler sees it would skip its redaction (`panic(r)` printed the token, `X-Admin-Key` and cookie). `ClientAddress` also recovers what gets past `RecoverPanics` (itself, `Security`, the dispatch, the access line), the same way, so nothing reaches `net/http`'s own unredacted panic log. `observe.Request` implements `Flush`, recording the implicit 200, so a flushed answer counts as started; an aborted request's line says `aborted: true`, kept under `ACCESS_LOG=errors`.
- **Panics:** `RecoverPanics` wraps the pages too, not only the API. Headers a handler set before panicking (a cookie) are dropped and the API's security headers set again. A panic once the answer has started can't be answered, so the connection is cut (`panic(http.ErrAbortHandler)`), rather than the client getting a half-written body that looks whole. The 500's message is the existing "Something went wrong. Please try again.".
- **Readiness** says why: `Database unavailable` or `Shutting down` (both 503). `cmd/server` calls `Server.Drain`, then keeps serving for **`SHUTDOWN_DELAY`** (new: 5 s by default in production, 0 elsewhere, 0 to 60 s) before `http.Server.Shutdown`, which closes the listener at once; without the wait a load balancer would see refused connections rather than the 503. A platform's own pre-stop hook can do it instead, with `SHUTDOWN_DELAY=0s`. Docker's stop timeout needs the delay plus 15 s.
- **Commands run by hand** (`migrate`, `seed`, `prune`, `admin`) install the handler on stderr but keep their own output (`log.Printf`, `log.Fatal`) as plain lines (`observe.UseForCommand`): `slog.SetDefault` would otherwise route it through the handler at info, where `LOG_LEVEL=warn` would hide why a command stopped. The server sends `net/http`'s own error log through the handler as warnings.
- **For phase 2: error text can hold personal data.** PostgreSQL type errors echo the value they couldn't read (Bun writes values into the query), `database/sql` scan errors quote one, and an SMTP refusal names the recipient. Phase 1 logs `error` values as they are (as before); the docs say so. Phase 2's scrubber should scrub `error` values **before they're logged**, not only before they're stored in `app_errors`, and map a client's `context canceled` (seen in e2e as 500s) to something that isn't an error. `pending_registrations.request_id` (an event's `evt_` ID) is unrelated to `events.request_id`; documented rather than renamed, as other work touches registrations.
- **Lines that had no request ID now do:** every `slog` call made during a request uses its context, the TOTP one included (`SecondFactor` takes the context).
- **The web app** shows the reference under the error notice (`ErrorNotice`, any `useAction` failure) and on the outage page (loaders answer `unavailable(error)`, and the root error boundary reads the thrown error), as the plan says. A load failure's `Notice` with Retry (`useResource`) and an action's error toast don't show one yet.

## Decisions (to confirm)

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Log format | JSON by default in production, text in development, `LOG_FORMAT` to override |
| 2 | The client IP in access logs | Logged: it's what ties abuse to a source. [deployment.md](../../../docs/deployment.md) recommends keeping logs 30 days at most; [compliance](compliance.md) lists it in the data map |
| 3 | Request IDs | Made per request, believed from trusted proxies only, returned in `X-Request-ID` and in 500 bodies, shown in the web app, stored on events |
| 4 | Where errors are grouped | In our database (`app_errors`), not Sentry or similar: no personal data goes to another processor and no new dependency. JSON logs let an operator add any log-based tool |
| 5 | Metrics storage | Never a row per request. One row per route per minute per server (insert only), kept 48 hours by the server itself; hourly sums kept 90 days through `prune`. About 90 rows a minute per server at most, whatever the traffic |
| 6 | Exposing metrics | Prometheus text on a separate, opt-in listener, hand-written (no `client_golang`), with an optional bearer token required on a public bind |
| 7 | Browser errors | Captured for signed-in users only, rate limited, grouped with server errors |
| 8 | Health | Split liveness (`/api/health/live`) from readiness (`/api/health`), and fail readiness while draining |
| 9 | Servers and versions | `server_instances` heartbeats and `server.started` / `server.stopped` events |
| 10 | Access log volume | `ACCESS_LOG=all` by default, with `errors` and `off` for operators whose log store charges by size |
| 11 | A switch for metric storage | `METRICS_STORE=on` by default; `off` stops every metric write, and the pages say so |

## Not included

- **Distributed tracing** (OpenTelemetry). Request IDs cover correlation in a single-process app; OTLP export is the next step if we split services.
- **Shipping logs** anywhere: the operator's log driver does that (Docker → Loki, CloudWatch, Datadog …). [compliance](compliance.md) asks that audit lines are among them.
- **`pprof` endpoints.** If wanted later, only on the metrics listener, behind its token.
- **Real-user performance metrics** (web vitals) from browsers.
- **An APM vendor** integration.
