# Findings: captcha challenges

Plan: captcha-challenges (on the ui branch it has moved to `plans/features/implemented/captcha-challenges.md`). Built on the **ui** worktree (`../lewcontext-ui`, branch `admin-run/ui`), with every recommendation in its Decisions table taken as accepted. Screenshots are in [captcha/](captcha/).

## What shipped

| Commit | What |
| --- | --- |
| `2c57905` | Go: every rate limit can have a **challenge level** below its block level. Above it, a request answers **428** unless it carries a valid Turnstile token (`X-Challenge-Token`), checked server-side by the new `internal/captcha` package. Levels are set on `auth:per-ip` (5), `register:global` (200), `password-reset:global` (50) and `invitation:global` (100). New settings: `CAPTCHA_PROVIDER`, `CAPTCHA_SITE_KEY`, `CAPTCHA_SECRET_KEY`, `PER_IP_LIMITS` |
| `bf90edc` | Web: `request()` answers a 428 itself, with a challenge dialog, then retries. No page or form needed changing |
| `285ba8f` | e2e against Cloudflare's real Turnstile script with its test keys (skipped offline) |
| `68af2ec` | Docs and plan |

**Screenshots:**
- [checking](captcha/10-challenge-checking.png);
- [the widget](captcha/10-challenge-widget.png), and [on a phone](captcha/10-challenge-widget-phone.png);
- [cancelled](captcha/10-challenge-cancelled.png): the form keeps its values.

**Which routes are challenged:** only sign-in, sign-up, a password-reset request and creating an invitation. The admin door never is, so admin pages never load Cloudflare's script. The reset link's own pages and the signed-in password check are never challenged.

## What you must configure

- **Production refuses to start unless `CAPTCHA_PROVIDER` is set**, to `turnstile` or `off`. This changes existing deployments: add it before deploying.
  - **With `turnstile`:** create a Turnstile widget in Cloudflare's dashboard for `APP_ORIGIN`'s hostname, then set `CAPTCHA_SITE_KEY` and `CAPTCHA_SECRET_KEY` (the secret in your secret store). The server needs outbound HTTPS to `challenges.cloudflare.com`. Production refuses Cloudflare's test keys.
  - **With `off`:** a warning is logged, and challenge levels are skipped (block levels still apply).
- **Behind a proxy,** set `CLIENT_IP_HEADER` and `TRUSTED_PROXIES`. Use `PER_IP_LIMITS=off` only for a proxy that can't pass the client's address.
- **Privacy policy:** once Turnstile is on, it's a third-party processor, so the [privacy policy](policies.md) template should name it. That wasn't added.

## For you to decide

- **Development uses Turnstile by default,** with Cloudflare's always-passing test keys. After 5 sign-ins in 15 minutes from one address, the dev app loads Cloudflare's script and the challenge passes by itself. That's the plan's decision, but it means a third-party script runs in development by default. The alternative is `off` by default outside production.
- **The admin build bundles the Turnstile adapter's code,** because the challenge provider lives in the shared `root.tsx`. It can never load there: no admin route is challenged, and the admin CSP doesn't allow Cloudflare. It could be moved out so the admin build doesn't carry it.

## Where the build differs from the plan (recorded in the plan)

- **Many limit names in the plan had already shipped** with the rate-limits plan. This build only added the challenge level.
- **A route names its challenge action** (`s.Challengeable(action, …)`). Handlers didn't change.
- **Cloudflare's test keys answer for `example.com` with no action,** so test-key answers skip the hostname and action checks. Production refuses test keys.
- **Existing e2e flows never meet the dialog,** because the e2e server runs with `RATE_LIMITS=off`. The captcha spec covers it.

## How the security points are met (the builder's account; the independent review follows)

- **Same answers:** a challenged sign-in and sign-up give identical 428s for a taken and a new email. The challenge depends only on the address and the counts.
- **A 428 has no side effects:** no user, event, invitation or mail.
- **One token covers every limit on one request, checked once.** A token for another action or hostname, or one already used, is refused.
- **Bounded calls to Cloudflare:** 5 s timeout, 64 KiB answers.
- **Fails closed:** if Cloudflare is down, requests above the challenge level get 503.
- **CSP:** exact origins only (`script-src` and `frame-src`), on app pages only, never `unsafe-inline`; absent when off.

## Checks

All passed:
- `make check`;
- `make test`: 21 Go packages, 353 Vitest tests;
- `make build`;
- e2e: 28 of 28, including the 3 captcha tests against the real script.

## Security review

The independent reviewer found these sound:
- **Siteverify calls:** the secret travels only in the POST body; the siteverify URL is constant and TLS defaults are kept; tokens and answers are bounded; the check fails closed.
- **Block before challenge:** a valid token never gets past a block level.
- **Same answers:** a 428 depends only on per-IP or global counts, never on whether an email has an account, with no row, mail or password hashing before it.
- **Challenge actions** are constants, never taken from the request.
- **CSP:** exact origins, app pages only, never `unsafe-inline`.
- **The web client** resends at most once, and never puts the token in a URL or storage.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | **Medium** | **Bots could lock everyone out without solving anything.** A 428'd request still counted against the shared limits (`register:global`, `password-reset:global`, `invitation:global`). The probe needed about 21 addresses to make every real password-reset request answer 503 for the hour, even with a valid token. This wasn't a regression (the block level already existed), but the feature's goal wasn't met | Above a challenge level, a request counts against a **shared** limit only once it has passed the challenge. Per-IP limits still count every hit |
| 2 | **Medium** (likely) | **The `idempotency_key` sent to Cloudflare wasn't a UUID.** With a real secret, Cloudflare may refuse every check, which would break sign-in for anyone challenged in production. The e2e test only uses test keys | Field dropped (it added nothing) |
| 3 | Low | Cloudflare's "test key" answers skipped the hostname and action checks even with a real secret | Honoured only when the configured secret is a test key |
| 4 | Low | A redirect from siteverify would have re-sent the secret to the new location | Redirects aren't followed |
| 5 | Low | Cloudflare's error codes were ignored and nothing was logged, so a wrong secret would show only as users looping on "check failed" | Configuration and outage codes answer 503 and are logged (never the secret or token) |
| 6 | Low | `PER_IP_LIMITS=off` was accepted with `CLIENT_IP_HEADER` set, silently removing the login challenge | Refused in production, warned elsewhere |
| 7 | Low | Two challenges at once could leave a request hanging | Challenges removed by id |
| 8–10 | Nits | Wording ("never on a token page"), a plain-object provider lookup, docs on the shared `auth:per-ip` budget | Fixed |

**For the merge:** A3's server config summary needs the captcha settings: the provider, whether the secret is set (never its value), whether test keys are in use, and `PER_IP_LIMITS`. A non-production server using test keys now says so at startup.

_Fix results: filled in once the fixes are committed._
