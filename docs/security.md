# Security model and API

## Authentication

Local account authentication uses Argon2id (19 MiB memory, 2 iterations, one lane, random 16-byte salt, 32-byte digest). Passwords must be at least 12 Unicode characters and at most 128 UTF-8 bytes. User-facing forms cap input length and the API is authoritative. Emails are trimmed/lowercased and names are bounded. Unknown users are checked against a dummy password hash to reduce timing differences; invalid login responses are generic. A duplicate registration gives a generic conflict response, but the status can reveal registration. Email verification and account-recovery are outside this demo.

Session identifiers are 32 random bytes; only SHA-256 digests are persisted. CSRF tokens are independent random values and compared in constant time. The token is returned in authenticated JSON/SSR data; neither session IDs nor bearer credentials are put in localStorage. Production cookies are `__Host-forma_session`, HttpOnly, Secure, SameSite=Lax, Path=/, with no Domain. Local HTTP uses `forma_session` without Secure. Session state is authoritative in PostgreSQL, with 30-minute idle expiry, 24-hour absolute expiry, token rotation on login, logout revocation, and user-triggered revocation of other sessions. Requests refresh idle time. A periodic cleanup removes expired sessions/limits. A session cap of five is enforced transactionally with a per-user row lock.

All mutations reject absent/mismatched Origin, cross-site fetch metadata, and non-JSON POST/PATCH bodies. Authenticated mutations additionally require `X-CSRF-Token`. Login/registration use Origin+JSON protection before any credentials are accepted. CORS is deliberately not enabled. Strict Origin checks apply to command-line clients too.

Authentication attempts are limited in PostgreSQL: 10 per normalized account / 15 minutes and 60 per connection IP / 15 minutes, with four concurrent password operations per process. Successful attempts count too. Go ignores spoofable forwarded-IP headers, so the local Next proxy shares an IP quota. In a scaled production deployment, add a trusted-edge client-IP rate limiter and calibrate application limits for legitimate shared traffic. Never simply trust user-provided forwarding headers. Account lockout abuse remains possible with this fixed-window demo limiter.

## Authorization and transport

Every project query includes the authenticated `user_id`; users never supply that ID. Other users' project IDs return 404 for mutations. Queries use bound parameters. HTTP bodies and headers have size limits; HTTP and query contexts have timeouts; backend failures log details server-side and return a generic message. Avoid logging passwords, cookies, or CSRF tokens.

Next proxies only to the configured fixed API origin. API listeners default to loopback. Authenticated HTML/API responses are no-store. The frontend serves a per-request CSP nonce for scripts, blocks objects/framing, and limits connections to the same origin (WebSocket support only in development). Inline CSS is permitted because Styletron and BaseUI use it. Fonts are self-hosted (Inter via Fontsource), so no third-party font origin is allowed. HSTS is enabled when APP_ENV=production. TLS is provided by deployment infrastructure; production config requires HTTPS and database `sslmode=verify-full`.

## Endpoints

| Method | Route | Authorization | Result |
| --- | --- | --- | --- |
| GET | `/api/health` | Public | DB readiness |
| POST | `/api/auth/register` | Trusted origin, JSON, rate limited | User + CSRF, new cookie |
| POST | `/api/auth/login` | Trusted origin, JSON, rate limited | User + CSRF, rotated cookie |
| GET | `/api/auth/me` | Session | User, CSRF, sign-in/expiry times |
| POST | `/api/auth/logout` | Session + CSRF + origin | 204; revoke cookie/session |
| DELETE | `/api/auth/sessions` | Session + CSRF + origin | 204; revoke other sessions |
| GET | `/api/projects` | Session | One page of the current user's projects |
| GET | `/api/projects/{id}` | Session | Owned project; 404 otherwise |
| POST | `/api/projects` | Session + CSRF + origin | 201; created project |
| PATCH | `/api/projects/{id}` | Session + CSRF + origin | Updated owned project |

`GET /api/projects` returns `{ "data": [...], "pages": { "current", "total", "perPage", "items" } }`, counting only the caller's projects. It accepts only `page` (1–10 000), `perPage` (1–100, default 20), `q` (≤ 100 characters, no control characters; matched case-insensitively against name and description with LIKE wildcards escaped), `status`, and `sort` (`name`, `status`, `updated`, `created`, `-` prefix for descending; default `-updated`). Unknown, repeated, or out-of-range parameters return 400. Sort keys map to fixed SQL in `store/projects.go`; no column name comes from input. The page cap bounds `OFFSET`, and a page past the end returns an empty `data` with the real totals.

Project mutations accept `name`, `description`, `status` (`Planned`, `In progress`, `Completed`), and `color` (`violet`, `blue`, `amber`, `emerald`, `rose`). Unknown fields are rejected; PATCH currently replaces the complete editable representation. No project deletion is exposed.

## Scope

This is a local/demo foundation with security controls and executable tests, not a claim of independent security certification. Before public use, implement email verification/recovery, decide on MFA/passkeys or OIDC Authorization Code + PKCE with a maintained identity provider, add email delivery, observability, backups, abuse monitoring, and production load testing. Keep dependencies current. BaseUI compatibility overrides unify React 18 types and use compatible patched minor versions of its older dependencies; check these when upgrading BaseUI.

References: [OWASP password storage](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html), [OWASP session management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html), [Next SSR](https://nextjs.org/docs/pages/building-your-application/rendering/server-side-rendering), [BaseUI](https://baseweb.design/).
