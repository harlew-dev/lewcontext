# Observability

Code: `internal/observe` (the log handler, request IDs, what an access line reports, the scrubber, the error collector), `internal/httpapi/client_ip.go` (`ClientAddress`) and `observability.go` (the request's ID, the access line, `RecoverPanics`, readiness), `internal/store/app_errors.go` (`RecordErrors`), `endpoints/account/client_errors.go` (errors from the browser), `apps/web/src/lib/client-errors.ts`, `internal/config/logging.go`. Plan: [observability](../../plans/features/admin/observability.md) (A3), whose first two phases this is: logs, request IDs, panic recovery and health; then grouped errors (`app_errors`) and errors from the browser. Metrics and `/metrics` come with its third phase.

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
- **An error's text is scrubbed before it's logged** ([below](#scrubbing)). A PostgreSQL type error echoes the value it couldn't read (Bun writes values into the query), a `database/sql` scan error quotes one, and an SMTP refusal names the recipient's address, so request and background code logs an error as `observe.ErrorAttr(err)` (its message through `observe.Scrub`, under `error`), never `"error", err`: `InternalError`, the page gates, the event worker, the cleanup loop and the error collector do. The log keeps the request's ID, which leads to everything else. Startup and the commands run by hand still log their own errors as they are: they come from configuration, not from a request.

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

- **`httpapi.InternalError(w, r, err)`** answers the generic 500 with the request's ID, after logging `err` at `error`, scrubbed (`msg: "request failed"`, with the route), and handing it to `observe.Report`. The error never reaches the client.
- **A client that went away isn't an error.** When the error is `context.Canceled` and the request's own context was canceled (`httpapi.ClientGone`: the tab closed, the user navigated), `InternalError` answers nothing, marks the request aborted (its access line says `aborted: true`, status `0` if nothing was written), logs `request aborted by the client` at `debug`, and reports nothing: no 500, no error group. The request's deadline (`context.DeadlineExceeded`, the 10 s timeout) is still a failure. The page gates do the same.
- **`RecoverPanics`** runs just inside `Security` for the API (keeping its headers), and around the pages. A handler's panic is logged at `error` (`msg: "request panicked"`, the route, the panic's value and the stack) and handed to `observe.Report`; the client gets the generic 500 with the request's ID, and none of what the handler set before panicking (a cookie). Once an answer has started (a status written, or a flush: `observe.Request` implements `Flush` and records it) nothing more can be written, so the connection is cut instead (`http.ErrAbortHandler`) and the access line says `aborted`, never a second status or a truncated body that looks whole. `http.ErrAbortHandler` itself passes through, as `net/http` expects.
- **What gets past it** (a panic in `ClientAddress`, `Security`, the dispatch to the API or pages, or the access line itself) is caught by `ClientAddress` (`finishRequest`) and handled the same way, so nothing reaches `net/http`'s own panic log.
- **A panic's value is logged only as `observe.PanicValue` allows**, since a value already formatted is past the handler's redaction: a request, its headers, cookies or a URL is `[redacted]`; a string or an error is scrubbed like any error ([below](#scrubbing)), then cut to 512 bytes (in that order, so a token at the cut can't leave a piece); anything else only its type (`httpapi.holder`). Its stack is logged as function names and lines only (`observe.ScrubStack`). The same goes for what `observe.Report` gets, and for a listener's panic in `event_deliveries.last_error`. A message built from a secret (`panic("bad " + token)`) loses the secret only if it has a token's shape: don't.
- **The event worker's listeners:** a panic is logged the same way (`msg: "event listener panicked"`, the listener, the event, the stack), reported with `source: worker`, and fails the delivery, which is retried ([events.md](events.md#delivery-eventsworker)). A listener's error is logged scrubbed on every attempt, stored scrubbed in `event_deliveries.last_error`, and reported once it has failed for good (after the last retry), as is a listener nobody registered.
- **`observe.Report`** hands a `Problem` (its source, the route or listener or job, the request's ID, the error, the stack; a browser's release) to the `Reporter` set with `observe.SetReporter`: in the server, the error collector ([below](#grouped-errors-app_errors)). Without one (the commands, most tests) nothing is grouped; the log is complete without it.

## Scrubbing

`observe.Scrub(message)` is what an error's text may keep, in the log and in `app_errors` alike. First the text becomes one line: control characters and line breaks become spaces, **format characters** (bidi controls, zero-width spaces, a soft hyphen) are removed, since one could split a token so half of it looked like a word, and invalid UTF-8 becomes a replacement character. Then it replaces, in this order:

| What | Becomes | For example |
| --- | --- | --- |
| the text V8's `JSON.parse` quotes (`, "{"name":…}" is not valid JSON`, also cut with `...`) | `"…"` | an API answer with someone's data in it |
| PostgreSQL's details: `Key (…)=(…)` and `Failing row contains (…)`, up to the line's last parenthesis (a value can hold its own) | `=(…)`, `contains (…)` | a duplicate email, a refused row |
| a URL (any `scheme://…`), a query string or fragment (`?a=b…`, `#a=b…`) | `<url>`, `?<query>`, `#<query>` | a link's token in a path, a search in a query |
| an email address, with or without `<…>`, its `@` also as `%40` or full-width `＠`, or with a quoted local part (`"ada lovelace"@…`) | `<email>` | an SMTP refusal's recipient |
| a public or request ID (three letters, `_`, 14 base62), a UUID | `<id>` | `usr_…`, `wsp_…`, `req_…` |
| an IPv4 address (and port), an IPv6 address (`[…]:port`, `%zone`, and when a sentence's `:` or `.` follows it) | `<ip>` | `dial tcp 10.0.0.5:5432` |
| a double-quoted (Go's `%q` included, or cut off at the end), curly (`“…”`, `‘…’`), guillemet (`«…»`), backquoted or single-quoted string; quotes nested so they can't be paired (a closing quote followed straight by more text) go to the line's last quote | `"…"`, `“…”`, `'…'` … | PostgreSQL's `invalid input syntax for type uuid: "…"` |
| a run of 20 or more base64 or base64url characters: always at 32 or more (a session, reset, invitation or block-link token is 43, whatever characters it happens to hold), and under 32 when it mixes two of upper case, lower case and digits. All hex is `<hex>` | `<token>` | tokens, `APP_KEY`-like base64, a JWT's parts |
| 16 or more of `A–Z` and `2–7` (base32) | `<token>` | an authenticator's secret |
| 16 or more hex digits, even inside a word (`digest_9f86…`) | `<hex>` | a SHA-256 digest |
| a recovery code: `xxxxx-xxxxx` in its alphabet (Crockford's base32), or its ten characters without the dash when they hold a digit and a letter | `<token>` | |
| any other number (`5.1.1`, `0x1f`, `80`), digits outside ASCII (`１２３`, `٣٤٥`) included | `<n>` | |

- **What says what broke stays:** a name PostgreSQL quotes after `constraint`, `relation`, `table`, `column`, `index`, `type`, `schema` and the like (`unique constraint "users_email_key"`, `column "name" of relation "projects"`), `SQLSTATE` codes, words with digits in them (`int64`, `utf8`), identifiers (`projects_workspace_id_fkey`; a CamelCase name without digits under 32 characters, `TestNothingSecretInTheLogs`; a lower-case path, a Go package's) and ten-letter words (`background`).
- **Accepted gaps:** a recovery code typed without its dash and holding no digit (about 2.4% of codes) reads as a ten-letter word and stays; so do a CamelCase name of 32 characters or more and an identifier with digits (they become `<token>`, the price of catching every token).
- **Bounded:** at most 1,000 characters (`MaxMessage`), so the column's `CHECK` holds.
- **Stacks** (`observe.ScrubStack`): each line is cleaned as above first, so no control or format character survives; lines are joined with line breaks and a Go location starts with a tab, nothing else. Go's `debug.Stack()` keeps each function's name, its arguments' values replaced by `(…)`, and `file.go:line` without the directory, **only when the location line is exactly a path and a line** (`^\S+\.go:\d+$` once the offset is gone); anything else in that place is scrubbed as a message. The goroutine header and `+0x…` offsets go. A browser's stack (Chrome's `at name (url:line:col)`, Firefox's and Safari's `name@url:line:col`) keeps `at name (file.js:line:col)`: the script's file name without its address or query, and `<page>` for a frame in the page itself (an inline script), whose path could hold a link's token. Any other line is scrubbed as a message, so a stack holds function names, file names and lines **plus scrubbed text** for lines in neither format. At most 8,000 characters.
- **A browser's text** (`observe.ScrubClient`, `ScrubClientStack`, for `source: client`) also loses bare host names and paths (`evil.example.com/login`, `evil[.]com`, `evil dot com` under a common top-level domain), which someone could leave for an admin to follow; and a browser's stack drops every Go-format line, which no browser writes. Never applied to the server's text, whose table and constraint names look alike.
- **It's a backstop,** like redaction: a word that is someone's data but looks like any other word (a name, a search typed into a message) stays. Don't build error messages from what a user typed.

`TestScrub` (`internal/observe/scrub_test.go`) feeds it emails (bare, bracketed, `%40`, `＠`, a quoted local part), IPs (followed by `:` or `.` too), `usr_` and `wsp_` IDs, a 64-hex digest (alone and after a word), a base64url token (also split by a zero-width space), a `req_` ID, recovery codes, a base32 secret, quoted values (straight, curly, guillemets, nested), V8's `JSON.parse` message, PostgreSQL's `invalid input syntax`, unique and not-null errors, key details with nested parentheses, a failing row, a scan error, an SMTP 550 reply, URLs, queries and fragments, and non-ASCII digits, and checks none survive while the names do. `TestScrubbedSecretShapes` scrubs 20,000 random tokens, base64 keys, base32 secrets and recovery codes (with and without the dash, holding a digit) each: none survives. `TestFingerprints` checks that errors differing only in those values group together.

## Grouped errors: `app_errors`

Every `InternalError`, panic (a request's or a listener's), final listener failure, failed cleanup run, failed claim by the event worker, failed `admin.access_refused` record, failed page-gate query (`RequirePageSession`, the admin page gate, and an admin session check under the previous key) and error from a browser is reported to an in-memory **collector** (`observe.Collector`, set by `cmd/server` with `observe.SetReporter`), grouped by fingerprint, and stored every 10 seconds, **only when there are any**.

| Column | Holds |
| --- | --- |
| `fingerprint` | hex SHA-256 of the source, the route and the scrubbed message (`observe.Fingerprint`): failures differing only in what scrubbing replaces share a row. Unique |
| `source` | `request` (`InternalError`, a page gate), `panic` (a handler's), `worker` (a listener's panic or final failure), `background` (the cleanup loop, the worker's claim loop, recording a refused admin address), `client` (a browser) |
| `route` | the route pattern or page label, the listener's name, the job's name (`cleanup`, `event worker`, `admin.access_refused`) or a browser page's route pattern. Never a path |
| `message` | the scrubbed message, 1 to 1,000 characters |
| `stack` | the latest occurrence's scrubbed stack, at most 8,000 characters, or empty |
| `count`, `first_seen_at`, `last_seen_at` | how often, and when first and last |
| `last_request_id` | the latest occurrence's request ID (ours only, `CHECK`ed like `events.request_id`), or empty |
| `last_version` | the latest occurrence's release: the server's (`observe.Version()`: the build's VCS revision, 12 characters and `-dirty` for uncommitted changes, or `dev`), or what a browser's build said it was (`RELEASE` at build time; empty without one) |
| `resolved_at` | set by an admin (the admin actions plan); a new occurrence clears it, so a regression shows as open |

- **Never in the request's way:** scrubbing and hashing happen on the reporting goroutine, then the report is a map update under a mutex, never a query. A flush takes the whole map out first, so reporting never waits on the database.
- **The flush** (`store.RecordErrors`) is one multi-row `INSERT … ON CONFLICT (fingerprint) DO UPDATE`: a row per fingerprint, `count = count + n`, the first and last times widened, the latest occurrence's request, release and stack, `resolved_at` cleared. An older flush arriving late (another server) adds its count without moving the latest occurrence back.
- **Bounded:** at most 1,000 groups (`MaxErrorGroups`) between flushes; a new one pushes out the least recently seen. At most 100 (`MaxClientErrorGroups`) come from browsers, and a browser's new group only ever pushes out another browser's (or is dropped), so browsers can never evict the server's. Dropped occurrences are logged as a count (`error groups dropped`) at the next flush.
- **When the database is down,** the flush fails, its groups go back into the collector (merged with what came meanwhile, within the same limits), a warning is logged, and nothing retries before the next tick: one attempt per 10 seconds. The log is complete either way. A flush that committed but still reported an error (the connection lost on the commit) puts its groups back too, so they're counted twice: accepted, as it's rare and only an overcount.
- **When a group's own data is bad,** it never stops the others. `RecordErrors` checks each group against the table's rules first (its `CHECK`s, and no control or format character: a NUL can't be stored at all, and Bun's refusal of one would fail the whole statement) and never sends one that breaks them. If the statement still fails on the data (SQLSTATE classes 21, 22, 23, 42), each group is sent on its own; those refused for their data are dropped, and only those that failed otherwise go back for the next flush (`observe.FlushError`). Dropped groups are logged as a count (`error groups dropped: refused for their data`).
- **On shutdown** the flush loop keeps running through `SHUTDOWN_DELAY` and the drain; once requests, the worker and the refusal queue have finished (10 s at most), `cmd/server` stops it, stops reporting, and flushes once more (5 s at most), so the last failures before a restart are kept. A container's stop timeout is therefore the delay plus 20 s ([deployment.md](../deployment.md)).
- **Pruned:** `make prune target=app_errors` deletes groups not seen for 30 days ([database.md](database.md#pruning)). The admin dashboard's Errors page (plan A4) will read the table; until then it is read with SQL.

## Errors from the browser

`POST /api/client-errors` (`account.ClientErrors.Store`) takes the web app's own errors, so a crash in someone's browser shows up beside the server's:

- **Signed in only,** behind `RequireSessionUnmetered`: the session, its CSRF token and the exact Origin, like every mutation, but **not the write budget**, so a page stuck in an error loop can't use it up and leave its user unable to save anything. Instead **`client-error:per-user`**, 30 an hour (counted after the CSRF check, so no other site can spend it), then, once the body is valid, **`client-error:global`**, 500 an hour for everyone (503) ([rate-limits.md](rate-limits.md)). Signed out, nothing is sent: an anonymous write endpoint would be a free way to fill a table. An account that must verify its email first gets 403, like every other route.
- **The body** is at most 4 KiB, one JSON object with `message` (required), `stack`, `route` and `release`, and nothing else (400 for an unknown field). It answers 202 with no body: the report is grouped in memory (`source: client`) and stored with the next flush, if the collector has room.
- **The route is one of the web app's page patterns** (`clientRoutes`: `/`, `/auth/login`, …, `/app/:workspace/projects/:project`, and `*` for the not-found page), or it's stored as `unknown`, so a browser can't mint labels of its own. `lib/client-errors.test.ts` checks the Go list, the web app's (`clientRoutePatterns`) and `routes.ts` agree. The message and stack are scrubbed as a browser's text (`ScrubClient`, `ScrubClientStack`: hosts and paths too, Go-format stack lines dropped), so a URL, a host, a token in a script's path or an email never reaches the table. A browser's group is fingerprinted on its first 120 scrubbed characters, so varying the end of a long message makes no new group. The release is kept only in `observe.Version`'s shape (`^[0-9a-f]{7,40}(-dirty)?$` or `dev`), anything else stored as empty (not refused: reporting keeps working), so nobody can leave words in it. The text is still a user's: the admin Errors page (A4) should label these groups as reported by a browser and never turn their text into links.
- **The web app** (`lib/client-errors.ts`) reports what its root error boundary shows the outage page for (not a 404, not an `ApiError`: the server has its own record of a 5xx) and what reaches `window`'s `error` and `unhandledrejection` events (not a network failure, not a cross-origin script's opaque `Script error.`). It sends each distinct error (a hash of its message and first stack frame) once per page load, ten at most, after asking `GET /api/auth/me` for the session and its CSRF token: a 401 there means signed out, and nothing is sent. Once the API refuses one for a limit (429 or 503) it sends nothing more until the page loads again. It removes the text V8's `JSON.parse` quotes before sending (the API scrubs it too). It sends the **route pattern** of the page, read from its path (`routePattern`), **never the URL or its query**. Its own failures are swallowed.
- **The admin build reports nothing:** its pages have an admin session, which this endpoint doesn't take, and an app session beside it would put the dashboard's errors in an account's name. The reporter checks it's in the app build (served from `/`) before anything else. An admin-session variant can come with the admin Errors page (A4) if it's wanted.

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
| Phase 2 (grouped errors: nothing on the success path; scrubbing and the collector run only for a failure), side by side with phase 1 under the same load | 4.1–4.2 µs quiet (phase 1: 4.7), 8–12 µs with two other suites running (phase 1 alike) | 8 |
| **Added** | **about 3.5–5 µs** | **2** (3 new: the request's record, its ID and the line's attributes; 1 gone: the address is no longer boxed into the context) |

Most of the time is the JSON line. To stay inside it: one context value per request (the `observe.Request`, which is also the response writer and holds the client address), the ID's bytes made inside it, the access line logged with its attributes at once from a fixed array (`requestId` first, so the handler doesn't add it again), the level written as a string (slog would otherwise marshal it), and no floats in the line.

**The log is written as the request ends, synchronously.** If whatever reads stdout falls behind (a log driver that blocks), requests wait on it: use a non-blocking driver ([deployment.md](../deployment.md#logs-and-health-checks)).

## Tests

- **Grouped errors:** `TestScrub`, `TestScrubBounds`, `TestFingerprints`, `TestScrubGoStack`, `TestScrubBrowserStack`, `TestScrubbedSecretShapes`, `TestScrubClient`, `TestScrubStackLinesAreSafe` (a NUL or free text in a Go location line, a browser's Go-format lines) (`scrub_test.go`); `TestCollectorGroups`, `TestCollectorClientRelease`, `TestCollectorClientGroups` (the 120-character fingerprint, a browser's text and stack), `TestCollectorDropsWhatTheStoreRefused`, `TestCollectorIsBounded` (1,000 groups, the stalest out; 100 from browsers, which never push out the server's), `TestCollectorKeepsGroupsWhenTheFlushFails`, `TestCollectorRunFlushesOncePerTick` (nothing flushed when empty, one attempt a tick while the store fails), `TestReportDoesntWaitForTheFlush` (`collector_test.go`); `TestRecordErrorsUpsertsOneRowPerGroup` (one row per fingerprint, counts added, a resolved group reopened, a late flush not moving the latest back) `TestRecordErrorsDropsBadGroups` (a NUL, a control character in a route, an unknown source and a time the database refuses: dropped, never retried, the others stored) and `TestAppErrorChecks` (`internal/store`); `TestPanicsAreGroupedInAppErrors` (three panics with different emails and tokens: one row, count 3, scrubbed, function names and lines only, the last request's ID; again after another flush), `TestPageGateFailuresAreReported`, `TestInternalErrorLogsScrubbedText`, `TestClientGoneIsNoError` (`internal/httpapi`); `TestPreviousAdminKeyInAnOutage` (the admin gates' reports, never naming the key); `TestFinalListenerFailuresAreReported` (`internal/events`); `TestLeasesRetriesAndOutcomes` (a scrubbed `last_error`); `TestClientErrors`, `TestClientErrorsRefusals`, `TestClientErrorsLimit`, `TestClientErrorsGlobalLimitAndBudget` (a used-up write budget doesn't stop a report; 503 past `client-error:global`), `TestClientErrorsCantPoisonTheFlush` (the review's poison stack beside a real server error: both stored, nothing planted kept) (`endpoints/account`); `TestPruneAppErrors`. Vitest: `lib/client-errors.test.ts` and `root.test.tsx`.
- **`internal/observe`:** both formats carry `requestId` from the context, once; the level filters; redaction of each sensitive part in any case, in nested groups, in group names, through `With`, and of requests, headers, cookies and URLs, with safe keys and the ID kept (`TestSensitiveKeys` lists every key the code logs, and the accepted over-redaction); no value breaks a line in either format; IDs' shape, uniqueness and an even spread over the 62 characters; `ValidRequestID`; `PanicValue`; `Method`; the record's status, size, 1xx, `ReadFrom` and `Flush`; `Report` reaching a reporter only once one is set.
- **`internal/httpapi` (unit):** `TestRequestIDs` (always a new ID of ours, in `X-Request-ID` and the line; a trusted proxy's own only as `proxyRequestId`; from an untrusted peer, malformed, too long, empty, sent twice or holding a CRLF, nowhere), `TestAccessLine` (fields, labels, no path or query, `OTHER` for an invented method, health at debug, `ACCESS_LOG=errors` and `off`), `TestRecoverPanics` (the 500 with the ID and no panic value or stack, the handler's cookie dropped, the security headers kept, the panic line with ID, route and stack, the report; after the answer started or a flush, one status, the connection cut and `aborted`; `http.ErrAbortHandler` through), `TestPanicValuesAreRedacted` (`panic(r)`, `panic(r.Header)`, a URL, a cookie, a struct and a long message, with a token in the path, query, `X-Admin-Key` and cookie: none logged or reported), `TestClientAddressRecovers`, `TestInternalError`.
- **Integration:** `TestNothingSecretInTheLogs` (a reset, invitation and block-link page, `/admin/<key>` and its inner pages, an admin file with its pass, admin API calls with `X-Admin-Key`, a session and a search or an email in the query, app calls with the session and CSRF cookies and a password or token in the body, a step-up code: no line holds any of them, and each request has one access line with the expected route and user), `TestEventsCarryTheRequestID` (an event's `request_id` matches `X-Request-ID` and the access line, always ours; a trusted proxy's ID only as `proxyRequestId`, a forged one nowhere; none outside a request), `TestOutagesAndShutdown` (a 500's `requestId` in the body, header and log line; readiness 503 and liveness 200 with the database down; readiness 503 once draining). `TestEventNameCheck` covers the column's `CHECK`; `TestListenerPanicsAreLogged` and `TestListenerPanicValuesAreRedacted` the worker.
- **Vitest:** the error notice and the outage page show the reference, and only an ID in the API's shape.
