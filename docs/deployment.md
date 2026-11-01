# Deploying lewcontext

How to run lewcontext in production, and what to set up around it. Everything marked **Planned** is designed in [`plans/features/`](../plans/features) but not built yet: don't configure it until its plan is implemented.

## What you deploy

One image (the root `Dockerfile`) holding four binaries:

| Command | Does | Run it |
| --- | --- | --- |
| `api` (the default `CMD`) | The Go server: the JSON API under `/api`, the web app built into the binary (and the admin dashboard's own build, served only behind its key), the event delivery worker, a 15-minute cleanup loop, each minute's metrics and the hourly rollup, and `/metrics` on its own listener when `METRICS_ADDR` is set | as a long-running service |
| `migrate` | Applies database migrations, inside a transaction each, under an advisory lock | once per release, **before** starting the new `api` |
| `prune` | Deletes old rows from log tables (`events`, `audit_events`, `app_errors`, `request_hours`, `metric_hours`) and minimises events over 30 days old (`pseudonymise`), recording `prune.completed` ([compliance.md](compliance.md#retention)) | from cron, daily |
| `seed` | Demo data | never: it refuses to run with `APP_ENV=production` |
| `admin` | The operator's commands, previewing without `-confirm`: `admin grant` (with `-role viewer|operator`), `revoke` and `list` for the admin dashboard; `admin reset-mfa -email … -confirm` turns off two-factor authentication for someone who lost their authenticator and recovery codes, and signs out their sessions; `admin blocks` lists blocked email addresses and `admin unblock -email … -confirm` takes one off the list; `admin suspend -email … -reason … -confirm` and `unsuspend` suspend an account (not an admin's: `revoke` first) or lift it; `admin sessions revoke-all -confirm` signs every account out, app and admin sessions alike (a suspected leak of the session tables); `admin erase -email … -confirm` deletes an account for someone who can't (an erasure request: not an admin's, nor the only owner of a workspace with other members), and `admin export -email … > file.json` writes an account's data as Download my data gives it ([compliance.md](compliance.md)); `admin app-key generate` and `admin route-key generate` print a new `APP_KEY` or `ADMIN_ROUTE_KEY` | by hand; check who is asking out of band before `reset-mfa`, `erase` and `export` |

There is one process and one origin: the same server sends pages and API responses, so there is no CORS and no separate frontend host. The server does **not** migrate on startup.

```sh
docker build -t lewcontext --build-arg APP_NAME=lewcontext .   # APP_NAME is baked into the web build
make build                                                     # or, without Docker: bin/api
```

## Requirements

- **PostgreSQL 15+** reachable over TLS with a verifiable certificate. Use dedicated roles, never a superuser: one that owns the tables for `migrate`, one with data access only for everything else ([Database roles](#database-roles)).
- **Something that terminates HTTPS in front of the server.** The server only speaks plain HTTP, and production refuses an `http://` origin. That can be a reverse proxy (nginx, Caddy), a cloud load balancer, or Cloudflare. See [Behind a proxy](#behind-a-proxy-or-load-balancer).
- **An SMTP provider** (Resend, Postmark, Amazon SES, Mailgun, …) and **outbound SMTP** from the server to it, on 587 or 465. Some hosts block outgoing SMTP ports on new accounts; ask them to open it. Or run with `MAIL_DRIVER=off` and no password reset or invitations. See [Email](#email).
- **Outbound HTTPS** from the server to `challenges.cloudflare.com`, once CAPTCHA challenges are built (**Planned**).

## Configuration

### Today

| Variable | Production value | Notes |
| --- | --- | --- |
| `APP_ENV` | `production` | Turns on `Secure` `__Host-lewcontext_session` cookies, HSTS on pages, and the strict checks below. Without it the server runs in development mode |
| `APP_ORIGIN` | `https://app.example.com` | The exact public origin: scheme and host, no trailing slash. **HTTPS is required.** Mutations from any other origin are refused, and every emailed link is built from it |
| `DATABASE_URL` | `postgresql://lewcontext:…@db.internal:5432/lewcontext?sslmode=verify-full` | **`sslmode=verify-full` is required.** For a provider with its own CA, mount the CA file and add `&sslrootcert=/path/to/ca.pem` |
| `API_ADDR` | `0.0.0.0:3000` (the image's default) | The listen address. Bind it to a private interface the proxy can reach, never directly to the internet without TLS in front |
| `MAIL_DRIVER` | `smtp` (the default), or `off` | `smtp` needs the settings below, or the server refuses to start. `off` runs without email: password reset and invitations answer 503, and it needs `EMAIL_VERIFICATION=off`. `log` (the development mailbox) is refused in production |
| `EMAIL_VERIFICATION` | `on` (the default), or `off` | `on`: a new sign-up enters a 6-digit code emailed to it before its account exists, and sign-up no longer tells anyone whether an email has an account. `off`: sign-up as before. See [Turning on email verification](#turning-on-email-verification) |
| `EMAIL_VERIFICATION_EXISTING` | `optional` (the default), or `required` | `required`: accounts that never verified (made while it was off) must verify at their next sign-in before anything else |
| `MAIL_FROM_ADDRESS`, `MAIL_FROM_NAME` | `no-reply@example.com`, `Example` | The sender. The address must be bare, on a domain your provider has verified. The name defaults to `APP_NAME` |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_TLS` | `smtp.resend.com`, `587`, `starttls` | `starttls` (port 587 by default) or `tls` (465). **`none` is refused in production.** Certificates are always verified |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | your provider's | Both or neither. **A secret:** pass it through your platform's secret store, not the image |
| `APP_KEY` | 32 random bytes in base64: `docker run --rm lewcontext admin app-key generate` prints one (or `openssl rand -base64 32`) | **A secret, and required.** Seals two-factor secrets at rest, keys recovery-code and emailed-code digests and signs the admin dashboard's assets pass; **every server on one database needs the same one** (one server's worker may send a code another checks). `.env.example`'s key is refused. **Back it up with the database and never change it:** without the same key, every account with two-factor authentication on is locked out (there is no key rotation yet), and `admin reset-mfa` is the only way back in, one account at a time |
| `ADMIN_ROUTE_KEY` | `docker run --rm lewcontext admin route-key generate` prints one (256 bits) | **A secret.** The admin dashboard's path, `/admin/<key>`, and its API header. Unset turns the admin dashboard off. `.env.example`'s throwaway key is refused. See [Admin dashboard](#admin-dashboard) |
| `ADMIN_ROUTE_KEY_PREVIOUS` | unset, except for the hours after a rotation | **A secret.** The key being retired: sessions made under it keep working, it opens no sign-in. See [Rotating the admin key](#rotating-the-admin-key) |
| `ADMIN_ALLOWED_IPS` | your office, VPN or bastion addresses, e.g. `203.0.113.10,198.51.100.0/24` | The only client addresses the admin dashboard answers; unset allows any. Needs `CLIENT_IP_HEADER` behind a proxy. See [Admin dashboard](#admin-dashboard) |
| `CLIENT_IP_HEADER`, `TRUSTED_PROXIES` | for example `X-Forwarded-For` and `127.0.0.1/32,::1/128` | The real client IP behind a proxy. Set both or neither. See [Behind a proxy](#behind-a-proxy-or-load-balancer) |
| `SHUTDOWN_DELAY` | unset: `5s` | How long a stopping server keeps serving with readiness already 503, so the load balancer takes it out first; 0 to 60 s. See [Releasing](#releasing-a-new-version) |
| `METRICS_STORE` | unset: `on` | Each server writes a minute's metrics to the database: a row per route with traffic, never a row per request. `off` stops it; `/metrics` and the logs still work. See [Metrics](#metrics) |
| `METRICS_ADDR`, `METRICS_TOKEN` | unset (off), or for example `10.0.0.5:9090` and 32 random characters | `/metrics` for Prometheus or similar, on a listener of its own, never the public one. **In production, anything but a loopback or private address (`127.0.0.1`, `10.0.0.5`, `fd00::5`, `localhost`) is refused without `METRICS_TOKEN`**: every interface (`:9090`, `0.0.0.0`, `::`, with a zone or not), a public or link-local address, a host name. In a container that means a token ([Metrics](#metrics)). The token is a secret |
| `SLOW_QUERY_MS` | unset: `500` | A query slower than this is logged at warn, without its values |
| `LOG_FORMAT`, `LOG_LEVEL`, `ACCESS_LOG` | unset: `json`, `info`, `all` | The server's log on stdout: one JSON object per line, a line per request. `ACCESS_LOG=errors` keeps only failures and slow requests, for a log store that charges by size. See [Logs and health checks](#logs-and-health-checks) |

`RATE_LIMITS` must stay unset (or `on`): `off`, for the end-to-end tests, is refused in production. The server refuses to start if any of these rules is broken, rather than starting insecurely. `DEMO_PASSWORD`, `MAIL_FILE` and `API_URL` are for development only. Tests use the committed `.env.testing`, never production settings.

### Planned

| Variable | Plan | Notes |
| --- | --- | --- |
| `PER_IP_LIMITS` | [captcha-challenges](../plans/features/captcha-challenges.md) | `off` only if the proxy can't send the client's IP at all |
| `CAPTCHA_PROVIDER`, `CAPTCHA_SITE_KEY`, `CAPTCHA_SECRET_KEY` | [captcha-challenges](../plans/features/captcha-challenges.md) | `turnstile` with real keys, or `off`. It must be set explicitly in production, and Cloudflare's test keys are refused |
| `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ZONE_ID` | [captcha-challenges](../plans/features/captcha-challenges.md) (future use case) | Only for the edge-protection listener, which isn't built |

## First deploy

1. **Freeze the migrations.** While the project is in development, migrations are squashed: tables are changed by editing their create migration, followed by `make migrate-fresh`. Before the first production database holds data, that stops. From then on, migrations are append-only and an applied migration is never edited ([api/database.md](api/database.md)). `migrate fresh` is refused with `APP_ENV=production`, but editing an applied migration isn't detected, so this step is on you.
2. **Create the database and roles**, with TLS: a migration role that owns the tables and a runtime role for the servers ([Database roles](#database-roles)).
3. **Migrate:** `docker run --rm --env-file prod.env lewcontext migrate`. `migrate status` shows what's applied. It is safe to run from several machines at once, because of the advisory lock.
4. **Start the server:** `docker run -d --env-file prod.env -p 10.0.0.5:3000:3000 lewcontext`.
5. **Check it:** `GET /api/health` returns `{"status":"ok"}`, or 503 when the database is unreachable or the server is shutting down: use it for the load balancer's health check (readiness). `GET /api/health/live` answers without the database: use it for an orchestrator's liveness probe, so a database outage doesn't restart every server ([Logs and health checks](#logs-and-health-checks)). Then open `APP_ORIGIN` and register an account. New accounts start with no workspace and go to onboarding.
6. **Schedule pruning:** daily, every target ([Retention and erasure](#retention-and-erasure)): `prune pseudonymise -confirm`, `prune events -confirm`, `prune audit_events -confirm`, `prune app_errors -confirm`, `prune request_hours -confirm` and `prune metric_hours -confirm`. `pseudonymise` cuts events' IPs to their network and replaces their emails with keyed pseudonyms at 30 days; `events` deletes events other than audit events at 90 days and `audit_events` audit events at a year, both keeping any with a pending delivery; then grouped errors not seen for 30 days, and hourly metric sums older than 90 days ([api/database.md](api/database.md#pruning)). All are allowed in production. Sessions, rate-limit windows, reset links, minute metrics (48 hours) and stopped servers' rows (a day) are cleaned up by the server itself.

## Admin dashboard

`https://app.example.com/admin/<ADMIN_ROUTE_KEY>`: every account and workspace, read-only apart from the actions operators take behind a fresh code and a reason (unblocking an email address, signing an account out, resetting its MFA, suspending it, revoking a workspace's invitations, retrying deliveries, IP blocks, signing another admin out), with every read and action recorded ([api/admin.md](api/admin.md#actions)).

1. **Set `ADMIN_ROUTE_KEY`** from your secret store, and restart.
2. **Grant the first admin:** they register an account in the app as anyone would, then `docker run --rm --env-file prod.env lewcontext admin grant -email ada@example.com -confirm`. Without two-factor authentication on, they set it up at their first admin sign-in, within 24 hours (run `grant` again for another day). Admins are **viewers** (every page, read-only) unless granted `-role operator`, which the actions need (each behind a fresh code and a reason, 60 an hour per admin); give it only to those who must act. `admin list` shows every admin, their role and MFA; `admin revoke -email … -confirm` takes it away and signs out their admin sessions. The dashboard's Admins page shows the same with last sign-ins and live sessions, and downloads it as CSV for an access review.
3. **Keep `/admin/` out of your proxy's access logs**, or redact the path: the key is in it. The server's own log never holds a path (its access line says `page /admin/*`), but Cloudflare and most proxies log paths; the key never travels in a header to anywhere but this server (`X-Admin-Key`), and admin pages send `Referrer-Policy: no-referrer`. The admin pages' code is a build of its own, at `/admin-assets/…` (no key in those paths), which the server sends only to a browser holding the assets pass an admin page set ([api/admin.md](api/admin.md#the-admin-build)).
4. **Never cache `/admin-assets/` in a shared cache.** Its files are sent `Cache-Control: private, max-age=28800` (browsers only, for the assets pass's 8 hours); don't add a "cache everything" or edge-cache rule (Cloudflare Cache Rules, a CDN, nginx `proxy_cache`) that covers it, or the cache would hand admin code to anyone. Leave its cookie alone too: the pass travels in `__Secure-lewcontext_admin_assets`.
5. **Allow only your own addresses** where you can: `ADMIN_ALLOWED_IPS` (office, VPN or bastion addresses or ranges) makes the dashboard, its API and its files an ordinary 404 from anywhere else, even with the key, and records each refused address (`admin.access_refused`, once per address per 15 minutes per server; the server log warns too). A refusal means the key, or an assets pass, has leaked: rotate the key outright. **Behind a proxy** it needs `CLIENT_IP_HEADER` and `TRUSTED_PROXIES`, or every request comes from the proxy and an allowlist naming it lets everyone in; production startup warns when it's set without `CLIENT_IP_HEADER`. **Stronger, where you have it:** put Cloudflare Access, a VPN or Tailscale in front of `/admin/` and `/admin-assets/`, so people authenticate before they reach the server; the allowlist is the layer that works everywhere.
6. **If the key may have leaked** (a log, a screenshot, browser sync, a refusal above), rotate it outright: set a new one and restart, **without** `ADMIN_ROUTE_KEY_PREVIOUS`. Every admin is signed out, every assets pass stops working, and the old URL becomes an ordinary 404. The key alone gets nobody in: an admin's password and second factor are still needed.

### Rotating the admin key

A routine rotation (every 90 days, or when someone with the URL leaves) needn't sign anyone out:

1. `docker run --rm lewcontext admin route-key generate` for a new key.
2. Set it as `ADMIN_ROUTE_KEY`, and the old one as `ADMIN_ROUTE_KEY_PREVIOUS`, on every server; restart. The log says the previous key is set (never its value).
3. Hand out the new URL. Sessions made under the old one carry on, their pages saying the address is being retired; the old URL opens no new sign-in.
4. After 8 hours at most (a session's longest), remove `ADMIN_ROUTE_KEY_PREVIOUS` and restart. Its last sessions end then.

Never set a key that may have leaked as the previous one: replace it outright (step 6 above). While it's set, the previous key fails closed: if the database is unreachable, its pages and API are the ordinary 404.

**IP blocks** (the dashboard's IP blocks page) refuse a few abusive addresses or ranges for up to 30 days, on every server within 30 seconds, through the same client address as everything else (so behind a proxy they need `CLIENT_IP_HEADER` and `TRUSTED_PROXIES`, or they'd block the proxy; a range overlapping `TRUSTED_PROXIES` is refused). A blocked request still reaches the server and costs a little: **for a flood, block at the edge** (a Cloudflare WAF rule or rate limit, your proxy's deny list). The health checks and the admin dashboard (with its current key, from an address `ADMIN_ALLOWED_IPS` allows) are never blocked, so a mistaken block can be lifted from the dashboard; a session still on `ADMIN_ROUTE_KEY_PREVIOUS` is blocked like anything else, so use the current key.

The admin sign-in stores the client's IP address, so the server must be reached over TCP (as the image and `make serve` are); over a unix socket the door answers 503 and logs why.

The image builds both web builds and fails if any admin code reached the app's. A server with `ADMIN_ROUTE_KEY` set and no admin build refuses to start.

## Releasing a new version

1. Build the new image.
2. Run `migrate` with the new image.
3. Replace the running servers. On SIGTERM the server first answers readiness (`GET /api/health`) with 503 and keeps serving for `SHUTDOWN_DELAY` (5 s by default in production), so the load balancer takes it out; then it stops accepting connections and gives in-flight requests and the delivery worker's current batch up to 10 seconds. A delivery that doesn't finish is retried when its lease ends. Last, within 5 seconds, it records `server.stopped`, writes the metrics counted since the last minute, and stores the errors it grouped since its last flush ([api/observability.md](api/observability.md#shutdown)). Give the container the delay plus 20 seconds (10 to drain, 5 for those writes, 5 to spare) before it is killed: Docker's default of 10 s is too short, so use `--stop-timeout 25` with the default delay. On a platform with its own pre-stop wait (Kubernetes' `preStop`), set `SHUTDOWN_DELAY=0s` and use that instead.

Migrations run before the new code, so each migration must work with the old code still running until it is replaced. Add columns and tables first and use them later; never rename or drop in the same release that stops using something.

`migrate rollback` only previews unless `-confirm` is passed. Down migrations destroy data: in production, prefer a forward fix.

## Logs and health checks

The server logs to stdout through one handler ([api/observability.md](api/observability.md)): in production one JSON object per line (`LOG_FORMAT`), from `info` up (`LOG_LEVEL`), with a line per request (`ACCESS_LOG`). Your platform's log driver ships it on (Docker's to Loki, CloudWatch, Datadog …); nothing in the app sends logs anywhere.

- **Every request has an ID**, `req_…`, in its `X-Request-ID` response header, on every line it logged and on any audit event it recorded. A 500 shows it to the user as "Reference: req_…": search the log for it. The ID is always the server's own: most proxies pass a client's `X-Request-ID` on unchanged, so one a proxy in `TRUSTED_PROXIES` sends (letters, digits, `.`, `_`, `-`, at most 64) is logged beside it as `proxyRequestId`, to find the proxy's line, and never used as the ID; from anyone else it's ignored. A request `net/http` refuses before the app sees it (a malformed request line, headers over 16 KiB) has no ID and no line.
- **The access line** names the route's pattern (`GET /api/workspaces/{workspace}/projects`) or a page's label (`page /auth/*`), never the path or query string, so no reset or invitation token, admin key, search or email reaches the log from a URL. It does hold the client's IP address and the signed-in account's `usr_` ID: personal data, so **keep logs 30 days at most** and limit who can read them. The one exception is the **audit lines** (`msg: "audit"`): keep them **a year**, as long as the audit events they stand for ([Retention and erasure](#retention-and-erasure)). An error's text is scrubbed before it's logged (emails, IP addresses, IDs, quoted values, numbers and token-like strings become placeholders, [api/observability.md](api/observability.md#scrubbing)), which is a backstop, not a promise that no word in a message is someone's.
- **Grouped errors:** each server groups its failures in memory and writes them to `app_errors` every 10 seconds, only when something failed: a row per distinct error, counted, scrubbed like the log, never a row per occurrence ([api/observability.md](api/observability.md#grouped-errors-app_errors)). Signed-in browsers report the web app's own crashes there too (`POST /api/client-errors`, 30 an hour per account, 500 an hour in all); that text comes from users, so treat it as such. Until the admin dashboard's Errors page, read it with SQL: `SELECT source, route, message, count, last_seen_at FROM app_errors WHERE resolved_at IS NULL ORDER BY last_seen_at DESC`. Schedule `prune app_errors` (above). A server that can't reach its database keeps at most 1,000 groups in memory and tries again every 10 seconds; the log is complete either way.
- **Logging is synchronous.** Each line is written to stdout as the request ends, so a log driver that falls behind makes requests wait. Use a non-blocking one (Docker: `--log-opt mode=non-blocking --log-opt max-buffer-size=4m`, which drops lines rather than stall when the buffer is full).
- **Health checks:** `GET /api/health` for the load balancer (503 when the database is down, and from the moment a shutdown begins); `GET /api/health/live` for liveness (no database). Both log at `debug`, so probes don't fill the log.
- **Slow queries** (over `SLOW_QUERY_MS`, 500 ms by default) are logged at warn with their operation, table, duration and store method, never their text or values ([api/observability.md](api/observability.md#slow-queries)).
- **External uptime checks.** Nothing inside the app can say the app is down. Set up a check from outside your network (your monitoring provider's, from two or more regions) on `GET /api/health` and `/` every minute, paging someone after two failures, and one on the TLS certificate's expiry.

## Retention and erasure

The retention schedule is in [compliance.md](compliance.md#retention). Every age is a `prune` target; run each daily from cron, with the image's environment, so `APP_KEY` (which `pseudonymise` needs) is the servers':

```cron
15 3 * * *  docker run --rm --env-file /etc/lewcontext/prod.env lewcontext prune pseudonymise -confirm
20 3 * * *  docker run --rm --env-file /etc/lewcontext/prod.env lewcontext prune events -confirm
25 3 * * *  docker run --rm --env-file /etc/lewcontext/prod.env lewcontext prune audit_events -confirm
30 3 * * *  docker run --rm --env-file /etc/lewcontext/prod.env lewcontext prune app_errors -confirm
35 3 * * *  docker run --rm --env-file /etc/lewcontext/prod.env lewcontext prune request_hours -confirm
40 3 * * *  docker run --rm --env-file /etc/lewcontext/prod.env lewcontext prune metric_hours -confirm
```

- **Evidence:** each confirmed run records `prune.completed` (its target, how many rows, the cutoff), an audit event kept a year: filter Events by **Retention** to show an auditor it runs. A run that fails exits non-zero; have cron mail or page on it.
- **Logs:** keep the server's log **30 days**, and ship it off the host to storage the servers can't change (S3 Object Lock, a SIEM, your provider's immutable archive). The **audit lines** in it (`msg: "audit"`, one per audit event, never an email or name, the IP cut to its /24 or /48) are the evidence that the `events` table hasn't been altered, so route them (filter on `msg`) to a store kept **a year**, as long as the audit events ([compliance.md](compliance.md#audit-lines)); everything else goes at 30 days. Keep `LOG_LEVEL` at `info` or below, or they aren't written (the server warns at startup).
- **Backups:** 30 days' rotation is recommended. Erased accounts leave them as they expire; say so in the privacy notice.
- **Requests from people** ([compliance.md](compliance.md)): someone who can't sign in asks for their data (`admin export -email … > file.json`) or for their account to be erased (`admin erase -email …`, then `-confirm`). Confirm who is asking outside the app first; both are recorded. Self-service: Account & security's **Download your data** and **Delete account**.

## Database roles

Use two PostgreSQL roles, so a compromised server can't drop the audit trail or change the schema:

- **`lewcontext_owner`**, the migration role: owns the database's tables, sequences and functions; used only by `migrate`.
- **`lewcontext_app`**, the runtime role: what `api`, `prune` and `admin` connect as. Only `SELECT, INSERT, UPDATE, DELETE` on the tables, `USAGE, SELECT` on the sequences, `EXECUTE` on the functions; no DDL, no `TRUNCATE`, no ownership, not a superuser.

```sql
CREATE ROLE lewcontext_owner LOGIN PASSWORD '…';
CREATE ROLE lewcontext_app LOGIN PASSWORD '…';
CREATE DATABASE lewcontext OWNER lewcontext_owner;
\c lewcontext
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO lewcontext_app;
-- After the first migrate (as lewcontext_owner), and so tables later
-- migrations add get the same:
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO lewcontext_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO lewcontext_app;
ALTER DEFAULT PRIVILEGES FOR ROLE lewcontext_owner IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO lewcontext_app;
ALTER DEFAULT PRIVILEGES FOR ROLE lewcontext_owner IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO lewcontext_app;
```

`migrate` then runs with `DATABASE_URL` naming `lewcontext_owner` (a separate env file, used only for releases), and everything else with `lewcontext_app`. The advisory locks need no grant. `events` can't be insert-only (erasure and `pseudonymise` update it, `prune` deletes from it), so the shipped audit lines are the tamper evidence ([compliance.md](compliance.md#database-roles)).

## Metrics

Each server counts requests by route, errors, rate-limit refusals, queries, deliveries and its runtime in memory ([api/observability.md](api/observability.md#metrics)). Two ways to read them:

- **In the database** (`METRICS_STORE=on`, the default): once a minute each server writes the minute just ended in one statement, a row per route with traffic and per series that changed, with its heartbeat in `server_instances`. **Ten requests a minute or ten million write the same rows**: typically about 90 a minute per server, and by construction never more than about 1,200 (at most 300 routes, plus about 9 labelled families of at most 101 series each, plus the gauges), so at most about 3.5 million rows per server over 48 hours. One server an hour (whichever takes the advisory lock) sums the hours into `request_hours` and `metric_hours`, each hour in a short transaction of its own, and deletes minutes older than 48 hours (7 days at worst, if the hours can't be summed), so the minute tables stay bounded without cron; times come from the database's clock, and a server whose clock is more than 5 minutes off has its minutes dropped; prune the hour tables after 90 days. If the database is down a minute is dropped, never retried. The admin dashboard's Performance page (plan A4) reads them.
- **`/metrics`** for Prometheus, Grafana Agent or similar: set `METRICS_ADDR` to a private address the scraper can reach (`10.0.0.5:9090`), and `METRICS_TOKEN` to 32 or more random characters (`openssl rand -base64 32`), sent as `Authorization: Bearer <token>`. **In Docker**, the container's own address isn't the host's, so the server must listen on `METRICS_ADDR=0.0.0.0:9090`, which production refuses without `METRICS_TOKEN`: set the token, and let the host side of `-p` limit who can reach it (`-p 10.0.0.5:9090:9090` publishes it on the host's private interface only). It's this process's view since it started: scrape every server. **Never route it through the public proxy.** Production refuses anything but a loopback or private address without the token.

`server_instances` lists the running servers, their version and a summary of their configuration (which settings are set, never a secret's value), and `server.started` and `server.stopped` events mark each start and graceful stop: a release shows as starts with a new version, a crash loop as starts without stops.

## Running several servers

Everything that must be shared lives in PostgreSQL, so several `api` processes behind one load balancer work without sticky sessions:

| Shared state | Where | Across servers |
| --- | --- | --- |
| Sessions, CSRF tokens | `sessions` | any server serves any request |
| Rate-limit counters | `rate_limits` | exact across servers |
| Event deliveries (mail, future listeners) | `event_deliveries`, claimed with `FOR UPDATE SKIP LOCKED` | each delivery runs on one server; a crashed one's lease expires after 5 minutes and another takes over |
| Reset links and invitations | digests in their tables | any server can check a link |
| IP blocks | `ip_blocks`, reloaded into memory every 30 seconds | a new block applies on the server that made it at once, and on every other within 30 seconds; a server started while the database was down blocks nothing until its retries (every second, doubling to 30) load the list, and logs an error meanwhile |
| The admin assets pass | an HMAC in the cookie, under `APP_KEY` and the admin key's digest | any server accepts another's, **if every server has the same `APP_KEY` and `ADMIN_ROUTE_KEY`** (and `ADMIN_ROUTE_KEY_PREVIOUS` and `ADMIN_ALLOWED_IPS`, or a session works on one server and not the next) |
| Sign-ups and email codes | `pending_registrations`, `email_verifications`: HMACs under `APP_KEY` | any server can check a code, **if every server has the same `APP_KEY`** |
| CAPTCHA tokens (**Planned**) | single use enforced by Cloudflare | no table needed |

Things that are per process:

- the 4 password-hashing slots;
- the in-memory read budget (`api-read:per-user`, [rate limits](api/rate-limits.md));
- which addresses outside `ADMIN_ALLOWED_IPS` were recorded lately (`admin.access_refused` once per address per 15 minutes, per server), and the queue of those still to record;
- the development mailbox file (development only).

With N servers, these allow N times as much. That is fine for load protection.

## Behind a proxy or load balancer

TLS ends at the proxy, so the server sees every connection coming from the proxy's address. Unless told otherwise, it uses that address, so behind a proxy:

- every per-IP limit (`auth:per-ip`, 60 sign-in attempts per 15 minutes; `register:per-ip`, 10 sign-ups an hour) is shared by all users together. Signed-in limits count by user, so they aren't affected;
- audit events record the proxy's address.

To fix that, set `CLIENT_IP_HEADER` to the header your proxy writes the client's address in, and `TRUSTED_PROXIES` to the addresses or CIDR ranges the proxy connects from (comma-separated). The header is believed **only** on connections from those addresses, because anyone can send a forged one. `X-Forwarded-For` is read from the right, skipping your own proxies, so entries a client adds at the left are never chosen. Plan: [client-ip](../plans/features/implemented/client-ip.md).

| Setup | `CLIENT_IP_HEADER` | `TRUSTED_PROXIES` | Proxy config |
| --- | --- | --- | --- |
| nginx or Caddy on the same machine | `X-Forwarded-For` | `127.0.0.1/32,::1/128` | nginx: `proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;` (Caddy sets it by default) |
| nginx with `X-Real-IP` | `X-Real-IP` | `127.0.0.1/32,::1/128` | `proxy_set_header X-Real-IP $remote_addr;`: the proxy must **overwrite** a single-value header, never pass on the client's own |
| Cloud load balancer in a private network | `X-Forwarded-For` | the load balancer's subnet, e.g. `10.0.0.0/16` | |
| Cloudflare → server | `CF-Connecting-IP` | Cloudflare's ranges from <https://www.cloudflare.com/ips-v4> and <https://www.cloudflare.com/ips-v6>. Recheck these occasionally, because Cloudflare does change them | firewall the server to Cloudflare's ranges only |
| Cloudflare Tunnel (`cloudflared` on the same machine) | `CF-Connecting-IP` | `127.0.0.1/32,::1/128` | no open inbound port |

- **Refused at startup:** one variable without the other, a header other than `X-Forwarded-For`, `X-Real-IP`, `CF-Connecting-IP` or `True-Client-IP` (any case; not RFC 7239 `Forwarded`), a range that doesn't parse, an IPv4-mapped IPv6 range (write it as IPv4), and `0.0.0.0/0` or `::/0`, which would be the same as believing the header from anyone. The server logs the header it uses when it starts.
- **A header that doesn't parse** as an address falls back to the last trusted hop (or the connection), never to a value the client chose. At most 20 `X-Forwarded-For` entries are read; a single-value header with more than one value is ignored.
- **IPv6 clients** are counted by their /64 in every per-IP limit, with or without a proxy. Events record the full address.
- **Only use `PER_IP_LIMITS=off`** (**Planned**, [captcha-challenges](../plans/features/captcha-challenges.md)) if the proxy can't send the client's address at all. It switches every per-IP limit off, and sign-in then relies on per-account limits and the edge.

Proxy timeouts should be at least the server's own: 15 s to read a request and 20 s to write a response.

## Cloudflare

These notes apply when the domain is proxied through Cloudflare.

- **Lock the origin to Cloudflare.** Use a firewall allowing only Cloudflare's IP ranges, or a Cloudflare Tunnel. Otherwise a client can skip Cloudflare's rules by connecting to the server's own address.
- **Bot rules on `/api/*` are fine.** Today `/api/*` is the web app's own same-origin API, called only by our pages. Managed challenges, Bot Fight Mode and rate-limiting rules there are normal practice:
  - the browser gets Cloudflare's clearance cookie when a page load is challenged, and sends it on API calls;
  - an API call without clearance gets Cloudflare's page instead of JSON, and the app shows "Unable to connect. Please try again."
- **Exclude any public API from bot and challenge rules.** A future programmatic API, for example `/api/v1/*` called with API keys, can't solve a challenge. Rules must exclude it, for example `starts_with(http.request.uri.path, "/api/") and not starts_with(http.request.uri.path, "/api/v1/")`. Protect it with its keys, its own rate limits, and edge rules that count and block, never challenge. **This is configured by the site owner in Cloudflare**, not by the app.
- **Turnstile (Planned):**
  - **Keys:** create a widget in the Cloudflare dashboard for `APP_ORIGIN`'s hostname, then set `CAPTCHA_PROVIDER=turnstile`, `CAPTCHA_SITE_KEY` and `CAPTCHA_SECRET_KEY`.
  - **When it loads:** the Turnstile script loads only while a rate limit is in challenge mode, and only from `https://challenges.cloudflare.com` (allowed in the page CSP).
  - **What it can read:** while loaded, it runs in our login and register pages, as any CAPTCHA does. The reset and invitation link pages never load it.
  - **If Cloudflare is unreachable** while a limit is in challenge mode, those requests get 503 (the check fails closed). Normal traffic never calls Cloudflare.
- **Edge protection from rate-limit events (Planned, future use case):** a listener on `rate_limit.reached` could enable a pre-made, disabled Cloudflare rule for the route under attack, and disable it when the window ends. It needs an API token with only "Zone WAF: Edit" on one zone. See [captcha-challenges](../plans/features/captcha-challenges.md#future-use-case-turning-on-cloudflare-edge-protection-not-built).

## Turning on email verification

`EMAIL_VERIFICATION` is on by default, so a new install verifies every sign-up from the start. For an install that already has accounts:

1. **Turn on `EMAIL_VERIFICATION`** (it's the default; just don't set `off`). New sign-ups are verified from then on. Existing accounts keep working; their Account settings say "Not verified" with a Verify email button, and admins see the same.
2. **Later, once most have verified, set `EMAIL_VERIFICATION_EXISTING=required`.** The rest verify at their next sign-in, which spreads out by itself over hours or days. A code is only sent when someone presses Send code, so the switch emails nobody by itself; `email-verification:global` (500 an hour) caps the mail if many sign in at once.
3. A completed password reset also verifies an account's email: the link went only to that inbox.

Someone whose old address no longer receives mail can't get past the verify page under `required`, and there's no way to change an email yet. Turning `required` off again lets them back in.

## Email

Password reset links and workspace invitations go out over SMTP (`MAIL_DRIVER=smtp`, [api/mail.md](api/mail.md)). Any provider that accepts SMTP works; changing provider changes only these settings:

| Provider | `SMTP_HOST` | `SMTP_PORT` / `SMTP_TLS` | `SMTP_USERNAME` | `SMTP_PASSWORD` |
| --- | --- | --- | --- | --- |
| Resend | `smtp.resend.com` | `587` / `starttls`, or `465` / `tls` | `resend` | an API key |
| Postmark | `smtp.postmarkapp.com` | `587` / `starttls` | a server API token | the same token |
| Amazon SES | `email-smtp.<region>.amazonaws.com` | `587` / `starttls` | SES SMTP credentials (not IAM access keys) | |
| Mailtrap's online sandbox (staging) | from your Mailtrap inbox | its settings | | |

Check each provider's own docs for current values.

- **Before the first email:** verify the sending domain with the provider and publish the DNS records it gives you: SPF, DKIM and a DMARC policy. Without them, mail lands in spam or is refused. The app doesn't sign mail itself; the provider does.
- **If the provider is down,** emails wait and retry for about an hour (1 s, 10 s, 1 min, 10 min, 1 h), then are marked failed and logged as an error. Requests keep working: handlers only queue mail. Failures are in `event_deliveries.last_error` (scrubbed: the recipient's address is `<email>`), and a delivery that fails for good is also a grouped error (`app_errors`, source `worker`).
- **Abuse:** each email someone can trigger has a per-recipient limit on the request (`password-reset:per-email`, 3 an hour; `invitation:per-email`, 5 an hour; `register:per-email`, 3 an hour) besides the per-IP, per-user and global ones ([api/rate-limits.md](api/rate-limits.md)), so the server can't be used to flood an inbox. "You already have an account" is also capped at 5 a day per account, silently.
- **Without a provider,** set `MAIL_DRIVER=off` and `EMAIL_VERIFICATION=off`: password reset answers 503 ("Password reset isn't available.") and the Invite dialog says "Invitations aren’t available on this server." Don't advertise either feature then. Sign-up is then unverified, and says when an email is taken.
- **Email verification** (on by default) sends a code to every sign-up, so sign-up needs mail to work at all ([api/auth.md](api/auth.md#email-verification)).
- **Blocked addresses:** every invitation carries a "Block this address" link and a `List-Unsubscribe` header; an address its owner blocks gets no more email and can't sign up ([api/auth.md](api/auth.md#blocking-an-email-address)). Someone who writes in after their link has expired (a year) can be unblocked with `docker run --rm --env-file prod.env lewcontext admin unblock -email … -confirm`, or by an operator on the admin dashboard's Email blocks page (with a reason, recorded); `admin blocks` lists them. The block list (`email_blocks`) is a suppression list: keep it with the database's backups.

## Security checklist

- [ ] `APP_ENV=production`, an HTTPS `APP_ORIGIN`, and `DATABASE_URL` with `sslmode=verify-full`.
- [ ] The database roles aren't superusers: `migrate` as the owner, the servers, `prune` and `admin` as a runtime role without DDL ([Database roles](#database-roles)); the database isn't reachable from the internet.
- [ ] The server's port is reachable only by the proxy (private interface, firewall, or Tunnel).
- [ ] Migrations frozen (append-only) before the first real data.
- [ ] `prune` scheduled daily for every target (`pseudonymise`, `events`, `audit_events`, `app_errors`, `request_hours`, `metric_hours`), its failures reported, and `prune.completed` showing on Events ([Retention and erasure](#retention-and-erasure)).
- [ ] `METRICS_ADDR` unset, or on a private address the public can't reach, with `METRICS_TOKEN` (32+ random characters) in the secret store; `/metrics` never routed through the public proxy.
- [ ] `APP_KEY` generated for this deployment, kept in the secret store, and backed up with the database.
- [ ] `ADMIN_ROUTE_KEY` generated for this deployment and kept in the secret store (or unset, with no admin dashboard); `ADMIN_ROUTE_KEY_PREVIOUS` unset outside a rotation; `/admin/` kept out of access logs or redacted; no shared-cache rule covering `/admin-assets/`; `ADMIN_ALLOWED_IPS` set to your own addresses where possible (with `CLIENT_IP_HEADER` behind a proxy), or Cloudflare Access or a VPN in front of `/admin/`; admins granted only to people who need it, as viewers unless they must act (`admin list`, the Admins page), and reviewed quarterly.
- [ ] `seed` never run (it refuses in production anyway).
- [ ] Behind a proxy: `CLIENT_IP_HEADER` and `TRUSTED_PROXIES` set, listing only your own proxies (or Cloudflare's current ranges), and a single-value header overwritten by the proxy.
- [ ] Behind Cloudflare: the origin locked to Cloudflare, and any public API excluded from bot and challenge rules.
- [ ] Email: `MAIL_DRIVER=smtp` with `SMTP_TLS` `starttls` or `tls`, a verified sending domain with SPF, DKIM and DMARC, and `SMTP_PASSWORD` in the secret store; or `MAIL_DRIVER=off` and `EMAIL_VERIFICATION=off` deliberately.
- [ ] Every server on the database has the same `APP_KEY`.
- [ ] Logs shipped off the server to storage the servers can't change, at `LOG_LEVEL=info` (the audit lines), kept 30 days at most (the audit lines a year) and readable only by those who need them; an external uptime check on `/api/health` and `/`, and one on the certificate's expiry.
- [ ] A privacy notice naming the processors (hosting, SMTP, Cloudflare), the retention schedule and how to ask for data or erasure; a breach register; the [incident runbook](incident-response.md) known to whoever is on call.
- [ ] Turnstile keys are real (not Cloudflare's test keys), once built.
- [ ] No secrets in the image or the repository: pass them as environment variables or through your platform's secret store.
- [ ] The server's own security headers aren't stripped or overridden by the proxy: CSP with script hashes, `X-Frame-Options: DENY`, HSTS, `no-store` caching on the app shell and API.

## Related

- [testing.md](testing.md): local setup, `make build`, `make serve` (the production binary on this machine), and `make docker-dev`.
- [api/README.md](api/README.md): environment variables and how Go serves the web app.
- [api/auth.md](api/auth.md): sessions, cookies, CSRF and headers.
- [api/rate-limits.md](api/rate-limits.md): every rate limit and quota.
- [api/database.md](api/database.md): migrations and pruning.
