# Deploying lewcontext

How to run lewcontext in production, and what to set up around it. Everything marked **Planned** is designed in [`plans/features/`](../plans/features) but not built yet: don't configure it until its plan is implemented.

## What you deploy

One image (the root `Dockerfile`) holding four binaries:

| Command | Does | Run it |
| --- | --- | --- |
| `api` (the default `CMD`) | The Go server: the JSON API under `/api`, the web app built into the binary (and the admin dashboard's own build, served only behind its key), the event delivery worker, and a 15-minute cleanup loop | as a long-running service |
| `migrate` | Applies database migrations, inside a transaction each, under an advisory lock | once per release, **before** starting the new `api` |
| `prune` | Deletes old rows from log tables (`events`) | from cron |
| `seed` | Demo data | never: it refuses to run with `APP_ENV=production` |
| `admin` | The operator's commands, previewing without `-confirm`: `admin grant` (with `-role viewer|operator`), `revoke` and `list` for the admin dashboard; `admin reset-mfa -email … -confirm` turns off two-factor authentication for someone who lost their authenticator and recovery codes, and signs out their sessions; `admin blocks` lists blocked email addresses and `admin unblock -email … -confirm` takes one off the list; `admin app-key generate` and `admin route-key generate` print a new `APP_KEY` or `ADMIN_ROUTE_KEY` | by hand; check who is asking out of band before `reset-mfa` |

There is one process and one origin: the same server sends pages and API responses, so there is no CORS and no separate frontend host. The server does **not** migrate on startup.

```sh
docker build -t lewcontext --build-arg APP_NAME=lewcontext .   # APP_NAME is baked into the web build
make build                                                     # or, without Docker: bin/api
```

## Requirements

- **PostgreSQL 15+** reachable over TLS with a verifiable certificate. Use a dedicated role that owns the app's tables, not a superuser.
- **Something that terminates HTTPS in front of the server.** The server only speaks plain HTTP, and production refuses an `http://` origin. That can be a reverse proxy (nginx, Caddy), a cloud load balancer, or Cloudflare. See [Behind a proxy](#behind-a-proxy-or-load-balancer).
- **An SMTP provider** (Resend, Postmark, Amazon SES, Mailgun, …) and **outbound SMTP** from the server to it, on 587 or 465. Some hosts block outgoing SMTP ports on new accounts; ask them to open it. Or run with `MAIL_DRIVER=off` and no password reset or invitations. See [Email](#email).
- **Outbound HTTPS** from the server to `challenges.cloudflare.com`, with `CAPTCHA_PROVIDER=turnstile` (a token check only while a limit is in challenge mode).

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
| `CAPTCHA_PROVIDER` | `turnstile`, or `off` deliberately | **Required in production.** `turnstile`: above a rate limit's challenge level, sign-in, sign-up, reset requests and invitations ask for a Cloudflare Turnstile check ([rate-limits.md](api/rate-limits.md#challenges)). `off`: challenge levels are skipped (block levels still apply), with a warning at startup. Outside production it defaults to `turnstile` with Cloudflare's always-passing test keys |
| `CAPTCHA_SITE_KEY`, `CAPTCHA_SECRET_KEY` | your Turnstile widget's keys | Required with `turnstile`. The site key is public (sent to browsers in the 428). **The secret key is a secret:** pass it through your secret store; it's never logged or sent anywhere but Cloudflare. Cloudflare's published test keys (`1x…`, `2x…`, `3x…`) are refused in production |
| `PER_IP_LIMITS` | `on` (the default) | `off` only if the proxy can't send the client's address at all: every per-IP limit (both levels) is then off, with a warning. Set `CLIENT_IP_HEADER` and `TRUSTED_PROXIES` instead; production refuses `off` together with `CLIENT_IP_HEADER` |
| `POLICIES` | `off` (the default), or `on` once the documents are yours | `on` publishes `/policy/terms-of-service` and `/policy/privacy-policy` and makes every sign-up agree to them. **Production refuses `on` while either document is still the template** (`boilerplate: true`). See [Terms of Service and Privacy Policy](#terms-of-service-and-privacy-policy) |
| `MAIL_FROM_ADDRESS`, `MAIL_FROM_NAME` | `no-reply@example.com`, `Example` | The sender. The address must be bare, on a domain your provider has verified. The name defaults to `APP_NAME` |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_TLS` | `smtp.resend.com`, `587`, `starttls` | `starttls` (port 587 by default) or `tls` (465). **`none` is refused in production.** Certificates are always verified |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | your provider's | Both or neither. **A secret:** pass it through your platform's secret store, not the image |
| `APP_KEY` | 32 random bytes in base64: `docker run --rm lewcontext admin app-key generate` prints one (or `openssl rand -base64 32`) | **A secret, and required.** Seals two-factor secrets at rest, keys recovery-code and emailed-code digests and signs the admin dashboard's assets pass; **every server on one database needs the same one** (one server's worker may send a code another checks). `.env.example`'s key is refused. **Back it up with the database and never change it:** without the same key, every account with two-factor authentication on is locked out (there is no key rotation yet), and `admin reset-mfa` is the only way back in, one account at a time |
| `ADMIN_ROUTE_KEY` | `docker run --rm lewcontext admin route-key generate` prints one (256 bits) | **A secret.** The admin dashboard's path, `/admin/<key>`, and its API header. Unset turns the admin dashboard off. `.env.example`'s throwaway key is refused. See [Admin dashboard](#admin-dashboard) |
| `ADMIN_ROUTE_KEY_PREVIOUS` | unset, except for the hours after a rotation | **A secret.** The key being retired: sessions made under it keep working, it opens no sign-in. See [Rotating the admin key](#rotating-the-admin-key) |
| `ADMIN_ALLOWED_IPS` | your office, VPN or bastion addresses, e.g. `203.0.113.10,198.51.100.0/24` | The only client addresses the admin dashboard answers; unset allows any. Needs `CLIENT_IP_HEADER` behind a proxy. See [Admin dashboard](#admin-dashboard) |
| `CLIENT_IP_HEADER`, `TRUSTED_PROXIES` | for example `X-Forwarded-For` and `127.0.0.1/32,::1/128` | The real client IP behind a proxy. Set both or neither. See [Behind a proxy](#behind-a-proxy-or-load-balancer) |

`RATE_LIMITS` must stay unset (or `on`): `off`, for the end-to-end tests, is refused in production. The server refuses to start if any of these rules is broken, rather than starting insecurely. `DEMO_PASSWORD`, `MAIL_FILE` and `API_URL` are for development only. Tests use the committed `.env.testing`, never production settings.

### Planned

| Variable | Plan | Notes |
| --- | --- | --- |
| `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ZONE_ID` | [captcha-challenges](../plans/features/implemented/captcha-challenges.md) (future use case) | Only for the edge-protection listener, which isn't built |

## First deploy

1. **Freeze the migrations.** While the project is in development, migrations are squashed: tables are changed by editing their create migration, followed by `make migrate-fresh`. Before the first production database holds data, that stops. From then on, migrations are append-only and an applied migration is never edited ([api/database.md](api/database.md)). `migrate fresh` is refused with `APP_ENV=production`, but editing an applied migration isn't detected, so this step is on you.
2. **Create the database and role**, with TLS.
3. **Migrate:** `docker run --rm --env-file prod.env lewcontext migrate`. `migrate status` shows what's applied. It is safe to run from several machines at once, because of the advisory lock.
4. **Start the server:** `docker run -d --env-file prod.env -p 10.0.0.5:3000:3000 lewcontext`.
5. **Check it:** `GET /api/health` returns `{"status":"ok"}`, or 503 when the database is unreachable. Use it for load-balancer health checks. Then open `APP_ORIGIN` and register an account. New accounts start with no workspace and go to onboarding.
6. **Schedule pruning:** for example, daily `prune events -confirm`. That deletes events older than 90 days, keeping any with a pending delivery. It is allowed in production. Sessions, rate-limit windows and reset links are cleaned up by the server itself.

## Admin dashboard

`https://app.example.com/admin/<ADMIN_ROUTE_KEY>`: every account and workspace, read-only apart from unblocking an email address (operators, behind a fresh code and a reason), with every read and action recorded ([api/admin.md](api/admin.md)).

1. **Set `ADMIN_ROUTE_KEY`** from your secret store, and restart.
2. **Grant the first admin:** they register an account in the app as anyone would, then `docker run --rm --env-file prod.env lewcontext admin grant -email ada@example.com -confirm`. Without two-factor authentication on, they set it up at their first admin sign-in, within 24 hours (run `grant` again for another day). Admins are **viewers** (every page, read-only) unless granted `-role operator`, which the actions need (today, unblocking an email address on Email blocks, behind a fresh code and a reason); give it only to those who must act. `admin list` shows every admin, their role and MFA; `admin revoke -email … -confirm` takes it away and signs out their admin sessions. The dashboard's Admins page shows the same with last sign-ins and live sessions, and downloads it as CSV for an access review.
3. **Keep `/admin/` out of access logs**, or redact the path: the key is in it. Cloudflare and most proxies log paths; the key never travels in a header to anywhere but this server (`X-Admin-Key`), and admin pages send `Referrer-Policy: no-referrer`. The admin pages' code is a build of its own, at `/admin-assets/…` (no key in those paths), which the server sends only to a browser holding the assets pass an admin page set ([api/admin.md](api/admin.md#the-admin-build)).
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

The admin sign-in stores the client's IP address, so the server must be reached over TCP (as the image and `make serve` are); over a unix socket the door answers 503 and logs why.

The image builds both web builds and fails if any admin code reached the app's. A server with `ADMIN_ROUTE_KEY` set and no admin build refuses to start.

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
| The admin assets pass | an HMAC in the cookie, under `APP_KEY` and the admin key's digest | any server accepts another's, **if every server has the same `APP_KEY` and `ADMIN_ROUTE_KEY`** (and `ADMIN_ROUTE_KEY_PREVIOUS` and `ADMIN_ALLOWED_IPS`, or a session works on one server and not the next) |
| Sign-ups and email codes | `pending_registrations`, `email_verifications`: HMACs under `APP_KEY` | any server can check a code, **if every server has the same `APP_KEY`** |
| CAPTCHA tokens | single use enforced by Cloudflare | no table needed |

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
- **Only use `PER_IP_LIMITS=off`** ([rate-limits.md](api/rate-limits.md#behind-a-proxy-per_ip_limits)) if the proxy can't send the client's address at all. It switches every per-IP limit off, and sign-in then relies on per-account limits and the edge. With `CLIENT_IP_HEADER` set the proxy does send it, so production refuses the two together (development warns).

Proxy timeouts should be at least the server's own: 15 s to read a request and 20 s to write a response.

## Cloudflare

These notes apply when the domain is proxied through Cloudflare.

- **Lock the origin to Cloudflare.** Use a firewall allowing only Cloudflare's IP ranges, or a Cloudflare Tunnel. Otherwise a client can skip Cloudflare's rules by connecting to the server's own address.
- **Bot rules on `/api/*` are fine.** Today `/api/*` is the web app's own same-origin API, called only by our pages. Managed challenges, Bot Fight Mode and rate-limiting rules there are normal practice:
  - the browser gets Cloudflare's clearance cookie when a page load is challenged, and sends it on API calls;
  - an API call without clearance gets Cloudflare's page instead of JSON, and the app shows "Unable to connect. Please try again."
- **Exclude any public API from bot and challenge rules.** A future programmatic API, for example `/api/v1/*` called with API keys, can't solve a challenge. Rules must exclude it, for example `starts_with(http.request.uri.path, "/api/") and not starts_with(http.request.uri.path, "/api/v1/")`. Protect it with its keys, its own rate limits, and edge rules that count and block, never challenge. **This is configured by the site owner in Cloudflare**, not by the app.
- **Turnstile** (`CAPTCHA_PROVIDER=turnstile`; it works without proxying the domain through Cloudflare too):
  - **Keys:** create a widget in the Cloudflare dashboard for `APP_ORIGIN`'s hostname, then set `CAPTCHA_PROVIDER=turnstile`, `CAPTCHA_SITE_KEY` and `CAPTCHA_SECRET_KEY`.
  - **When it loads:** the Turnstile script loads only when a request is challenged (a rate limit in challenge mode), and only from `https://challenges.cloudflare.com`, which the app pages' CSP allows in `script-src` and `frame-src` only while the provider is set. The admin pages never load it.
  - **What it can read:** while loaded, it runs in our login and register pages, as any CAPTCHA does. The reset and invitation link pages never load it.
  - **If Cloudflare is unreachable** while a limit is in challenge mode, those requests get 503 (the check fails closed). Normal traffic never calls Cloudflare.
- **Edge protection from rate-limit events (Planned, future use case):** a listener on `rate_limit.reached` could enable a pre-made, disabled Cloudflare rule for the route under attack, and disable it when the window ends. It needs an API token with only "Zone WAF: Edit" on one zone. See [captcha-challenges](../plans/features/implemented/captcha-challenges.md#future-use-case-turning-on-cloudflare-edge-protection-not-built).

## Turning on email verification

`EMAIL_VERIFICATION` is on by default, so a new install verifies every sign-up from the start. For an install that already has accounts:

1. **Turn on `EMAIL_VERIFICATION`** (it's the default; just don't set `off`). New sign-ups are verified from then on. Existing accounts keep working; their Account settings say "Not verified" with a Verify email button, and admins see the same.
2. **Later, once most have verified, set `EMAIL_VERIFICATION_EXISTING=required`.** The rest verify at their next sign-in, which spreads out by itself over hours or days. A code is only sent when someone presses Send code, so the switch emails nobody by itself; `email-verification:global` (500 an hour) caps the mail if many sign in at once.
3. A completed password reset also verifies an account's email: the link went only to that inbox.

Someone whose old address no longer receives mail can't get past the verify page under `required`, and there's no way to change an email yet. Turning `required` off again lets them back in.

## Terms of Service and Privacy Policy

The app ships two documents as templates: `apps/web/content/policy/terms-of-service.md` and `privacy-policy.md`. They are a starting point for a small SaaS, **not legal advice**; you are responsible for what yours say.

1. **Edit both files.** Replace every `[bracketed]` placeholder, check the privacy policy against what your deployment really does (providers, regions, retention), and have them reviewed. Markdown is limited to `##`–`####` headings, paragraphs, bold, italic, inline code, lists, tables, `---` and links to `https:`, `mailto:` or a path on the site; anything else (raw HTML, images, code blocks, quotes, strikethrough, other links, and reference-style link definitions such as `[name]: https://…`) fails the build, naming the file and line.
2. **Set `updated`** in each file's front matter to the date it takes effect (`YYYY-MM-DD`). That date is the document's version: what the page shows as "Last updated", and what each new account is recorded as agreeing to. Change it whenever you change the text.
3. **Delete the `boilerplate: true` line**, then rebuild the image (`make build` or `docker build`): the pages are prerendered from the files at build time.
4. **Set `POLICIES=on`** and restart. The server checks the build holds both documents, and in production that neither is still a template. Home, login and sign-up then link both, sign-up needs the box ticked, and the API refuses a sign-up without agreement to the current versions.

Accounts made before you turn it on, or while it's off, have no agreement on record and aren't asked; neither is anyone after you change a document. Asking them is a later plan. To try it out locally first, `POLICIES=on` works with the templates outside production, showing a "Template text" warning on each page.

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
- **If the provider is down,** emails wait and retry for about an hour (1 s, 10 s, 1 min, 10 min, 1 h), then are marked failed and logged as an error. Requests keep working: handlers only queue mail. Failures are in `event_deliveries.last_error`.
- **Abuse:** each email someone can trigger has a per-recipient limit on the request (`password-reset:per-email`, 3 an hour; `invitation:per-email`, 5 an hour; `register:per-email`, 3 an hour) besides the per-IP, per-user and global ones ([api/rate-limits.md](api/rate-limits.md)), so the server can't be used to flood an inbox. "You already have an account" is also capped at 5 a day per account, silently.
- **Without a provider,** set `MAIL_DRIVER=off` and `EMAIL_VERIFICATION=off`: password reset answers 503 ("Password reset isn't available.") and the Invite dialog says "Invitations aren’t available on this server." Don't advertise either feature then. Sign-up is then unverified, and says when an email is taken.
- **Email verification** (on by default) sends a code to every sign-up, so sign-up needs mail to work at all ([api/auth.md](api/auth.md#email-verification)).
- **Blocked addresses:** every invitation carries a "Block this address" link and a `List-Unsubscribe` header; an address its owner blocks gets no more email and can't sign up ([api/auth.md](api/auth.md#blocking-an-email-address)). Someone who writes in after their link has expired (a year) can be unblocked with `docker run --rm --env-file prod.env lewcontext admin unblock -email … -confirm`, or by an operator on the admin dashboard's Email blocks page (with a reason, recorded); `admin blocks` lists them. The block list (`email_blocks`) is a suppression list: keep it with the database's backups.

## Security checklist

- [ ] `APP_ENV=production`, an HTTPS `APP_ORIGIN`, and `DATABASE_URL` with `sslmode=verify-full`.
- [ ] The database role isn't a superuser, and the database isn't reachable from the internet.
- [ ] The server's port is reachable only by the proxy (private interface, firewall, or Tunnel).
- [ ] Migrations frozen (append-only) before the first real data.
- [ ] `prune` scheduled.
- [ ] `APP_KEY` generated for this deployment, kept in the secret store, and backed up with the database.
- [ ] `ADMIN_ROUTE_KEY` generated for this deployment and kept in the secret store (or unset, with no admin dashboard); `ADMIN_ROUTE_KEY_PREVIOUS` unset outside a rotation; `/admin/` kept out of access logs or redacted; no shared-cache rule covering `/admin-assets/`; `ADMIN_ALLOWED_IPS` set to your own addresses where possible (with `CLIENT_IP_HEADER` behind a proxy), or Cloudflare Access or a VPN in front of `/admin/`; admins granted only to people who need it, as viewers unless they must act (`admin list`, the Admins page), and reviewed quarterly.
- [ ] `seed` never run (it refuses in production anyway).
- [ ] With sign-up open to the public: both policy documents edited, reviewed, dated and without `boilerplate`, and `POLICIES=on`.
- [ ] Behind a proxy: `CLIENT_IP_HEADER` and `TRUSTED_PROXIES` set, listing only your own proxies (or Cloudflare's current ranges), and a single-value header overwritten by the proxy.
- [ ] Behind Cloudflare: the origin locked to Cloudflare, and any public API excluded from bot and challenge rules.
- [ ] Email: `MAIL_DRIVER=smtp` with `SMTP_TLS` `starttls` or `tls`, a verified sending domain with SPF, DKIM and DMARC, and `SMTP_PASSWORD` in the secret store; or `MAIL_DRIVER=off` and `EMAIL_VERIFICATION=off` deliberately.
- [ ] Every server on the database has the same `APP_KEY`.
- [ ] `CAPTCHA_PROVIDER` set: `turnstile` with your own widget's keys (the secret in the secret store), or `off` deliberately.
- [ ] No secrets in the image or the repository: pass them as environment variables or through your platform's secret store.
- [ ] The server's own security headers aren't stripped or overridden by the proxy: CSP with script hashes, `X-Frame-Options: DENY`, HSTS, `no-store` caching on the app shell and API.

## Related

- [testing.md](testing.md): local setup, `make build`, `make serve` (the production binary on this machine), and `make docker-dev`.
- [api/README.md](api/README.md): environment variables and how Go serves the web app.
- [api/auth.md](api/auth.md): sessions, cookies, CSRF and headers.
- [api/rate-limits.md](api/rate-limits.md): every rate limit and quota.
- [api/database.md](api/database.md): migrations and pruning.
