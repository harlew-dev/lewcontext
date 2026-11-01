# CAPTCHA challenges for rate limits

Status: **implemented 2026-11-01**, every recommendation in the Decisions table confirmed and built; see [How the build differs](#how-the-build-differs) for the differences and the docs updated. Builds on [rate limits for every endpoint](rate-limits.md), which this plan's limits, renames and event replace where they overlap. Covers login, register, password reset, and [workspace invitations](workspace-invitations.md).

## Why

Today, reaching a limit shared by everyone (`password-reset:global`; and, once planned, `register:global` and `invitation:global`) refuses every request with 503 until the window resets. A bot flood therefore locks out every real person. Scripted sign-ins from one address get 60 free attempts every 15 minutes before anything stops them.

Any limit can now have two levels instead of one. Each level is **optional**: a limit may have either, both, or a level that is switched off by configuration.

| Level | Above it | Response |
| --- | --- | --- |
| `<limit>:challenge` | every request must carry a solved CAPTCHA token | 428 with what the browser needs to show the CAPTCHA |
| `<limit>:block` | every request is refused | 503 with the `busy` message for shared limits (today's behaviour); 429 for per-subject limits |

For example, `auth:per-ip:challenge`, `register:global:challenge` and `register:global:block`.

- The challenge level is always lower than the block level, so real people get a check, usually with nothing to click, long before anyone is refused.
- `auth:per-ip:challenge` at 5 makes scripted sign-ins from one address pay for a CAPTCHA almost at once.
- In the web app, every challenge opens the same `ChallengeDialog`, from inside the HTTP client, so no page or form has to handle it.

**The provider is Cloudflare Turnstile**, behind a small `captcha.Provider` interface and a `CAPTCHA_PROVIDER` setting (`turnstile` or `off`). Other providers (hCaptcha, reCAPTCHA v2, a self-hosted proof-of-work) are left for later and added when a deployment needs one. Each would be one adapter in Go and one in the web app, with no change to limits, handlers or the dialog.

The plan also adds one event, `rate_limit.reached`, recorded the first time any limit or level is reached in a window. A listener can then react, for example by turning on Cloudflare's own edge protection. That use case is designed here but **not built**.

## Limits and levels

| Limit | Window | `challenge` above | `block` above | Block response |
| --- | --- | --- | --- | --- |
| `auth:per-ip` (existing) | 15 min | **5** | 60 (as today) | 429 |
| `register:global` | 1 hour | 200 | 1,000 | 503 |
| `password-reset:global` (renamed from `all-accounts`) | 1 hour | 50 | 100 | 503 |
| `invitation:global` | 1 hour | 100 | 200 | 503 |

Other limits (`auth:per-account`, `register:per-ip`, `password-reset:per-*`, `invitation:per-user`, …) have a block level only, as today.

- **There is no `login:global`.** Sign-in is protected by:
  - `auth:per-ip`: a challenge above 5, refused above 60;
  - `auth:per-account`: 10 per 15 minutes per account;
  - the 4 password-hashing slots.

  A shared login limit would let anyone degrade sign-in for everyone by sending junk logins.
- **The route decides whether a challenge can be shown, not the limit.** `auth:per-ip` is counted on many routes, some of which must never load the provider's script. Each route passes its `action`, or none:

  | Route | Action | Counted against `auth:per-ip` | Can be challenged |
  | --- | --- | --- | --- |
  | `POST /api/auth/login` | `login` | ✓ | ✓ |
  | `POST /api/auth/register` | `register` | ✓ | ✓ |
  | `POST /api/auth/register/resend` *(added in review: it counts `register:global`)* | `register` | ✓ | ✓ |
  | `POST /api/auth/password-reset` (request a link) | `password_reset` | ✓ | ✓ |
  | `POST /api/workspaces/{ws}/invitations` *(planned, signed in)* | `invitation` | no (counted by user instead) | ✓ for `invitation:global` |
  | `POST /api/auth/password-reset/check`, `PATCH …/password-reset` | none | ✓ | **no**: these run on `/auth/forgot-password/<token>`, and the script would see the token in the URL |
  | `POST /api/auth/password/verify`, `PATCH /api/auth/password` | none | ✓ | no: already behind a session and `auth:per-account` |
  | `DELETE /api/workspaces/{ws}`, `DELETE /api/auth/me` *([deletion](deletion.md))* | none | ✓ (password check) | no: behind a session, a password and `auth:per-account` |
  | `POST /api/invitations/check`, `POST /api/invitations/accept` *([invitations](workspace-invitations.md))* | none | check only | **no**: they run on `/auth/invitations/<token>` |

  On a route without an action, a challenge level is skipped: the request is counted and only the block level applies.
- **`auth:per-ip:challenge` at 5** covers login, register and reset requests together, counting successes too. After the fifth in 15 minutes from one address, each further one needs a token.
- **Order is unchanged:** per-subject limits first, then global, so a client its own limit refuses doesn't spend the shared count. A request can need a token for two limits (register: `auth:per-ip` and `register:global`). One token covers both, because it is verified once per request. Every check runs **before any side effect** (before hashing, creating a user, or queuing mail), so a request refused with 428 changed nothing and can be safely sent again.
- **Every request counts,** including the resend after a challenge. A challenged login therefore counts twice against `auth:per-ip` and `auth:per-account` (decision 6).
- **With `CAPTCHA_PROVIDER=off`**, challenge levels are skipped everywhere and only block levels apply, which is today's behaviour.

### Levels are optional (nullable)

A level is a `threshold` that is either set to a count or `never`. There is no magic `0`, so "this level doesn't trigger" is written out:

```go
// httpapi/limits.go
// threshold is the count a level triggers above, or never.
type threshold struct {
	at  int
	set bool
}

func above(n int) threshold { return threshold{at: n, set: true} }

var never threshold

// exceeds reports whether count is past this level; never is never exceeded.
func (t threshold) exceeds(count int) bool { return t.set && count > t.at }

type rateLimit struct {
	name      string // "<feature>:<counted by>"; events add ":challenge" / ":block"
	challenge threshold
	block     threshold
	window    time.Duration
	busy      string // set for shared limits: 503 with this message instead of 429
}

var (
	authPerIP      = rateLimit{name: "auth:per-ip", challenge: above(5), block: above(60), window: 15 * time.Minute}
	authPerAccount = rateLimit{name: "auth:per-account", challenge: never, block: above(10), window: 15 * time.Minute}
	registerGlobal = rateLimit{name: "register:global", challenge: above(200), block: above(1000), window: time.Hour, busy: "Sign-up is busy. Try again later."}
)
```

### Behind a proxy: real client IPs, or `PER_IP_LIMITS=off` as a fallback

Behind a reverse proxy or load balancer, every connection comes from the proxy. A per-IP limit would then count the whole site together: the challenge at 5 would challenge everyone after 5 sign-ins, and the block at 60 would refuse everyone after 60.

**The fix is [client IP behind a proxy](client-ip.md):** `CLIENT_IP_HEADER` and `TRUSTED_PROXIES` make every per-IP limit count the real client, so the levels here work unchanged behind a proxy.

**`PER_IP_LIMITS=off` is the fallback** for a proxy that can't send the client's address at all:

- `PER_IP_LIMITS=off` sets **every level of every per-IP limit to `never`**: `auth:per-ip`, `register:per-ip` and `password-reset:per-ip`, both challenge and block. The server logs a warning at startup.
- Everything else still applies: per-account, per-email and per-user limits, global limits with their challenge and block levels, quotas, and the hashing slots.
- The setting is applied once, when the server is built (`limits.withoutPerIP()`), so handlers never check it.
- With `CLIENT_IP_HEADER` set, it isn't needed and should stay `on`.
- With it `off`, the edge can still rate limit by real IP behind Cloudflare (see the future use case below).

`s.allow(w, r, l, key, action)` replaces `allowAuth`. It increments the counter (`Store.Allow`), then:

- `l.block.exceeds(count)`: 429, or 503 with `busy`, plus `Retry-After`;
- `!l.challenge.exceeds(count)`, or the route has no `action`, or the provider is `off`: pass;
- otherwise: check `X-Challenge-Token`. It is verified once per request and remembered in the request context, so two limits on one request don't spend two tokens. A valid token passes. A missing or invalid token gets 428.

## API

### The 428 response

```json
{ "error": "Complete the check to continue.",
  "challenge": { "provider": "turnstile", "siteKey": "0x4AAAA…", "action": "login" } }
```

- **428 Precondition Required**, not 429. The client then reacts only to this code and needs no special handling for ordinary 429s.
- `provider` tells the web app which adapter to load. Today that is always `turnstile`, but the field means a second provider needs no API change. The site key is public, so it comes in the response, and the web app needs no build-time configuration.
- An invalid, expired or reused token gets another 428 with `"error": "The check failed. Try again."`.
- If Cloudflare can't be reached, the response is 503 "Verification is unavailable. Try again shortly." This **fails closed**, but only while a limit is in challenge mode. Below the challenge level, Cloudflare is never contacted.

### Provider (`internal/captcha`, new package)

```go
// Provider checks CAPTCHA tokens with one service. Turnstile is the only one
// for now; another is one more implementation (see Not included).
type Provider interface {
	Name() string    // "turnstile": the 428's provider
	SiteKey() string // public: sent in the 428
	// Verify checks a token for one action, from the request's IP.
	Verify(ctx context.Context, token, action, ip string) error
	// CSP lists the origins the provider's widget needs, per directive.
	CSP() Sources
}

type Sources struct{ Script, Frame, Style, Connect []string }

// New returns the configured provider, or nil for "off".
func New(c config.Captcha, client *http.Client) (Provider, error)
```

`Turnstile` uses `net/http` only, so there is no new Go dependency:

- **Verify:** POST `secret`, `response`, `remoteip` and an `idempotency_key` to `https://challenges.cloudflare.com/turnstile/v0/siteverify`. Require `success`, `hostname` equal to `APP_ORIGIN`'s host, and `action` equal to the route's action, so a token solved on the register page can't be spent on login.
- **Single use is enforced by Cloudflare** (`timeout-or-duplicate`), and tokens expire after 300 s. That works across any number of servers, so we need no table of used tokens.
- **Bounds:**
  - the token header can be at most 2,048 bytes (it is checked before any call);
  - each call has a 5 s timeout;
  - the response body is read through `io.LimitReader` (64 KiB).
- **CSP:** `script-src` and `frame-src` `https://challenges.cloudflare.com`.
- Handlers depend only on `captcha.Provider`, so tests use a fake. `Turnstile` is tested against an `httptest` server.

### Config (`internal/config`)

| Variable | Outside production | Production |
| --- | --- | --- |
| `CAPTCHA_PROVIDER` | `turnstile` or `off`; default `turnstile` | **required**, so `off` is a deliberate choice. `off` logs a warning at startup |
| `CAPTCHA_SITE_KEY` | defaults to Cloudflare's test key that always passes, `1x00000000000000000000AA` | required unless `off`. Cloudflare's published test keys (`1x…`, `2x…`, `3x…`) are refused |
| `CAPTCHA_SECRET_KEY` | defaults to the matching test secret, `1x0000000000000000000000000000000AA` | required unless `off`; never logged |
| `PER_IP_LIMITS` | `on` (default) or `off` | the same. Behind a proxy, set `CLIENT_IP_HEADER` and `TRUSTED_PROXIES` instead ([client IP](client-ip.md)). Use `off` only when the proxy can't send the client's address; it logs a warning |

The variables are named `CAPTCHA_*` rather than `TURNSTILE_*`, so a second provider reuses them. With the test keys, `make dev` shows a real widget that always passes. Cloudflare's other test keys (always fails, forces an interactive challenge) are used by the e2e tests.

## Event: `rate_limit.reached`

One event type for every limit, which replaces `auth.rate_limited` (and the `api.rate_limited` that the rate-limits plan proposed):

| Field | Holds |
| --- | --- |
| `limit` | the limit and the level reached: `auth:per-ip:challenge`, `auth:per-ip:block`, `project:per-user:block`, `register:global:challenge`, `register:global:block` |
| `user` | the session user's `usr_` ID, when signed in |
| `workspace` | the `wsp_` ID, when the request was inside a workspace |
| `ip` | the connection IP (as `auth.login_attempted` already stores) |
| `window` | the window in seconds |
| `resetsAt` | when the window ends |

- It is recorded the **first time** each limit or level is reached in a window: the request that crosses it. A flood therefore writes one event per limit per level per window, as today.
- With `CAPTCHA_PROVIDER=off`, challenge levels are still recorded, so the signal exists either way. A level set to `never` (including by `PER_IP_LIMITS=off`) is never reached, so it records nothing.
- **Why one event type, not `limits.{limit}.reached`:** event names are fixed strings per Go type, and listeners subscribe by type (`Listen[E]`). A name per limit would need a type per limit. A `limit` field gives the same information, and a listener filters on it (`strings.HasSuffix(e.Limit, ":global:block")`).
- `ip` is personal data. It is pruned with all events after 90 days. `TestPayloadsHoldNothingSensitive` covers the event, and the token never appears in it.

## Web app

### Where the challenge is handled: the HTTP client

`useAction` only wraps a function, and some mutations don't go through it: sign-in and register use `useAuthSubmit`, and some pages call `api.…` directly. The one place every mutation passes through is `request()` in `lib/api/http.ts`, so that is where the challenge is caught:

```
api.auth.login(…)  ─▶  request()  ─▶  428 {challenge}
                                        │  transport.solve(challenge)   ← opens ChallengeDialog, waits
                                        ▼
                       request() again, same method, path and body, + X-Challenge-Token
                                        │
                       ok ──▶ resolves as if nothing happened
                       428 again / cancelled ──▶ throws ApiError, as any failure does
```

- `browserHttp(csrfToken, solve?)` takes the solver, and `useApi()` passes it from `useChallenge()`. Every caller then gets challenges without changing anything: `useAction`, `useAuthSubmit`, direct calls. Loaders use the read-only client and are never challenged (GETs have no challenge levels).
- **Pending state works unchanged.** The awaited promise stays pending while the dialog is open, so `useAction`'s `busy` and `loading` stay true. `<Button loading>` keeps spinning and a double submit is still ignored.
- **Cancelling** throws `ApiError("Check cancelled.", 428)`. It shows in the form's `ErrorNotice` like any other error, and the form stays filled in.
- Each request is resent **at most once**. A second 428 is an error, so the client can't loop.
- The 12 s request timeout applies to each fetch. Time spent in the dialog doesn't count.

### The Turnstile adapter (`lib/challenge/providers.ts`)

```ts
type CaptchaWidget = {
  script: string; // https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit
  render(el: HTMLElement, options: {
    siteKey: string; action?: string; theme: "light" | "dark" | "auto";
    onToken(token: string): void; onError(message: string): void; onExpire(): void;
  }): () => void; // removes the widget
};
export const providers: Record<"turnstile", CaptchaWidget> = { turnstile: { … } };
```

- Turnstile renders with `appearance: "interaction-only"` and `theme: "auto"`, so it is usually invisible: the dialog shows "Checking…" and closes by itself. When Cloudflare wants an interaction, the widget appears inside the dialog.
- There is no npm dependency, just a small loader.
- An unknown `provider` in a 428 shows "This check isn't supported. Refresh the page and try again."

### `ChallengeDialog` and `ChallengeProvider`

- `lib/challenge/challenge.tsx`: `ChallengeProvider`, mounted once in `root.tsx`, owns the dialog. `useChallenge()` returns `solve(challenge): Promise<string>`. Challenges are queued, so two at once show one after the other. Each token works once, so each request needs its own.
- `components/app/challenge/challenge-dialog.tsx`: `ChallengeDialog`, built on the existing `Dialog`. It is shown on several pages (login, register, forgot password, members), so it lives in `components/app`.
  - Title "Quick security check". Description: "We're seeing a lot of traffic. Complete this check to continue." A Cancel button.
  - **Loading the script on demand:** the provider's script loads only the first time a challenge opens. In normal operation no third-party code is ever loaded.
  - The widget renders inside the dialog, with a `role="status"` line for screen readers ("Checking…", "Done").
  - **Failures:**
    - the script fails to load (offline, or an ad blocker): "Couldn't load the check. Check your connection or turn off blockers for this site, then try again.", with Retry;
    - the widget reports an error: its message, with Retry;
    - the token expires before it's used: the widget resets.
  - Focus moves into the dialog and returns to the submit button afterwards, using the existing `Dialog` and `return-focus` behaviour.

### CSP (`internal/web/csp.go`)

The page CSP adds **only the configured provider's** `Sources`: for Turnstile, `https://challenges.cloudflare.com` in `script-src` (beside the existing hashes, still with no `'unsafe-inline'` script) and in a new `frame-src`. With `off`, the CSP is unchanged from today. `internal/web` takes the sources as input from `cmd/server`, so it doesn't import `captcha`. The API's own responses are unchanged.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| **Cloudflare's script runs in our pages** and could read the URL or form fields, including a password being typed or an invitation token in `?next=` | It loads only while a limit is in challenge mode, never in normal use, and only from `https://challenges.cloudflare.com`, pinned in the CSP. The token pages (`/auth/forgot-password/<token>`, `/auth/invitations/<token>`) call no challenged endpoint, so the script never loads there. On login and register with an invitation `next`, the remaining exposure is to Cloudflare, a processor the deployment has chosen to trust, and accepting still requires signing in as the invited email (decision 4). `auth.md` and AGENTS.md change "pages load no third-party resources" to name this exception. |
| A token solved for one form is reused on another | `action` must match the route. Single use and the 300 s expiry are enforced by Cloudflare. |
| A token made for another site | `hostname` must equal `APP_ORIGIN`'s host. |
| A forged or oversized header | At most 2,048 bytes, checked before any call. Verified only by Cloudflare with our secret. |
| Cloudflare is down or slow | 5 s timeout. Fails closed (503), but only in challenge mode. Below the threshold, nothing calls Cloudflare. |
| The secret leaks | Environment variable only, never logged or sent to the browser. Test keys are refused in production. |
| CAPTCHAs silently off in production | `CAPTCHA_PROVIDER` must be set in production, and `off` logs a warning. |
| **Everyone shares one IP behind a proxy** | `CLIENT_IP_HEADER` with `TRUSTED_PROXIES` ([client IP](client-ip.md)). Failing that, `PER_IP_LIMITS=off` turns every per-IP level to `never` (with a warning), instead of the site challenging or refusing itself. |
| Sign-in with `PER_IP_LIMITS=off` | No per-IP challenge, so sign-in relies on `auth:per-account` (10 per 15 min per account), the hashing slots, and the edge if there is one. Spraying one password across many accounts isn't slowed in-app. Only a deployment whose proxy can't forward the client's address is in this position. |
| A level silently disabled by a typo | Levels are `above(n)` or `never`, never a bare number where `0` might mean "off". A unit test lists every limit's levels. |
| The script loading on a token page | Routes on token pages pass no action, so their requests are never challenged. |
| The client resends a request that had side effects | Every check runs before any side effect, so a 428 means nothing happened. The client resends once. |
| A blocked script leaves people stuck | The dialog explains it and offers Retry. Only users in challenge mode are affected, and those users would otherwise be refused outright (the block level). |
| Privacy | Turnstile is a third-party service. The privacy notice (when there is one) should name it. |

## Future use case: turning on Cloudflare edge protection (not built)

`rate_limit.reached` lets a listener react to a flood outside the app. The intended first listener:

```
rate_limit.reached {limit: "register:global:challenge", ip, resetsAt, …}
  └▶ listener cloudflare.edge_protection   (registered only when CLOUDFLARE_* is configured)
       limit ends in :global:challenge or :global:block?   else → done
       map the limit to its route (register:global → /api/auth/register)
       Cloudflare Rulesets API: enable the pre-made, disabled custom rule for that path
         (action managed_challenge, or block for :global:block)
       record cloudflare.edge_protection_enabled {limit, rule, until: resetsAt}
  cleanup loop: rules whose `until` has passed → disable, record …_disabled
```

Design notes for when it's built:

- **Enable a pre-made rule; never create rules.** The API token then needs only "Zone WAF: Edit" on one zone, and enabling is idempotent, which suits at-least-once delivery.
- **Edge challenges on `/api/*` are acceptable.** Today's `/api/*` is the web app's own API: same origin, called only by our pages. Putting Cloudflare bot rules (managed challenge, Bot Fight Mode, rate limiting) on it is common practice in production.
  - The browser gets Cloudflare's clearance cookie (`cf_clearance`) when a page load is challenged, and sends it with every same-origin `fetch`.
  - A request without clearance gets Cloudflare's challenge response instead of JSON. `request()` already turns a non-JSON error into "Unable to connect. Please try again.", so the app fails safely. Optionally, Turnstile's pre-clearance mode can set `cf_clearance` from `ChallengeDialog`; check Cloudflare's current docs when building this.
- **Exclude a future public API.** When separate routes for programmatic clients exist (for example `/api/v1/*`, called with API keys rather than a browser session), every bot or challenge rule must exclude them. Cloudflare rule expressions would match `starts_with(http.request.uri.path, "/api/") and not starts_with(http.request.uri.path, "/api/v1/")`. A script or server can't solve a challenge, so a challenge rule there would just break the integration. Those routes are protected by their API keys, per-key rate limits in the app, and edge rate limiting rules (count and block, never challenge). The public API's own plan must carry this as a requirement.
- **Edge rate limiting counts real IPs.** Behind Cloudflare, the edge sees each client's address, so it can do what `PER_IP_LIMITS=off` gives up in the app.
- **Turning a rule off again:** listeners can't schedule work, so the server's existing cleanup loop turns off rules whose window has ended, using a small `edge_protections` table (rule, until). That is a new table, planned when this is built.
- **Requirements:** the domain must be proxied through Cloudflare, plus a Cloudflare API token and zone ID (`CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ZONE_ID`).

## Tests

- **Go unit (`httpapi`, thresholds):**
  - `never` is never exceeded, and `above(n)` is exceeded at n+1;
  - a table of every limit's challenge and block levels, so a change is deliberate;
  - `withoutPerIP()` sets every per-IP level to `never` and leaves every other limit unchanged.
- **Go unit (`internal/captcha`), Turnstile against an `httptest` fake of siteverify:**
  - success;
  - a wrong hostname;
  - a wrong action;
  - `timeout-or-duplicate`;
  - a non-200 response or a timeout gives an error;
  - an oversized body is cut off;
  - the secret, `remoteip` and idempotency key are sent;
  - `CSP()` lists the documented origin;
  - `New` returns nil for `off` and an error for an unknown name.
- **Go unit (`config`):**
  - production requires `CAPTCHA_PROVIDER`;
  - production refuses missing keys and Cloudflare's test keys;
  - `off` needs no keys;
  - outside production, the test keys are the defaults;
  - `PER_IP_LIMITS` accepts only `on` and `off`.
- **Go integration, with a fake `Provider`:**
  - **Levels:**
    - at a challenge level a request passes without a token;
    - one over gets 428 with `provider`, `siteKey` and `action`;
    - a valid token passes, and an invalid one gets 428;
    - above the block level the response is 503 (global) or 429 (per subject), even with a valid token;
    - with the provider unavailable, 503;
    - with no provider (`off`), challenge levels are skipped and block levels still apply.
  - **`auth:per-ip:challenge`:**
    - the 6th login, register or reset request from one IP in a window gets 428, and a valid token passes it;
    - request 61 gets 429 even with a token;
    - reset check and complete, and the two signed-in password routes, are never challenged above 5;
    - another IP is unaffected;
    - one token satisfies both `auth:per-ip` and `register:global` in the same request.
  - **`PER_IP_LIMITS=off`:**
    - a 7th and a 61st login from one IP pass;
    - `auth:per-account` still refuses its 11th;
    - `register:global` still challenges.
  - **Test servers** default to no provider (`off`), so the existing tests (which send more than 5 auth requests from one test IP) keep working. Challenge tests set the fake.
  - **No side effects on 428:** no user created, no password hashed (spy on `authSlots`), no reset or invitation row, no mail queued.
  - **Order:** a request refused by its per-subject limit doesn't increment the global counter.
  - **Events:**
    - `rate_limit.reached` once per limit and level per window, with `limit`, `ip`, `user` and `workspace` where they apply;
    - challenge levels are recorded with `off` too;
    - a `never` level records nothing;
    - no token in any payload.
  - **Rename:** `password-reset:global` and the existing reset tests.
- **Go (`internal/web`):** the CSP has Cloudflare's origin only when `CAPTCHA_PROVIDER=turnstile`, and still no `'unsafe-inline'` script.
- **Vitest:**
  - **`http.ts`:**
    - a 428 calls `solve` and resends once with `X-Challenge-Token` and the same body;
    - a second 428 throws;
    - cancelling throws `ApiError` 428;
    - an ordinary 429 never calls `solve`;
    - no `solve` (the loader client) throws as before.
  - **`useAction`:** `loading` stays true while solving.
  - **`ChallengeDialog`:**
    - with a stub `window.turnstile`: the loading, token, error, expiry and Retry states;
    - an unknown provider shows the message;
    - two challenges at once queue.
- **Playwright,** against the real Turnstile script with Cloudflare's test keys (this needs network access to Cloudflare; the test is skipped offline). With six sign-ins from the test IP:
  - the sixth shows the dialog, it passes by itself, and you're signed in;
  - the "forces interactive" key shows the widget;
  - the "always fails" key shows the error and the form keeps its values.

  The existing e2e tests sign in from one IP many times, so they pass through the dialog after the fifth, which the client handles by itself.

## Docs

- The new `docs/api/rate-limits.md` (from the rate-limits plan) gains:
  - optional levels and `never`;
  - `PER_IP_LIMITS`;
  - the 428;
  - the provider interface and how to add a provider;
  - the edge-protection notes, including excluding a future `/api/v1`.
- `docs/api/endpoints.md`: 428, and the `X-Challenge-Token` header.
- `docs/api/events.md`: `rate_limit.reached` and the future listener.
- `docs/api/auth.md`: the CSP exception, the "no third-party resources" wording, and the proxy note pointing to `PER_IP_LIMITS`.
- `docs/api/README.md`: `internal/captcha` and the env vars.
- `docs/ui/api.md`: challenges are handled by the client.
- `docs/ui/design.md`: `ChallengeDialog`.
- `.env.example`: `CAPTCHA_PROVIDER`, `CAPTCHA_SITE_KEY`, `CAPTCHA_SECRET_KEY`, `PER_IP_LIMITS`.
- AGENTS.md: the CSP line, and the third-party exception.

## Decisions (confirmed)

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Level names | `<limit>:challenge` and `<limit>:block`, as agreed: `auth:per-ip:challenge`, `register:global:challenge`, `register:global:block`. |
| 2 | Provider | Turnstile only, behind `captcha.Provider` and `CAPTCHA_PROVIDER` (`turnstile` or `off`). Other providers are added when a use case needs one. |
| 3 | Where the web app handles it | In `request()` (`lib/api/http.ts`), so `useAction`, `useAuthSubmit` and direct calls all get it. Agreed. |
| 4 | Turnstile on login and register while an invitation token is in `?next=` | Accept it (Cloudflare is a trusted processor, the script loads only in challenge mode, and accepting is still tied to the invited email). The alternative, stripping `next` from the URL before loading the script, breaks reloading the page. |
| 5 | Status code | 428 for "solve a challenge", 503 at the block level for shared limits, 429 unchanged for per-subject ones. |
| 6 | Counting the resend | Every request counts, including the resend after a challenge. A challenged login therefore costs two attempts on the per-IP and per-account limits. That's simple, and only matters during a flood. |
| 7 | Levels | `auth:per-ip` challenge above 5, block above 60; register 200 / 1,000 per hour; password reset 50 / 100; invitations 100 / 200. |
| 8 | When Cloudflare is down in challenge mode | Fail closed (503). Failing open would let a flood through exactly when it's happening. |
| 9 | The event | One `rate_limit.reached` with a `limit` field, replacing `auth.rate_limited`, rather than an event name per limit. Agreed. |
| 10 | Dialog name | `ChallengeDialog`. Agreed. |
| 11 | e2e | The real Turnstile script with Cloudflare's test keys, skipped when offline, instead of a local stub of the script. |
| 12 | `login:global` | None, as agreed. Sign-in relies on `auth:per-ip` (challenge and block), `auth:per-account` and the hashing slots. |
| 13 | Behind a proxy | **Agreed.** Real client IPs through `CLIENT_IP_HEADER` and `TRUSTED_PROXIES` ([client IP](client-ip.md)), with `PER_IP_LIMITS=off` (every per-IP level set to `never`) as the fallback for a proxy that can't send the address. |
| 14 | Nullable levels | **Agreed.** A `threshold` type (`above(n)` or `never`) for both levels of every limit, rather than `0` meaning off or `*int` fields. |
| 15 | What `PER_IP_LIMITS=off` covers | **Agreed.** Every per-IP limit, both levels, not only `auth:per-ip:challenge`. Behind a proxy, the block levels are just as wrong. |
| 16 | Production setting | `CAPTCHA_PROVIDER` must be set explicitly in production; `off` is allowed, with a warning. |
| 17 | Libraries | None: our own small Turnstile adapter in Go and TypeScript. |
| 18 | Edge challenges on `/api/*` | **Agreed.** Acceptable, because `/api/*` serves only our own pages. Any future public API, such as `/api/v1`, must be excluded from Cloudflare bot and challenge rules and protected by API keys and rate limits instead. **That exclusion is the site owner's job in Cloudflare's dashboard, not app code**, and is listed in [deployment.md](../../../docs/deployment.md). |

## How the build differs

Built 2026-11-01 on `admin-run/ui`. The rate-limits plan had already shipped the names this plan asked for (`password-reset:global`, `invitation:global`, `register:global`) and `rate_limit.reached` with a `:block` level, so this run added the challenge level beside them. Where the build differs from the design above:

- **The limit type** stays the exported `ratelimit.Limit`, with `Challenge` and `Block` fields of type `ratelimit.Threshold` (`ratelimit.Above(n)`, `ratelimit.Never`, `Exceeds`, `Crossed`) in place of `Max`, and its `Busy`, `Attempts`, `TooMany` and `InMemory` as before; not an unexported `rateLimit` in `httpapi`.
- **The action comes from the route, not an argument.** `Allow(w, r, l, key)` keeps its signature; `s.Challengeable(action, …)` (and `ChallengeableFunc`) in `router/routes.go` puts the action, and whether this request's token has passed, in the request's context, outside `LimitAuth`. So no handler changed, and a route without it is never challenged. The actions are constants (`httpapi.ChallengeLogin`, `ChallengeRegister`, `ChallengePasswordReset`, `ChallengeInvitation`).
- **`PER_IP_LIMITS=off` is applied in `Allow`** (`Limit.WithoutPerIP()` when the config says off), not once when the server is built: handlers use the package's limit values directly. Still no handler checks it. The config field is `PerIPLimitsOff`, so a config built by hand (the tests) has per-IP limits on.
- **Counting returns the count:** `Store.Allow` became `Store.CountHit(ctx, key, window) (hits, resetsAt)` and `Memory.Count`, so the request that first crosses each level is known exactly; the `account-exists:per-account` listener uses it too.
- **Cloudflare's test secrets** answer siteverify with `hostname: example.com`, no action and `metadata.result_with_testing_key: true`. Such answers are accepted without the hostname and action checks, or the test keys couldn't pass anywhere, but only when the configured secret is itself one of Cloudflare's test secrets (`config.IsTurnstileTestKey`, the pattern production refuses), never because the answer says so. A non-production server on the test keys logs `captcha: test keys, every check passes` at startup (`every check fails` for the failing test secrets).
- **Siteverify (security review):** no `idempotency_key` is sent (Cloudflare documents a UUID, and with no retry it adds nothing); redirects are never followed (`NewTurnstile` copies the client with `CheckRedirect` returning `http.ErrUseLastResponse`), a redirect being `ErrUnavailable`; the error codes for our secret or request, or Cloudflare's own failure (`missing-input-secret`, `invalid-input-secret`, `bad-request`, `internal-error`), are `ErrUnavailable` (503) and logged as errors, and the token's codes stay `ErrRejected`, logged as warnings, never with the secret or token.
- **The CSP** is set by `web.Pages.AllowThirdParty(web.ThirdParty{Script, Frame})`, which `cmd/server` calls with the provider's `CSP()`; it recomputes the app pages' policies from their stored hashes. The admin build's shell never gets it: no admin route has an action (the admin door included), so the admin pages never load the script. `captcha.Sources` has `Script` and `Frame` only: Turnstile needs no `style-src` or `connect-src` beyond ours.
- **Test servers:** `.env.testing` sets `CAPTCHA_PROVIDER=off`, and the main e2e server runs with `RATE_LIMITS=off`, so the existing flows never meet the dialog (the plan expected them to pass through it). `captcha.spec.ts` starts its own server with limits on. Turnstile draws its widget in a closed shadow root, so the "forces interactive" test measures the widget's slot rather than finding the iframe.
- **Go tests:** the no-side-effects check looks at what a 428'd request would have written (no user, no `auth.registered`, no reset request recorded, no invitation row) rather than spying on the hashing slots. `TestSignUpLooksTheSameForATakenEmail` empties the counters before each run, since `auth:per-ip`'s challenge level now records an event at the sixth request from the test's one address. `useAction`'s loading while solving isn't tested on its own: the client's promise simply stays pending.
- **Shared limits count only what got through (security review).** As designed, every request counted toward the block level, so requests answered 428 and never solved pushed `register:global`, `password-reset:global` and `invitation:global` to 503 for everyone (101 unsolved reset requests from 21 addresses blocked resets for the hour). Above a `global` limit's challenge level, its block level now counts the challenge level's worth plus the requests that passed the challenge, in a `<name>:passed` counter whose window ends with the limit's (`Store.CountHitUntil`, `PeekHits`; `admit` in `limits.go`). Per-IP limits still count unsolved requests. Decision 6 (every request counts) still holds for per-subject limits and for the challenge level. A request counts as passed only once its token is verified. A second review found `POST /api/auth/register/resend` counted `register:global` without being `Challengeable`, so its resends passed for free (400 free sign-ups × 2 resends could reach the block level); it is now `Challengeable(ChallengeRegister)` (its page holds a cookie, not a URL token), and `Allow` refuses, with 503 and a logged error, any route that counts a shared limit with a challenge level without being `Challengeable` (`challengeableFor`), even with `RATE_LIMITS=off`. A late request counted into the next window takes that window's end for its event and `Retry-After`.
- **Startup:** with a provider the server logs its name, never its keys; `CAPTCHA_PROVIDER=off` in production and `PER_IP_LIMITS=off` log warnings. `PER_IP_LIMITS=off` together with `CLIENT_IP_HEADER` (security review) is refused in production, since the proxy does send the address, and warned about outside it.

**Docs updated:** [api/rate-limits.md](../../../docs/api/rate-limits.md#levels) (levels, challenges, the provider and adding one, `PER_IP_LIMITS`, responses, limiting a new endpoint), [api/endpoints.md](../../../docs/api/endpoints.md) (428), [api/events.md](../../../docs/api/events.md), [api/auth.md](../../../docs/api/auth.md) (the CSP exception, challenges, the proxy note), [api/README.md](../../../docs/api/README.md), [api/testing.md](../../../docs/api/testing.md), [api/database.md](../../../docs/api/database.md), [naming.md](../../../docs/naming.md), [ui/api.md](../../../docs/ui/api.md#challenges), [ui/design.md](../../../docs/ui/design.md), [ui/README.md](../../../docs/ui/README.md), [ui/testing.md](../../../docs/ui/testing.md), [deployment.md](../../../docs/deployment.md) (the variables move from Planned, Turnstile notes, the checklist), [testing.md](../../../docs/testing.md), `.env.example`, `.env.testing`, AGENTS.md (the third-party exception).

## Not included

- Other CAPTCHA providers: hCaptcha (the same shape as Turnstile, but it can't bind a token to a form on the free tier), reCAPTCHA v2, and a self-hosted proof-of-work provider. Each is one `captcha.Provider` and one widget adapter, added for a real use case. reCAPTCHA v3 is score-based with no widget, so it would need a different design.
- The Cloudflare edge-protection listener (designed above).
- A shared login limit (`login:global`).
- Trusted proxies and real client IPs: their own plan, [client IP behind a proxy](client-ip.md).
- Changing a limit's levels without a code change, apart from `PER_IP_LIMITS`.
- Challenges on per-subject limits other than `auth:per-ip`, or on any GET.
- Turnstile pre-clearance (`cf_clearance`) from the dialog.
- A public API (`/api/v1`); it gets its own plan, which must carry the edge-rule exclusion.
- A privacy notice.
