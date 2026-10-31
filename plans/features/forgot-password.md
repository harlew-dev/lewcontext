# Forgot password

Status: **implemented 2026-10-31** with the recommended decisions below. Docs: [auth.md](../../docs/api/auth.md#password-reset-by-link), [mail.md](../../docs/api/mail.md). Builds on [events](events.md) (first real listener) and [change password](change-password.md) (the new-password step).

## Why

A user who forgets their password has no way back in. Add "Forgot password?" to the login page. It records an event, and a listener issues a single-use link. There is no email delivery yet, so in development the listener writes the link to a local mailbox file. That also makes this the first listener on the event bus.

## Flow

```
/auth/login  "Forgot password?"  →  /auth/forgot-password  (email, prefilled from login)
  POST /api/auth/password-reset {email}
    rate limits (IP, email, global)
    record auth.password_reset_requested {email, ip}     ← no user lookup here
    → 202 "If an account uses that email, we've sent a link."  (same for every email)

worker → listener mail.password_reset (≈1 s later)
    event older than 30 min?             → drop (stale retry)
    no account for email?                → nothing, no event
    new token; store its digest (replaces any earlier link for that user)
    mailer.Send(ctx, email, mail.ForgotPassword, ForgotPasswordData{Name, URL})
                                         URL = APP_ORIGIN + /auth/forgot-password/<token>
    record auth.password_reset_sent {user, email, request: evt_…}

/auth/forgot-password/<token>
    POST /api/auth/password-reset/check {token}   → valid: show form | invalid: "expired" + request again
    New password + Confirm (as step 2 of the change-password dialog)
    PUT /api/auth/password-reset {token, newPassword}
      one transaction: lock the reset row and user, set hash, delete every session,
      delete the reset row, record auth.password_reset_completed
    → /auth/login (email prefilled), toast "Password updated. Sign in with your new password."
```

## Events

The project convention is `<area>.<past_tense>`, snake_case (events plan, decision 3). The names you asked for (`auth.forgot-password`, `auth.forgot-password.sent`) become:

| Event | Payload | Recorded by |
| --- | --- | --- |
| `auth.password_reset_requested` | `email`, `ip` | request handler, for every request that passes the rate limits, whether or not the account exists. `user_id` scope stays null |
| `auth.password_reset_sent` | `user`, `email`, `request` (the requested event's `evt_` ID) | listener, after the link is written |
| `auth.password_reset_completed` | `user`, `otherSessionsRevoked` | `ResetPassword`, in its transaction |

The token and its digest never appear in a payload. `TestPayloadsHoldNothingSensitive` covers the new structs.

## Where the token is made: in the listener

The request handler does not create the token. The listener does, when it runs:

- **The raw token never touches the database.** If the handler made it, the token would have to sit in the event payload or a column for the listener to read. Anyone with read access to `events`, a backup or a replica could then take over any account. Made in the listener, it exists only in the listener's memory and in the mailbox.
- **The request path is the same for every email.** The handler doesn't look the user up at all, so the response's status, body and timing can't reveal whether an account exists. Lookups happen later, in the worker.
- **At-least-once delivery is harmless.** A rerun (crash after the write, failed `sent` record) issues a fresh token that replaces the row, and writes a new link. The older link stops working. There is at most one live link per user.

## API

| Endpoint | Body | Success | Failure |
| --- | --- | --- | --- |
| `POST /api/auth/password-reset` | `{ email }` | `202` | `422` invalid email shape · `429` / `503` limits (below) |
| `POST /api/auth/password-reset/check` | `{ token }` | `204` | `422` `field: "token"` "This link has expired or was already used." |
| `PUT /api/auth/password-reset` | `{ token, newPassword }` | `200 { email }` (prefills login) | `422` `field: "token"` (as above) or `field: "newPassword"` (12–128 characters) |

- No session needed. The existing `security` middleware still enforces an exact `Origin`, no cross-site `Sec-Fetch-Site`, and a JSON content type. Bodies stay under 16 KiB.
- Tokens travel only in JSON bodies, never in API URLs.
- Token shape is checked before any query: 43 base64url characters (`auth.Token()`). Lookup is by SHA-256 digest, like sessions.
- Same-as-current isn't checked: it would cost an extra Argon2id verify and isn't a security property.

### Table: `password_resets` (new create migration `009`, squashed)

```sql
CREATE TABLE password_resets (
 user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,  -- one live link per user
 token_hash text NOT NULL UNIQUE,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL
);
```

Factory in `internal/factory`, required by `TestEveryTableHasAFactory`. Expired rows are deleted by the existing `Store.Cleanup` loop.

### Store

- `IssuePasswordReset(ctx, email, digest, expires) (User, error)`: upsert on `user_id`. Returns `sql.ErrNoRows` for an unknown email, and the listener treats that as done.
- `PasswordResetValid(ctx, digest) (bool, error)`: used by check. Requires `expires_at > now()`.
- `ResetPassword(ctx, digest, hash func() (string, error)) (User, error)`: one transaction. It selects the reset row `FOR UPDATE` (unexpired), locks the user row, hashes, updates `password_hash`, deletes every session and the reset row, and records `auth.password_reset_completed`. A second concurrent request with the same token waits on the row lock, then finds nothing, so a link works only once.
- `ChangePassword` (signed in) also deletes the user's `password_resets` row, so changing your password kills any outstanding link.

### Mail helper (`internal/mail`)

A small package that any listener can use: `mailer.Send(ctx, to, template, data)`.

```
apps/api/internal/mail/
  mail.go                     Mailer, Send, the template registry, Message
  file.go                     the file driver (development mailbox)
  templates/forgot-password.html
  mail_test.go                every template renders; escaping tests
```

The templates live inside the package, not in a top-level `templates/mail/`, because `//go:embed` can only reach files under its own package directory. They are compiled into the binary like the web build.

```go
// mail.go (sketch)
type Template string

const ForgotPassword Template = "forgot-password"

// Subjects are fixed text kept in Go, not in the HTML file: a subject is a mail
// header, and html/template would HTML-escape it ("&" → "&amp;").
var subjects = map[Template]string{ForgotPassword: "Reset your password"}

type ForgotPasswordData struct {
	Name string // the account's name: untrusted, escaped by html/template
	URL  string // built from APP_ORIGIN by the caller
}

type Message struct{ To, Subject, HTML string }

// Driver delivers a rendered message: the file mailbox now, SMTP or a provider later.
type Driver interface {
	Deliver(ctx context.Context, m Message) error
}

type Mailer struct {
	templates *template.Template // html/template, parsed once at startup
	driver    Driver
}

func (m *Mailer) Send(ctx context.Context, to string, t Template, data any) error
```

`Send` checks `to` and the template name, renders the template into a buffer, and passes the result to the driver. If rendering fails, nothing is sent, so a broken template can't send half an email.

**Templates.** The placeholders use Go's syntax, `{{ .URL }}` and `{{ .Name }}`. The leading dot is what reads a field of `data`; a bare `{{ url }}` would mean "call a function named url". Each template has a data struct, `ForgotPasswordData` above, so a template's placeholders are listed in Go. A mismatch fails at render time, and a test renders every template, so it fails in CI rather than in production.

```html
<!-- templates/forgot-password.html -->
<p>Hi {{ .Name }},</p>
<p>Someone asked to reset the password for your lewcontext account.
   This link works once and expires in 30 minutes:</p>
<p><a href="{{ .URL }}">Reset your password</a></p>
<p>If that wasn't you, ignore this email. Your password hasn't changed.</p>
```

A shared `layout.html`, with a header and footer around each template's content, is only worth adding once there's a second template.

**File driver.** `FileDriver` appends each message to `MAIL_FILE` (default `tmp/mail.log`, gitignored, created `0600`): a separator line, `To`, `Subject` and the time, then the rendered HTML. The server's stdout log gets only "mail written to tmp/mail.log (to=…, template=forgot-password)", without the body, so reset links don't spread into container or aggregated logs. To see the email rendered, copy the HTML block into a `.html` file and open it.

**Listener.** `PasswordReset(store, mailer, origin)` returns the listener, registered in `cmd/server` as `events.Listen(bus, "mail.password_reset", …)`. It builds the link from `APP_ORIGIN` (config), never from a request's `Host`. It respects `ctx` (30 s per run), and returning an error triggers the existing retries.

### Keeping templates safe

The user's name is the untrusted value: it can be any 1–80 characters, including `<script>alert('XSS')</script>` or `<h1>Hack</h1>`. Email clients don't run scripts, but they do render injected HTML: a fake "Click here to verify" button, a different link, or a hidden message. That injection is the real risk.

**Use `html/template`, never `text/template`.** It escapes each value for where it appears in the HTML (text, attribute, URL, CSS or JS), not with one blanket rule. Its security model is that templates are trusted and the data passed to them is not, which matches ours:

| Data | Where in the template | Output |
| --- | --- | --- |
| `Name: "<script>alert('XSS')</script>"` | `Hi {{ .Name }}` | `Hi &lt;script&gt;alert(&#39;XSS&#39;)&lt;/script&gt;`: shows as text |
| `Name: "<h1>Hack</h1>"` | `Hi {{ .Name }}` | `Hi &lt;h1&gt;Hack&lt;/h1&gt;`: shows as text |
| `Name: "\" onmouseover=\"x"` | `title="{{ .Name }}"` | quotes escaped; stays inside the attribute |
| `URL: "javascript:alert(1)"` | `href="{{ .URL }}"` | `#ZgotmplZ`: the URL is replaced, because only `http`, `https` and `mailto` pass |

Rules the package follows:

1. **Only `html/template` in `internal/mail`** for HTML bodies. A test asserts `text/template` isn't imported for them.
2. **Never wrap data in `template.HTML`, `template.URL`, `template.HTMLAttr`, `template.JS` or `template.CSS`.** Those types mean "already safe, don't escape" and switch the protection off. Data structs hold plain `string`s only.
3. **Templates come only from the embedded files**, never from the database, a request or the user. The trust model depends on template authors being trusted.
4. **Parse once at startup** (`template.ParseFS` over the embedded `templates/*.html`). A missing or broken template stops the server from starting. `Send` with an unknown template name is an error.
5. **No user data in headers.** Subjects are fixed strings in Go. `to` must parse with `net/mail` and contain no CR or LF, which prevents header injection (adding a `Bcc:` line) once a real SMTP driver exists.
6. **Inline styles only, no scripts, no remote images.** Mail clients strip scripts anyway, and remote images act as read-tracking pixels.
7. **Render to a buffer, then deliver.** Delivery never starts from a half-rendered template.

A later SMTP driver would add a plain-text alternative. That would be `forgot-password.txt` rendered with `text/template`: plain text doesn't need HTML escaping, but its subject and headers still follow rule 5.

### Config

`MAIL_DRIVER`: `file` (the default outside production) or `off`. **With `APP_ENV=production`, `file` is refused at startup** (`internal/config`): a mailbox of live reset links is an account-takeover log. Production therefore runs with `off`. In that mode the request endpoint answers `503` "Password reset isn't available.", and the listener isn't registered. A real email driver later replaces this without touching the handlers.

## Rate limits

`AllowAuth` today has a hard-coded 15-minute window. It gains a `window` parameter (`now() + ?::interval`), and `Retry-After` follows it.

**Naming.** Every limit has one name, `<feature>:<counted by>`, used everywhere: in the `auth_attempts.key` prefix, in `auth.rate_limited`'s `scope`, in the Go constant, and in docs. A key in `auth_attempts` is that name plus its subject: `<feature>:<counted by>:<sha256 hex of IP or email>`. A global limit has no subject. The two existing limits are renamed to match (`ip:` → `auth:per-ip:`, `account:` → `auth:per-account:`). Their rows only last 15 minutes and old `auth.rate_limited` events are local test data, so nothing needs migrating.

In Go, each limit is a named value, so its name, number and window are defined in one place:

```go
// httpapi/limits.go
type rateLimit struct {
	name   string // "<feature>:<counted by>", also auth.rate_limited's scope
	limit  int
	window time.Duration
}

var (
	authPerIP                = rateLimit{"auth:per-ip", 60, 15 * time.Minute}
	authPerAccount           = rateLimit{"auth:per-account", 10, 15 * time.Minute}
	passwordResetPerIP       = rateLimit{"password-reset:per-ip", 10, time.Hour}
	passwordResetPerEmail    = rateLimit{"password-reset:per-email", 3, time.Hour}
	passwordResetAllAccounts = rateLimit{"password-reset:all-accounts", 100, time.Hour}
)
```

| Name (`scope`) | Full key in `auth_attempts` | Limit | Used on | Refusal | Purpose |
| --- | --- | --- | --- | --- | --- |
| `auth:per-ip` (was `ip`) | `auth:per-ip:<sha256 of connection IP>` | 60 / 15 min | register, login, both signed-in password endpoints (existing); password-reset request, check and complete (new) | 429 | general budget for one IP across all sign-in work |
| `auth:per-account` (was `account`) | `auth:per-account:<sha256 of email>` | 10 / 15 min | register, login, both signed-in password endpoints (existing only) | 429 | password guessing on one account; **not** touched by password reset |
| `password-reset:per-ip` | `password-reset:per-ip:<sha256 of connection IP>` | 10 / hour | reset request | 429 | one IP can't mail many different people |
| `password-reset:per-email` | `password-reset:per-email:<sha256 of email>` | 3 / hour | reset request | 429 | **per account, across all IPs**: caps mail to one inbox |
| `password-reset:all-accounts` | `password-reset:all-accounts` (no subject) | 100 / hour | reset request | 503 "Password reset is busy. Try again later." | **global, across all accounts**: caps total mail during a distributed flood |

The complete endpoint also takes one of the 4 Argon2id hashing slots (`limitAuth`, 503 when busy). Check doesn't hash, and probing tokens is useless anyway at 256 bits.

- **Order matters:** `auth:per-ip` → `password-reset:per-ip` → `password-reset:per-email` → `password-reset:all-accounts`. A request refused by an earlier limit doesn't spend the later ones, so one noisy client can't use up the global budget alone. Only many IPs and many emails together can.
- **Unknown emails count exactly like real ones**, so a 429 reveals nothing about which accounts exist.
- **Separate from login.** Password reset never counts against `auth:per-account`, so flooding resets can't lock someone out of signing in, and the reverse is also true.
- `password-reset:all-accounts` is one hot row in `auth_attempts`. Every request upserts it under a row lock. At the volumes the per-IP limits allow, that is fine.
- When the global limit trips, legitimate users are refused for up to an hour. That trade-off is deliberate, and the `auth.rate_limited {scope: "password-reset:all-accounts"}` event plus a `slog.Warn` is the alert hook.
- Every limit still records `auth.rate_limited` only for the first refusal in its window.

## Security risks and how we avoid them

| Risk | Mitigation |
| --- | --- |
| **Account enumeration** (does this email have an account?) | Identical `202` and message for every email. No user lookup in the request, so timing is equal too. Limits apply to unknown emails as well. Work for real accounts happens later in the worker. (Register's `409` already reveals registered emails. That leak exists today and is out of scope here.) |
| **Database, backup or audit log leaks a usable token** | Only the SHA-256 digest is stored. The raw token is never in `events`, payloads, the API URL or stdout logs. It exists in listener memory and the mailbox file only. |
| **Guessing a token** | 256 random bits (`auth.Token()`), 30-minute lifetime, IP limits. Unguessable regardless of rate. |
| **Token leaks from the page URL** (browser history, shared screen, `Referer`, proxy logs) | Single use, 30-minute lifetime, replaced by any newer request, killed by a password change or reset. Web pages send `Referrer-Policy: strict-origin-when-cross-origin`, so other sites get the origin only. Pages load no third-party resources. Go keeps no access log, so paths aren't logged. The page sends the token to the API in a POST body, not a URL. See decision 4. |
| **Host-header poisoning** (link points at an attacker's domain) | The link is built from `APP_ORIGIN` only. |
| **Mail bombing a victim** | `password-reset:per-email` 3 / hour, `password-reset:per-ip` 10 / hour, `password-reset:all-accounts` 100 / hour. |
| **Distributed flood** (cost, event-table growth, sender reputation once real email exists) | Global cap. Refused requests aren't recorded one by one: one `auth.rate_limited` per window. |
| **Attacker burns a victim's reset quota** | Accepted. The quota resets within an hour, and it doesn't affect signing in (separate keys). |
| **Replay or two tabs submitting the same link** | Reset row locked `FOR UPDATE` and deleted in the same transaction as the password change. |
| **Late retry sends an old link** (retries back off up to an hour) | The listener drops requests older than the token lifetime (`d.OccurredAt`). |
| **Attacker stays signed in after the owner resets** | The reset deletes **every** session. No automatic sign-in: the user signs in with the new password, which also lets password managers save it. |
| **Dev mailbox reaches production** | `MAIL_DRIVER=file` refused at startup in production. Mailbox file `0600`, gitignored, separate from stdout. |
| **CSRF on the anonymous endpoints** | Exact `Origin` and JSON content type (existing `security` middleware). Completing a reset needs the token, which an attacker doesn't have. |
| **CPU exhaustion through hashing** | Only completion hashes, behind `limitAuth`'s 4 slots. Request and check do no hashing. |
| **Personal data in events** | `email` (only once it parses as an address) and `ip` on `requested`, as on `auth.login_attempted`. Pruned after 90 days by `make prune`. |
| **Clickjacking the reset page** | Existing `frame-ancestors 'none'` and `X-Frame-Options: DENY`. |

## UI

- `pages/auth/login.tsx`: a "Forgot password?" link beside the Password label. It goes to `/auth/forgot-password` and carries the typed email in router `state`, not the query string, so the email stays out of URLs.
- `pages/auth/forgot-password.tsx`: `AuthLayout`, an email field, and `<Button loading>`. On `202`, the form is replaced by a notice: "If an account uses that email, we've sent a link. It expires in 30 minutes." 429/503 messages show inline.
- `pages/auth/forgot-password/[token].tsx`:
  - checks the token's shape locally, then calls check;
  - while checking, shows a skeleton; if the link is invalid or expired, shows `<Notice variant="error">` with a link to request a new one;
  - otherwise shows New password + Confirm with `autocomplete="new-password"`. On success it navigates to login with the email and a toast.
  - Passwords live only in component state.
- Routes in `src/routes.ts` and `lib/routes.ts` (`forgotPassword`, `resetPassword(token)`). Neither is prerendered: Go already serves `/auth/*` through the SPA fallback with `no-store`.
- **Shared new-password UI:** `PasswordField`, the new + confirm pair and `newPasswordProblems` move out of `pages/app/settings.tsx` into `components/app/account/password-fields.tsx`, because two pages now use them (rule 3).
- `lib/api/auth.ts`: `requestPasswordReset(email)`, `checkPasswordReset(token)` and `resetPassword({ token, newPassword })`, with paths in `paths.ts`. A 422 on the token sends the page to its "expired" state.

## Tests

- **Go integration:**
  - **Request:**
    - an unknown and a known email give byte-identical responses;
    - one `requested` event each;
    - the listener issues a token only for the known account and records `sent`;
    - the mailbox line holds a link that works.
  - **Limits:**
    - each limit, including unknown emails hitting `password-reset:per-email`;
    - `password-reset:all-accounts` trips at 100;
    - a refused request doesn't spend later limits;
    - `auth.rate_limited` carries the new scope names, including the renamed `auth:per-ip` and `auth:per-account`.
  - **Check:** valid, expired, malformed, used, and replaced by a newer request.
  - **Complete:**
    - the new password signs in and the old one doesn't;
    - every session is revoked;
    - the link works once, including two concurrent completions;
    - `ChangePassword` kills an outstanding link;
    - a short password gives 422 and changes nothing;
    - bad Origin and wrong content type are refused.
  - **Listener:** a stale event is dropped; running twice leaves one live token; nothing sensitive appears in any payload.
  - **Config:** `MAIL_DRIVER=file` with `APP_ENV=production` is refused.
- **Go unit (`internal/mail`):**
  - every registered template renders with its data struct, and has a subject;
  - an unknown template name is an error;
  - the names `<script>alert('XSS')</script>` and `<h1>Hack</h1>` come out escaped, with no `<script` or `<h1` in the output;
  - a `javascript:` URL renders as `#ZgotmplZ`;
  - a `to` with CR/LF or an invalid address is refused before rendering;
  - the file driver writes `0600` and appends.
- **Vitest:**
  - the login link carries the email;
  - the request page shows the same confirmation for every result;
  - the token page states (checking, expired, form, mismatch, server field errors).
- **Playwright:** register → log out → forgot password → read the link from `MAIL_FILE` → set a new password → sign in with it.
- **Docs:**
  - `docs/api/auth.md`: passwords, rate-limit table, and remove "password recovery" from Not implemented, saying it's development-only without email;
  - `docs/api/events.md`: catalogue, and "No listeners are registered yet";
  - `docs/api/endpoints.md`, `docs/api/database.md`, `docs/api/README.md` (env vars and the new `internal/mail` package);
  - a new `docs/api/mail.md` (sending mail, adding a template, the safety rules), indexed in `docs/README.md`;
  - the AGENTS.md line about not claiming password recovery.

## Decisions (to confirm)

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Event names | `auth.password_reset_requested` / `_sent` / `_completed` (convention), not `auth.forgot-password` / `.sent`. |
| 2 | Where the token is created | In the listener, so the raw token is never stored and the request path is identical for every email. |
| 3 | "Log file" | `internal/mail` with `mailer.Send(ctx, to, template, data)`, `html/template` files embedded in the binary, and a file driver writing rendered HTML to `tmp/mail.log` (`0600`), not the server's stdout log. Refused in production. |
| 9 | Placeholder syntax | Go's `{{ .URL }}` with a data struct per template, rather than custom `{{ url }}` replacement, so `html/template` does the escaping. |
| 4 | Token in the page path (`/auth/forgot-password/<token>`) | Keep it, as you asked, with the mitigations above. The alternative is a fragment (`…/forgot-password#<token>`), which the browser never sends to any server. AGENTS.md forbids tokens in query strings, and a path isn't one, but this exception should be written into `auth.md`. |
| 5 | After a successful reset | All sessions revoked, sent to login with the email prefilled. No automatic sign-in. |
| 6 | Limits | `password-reset:per-ip` 10/h, `password-reset:per-email` 3/h, `password-reset:all-accounts` 100/h, as named `rateLimit` values in `httpapi/limits.go`. Make them configurable only if needed. |
| 10 | Limit names | `<feature>:<counted by>` everywhere, renaming the existing `ip` and `account` to `auth:per-ip` and `auth:per-account`. |
| 7 | Production | `MAIL_DRIVER=off`, so the endpoint answers 503 until real email exists. The login link still shows and leads to that message. |
| 8 | Link lifetime | 30 minutes, one live link per user (a newer request replaces the older link). |

## Not included

- Real email delivery: SMTP or a provider, templates, bounce handling.
- A "your password was reset" notification. It would be a listener on `auth.password_reset_completed` once email exists.
- MFA, breached-password checks, and CAPTCHA on the request form.
- Fixing register's existing enumeration (409 on a taken email).
