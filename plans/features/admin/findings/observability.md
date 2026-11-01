# Findings: A3, observability

Plan: [observability](../observability.md), built in three phases. Each phase is built by a sub-agent, checked by the orchestrator, reviewed by a second agent that sees only the diff and the plan, then committed.

| Phase | Scope | State |
| --- | --- | --- |
| 1 | Structured logs, request IDs, panic recovery, the health split | **committed** (see below) |
| 2 | Grouped errors (`app_errors`), errors from the browser | **committed** (see below) |
| 3 | Metrics, `request_minutes`/`metric_minutes`, server heartbeats, slow queries, `/metrics` | after phase 2 |

## Phase 1

### What shipped

- **The `internal/observe` package:**
  - JSON logs by default in production, text in development (`LOG_FORMAT`, `LOG_LEVEL`);
  - the request ID on every line logged during a request;
  - a redacting backstop that blanks any attribute whose key, or enclosing group, mentions a password, token, secret, cookie, authorization, code or key, plus any request, header, cookie, URL or query value.
- **One access line per request** (`ACCESS_LOG=all|errors|off`):
  - `route` is the matched pattern or a fixed label (`page /app/*`, `page /admin/*`, `admin asset`, `page 404`, …), **never the path or query**;
  - `user` is the `usr_` public ID, or `admin`.
- **Request IDs** (`req_` plus 14 base62 characters):
  - on every response as `X-Request-ID`, in 500 bodies, and on every event (`events.request_id`);
  - a proxy's `X-Request-ID` is believed only from `TRUSTED_PROXIES` and only in a safe shape;
  - the web app's error notice and outage page show "Reference: req_…".
- **Panic recovery:** a generic 500 with the request ID. The stack goes only to the log, and cookies the handler had set are dropped.
- **Health:** `GET /api/health/live` (no database). `GET /api/health` answers 503 while the server drains.

### How it was tested

- **`TestNothingSecretInTheLogs`** sends 27 requests carrying every kind of secret the app has:
  - reset, invitation and block-link tokens in page paths;
  - the admin key in `/admin/<key>/…`, the assets pass, `X-Admin-Key`;
  - session and CSRF cookies;
  - search terms, emails in query strings, passwords and codes in bodies.

  It asserts no log line contains any of them. The builder checked the test works by deliberately logging `r.URL`, which it caught 6 times.
- **The whole e2e suite's server log** (2,715 access lines) was scanned: no paths, queries or emails.
- **Benchmark:** `ClientAddress` went from 0.54 µs and 6 allocations to about 4–5.7 µs and 8 allocations per request. That's inside the plan's 20 µs and 3-allocation budget, with one allocation to spare for phase 3.

### Where the build differs from the plan (recorded in the plan)

- **`durationMs` is whole milliseconds.** Floats cost 2 allocations per request in slog's JSON output.
- **`events.request_id` has a CHECK on its shape** (letters, digits, `.`, `_`, `-`, at most 64 characters).
- **`RecoverPanics` wraps the pages as well as the API.** A panic after the answer has started cuts the connection rather than sending a truncated body.
- **The command-line tools keep plain output on stderr,** so `LOG_LEVEL=warn` can't hide why a command stopped. The server logs JSON to stdout.
- **Not built:** the reference on load-error notices and toasts; showing `request_id` on the admin event page; an index on `request_id`.

### Things to know

- **Logs now hold client IPs and public user IDs on every request** (decision 2, confirmed). `docs/deployment.md` recommends keeping logs 30 days at most.
- **`docs/api/auth.md` used to say "Go keeps no access log".** That's no longer true, and the doc has been updated.
- **Readiness has no grace period:** it answers 503 during drain, but nothing waits between draining and closing the listener. A load balancer only sees the 503 on a connection it already holds. Adding a pre-stop delay, e.g. `SHUTDOWN_DELAY`, needs a decision. **Recommended:** add one, 5 s by default in production, when phase 3 touches startup and shutdown again.
- **Error text may hold personal data** (not secrets). SMTP errors can name the recipient's email, and PostgreSQL errors can echo a value that failed to parse. Phase 2's scrubber must handle both before anything is stored in `app_errors`.
- **Client-cancelled requests show up as `ERROR "request failed" error="context canceled"`** with a 500 in the log. This predates phase 1. Phase 2 should map them to a non-error, or they'll be the noisiest error group.

### Security review

The reviewer confirmed sound:
- route labels never come from client input, and there's a bounded set of them;
- a wrong admin key and an unknown route still differ only by a random `X-Request-ID` of the same shape;
- request IDs are unbiased, from `crypto/rand`, and can't break a log line or make an event fail to record;
- the 500 body from a panic is byte-identical to `InternalError`'s;
- a half-done session rotation fails closed on a panic;
- the web app shows the reference as text only.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | **Medium** | **A client could choose its own request ID behind real proxies.** `X-Request-ID` was believed from `TRUSTED_PROXIES`, but Cloudflare, ALB and default nginx pass a client's header through. An attacker could copy a victim's ID (from a support ticket, say) into their own log lines and audit events | **Changes plan decision 3:** the server always makes its own ID. A trusted proxy's ID is logged beside it as `proxyRequestId`, never stored on events. _See [questions](../read/questions.md)._ |
| 2 | Low–medium | **Panic values skipped redaction.** `panic(r)` or `panic(r.Header)` would have logged tokens, the admin key and cookies | Request, header, URL and cookie values are redacted; other values log only their type; strings are truncated |
| 3 | Low | **The docs promised "no email reaches the log",** but error text (PostgreSQL, SMTP 550 replies) can hold emails and input values. This was already so before phase 1 | An honest caveat in the docs; phase 2's scrubber will clean error values before they're logged and stored |
| 4 | Low | **Redaction missed names this app uses:** `assetsPass`, `session`, `csrf`, `mfa`, `otp`, `email`, `digest`, `hash` | Added, with false-positive checks |
| 5 | Low | **Readiness's 503 during drain was practically never seen,** because `Shutdown` closes connections at once | `SHUTDOWN_DELAY` (5 s by default in production) between draining and shutting down |
| 6 | Nit | A flush, then a panic, would append a 500 body to a 200 answer (nothing flushes today) | `Flush` tracks the status |
| 7 | Nit | Panics in the outer middleware were logged as 200, unredacted, by net/http | Recovered in `ClientAddress` too |
| 8 | Nit | An aborted answer was logged with its written status | `aborted: true` on the line |
| 9 | Nit | `method` was logged raw (up to about 16 KB) | Unknown methods logged as `OTHER` |
| 10 | Nits | Docs: net/http's own 400s carry no ID; `pending_registrations.request_id` means something else; blocking stdout logging | Documented |

**Fix results.** All ten are fixed.

- **Request IDs:** `events.request_id`'s CHECK now accepts only our own `req_` shape. `TestRequestIDs` and `TestEventsCarryTheRequestID` prove a forged ID appears nowhere except as `proxyRequestId` on the access line.
- **Panics:** `TestPanicValuesAreRedacted` sends `panic(r)`, `panic(r.Header)`, a URL and a cookie, with a token in the path, the query, `X-Admin-Key` and a cookie. None of those reaches the log, the report or a delivery's stored error.
- **`SHUTDOWN_DELAY`:** 5 s by default in production, 0–60 s. The Docker stop timeout is raised to 20 s in the docs.
- **Benchmark after the fixes:** 4.2–5.3 µs and 8 allocations per request (+2), still inside the budget.
- **All checks passed:**
  - `make check`;
  - `make test`: 21 Go packages, 296 Vitest tests;
  - `make build`;
  - e2e: 23 of 23.

  The e2e server log (2,697 access lines) held no tokens, key, query strings or emails, and no ERROR lines.

**Still open, by design (phase 1):** a panic message *built from* a secret (`panic("bad " + token)`) is logged as text, cut to 512 bytes. The docs say never to do that. Scrubbing error values is phase 2's job.

## Phase 2

### What shipped

- **Grouped errors:**
  - every internal error, panic, final listener failure, cleanup-loop failure and failed page-gate query is reported to an in-memory collector;
  - the collector groups them by fingerprint, holds at most 1,000 groups (client errors at most 100, and only able to push out other client errors), and writes them to the new `app_errors` table every 10 s;
  - the write is one statement, and only when there's something to write;
  - a group an admin resolves opens again on a new occurrence;
  - `make prune target=app_errors` removes groups not seen for 30 days.
- **The scrubber runs before anything is logged or stored.** It replaces emails, IP addresses, public IDs, request IDs, UUIDs, numbers, quoted values, URLs and query strings, PostgreSQL's `Key (…)=(…)` details, long hex runs, token-like base64 and recovery-code shapes with placeholders. Constraint, table and column names survive. Stacks keep only function names and `file:line`. Delivery errors (`last_error`), including SMTP replies naming the recipient, are scrubbed too.
- **A client hanging up is no longer an error:** `context.Canceled` from the client going away is logged as aborted (status 0, debug), not a 500. A server-side deadline is still a 500.
- **Errors from the browser:**
  - `POST /api/client-errors` takes reports from signed-in people only, with CSRF, at most 30 an hour per person and 4 KiB each;
  - the route is checked against an allowlist that a test keeps in step with `routes.ts`;
  - the web app's root error boundary and `window.onerror` / `unhandledrejection` send one report per distinct error per page load, never the URL.

### Where the build differs from the plan (recorded in the plan)

- **The scrubber removes more than the plan listed** (URLs, queries, PostgreSQL details, recovery-code shapes) and deliberately keeps database object names.
- **More `CHECK`s on `app_errors`.**
- **More report sources than listed:** the worker's claim loop, an unregistered listener, the previous-key session check, and a failed `admin.access_refused` record.
- **Admin pages don't report browser errors.** The endpoint needs an app session, and an app session in the same browser would file dashboard errors under that account. An admin variant can come with A4's Errors page.
- **The server's version is the VCS revision, or `dev`.** Docker builds have no `.git`, so they say `dev` until a build argument supplies one.

### Things to know

- **Scrubbing is a backstop.** Plain words someone typed into an error message (a name, a search term) aren't recognised.
- **During the full e2e run** nothing reached `app_errors`, and the server log held no ERROR lines.

### Security review

_Filled in after the independent review._

The reviewer probed the scrubber with adversarial inputs in throwaway tests.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | **High** | **One signed-in user could stop every `app_errors` write on a server.** A browser stack line shaped like a Go location (`\t… x.go:1`) skipped the scrubber, NUL byte included. The NUL broke the one-statement flush, the failed group was put back, every later flush failed, and resending every 2 minutes kept it alive until real errors were dropped | Go-location lines are accepted only in their exact shape; nothing stored can hold control characters; rows are checked before the insert, and a batch failing on data is retried row by row, dropping the bad row |
| 2 | Medium | **Client stacks skipped the scrubber through the same path,** storing free text, emails and tokens, and could replace a real group's stack | As 1; Go-format lines are dropped from browser stacks entirely |
| 3 | Medium | **Chrome's `JSON.parse` message** (`"{"name":"Ada Lovelace",…}" is not valid JSON`) leaked the values, because the nested quotes paired the wrong way | A specific rule for that message plus a general backstop; the web reporter strips the snippet too |
| 4 | Low–medium | **Missed secret shapes:** every TOTP secret (base32) survived; about 2.4% of recovery codes, and every one without its dash; about 1 in 2,000 session, reset and invitation tokens; hex next to a word character | Base32, recovery-code and long-token rules; the docs' coverage table corrected |
| 5 | Low | Smaller leaks: IPv6 followed by punctuation, zero-width and bidi characters (which can split a token), curly quotes, `%40` and full-width `＠`, `Failing row contains (…)` | Each handled |
| 6 | Low | **Users can write text admins will read** ("visit evil.com/login"), and `release` was stored as sent | `release` accepted only as a revision or `dev`; hostnames scrubbed from browser text; A4 told to label browser groups as user-reported and never link them |
| 7 | Low | No global bound on stored browser rows | A `client-error:global` limit; browser messages fingerprinted on their first 120 characters |
| 8 | Low | A page stuck in an error loop could use up the user's whole write budget (`api:per-user`) | Reports metered only by their own limit; the web app stops reporting after a 429 |
| 9–10 | Nits | The shutdown budget equalled Docker's stop timeout; a flush that commits but errors is counted twice | The final flush gets its share; the double count documented |

**Fix results (phase 2).** All ten are fixed. Rerunning the reviewer's probes:
- **Secrets:** none survive. That covers 200,000 session and reset tokens, 10,000 base32 TOTP secrets, 100,000 recovery codes and 100,000 `APP_KEY`-like keys.
- **The poison report** is stored safely next to a real error, with nothing left pending.
- **Chrome's JSON message** comes out as `"…" is not valid JSON`.

**New things to know from the fixes:**
- **`client-error:global`**: 500 browser error reports an hour across everyone, then 503.
- **Error reports skip the general write budget** (`api:per-user`) and count only against their own limits, so a broken page can't lock a user out of saving. This is a documented exception to "budgets in `RequireSession`" (AGENTS.md and `rate-limits.md` updated).
- **Docker's stop timeout** should be `SHUTDOWN_DELAY` + 20 s (`--stop-timeout 25` by default).
- **Long CamelCase names** (32+ characters, e.g. long test names in stacks) now show as `<token>`.

**Gaps accepted and documented:** emails written as `ada @ example.com` or `ada(at)example.com`; ordinary words that happen to be someone's data, such as names; and a recovery code typed without its dash that has no digit, about 2.4% of codes.

All checks passed:
- `make check`;
- `make test`: 21 Go packages, 310 Vitest tests;
- `make build`;
- e2e: 23 of 23, with no ERROR lines and an empty `app_errors`;
- benchmark: still 4.2 µs and 8 allocations.
