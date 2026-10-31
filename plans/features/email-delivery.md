# Email delivery over SMTP

Status: **decisions confirmed 2026-10-31**, to implement after the test environment. Builds on [test environment](test-environment.md) (`.env.testing`, the test database, Playwright starting the server), which ships first. Completes what [forgot password](implemented/forgot-password.md) and [workspace invitations](implemented/workspace-invitations.md) left out: real delivery.

## Why

Password reset and invitations work, but their emails only reach a development mailbox file (`MAIL_DRIVER=file`), which production refuses. Both features are therefore switched off in production and answer 503 ([deployment.md](../../docs/deployment.md#email)).

**The approach is one SMTP driver, not a provider's HTTP API:**

- **Every provider accepts SMTP:** Resend, Postmark, Amazon SES, Mailgun, SendGrid, and Mailtrap's sending service. Changing provider is a change to `.env`, not to code.
- **Local and production run the same code.** The local test inbox, [Mailpit](https://mailpit.axllent.org/), is an SMTP server, so development and the end-to-end tests send exactly as production does. An API driver would need its own fake.
- **It's the common default.** Laravel's mailer and Rails' ActionMailer both default to SMTP, with provider APIs as optional extras.
- **What an API would add:** idempotency keys, clearer errors, slightly lower latency, and working on hosts that block outgoing SMTP ports (some do by default). None is needed now. `mail.Driver` is a one-method interface, so an API driver can be added later without touching templates, listeners or config rules.

**The client is [`wneessen/go-mail`](https://github.com/wneessen/go-mail):**

- `net/smtp` is frozen: bugs still get fixed, but no new features, and it has no `context` support. We'd have to build the MIME message and encode headers ourselves, which is where injection bugs come from.
- `go-mail` is maintained and supports `context`. It builds the multipart message (Date, Message-ID, encoding) and can be set to require TLS.
- It's a new direct dependency, the seventh. Its v0.7.1 release fixed a vulnerability in address handling, so `govulncheck` covers it like every other dependency.

## Design

### Configuration (`internal/config`)

| Variable | Default | Meaning |
| --- | --- | --- |
| `MAIL_DRIVER` | `smtp` | `smtp` sends over SMTP. `log` appends to `MAIL_FILE` (today's `file` driver, renamed; refused in production). `off` sends nothing, and reset and invitations answer 503 |
| `MAIL_FROM_ADDRESS` | none; required for `smtp` | The sender, a bare address such as `no-reply@example.com`. Your provider must have verified its domain |
| `MAIL_FROM_NAME` | `APP_NAME` (`lewcontext`) | The sender's display name |
| `SMTP_HOST` | none; required for `smtp` | e.g. `smtp.resend.com`, or `127.0.0.1` for Mailpit |
| `SMTP_PORT` | 587 with `starttls`, 465 with `tls`; required with `none` | |
| `SMTP_TLS` | `starttls` | `starttls` (upgrade a plain connection; required, never optional), `tls` (TLS from the start, usually 465), or `none` (Mailpit only) |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | empty | Set both or neither. Sent with AUTH PLAIN, only over TLS |

Startup refuses:

- `MAIL_DRIVER` other than `smtp`, `log` or `off`;
- `smtp` without `SMTP_HOST` or `MAIL_FROM_ADDRESS`. The message says to set them, or `MAIL_DRIVER=off` to run without email;
- a `MAIL_FROM_ADDRESS` that isn't a bare address, and a `MAIL_FROM_NAME` with a line break (both go into the From header);
- a port outside 1–65535, an unknown `SMTP_TLS`, a username without a password or a password without a username;
- **`SMTP_TLS=none` with credentials, anywhere** (the password would cross the network unencrypted), **and `none` in production**;
- `log` in production, as `file` is refused today.

Error messages name the variable, never its value, so `SMTP_PASSWORD` can't appear in a log. The address is split into two variables, as Laravel does (`MAIL_FROM_ADDRESS`, `MAIL_FROM_NAME`): a `Name <address>` value has spaces, which bash's `source` and Make's `include` read differently, and `.env` is read by both.

**Production when unset:** `MAIL_DRIVER` defaults to `smtp` there too, so a production server with no SMTP settings refuses to start instead of silently running without email. Running without email takes an explicit `MAIL_DRIVER=off` (decision 6).

### The driver (`internal/mail/smtp.go`)

```go
// SMTPDriver sends each message over its own SMTP connection. TLS is
// required unless the config says none (Mailpit), and certificates are
// verified.
type SMTPDriver struct { /* go-mail client options, from, fromName */ }

func NewSMTPDriver(c SMTPConfig) (*SMTPDriver, error)
func (d *SMTPDriver) Deliver(ctx context.Context, m Message) error
```

- **One connection per message,** sent with go-mail's `DialAndSendWithContext` and the listener's context, which already has a 30 s timeout. A pool isn't worth it at this volume.
- **TLS:**
  - `starttls` uses go-mail's mandatory policy: if the server doesn't offer STARTTLS, nothing is sent.
  - `tls` connects with TLS from the start.
  - Certificates are always verified, against the system's root certificates.
- **Headers:**
  - **From:** `MAIL_FROM_NAME <MAIL_FROM_ADDRESS>`, from config.
  - **To:** the recipient `Send` already checks (a bare address, no line breaks).
  - **Subject:** the fixed one for the template.
  - **Date.**
  - **Message-ID:** go-mail's random ID, on the From address's domain.
  - **`Auto-Submitted: auto-generated`** (RFC 3834), so out-of-office replies don't answer it.

  No user data goes into any header.
- **Body:** `multipart/alternative` with the plain-text part first and the HTML part second (below).
- **Logging:** on success, `mail sent` with the template and Message-ID. The recipient and body are never logged, matching the `log` driver.
- **Failures return an error,** so the event worker retries as it does now (1 s, 10 s, 1 min, 10 min, 1 h, then failed). A permanent 5xx refusal is retried too (see Not included). The error goes into `event_deliveries.last_error`. SMTP replies can include the recipient's address, which is the same personal data the event already holds. An authentication failure never includes the password.
- **A duplicate is harmless.** If the server accepts the message and the process stops before the delivery is marked done, the listener runs again and sends a new link, which replaces the first. Only the newer email works.
- `cmd/server` builds the driver from config: `smtp` → `NewSMTPDriver`, `log` → `FileDriver`, `off` → no listeners, as today. Its startup log line names the host, port, TLS mode and From address, never the credentials.

Our package is also called `mail`, so go-mail is imported as `gomail`.

### Plain-text part

- **Templates:** each email gains `templates/<name>.txt` beside its `.html`. Mail clients that don't show HTML show this part, and sending both lowers spam scores.
- **Rendering:** `.txt` files are rendered with `text/template`, the right choice for plain text, where there's no HTML to escape. That happens only in `internal/mail/text.go`, which parses only `*.txt` files.
- **The import ban:** `TestNoTextTemplate` keeps banning `text/template` in every other file, and checks that `text.go` never touches `.html`.
- **Data and startup:** the data structs are the same, and `mail.New` still parses everything at startup, so a missing or broken `.txt` stops the server.
- **`Message`** gains `Text`. The `log` driver writes it above the HTML.
- **No subject in the files:** subjects stay fixed strings in Go, never in the `.txt` file.

### Mailpit for development and tests

```yaml
# compose.yaml
  mailpit:
    image: axllent/mailpit:<pinned release>
    restart: unless-stopped
    ports:
      - "127.0.0.1:1025:1025"   # SMTP
      - "127.0.0.1:8025:8025"   # web inbox and API
    environment:
      MP_MAX_MESSAGES: 5000     # oldest deleted beyond this
```

- **`make db`** starts `postgres` and `mailpit`, so `make dev` and the tests always have it. The `app` container depends on it and sends to `mailpit:1025`.
- **Browse `http://localhost:8025`** to read every email sent locally, rendered, with its plain-text part and headers.
- **Localhost only**, like the database. Its inbox holds working reset links.
- **The image tag is pinned** to a release, never `latest`.

`.env.example` (development) and `.env.testing` both get:

```sh
MAIL_DRIVER=smtp
MAIL_FROM_ADDRESS=no-reply@lewcontext.test
SMTP_HOST=127.0.0.1
SMTP_PORT=1025
SMTP_TLS=none
SMTP_USERNAME=
SMTP_PASSWORD=
```

`.env.testing` also gets `MAILPIT_URL=http://127.0.0.1:8025` and loses `MAIL_FILE`. `.env.example` keeps a commented example for a real provider and for Mailtrap's online sandbox (staging), which work with the same driver.

### End-to-end tests read Mailpit

`mailedLink` in `workspace.spec.ts` polls `GET {MAILPIT_URL}/api/v1/search?query=to:"<address>"` for the newest message, fetches it (`/api/v1/message/{ID}`), and takes the link from the HTML part. It also checks the plain-text part has the same link. Each run's recipients are unique (`e2e-<timestamp>-…`), so messages from other runs or from development never match. Nothing needs clearing between runs.

CI adds a `mailpit` service container with the same ports. The e2e preflight ([test environment](test-environment.md#running-several-at-once-e2e_port)) also checks that Mailpit answers at `MAILPIT_URL` and on `SMTP_PORT`, and if not, stops before the build with "Mailpit isn't running: start it with `make db`". Parallel runs share one Mailpit, since each finds its mail by its own recipients.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| Credentials or links read on the network | TLS is required in production (`none` refused). STARTTLS is mandatory, never opportunistic. Certificates are verified. Credentials are refused over `none` everywhere. |
| Leaking the SMTP password | Only from the environment. Never in error messages, startup logs or `last_error`. `.env.testing` holds an empty `SMTP_PASSWORD` (`TestEnvTestingHoldsNoSecrets`). The deployment checklist says to pass it through the platform's secret store. |
| Header injection | From is validated at startup. To is checked by `Send`. Subjects are fixed. go-mail encodes headers. No user data in any header. |
| HTML injection through names | Unchanged: `html/template` escapes the HTML part. The plain-text part shows names as typed, so a name containing a URL can be turned into a link by the mail client. That's the same as in the HTML part today, where clients link bare URLs in text. The emails say who sent them (the inviter's address) so a misleading name can be judged. |
| Mailpit's inbox holding working links | Bound to `127.0.0.1`. Development and test only. Production refuses `none`, so it can't send to Mailpit there. |
| Spam sent from our domain once delivery is real | Existing limits: reset per email, per IP and for everyone, and invitations per user, per workspace and for everyone ([rate-limits.md](../../docs/api/rate-limits.md)). CAPTCHA challenges are planned. Deployment docs require SPF, DKIM and DMARC through the provider. |
| Turning email on in production changes what's exposed | Reset links and invitations become real. The rules around them (single use, digests only, 30-minute and 7-day lifetimes, no account lookup at sign-up) already assume a real inbox. AGENTS.md, [auth.md](../../docs/api/auth.md) and [deployment.md](../../docs/deployment.md) change from "no email delivery" to "SMTP when configured". Email verification, MFA and OIDC still don't exist. |
| A hanging SMTP server blocking the worker | The 30 s listener context covers the dial and the send. One slow message doesn't hold up other listeners in the batch. |

## Tests

- **Go unit (`internal/mail/smtp_test.go`)** against a small **in-process SMTP server** written in the test file (listening on `127.0.0.1:0`, a few dozen lines, no dependency). It can advertise STARTTLS with a certificate generated in the test, accept AUTH PLAIN, record what it receives, and answer with a given error. The driver has an unexported way to trust that test certificate.
  - **The message:** From, To, Subject, Date, Message-ID and `Auto-Submitted` are present. The body is `multipart/alternative`, text then HTML, both with the link. A name holding CR/LF or `<h1>` changes no header.
  - **TLS:** `starttls` against a server without STARTTLS sends nothing (no `MAIL FROM` received). An untrusted certificate fails. `tls` mode works.
  - **Auth:** credentials are sent only after TLS. A 535 refusal returns an error that doesn't contain the password.
  - **Failures:** 4xx and 5xx at `RCPT` or `DATA` return errors. A server that never answers returns by the context deadline.
- **Go unit (templates):** `TestEveryTemplateRenders` also renders each `.txt`. A missing `.txt` stops `mail.New`. `TestNoTextTemplate` allows `text/template` only in `text.go`.
- **Go unit (`config`):**
  - `smtp` is the default everywhere, and `log` and `off` are accepted;
  - every refusal in the list above;
  - port defaults by TLS mode;
  - no error message contains `SMTP_PASSWORD`'s value.
- **Go integration:** unchanged. `mailServer(t)` already registers the listeners with an in-memory `mailbox` driver (the equivalent of Laravel's `Mail::fake()`). No Go test needs SMTP settings.
- **Playwright:** the forgot-password and invitation flows read their links from Mailpit, so the real driver, Mailpit and both parts are exercised end to end, locally and in CI.

## Docs

- `docs/api/mail.md`: drivers, the SMTP driver, plain-text templates, adding an email (both files), Mailpit.
- `docs/deployment.md`: requirements (outgoing SMTP on 587 or 465), config table, an Email section with provider examples (host, port, which credential is the password), SPF/DKIM/DMARC, the checklist.
- `docs/api/README.md`: package table and environment variables.
- `docs/api/auth.md`: the "no email delivery" lines.
- `docs/api/events.md`: the listeners now send real email.
- `docs/testing.md`: Mailpit in setup, `http://localhost:8025`.
- `docs/ui/testing.md`: the mailbox is Mailpit.
- `docs/api/testing.md`: SMTP driver tests.
- `README.md`, plus the authentication/security invariants and Commands in AGENTS.md.
- `compose.yaml`, `.env.example`, `.env.testing`, `.github/workflows/ci.yml`.

## Decisions

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | SMTP or a provider API | SMTP only; an API driver later if a host blocks SMTP. **Confirmed.** |
| 2 | Client library | `wneessen/go-mail`. **Confirmed.** |
| 3 | Local inbox | Mailpit in `compose.yaml`, the development default; e2e tests read its API. **Confirmed.** |
| 4 | Variable names | `MAIL_*` for mail in general, `SMTP_*` for the driver. **Confirmed.** The sender is split into `MAIL_FROM_ADDRESS` and `MAIL_FROM_NAME` (defaulting to `APP_NAME`), because a value with spaces isn't read the same by bash and Make. **Split confirmed.** |
| 5 | Driver values | `smtp` (default), `log`, `off`. Rename `file` to `log`, matching Laravel. `MAIL_FILE` keeps its name. **Confirmed.** |
| 6 | Production with nothing set | `smtp` is the default there too, so the server refuses to start without SMTP settings. `off` must be set explicitly. **Confirmed.** |
| 7 | TLS | `starttls` by default. `none` refused in production and with credentials anywhere. Certificates always verified. **Confirmed.** |
| 8 | Credentials required in production | No. Some relays authorize by IP. TLS is still required. |
| 9 | Plain-text part | A `.txt` template per email, rendered with `text/template` in `text.go` only. |
| 10 | Testing the driver | An in-process SMTP server in the Go test: no test SMTP variables, always runs in `make test`. Mailpit is covered by the e2e tests. |
| 11 | Checking SMTP at startup | No. A mail outage shouldn't stop the app; the worker retries for an hour, and failures are logged. |
| 12 | Extra headers | `Auto-Submitted: auto-generated`, and a Message-ID on the sender's domain. |

## Not included

- **Provider API drivers** (Resend, Postmark, SES).
- **Idempotency keys.** Resend reads a `Resend-Idempotency-Key` header even over SMTP; a duplicate already carries a replaced link.
- **Bounce and complaint webhooks**, and a suppression list.
- **Telling permanent from temporary SMTP failures.** A 5xx is retried like a 4xx, five times over an hour, then marked failed.
- **DKIM signing in the app** (the provider signs), and `SMTP_PASSWORD_FILE` for file-based secrets.
- **Connection pooling, attachments, `List-Unsubscribe`, and marketing or bulk email.**
- **Email verification**, which would build on this but changes sign-up. It needs its own plan.
