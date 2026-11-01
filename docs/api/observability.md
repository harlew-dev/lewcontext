# Observability

Code: `internal/observe` (the log handler, request IDs, what an access line reports, the hook for grouped errors), `internal/httpapi/client_ip.go` (`ClientAddress`) and `observability.go` (the request's ID, the access line, `RecoverPanics`, readiness), `internal/config/logging.go`. Plan: [observability](../../plans/features/admin/observability.md) (A3), whose first phase this is: logs, request IDs, panic recovery and health. Grouped errors (`app_errors`), metrics and `/metrics` come with its later phases.

How the server says what it's doing, without ever saying what a request carried. Two rules from the plan: **nothing slows a request** (the [budget](#the-budget)), and **no request is written to the database** (a request's line goes to the log, never a table).

## Logs

Every command installs one handler (`observe.NewLogger`) with `slog.SetDefault` before anything else, reading `LOG_FORMAT` and `LOG_LEVEL` first (`config.LoadLogging`), so even a refused configuration is logged as asked.

| Variable | Default | Notes |
| --- | --- | --- |
| `LOG_FORMAT` | `json` in production, `text` otherwise | `json`: one object per line (`time`, `level`, `msg`, then the attributes), for any log collector. `text`: slog's `key=value` lines |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`: the least severe level written |
| `ACCESS_LOG` | `all` | `all`: a line per request; `errors`: only 4xx and 5xx answers, requests over a second and requests aborted before any answer; `off`: none. Anything else refuses to start |

- **Where:** the server writes to stdout, and sends `net/http`'s own complaints (a broken connection) through the same handler as warnings. The commands run by hand (`migrate`, `seed`, `prune`, `admin`) log through it to stderr, but keep their own output (previews, results, the reason they stopped) as plain lines (`observe.UseForCommand`), so `LOG_LEVEL=warn` can't hide why a command failed.
- **The request's ID on every line logged during a request:** request code logs with the context's functions, `slog.ErrorContext(r.Context(), …)`, and the handler adds `requestId` from the context. A line logged without the context has no ID. A logger made with `WithGroup` puts it inside the group.
- **Never log what a request carried:** its path, query string, headers, cookies or body. Paths hold reset, invitation and block-link tokens and the admin key; query strings hold searches and email addresses; headers hold `X-Admin-Key`, the CSRF token and cookies. Name the route, the record's public ID, or the error.
- **But an error's text can hold personal data.** A PostgreSQL type error echoes the value it couldn't read (Bun writes values into the query), a `database/sql` scan error quotes one, and an SMTP refusal names the recipient's address. `InternalError`, the worker and the startup checks log `error` values as they are, so the log can hold an email address or a value someone typed. Grouped errors (the plan's second phase) will scrub `error` values before they are logged and stored; until then, treat the log as personal data ([deployment.md](../deployment.md#logs-and-health-checks)).

### The access line

One line per request (`msg: "request"`), logged by `ClientAddress` once the answer is written:

| Field | Holds |
| --- | --- |
| `requestId` | The request's ID, always ours ([below](#request-ids)) |
| `method` | `GET`, `POST`, … one of the methods HTTP defines, or `OTHER` (`observe.Method`): a client can't fill the line with an invented one |
| `route` | **The pattern that matched**, `GET /api/workspaces/{workspace}/projects`, or a page's fixed label: `page /`, `page /app/*`, `page /auth/*`, `page /admin/*`, `asset`, `admin asset` (`/admin-assets/…` with a valid pass), `page 404`, or `unmatched` (no route; an API 404 or 405). **Never the path or the query.** A wrong admin key is `page 404`, as it answers |
| `status` | The status sent; `0` for a request aborted before any answer |
| `durationMs` | Whole milliseconds, rounded (slog's JSON handler would marshal a float through `encoding/json`, two allocations per request) |
| `bytes` | The body's size |
| `user` | The signed-in account's `usr_` ID, `admin` for an admin session (each admin read is recorded in the audit trail anyway), or empty |
| `ip` | `httpapi.ClientIP(r)`: the connection's address, or a trusted proxy's header ([auth.md](auth.md#rate-limits)) |
| `proxyRequestId` | Only when a trusted proxy sent its own `X-Request-ID` ([below](#request-ids)), to find the proxy's line for this request |
| `aborted` | Only when the request was cut off: `true`, with the status already written (or `0`). Kept under `ACCESS_LOG=errors` |

- **net/http's own refusals have no line and no ID.** A request line or headers it can't read (400) or too large (431, over 16 KiB) is answered by `net/http` before any handler runs, so it has no access line and no `X-Request-ID`.
- **The route is set as the request goes:** the API's `unknownRoutesAsJSON` names the pattern `ServeMux` matched (a `CONNECT` stays `unmatched`, since `ServeMux` hands back a path it sent for one), the page routes name their label (`pageRoute` in `router/routes.go`), and `internal/web` names what it served (`observe.SetRoute`). `RequireSession` and `RequireAdminSession` name the user (`observe.SetUser`).
- **Health checks** (`GET /api/health`, `/api/health/live`) log at `debug`, so probes don't fill the log at the default level.
- **Volume:** `ACCESS_LOG=errors` for a log store that charges by size. The client's IP is personal data: keep logs 30 days at most ([deployment.md](../deployment.md#logs-and-health-checks)).

### Redaction, the backstop

The handler's `ReplaceAttr` (`redact`) replaces with `[redacted]`:

- the value of any attribute whose key, or any group around it, contains `password`, `pass`, `token`, `secret`, `cookie`, `authorization`, `session`, `csrf`, `code`, `otp`, `mfa`, `key`, `digest`, `hash` or `email`, in any case (`csrfToken`, `X-Admin-Key`, `Set-Cookie`, `recoveryCode`, `password_hash`), however deeply nested, `With` attributes included. It is broad on purpose: `passed` or `bypass` are redacted too;
- a value that is an `*http.Request`, `http.Header`, `*http.Cookie`, `[]*http.Cookie`, `*url.URL`, `url.URL` or `url.Values`, whatever its key.

The access line's keys, the request IDs and a panic line's `panic` and `stack` are allow-listed (`safeKeys`), so a part can't blank them; a key that is safe despite holding a part goes there. It is a backstop, not permission: a secret inside a value logged under an innocent key (an error's message, a map) isn't caught, so don't log such values. Both formats escape what they write, so no value (a route, a proxy's request ID) can break a line or start a forged one.

## Request IDs

- **Made by `ClientAddress`**, the outermost middleware, so pages get one too: `req_` and 14 base62 characters from one 16-byte `crypto/rand` read (bytes 248 and above skipped, so every character is equally likely; about 83 bits).
- **Always ours.** Cloudflare, cloud load balancers and nginx by default pass a client's own `X-Request-ID` on unchanged, so an ID believed from a trusted proxy could be chosen by the client, who could then put someone else's ID on its own lines and events. So the ID is never taken from a request.
- **A proxy's own `X-Request-ID`** goes on the access line as `proxyRequestId`, for finding the proxy's line, and nowhere else: never as `requestId`, in a response or on an event. Only from a connection from `TRUSTED_PROXIES` (`clientip.FromTrustedProxy`), sent once, in the shape `^[A-Za-z0-9._-]{1,64}$` (`observe.ValidRequestID`), so nothing can break the line through it; anything else is ignored.
- **Sent back** in `X-Request-ID` on every response, pages and API (not on `net/http`'s own 400 and 431, [above](#the-access-line)).
- **In every 500:** `{"error": "Something went wrong. Please try again.", "requestId": "req_…"}`. The web app shows it as "Reference: req_…" under the error notice and on the outage page ([ui/api.md](../ui/api.md#changing-data)), so someone can quote it and support can find the log lines.
- **On events:** `events.request_id` (`text NOT NULL DEFAULT ''`, `CHECK`ed to be empty or `req_` and 14 base62), set from the context like `user_agent` (`events.WithRequest`, with the IP); empty for the CLI and listeners. An `admin.access_refused` written later by the refusal worker carries the refused request's ID, and so does its log line. An audit row leads straight to its log lines ([events.md](events.md#rows)).

## Errors and panics

- **`httpapi.InternalError(w, r, err)`** answers the generic 500 with the request's ID, after logging `err` at `error` (`msg: "request failed"`, with the route) and handing it to `observe.Report`. The error never reaches the client.
- **`RecoverPanics`** runs just inside `Security` for the API (keeping its headers), and around the pages. A handler's panic is logged at `error` (`msg: "request panicked"`, the route, the panic's value and the stack) and handed to `observe.Report`; the client gets the generic 500 with the request's ID, and none of what the handler set before panicking (a cookie). Once an answer has started (a status written, or a flush: `observe.Request` implements `Flush` and records it) nothing more can be written, so the connection is cut instead (`http.ErrAbortHandler`) and the access line says `aborted`, never a second status or a truncated body that looks whole. `http.ErrAbortHandler` itself passes through, as `net/http` expects.
- **What gets past it** (a panic in `ClientAddress`, `Security`, the dispatch to the API or pages, or the access line itself) is caught by `ClientAddress` (`finishRequest`) and handled the same way, so nothing reaches `net/http`'s own panic log.
- **A panic's value is logged only as `observe.PanicValue` allows**, since a value already formatted is past the handler's redaction: a request, its headers, cookies or a URL is `[redacted]`; a string or an error is logged cut to 512 bytes; anything else only by its type (`httpapi.holder`). The same goes for what `observe.Report` gets, and for a listener's panic in `event_deliveries.last_error`. A message built from a secret (`panic("bad " + token)`) is still a message: don't.
- **The event worker's listeners:** a panic is logged the same way (`msg: "event listener panicked"`, the listener, the event, the stack), reported with `source: worker`, and fails the delivery, which is retried ([events.md](events.md#delivery-eventsworker)).
- **`observe.Report` is the hook for grouped errors** (the plan's `app_errors`, its second phase): a `Problem` (`source` `request`, `panic` or `worker`, the route or listener, the request's ID, the error, the stack) goes to the `Reporter` set with `observe.SetReporter`. None is set yet, so nothing is stored; the log is complete without it.

## Health

| Route | Answers | For |
| --- | --- | --- |
| `GET /api/health/live` | 200 `{"status":"ok"}` while the process answers; no database | an orchestrator's liveness probe: a database outage shouldn't restart every server |
| `GET /api/health` | 200 `{"status":"ok"}` when the database answers; 503 `Database unavailable` when it doesn't, and 503 `Shutting down` once graceful shutdown has begun | readiness and the load balancer |

On SIGTERM `cmd/server` calls `Server.Drain`, so readiness answers 503, then **keeps serving for `SHUTDOWN_DELAY`** (5 s by default in production, 0 elsewhere; 0 to 60 s), logging that it's waiting, and only then calls `http.Server.Shutdown`, which closes the listener at once and gives requests in flight 10 s. The delay is what makes the 503 useful: a load balancer polling readiness every few seconds sees it and stops sending requests before connections start being refused. A platform with its own pre-stop hook (Kubernetes' `preStop` sleep, an ECS stop delay) can do the same, with `SHUTDOWN_DELAY=0s`. Neither route is rate limited.

## The budget

`BenchmarkRequestOverhead` (`internal/httpapi/observability_test.go`) sends a request through `ClientAddress` with a handler that does nothing and the access line written as JSON to a discarding writer. **The budget is 20 µs and 3 allocations per request** for what this adds (request ID, access line; metrics later): a change that exceeds it needs a reason in review.

```sh
cd apps/api && go test ./internal/httpapi -run '^$' -bench RequestOverhead -benchmem
```

| Measured 2026-11-01, i5-6400T (4 cores, 2.2 GHz) | Time | Allocations |
| --- | --- | --- |
| `ClientAddress` before (the client address only) | 0.54 µs | 6 |
| `ClientAddress` now | 4.0–5.7 µs (with the machine's other load) | 8 |
| After the security review (panics caught in `ClientAddress`, `OTHER` methods, optional attributes) | 4.2–4.3 µs | 8 |
| **Added** | **about 3.5–5 µs** | **2** (3 new: the request's record, its ID and the line's attributes; 1 gone: the address is no longer boxed into the context) |

Most of the time is the JSON line. To stay inside it: one context value per request (the `observe.Request`, which is also the response writer and holds the client address), the ID's bytes made inside it, the access line logged with its attributes at once from a fixed array (`requestId` first, so the handler doesn't add it again), the level written as a string (slog would otherwise marshal it), and no floats in the line.

**The log is written as the request ends, synchronously.** If whatever reads stdout falls behind (a log driver that blocks), requests wait on it: use a non-blocking driver ([deployment.md](../deployment.md#logs-and-health-checks)).

## Tests

- **`internal/observe`:** both formats carry `requestId` from the context, once; the level filters; redaction of each sensitive part in any case, in nested groups, in group names, through `With`, and of requests, headers, cookies and URLs, with safe keys and the ID kept (`TestSensitiveKeys` lists every key the code logs, and the accepted over-redaction); no value breaks a line in either format; IDs' shape, uniqueness and an even spread over the 62 characters; `ValidRequestID`; `PanicValue`; `Method`; the record's status, size, 1xx, `ReadFrom` and `Flush`; `Report` reaching a reporter only once one is set.
- **`internal/httpapi` (unit):** `TestRequestIDs` (always a new ID of ours, in `X-Request-ID` and the line; a trusted proxy's own only as `proxyRequestId`; from an untrusted peer, malformed, too long, empty, sent twice or holding a CRLF, nowhere), `TestAccessLine` (fields, labels, no path or query, `OTHER` for an invented method, health at debug, `ACCESS_LOG=errors` and `off`), `TestRecoverPanics` (the 500 with the ID and no panic value or stack, the handler's cookie dropped, the security headers kept, the panic line with ID, route and stack, the report; after the answer started or a flush, one status, the connection cut and `aborted`; `http.ErrAbortHandler` through), `TestPanicValuesAreRedacted` (`panic(r)`, `panic(r.Header)`, a URL, a cookie, a struct and a long message, with a token in the path, query, `X-Admin-Key` and cookie: none logged or reported), `TestClientAddressRecovers`, `TestInternalError`.
- **Integration:** `TestNothingSecretInTheLogs` (a reset, invitation and block-link page, `/admin/<key>` and its inner pages, an admin file with its pass, admin API calls with `X-Admin-Key`, a session and a search or an email in the query, app calls with the session and CSRF cookies and a password or token in the body, a step-up code: no line holds any of them, and each request has one access line with the expected route and user), `TestEventsCarryTheRequestID` (an event's `request_id` matches `X-Request-ID` and the access line, always ours; a trusted proxy's ID only as `proxyRequestId`, a forged one nowhere; none outside a request), `TestOutagesAndShutdown` (a 500's `requestId` in the body, header and log line; readiness 503 and liveness 200 with the database down; readiness 503 once draining). `TestEventNameCheck` covers the column's `CHECK`; `TestListenerPanicsAreLogged` and `TestListenerPanicValuesAreRedacted` the worker.
- **Vitest:** the error notice and the outage page show the reference, and only an ID in the API's shape.
