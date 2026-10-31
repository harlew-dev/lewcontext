# Deploying lewcontext

How to run lewcontext in production, and what to set up around it. Everything marked **Planned** is designed in [`plans/features/`](../plans/features) but not built yet: don't configure it until its plan is implemented.

## What you deploy

One image (the root `Dockerfile`) holding four binaries:

| Command | Does | Run it |
| --- | --- | --- |
| `api` (the default `CMD`) | The Go server: the JSON API under `/api`, the web app built into the binary, the event delivery worker, and a 15-minute cleanup loop | as a long-running service |
| `migrate` | Applies database migrations, inside a transaction each, under an advisory lock | once per release, **before** starting the new `api` |
| `prune` | Deletes old rows from log tables (`events`) | from cron |
| `seed` | Demo data | never: it refuses to run with `APP_ENV=production` |

There is one process and one origin: the same server sends pages and API responses, so there is no CORS and no separate frontend host. The server does **not** migrate on startup.

```sh
docker build -t lewcontext --build-arg APP_NAME=lewcontext .   # APP_NAME is baked into the web build
make build                                                     # or, without Docker: bin/api
```

## Requirements

- **PostgreSQL 15+** reachable over TLS with a verifiable certificate. Use a dedicated role that owns the app's tables, not a superuser.
- **Something that terminates HTTPS in front of the server.** The server only speaks plain HTTP, and production refuses an `http://` origin. That can be a reverse proxy (nginx, Caddy), a cloud load balancer, or Cloudflare. See [Behind a proxy](#behind-a-proxy-or-load-balancer).
- **Outbound HTTPS** from the server to `challenges.cloudflare.com`, once CAPTCHA challenges are built (**Planned**).

## Configuration

### Today

| Variable | Production value | Notes |
| --- | --- | --- |
| `APP_ENV` | `production` | Turns on `Secure` `__Host-lewcontext_session` cookies, HSTS on pages, and the strict checks below. Without it the server runs in development mode |
| `APP_ORIGIN` | `https://app.example.com` | The exact public origin: scheme and host, no trailing slash. **HTTPS is required.** Mutations from any other origin are refused, and every emailed link is built from it |
| `DATABASE_URL` | `postgresql://lewcontext:…@db.internal:5432/lewcontext?sslmode=verify-full` | **`sslmode=verify-full` is required.** For a provider with its own CA, mount the CA file and add `&sslrootcert=/path/to/ca.pem` |
| `API_ADDR` | `0.0.0.0:3000` (the image's default) | The listen address. Bind it to a private interface the proxy can reach, never directly to the internet without TLS in front |
| `MAIL_DRIVER` | `off` (the production default) | There is no email delivery yet. `file` (the development mailbox) is refused in production. With `off`, password reset answers 503 |
| `CLIENT_IP_HEADER`, `TRUSTED_PROXIES` | for example `X-Forwarded-For` and `127.0.0.1/32,::1/128` | The real client IP behind a proxy. Set both or neither. See [Behind a proxy](#behind-a-proxy-or-load-balancer) |

`RATE_LIMITS` must stay unset (or `on`): `off`, for the end-to-end tests, is refused in production. The server refuses to start if any of these rules is broken, rather than starting insecurely. `DEMO_PASSWORD`, `MAIL_FILE` and `API_URL` are for development only.

### Planned

| Variable | Plan | Notes |
| --- | --- | --- |
| `PER_IP_LIMITS` | [captcha-challenges](../plans/features/captcha-challenges.md) | `off` only if the proxy can't send the client's IP at all |
| `CAPTCHA_PROVIDER`, `CAPTCHA_SITE_KEY`, `CAPTCHA_SECRET_KEY` | [captcha-challenges](../plans/features/captcha-challenges.md) | `turnstile` with real keys, or `off`. It must be set explicitly in production, and Cloudflare's test keys are refused |
| `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ZONE_ID` | [captcha-challenges](../plans/features/captcha-challenges.md) (future use case) | Only for the edge-protection listener, which isn't built |

## First deploy

1. **Freeze the migrations.** While the project is in development, migrations are squashed: tables are changed by editing their create migration, followed by `make migrate-fresh`. Before the first production database holds data, that stops. From then on, migrations are append-only and an applied migration is never edited ([api/database.md](api/database.md)). `migrate fresh` is refused with `APP_ENV=production`, but editing an applied migration isn't detected, so this step is on you.
2. **Create the database and role**, with TLS.
3. **Migrate:** `docker run --rm --env-file prod.env lewcontext migrate`. `migrate status` shows what's applied. It is safe to run from several machines at once, because of the advisory lock.
4. **Start the server:** `docker run -d --env-file prod.env -p 10.0.0.5:3000:3000 lewcontext`.
5. **Check it:** `GET /api/health` returns `{"status":"ok"}`, or 503 when the database is unreachable. Use it for load-balancer health checks. Then open `APP_ORIGIN` and register an account. New accounts start with no workspace and go to onboarding.
6. **Schedule pruning:** for example, daily `prune events -confirm`. That deletes events older than 90 days, keeping any with a pending delivery. It is allowed in production. Sessions, rate-limit windows and reset links are cleaned up by the server itself.

## Releasing a new version

1. Build the new image.
2. Run `migrate` with the new image.
3. Replace the running servers. On SIGTERM the server stops accepting connections, then gives in-flight requests and the delivery worker's current batch up to 10 seconds. A delivery that doesn't finish is retried when its lease ends. Give the container at least that long before it is killed: Docker's default of 10 s is tight, so use `--stop-timeout 15`.

Migrations run before the new code, so each migration must work with the old code still running until it is replaced. Add columns and tables first and use them later; never rename or drop in the same release that stops using something.

`migrate rollback` only previews unless `-confirm` is passed. Down migrations destroy data: in production, prefer a forward fix.

## Running several servers

Everything that must be shared lives in PostgreSQL, so several `api` processes behind one load balancer work without sticky sessions:

| Shared state | Where | Across servers |
| --- | --- | --- |
| Sessions, CSRF tokens | `sessions` | any server serves any request |
| Rate-limit counters | `rate_limits` | exact across servers |
| Event deliveries (mail, future listeners) | `event_deliveries`, claimed with `FOR UPDATE SKIP LOCKED` | each delivery runs on one server; a crashed one's lease expires after 5 minutes and another takes over |
| Reset links and invitations | digests in their tables | any server can check a link |
| CAPTCHA tokens (**Planned**) | single use enforced by Cloudflare | no table needed |

Things that are per process:

- the 4 password-hashing slots;
- the in-memory read budget (`api-read:per-user`, [rate limits](api/rate-limits.md));
- the development mailbox file (development only).

With N servers, these allow N times as much. That is fine for load protection.

## Behind a proxy or load balancer

TLS ends at the proxy, so the server sees every connection coming from the proxy's address. Unless told otherwise, it uses that address, so behind a proxy:

- every per-IP limit (`auth:per-ip`, 60 sign-in attempts per 15 minutes; `register:per-ip`, 10 sign-ups an hour) is shared by all users together. Signed-in limits count by user, so they aren't affected;
- audit events record the proxy's address.

To fix that, set `CLIENT_IP_HEADER` to the header your proxy writes the client's address in, and `TRUSTED_PROXIES` to the addresses or CIDR ranges the proxy connects from (comma-separated). The header is believed **only** on connections from those addresses, because anyone can send a forged one. `X-Forwarded-For` is read from the right, skipping your own proxies, so entries a client adds at the left are never chosen. Plan: [client-ip](../plans/features/client-ip.md).

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

## Email

There is **no email delivery** yet. In production `MAIL_DRIVER` is `off`, so:

- password reset answers 503 ("Password reset isn't available.");
- inviting someone to a workspace answers 503 too, and the Invite dialog says "Invitations aren’t available on this server." ([auth.md](api/auth.md#invitations)).

A real mail driver (SMTP or a provider) is a future plan. Don't advertise password reset or invitations on a production deployment until it exists.

## Security checklist

- [ ] `APP_ENV=production`, an HTTPS `APP_ORIGIN`, and `DATABASE_URL` with `sslmode=verify-full`.
- [ ] The database role isn't a superuser, and the database isn't reachable from the internet.
- [ ] The server's port is reachable only by the proxy (private interface, firewall, or Tunnel).
- [ ] Migrations frozen (append-only) before the first real data.
- [ ] `prune` scheduled.
- [ ] `seed` never run (it refuses in production anyway).
- [ ] Behind a proxy: `CLIENT_IP_HEADER` and `TRUSTED_PROXIES` set, listing only your own proxies (or Cloudflare's current ranges), and a single-value header overwritten by the proxy.
- [ ] Behind Cloudflare: the origin locked to Cloudflare, and any public API excluded from bot and challenge rules.
- [ ] Turnstile keys are real (not Cloudflare's test keys), once built.
- [ ] No secrets in the image or the repository: pass them as environment variables or through your platform's secret store.
- [ ] The server's own security headers aren't stripped or overridden by the proxy: CSP with script hashes, `X-Frame-Options: DENY`, HSTS, `no-store` caching on the app shell and API.

## Related

- [testing.md](testing.md): local setup, `make build`, `make serve` (the production binary on this machine), and `make docker-dev`.
- [api/README.md](api/README.md): environment variables and how Go serves the web app.
- [api/auth.md](api/auth.md): sessions, cookies, CSRF and headers.
- [api/rate-limits.md](api/rate-limits.md): every rate limit and quota.
- [api/database.md](api/database.md): migrations and pruning.
