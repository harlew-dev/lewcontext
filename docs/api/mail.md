# Mail

Code: `internal/mail`. Plans: [forgot-password](../../plans/features/implemented/forgot-password.md), [email-delivery](../../plans/features/implemented/email-delivery.md), [email-blocks](../../plans/features/implemented/email-blocks.md).

The API sends email through `mailer.Send(ctx, to, template, data)`. It renders the email's HTML and plain-text parts from templates and hands them to a driver: SMTP (`MAIL_DRIVER=smtp`, the default), a development mailbox file (`log`), or nothing (`off`). In development and in the tests, SMTP goes to Mailpit, a local inbox at `http://localhost:8025`.

## Sending

```go
// cmd/server, at startup: the driver is mail.NewSMTPDriver(…) or &mail.FileDriver{…}
mailer, _ := mail.New(mail.Options{Driver: driver, App: c.AppName, Origin: c.Origin, Blocks: repository})
err := mailer.Send(ctx, user.Email, mail.ForgotPassword, mail.ForgotPasswordData{Name: user.Name, URL: link})
```

- **Listeners send mail, handlers don't.** A handler records an event, and a listener on it sends the email ([events.md](events.md#listeners)), so a slow or failing driver never holds up a request and is retried.
- **`to`** must be a bare address (no display name) with no CR or LF, so it can't add headers. Anything else is refused before rendering.
- **Rendered first, then delivered.** If either part fails to render (the wrong data struct, for example), nothing is sent.
- **Never to a blocked address.** Every `Send` checks the block list first, whatever the template, and returns `mail.ErrBlocked` without sending ([Blocked addresses](#blocked-addresses)). `New` refuses to make a mailer without `Blocks`. An admin's retry of a failed delivery ([admin.md](admin.md#retrying-deliveries)) runs its listener again, through `Send`, so a retry never reaches a blocked address either, and reset and invitation retries make a fresh link.
- **Nothing for a suspended account's requests:** the reset listener finds no account for one ([auth.md](auth.md#suspended-accounts)).

## Who gets email, and how often

Our server must never be usable to flood someone's inbox, and important mail must never be held back. So **limits go on the request that causes an email, never on sending it**: the driver and listeners don't delay, throttle or drop anything once it's queued. Every email type is one of three kinds, and a new one isn't done until it fits one:

| Kind | Examples | Rule |
| --- | --- | --- |
| **Asked for by a user**, for themselves or someone else | password reset, invitations, sign-up codes and "You already have an account", an account's verification code | A per-email limit on the request, `<feature>:per-email` ([rate-limits.md](rate-limits.md)), counted for every address whether or not it has an account, refused with 429 before anything is queued: `password-reset:per-email` (3 an hour), `invitation:per-email` (5 an hour), `register:per-email` (3 an hour), `email-verification:per-email` (3 an hour). One that can reach someone who never heard of us (an invitation) is blockable too: its recipient can stop all our email for good ([below](#blocked-addresses)) |
| **Security mail** about the recipient's own account | password changed, new sign-in (none yet) | Never limited: an attacker must not be able to spend a victim's budget and hold back the warning. The actions that trigger it are already limited |
| **Operational mail to admins** | alert emails ([monitoring.md](monitoring.md#email-to-admins)) | Triggered by no request, so no per-email limit; each admin's own `alert_email` (`all`, `critical` by default, `none`) is the opt-in, and the alert rules' bounds (one open alert per signal, an hour's cooldown per signal, a digest past 5 openings in 10 minutes, a reminder every 4 hours for critical alerts only) stop a flood, including one someone raises on purpose. They go through `mailer.Send`, so the block list holds |
| **Notifications** about things in a workspace | a project or workspace deleted (none yet) | Not rate limited, because they matter, but **opt-in only: never subscribe anyone automatically.** Only the user can turn them on (a setting such as "Workspace notifications: All / Deletions only / None", off by default). Otherwise someone could add a person to 20 projects and delete them all, sending 20 emails they never asked for |

### Blocked addresses

Someone who never asked for our email (an invitation's recipient) can **block their address** from a link in it, and an operator can unblock it from the command line ([auth.md](auth.md#blocking-an-email-address)). A blocked address has no account, so blocking never touches security mail or codes for an account.

- **Blockable templates** (`blockable` in `mail.go`; only `WorkspaceInvitation`) get a block link of their own on every send: a 256-bit token (`auth.Token`), stored as its digest in `email_block_links` once both parts have rendered, for a year. Both layouts end with it, read as `{{ blockURL }}`: "Don’t want email from lewcontext? Block this address." (a link in the HTML, the URL in the text). `Send` renders a clone of the parsed templates with that function set, so concurrent sends don't share it. The link is also `Message.Unsubscribe`, sent as `List-Unsubscribe: <…>` (the URL form only, never `List-Unsubscribe-Post`: one click would block without the page's confirmation).
- **Not blockable:** password reset and "You already have an account" (only accounts get them; the page would say "delete your account first"), sign-up codes (asked for that moment) and security mail. Making another email blockable is one line in `blockable`, plus a test.
- **Listeners treat `ErrBlocked` as done** (`done(err)`): no retry, and no `.sent` event. An invitation's `sent_at` was set when its link was issued, before `Send`, so it shows "Sent" to the inviter like any other: nothing anyone can type says an address is blocked. A sign-up's row still gets a code nobody is sent, like a taken email's.
- **No confirmation email** after blocking: the page confirms it, and mailing someone who just asked for no mail would ignore the request.

### A limit in a listener

One exception, on purpose: **"You already have an account"** is capped at 5 a day per account in the `mail.registration` listener (`mail.AccountExistsPerAccount`, `account-exists:per-account`), which then sends nothing and records `rate_limit.reached` the first time in a window. A limit on the request would give the account away, since only taken emails would hit it: the cap can only live where the account is looked up, after the response. Nothing anyone was promised is held back (the person signing up was told only "check your email", and the owner misses a sixth copy of the same notice in a day), and it never applies to security mail or codes. It counts a delivery's first attempt only, so retries don't spend it. Don't add another without the same reasons.

Delaying sends by how many a recipient has had was considered and rejected: a delayed reset link arrives expired or replaced, the person who asked is told it worked while nothing arrives, and a send budget per recipient is exactly what an attacker would spend to suppress security mail.

## Templates

`internal/mail/templates/<name>.html` and `<name>.txt`, embedded in the binary with `//go:embed` (so they live inside the package, not at the repository root). Every email has both: mail clients that don't show HTML show the plain text, and sending both lowers spam scores. `mail.New` parses them all at startup, and a missing or broken template, of either kind, stops the server from starting.

| Template | Constant | Data | Subject |
| --- | --- | --- | --- |
| `forgot-password.html` | `mail.ForgotPassword` | `ForgotPasswordData{Name, URL}` | Reset your password |
| `verify-email.html` | `mail.VerifyEmail` | `VerifyEmailData{Code}` (spaced, "482 913"). **No name and no link:** anyone can send a sign-up's code to any address, so none of its words may be theirs, and a code is typed where it was asked for, on any device | Confirm your email address |
| `account-exists.html` | `mail.AccountExists` | `AccountExistsData{Name, LoginURL, ResetURL}`: the account's own name, never the one typed; links to sign in (with the account-exists notice) and to ask for a reset link. No code | You already have an account |
| `alert.html`, `alert-digest.html` | `mail.Alert`, `mail.AlertDigest` | `AlertData{Kind, Level, Title, Value, Opened, OpenFor, Meaning}`, `AlertDigestData{Count, Alerts}`: **fixed text and numbers only**, no link (the email says to open the admin dashboard: never the key or a URL), no address, name or ID. Sent to admins by the alert listeners ([monitoring.md](monitoring.md#email-to-admins)) | `Alert`'s is built from the signal's title (`AlertData.Subject`: "Critical alert: …", "Warning: …", "Now critical: …", "Still open: …", "Resolved: …"; `titled`, checked by `subjectOf` against a strict shape: words, digits and a little punctuation); the digest's is fixed |
| `workspace-invitation.html` | `mail.WorkspaceInvitation` | `WorkspaceInvitationData{Workspace, Inviter, InviterEmail, Role, URL, Home}` (`Role` with its article, "a member"; `Inviter` empty once that account is gone; `Home` the homepage, for the "Learn about …" button beside "View the invitation", since the recipient may not know the app) | You’ve been invited to join a workspace |

Every HTML email shares `templates/layout.html`: the document, the grey page, the white card and, at its top, the logo and the app's name; below the card, a blockable email's block link. The logo is `templates/logo.png`, the web app's `favicon.svg` rendered at 64×64 and shown at 32×32 (`node scripts/email-logo.mjs` regenerates it after a logo change); `Send` embeds it in every message as an inline attachment, which the layout shows with `src="cid:logo"`. The name beside it is text, so the email is branded even with images off. Each email's file defines only its `title` and `content`, and `mail.New` parses each one with the layout as a set of its own. Every plain-text email shares `templates/layout.txt`, which opens with the app's name on its own line (and for a blockable email ends with its block link); each `.txt` file defines its `content`: the same words, the link written out, no markup.

The app's name is `APP_NAME` (`lewcontext` by default), the same as the web build's; `mail.New` takes it as `Options.App`, and templates read it with `{{ app }}`. The Docker image sets it from its `APP_NAME` build argument, so the web app and the emails agree.

Placeholders use Go's syntax, `{{ .Name }}` and `{{ .URL }}`: the dot reads a field of the data struct.

### Adding one

1. Write `templates/<name>.html`, defining `{{ define "title" }}` and `{{ define "content" }}`: inline styles only, no scripts, no remote images (they act as tracking pixels). Write `templates/<name>.txt` defining `{{ define "content" -}}` … `{{- end }}`, with the same text and the link on a line of its own.
2. Add a `Template` constant, its subject in `subjects`, and a `<Name>Data` struct of plain `string` fields.
3. Add example data to `examples` in `mail_test.go`. `TestEveryTemplateRenders` fails without it.
4. Decide its kind ([above](#who-gets-email-and-how-often)): asked-for mail gets a `<feature>:per-email` limit on the request that causes it, with an integration test; security mail gets none; notifications need an opt-in setting first.
5. Decide whether it's blockable ([Blocked addresses](#blocked-addresses)): mail that can reach someone without an account, who didn't ask for it, should be.

## Keeping templates safe

An account's name is whatever the user typed, such as `<script>alert('XSS')</script>` or `<h1>Hack</h1>`. Mail clients don't run scripts, but they do render injected HTML: a fake button, a different link, a hidden message. The package prevents that:

- **`html/template`, never `text/template`.** It escapes each value for where it appears: text, attribute, URL, CSS or JS. `<h1>Hack</h1>` becomes `&lt;h1&gt;Hack&lt;/h1&gt;` and shows as text. A `javascript:` value in `href` becomes `#ZgotmplZ`; only `http`, `https` and `mailto` links pass. `TestNoTextTemplate` checks the import.
- **Never convert data to `template.HTML`, `template.URL`, `template.HTMLAttr`, `template.JS` or `template.CSS`.** Those types mean "already safe" and switch escaping off.
- **Images:** no SVG (Gmail and Outlook don't display it, inline or as an image), no `data:` URLs (Gmail blocks them) and no remote images (blocked by default in many clients, and they act as tracking pixels). An image must be a PNG embedded in the message as an inline attachment (`Message.Inline`) and referenced with `cid:`, as the logo is. A hosted logo URL was considered and rejected: blocked by default in Outlook and many work clients, a request to our server on every open, and a URL that could never change.
- **Templates are trusted; data isn't.** That is `html/template`'s security model, so templates come only from the embedded files, never from the database or a request.
- **No user data in headers.** Subjects are fixed strings in Go, not in the template files. They're mail headers, and HTML escaping would garble them (`&` → `&amp;`). The From header comes from config, the recipient is checked by `Send`, `List-Unsubscribe` is `APP_ORIGIN` and a base64url token (the driver refuses CR, LF, `<` and `>` in it), and nothing else is set from data.
- **The plain-text part uses `text/template`,** in `text.go` only, which parses only `.txt` files (`TestNoTextTemplate` checks both). Plain text has no markup to inject, so nothing needs escaping, and names can't add lines: the API refuses control characters in them. A name containing a URL may be shown as a link by a mail client, as it may in the HTML part.

## Drivers

| `MAIL_DRIVER` | Driver | Notes |
| --- | --- | --- |
| `smtp` (the default, production included) | `SMTPDriver` (`smtp.go`, [`go-mail`](https://github.com/wneessen/go-mail)) | Sends each message over its own connection, within the listener's 30 s, with a 10 s limit on each step. See below |
| `log` | `FileDriver` | Appends each message to `MAIL_FILE` (`tmp/mail.log`; `make` uses the repository's `tmp/`): a `====` line, `To`, `Subject`, `Template`, `List-Unsubscribe` for a blockable email, the plain text, then `---- HTML` and the HTML. The file is created `0600`, and the server log notes only that mail was written, never the body. **Refused in production:** it holds working reset links. For working without Docker |
| `off` | none | Nothing is sent. No listener is registered, and requests for a reset link or an invitation answer 503. `EMAIL_VERIFICATION` must be `off` too (it sends codes). Production must say `off` explicitly to run without email |

The old value `file` is refused with a message saying it's now `log`.

### SMTP

| Variable | Default | Notes |
| --- | --- | --- |
| `MAIL_FROM_ADDRESS` | (required) | A bare address, such as `no-reply@example.com`; the provider must have verified its domain |
| `MAIL_FROM_NAME` | `APP_NAME` | The display name. Two variables rather than `Name <address>`, whose spaces bash and make read differently in `.env` |
| `SMTP_HOST` | (required) | `127.0.0.1` for Mailpit. A host name only: no spaces, `/` or `@` (credentials go in `SMTP_USERNAME` and `SMTP_PASSWORD`, never `user:pass@host`, which the admin Configuration page would show) |
| `SMTP_TLS` | `starttls` | `starttls` (upgrade a plain connection; a server without STARTTLS gets nothing), `tls` (TLS from the start), or `none` (Mailpit only: refused in production, and refused anywhere with credentials) |
| `SMTP_PORT` | 587 for `starttls`, 465 for `tls` | Required with `none` (Mailpit: 1025) |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | (empty) | Both or neither; sent with AUTH PLAIN, only over TLS. Some relays authorise by IP and need none |

Startup refuses `smtp` without `SMTP_HOST` or `MAIL_FROM_ADDRESS`, and any rule above broken; messages name the setting, never its value, so the password can't reach a log. The startup log line names the host, port, TLS mode and sender, never the credentials.

- **Certificates are always verified** against the system's roots, TLS 1.2 at least.
- **Headers:** From (`MAIL_FROM_NAME <MAIL_FROM_ADDRESS>`), To, the fixed Subject, Date, a random Message-ID on the sender's domain, `Auto-Submitted: auto-generated` (RFC 3834, so out-of-office replies don't answer), and for a blockable email `List-Unsubscribe: <block link>` (RFC 2369, so mail clients offer "Unsubscribe", which opens the block page). No `User-Agent`. The body holds the plain text, then the HTML, then the logo (`image/png`, `Content-ID: <logo>`).
- **A failure** (an unreachable server, a refused login, a 4xx or 5xx reply) returns an error, so the event worker retries: 1 s, 10 s, 1 min, 10 min, 1 h, then failed ([events.md](events.md#delivery-eventsworker)). A permanent refusal is retried too. The error goes into `event_deliveries.last_error` scrubbed, so an SMTP reply's recipient is `<email>` there and in the log ([observability.md](observability.md#scrubbing)); never the password.
- **A duplicate is harmless:** if the server accepted a message and the process stopped before the delivery was marked done, the listener runs again and sends a new link, which replaces the first.
- **On success** the log says `mail sent` with the template and Message-ID: never the recipient or the body.

### Reading mail locally

`make db` starts Mailpit with the database (`compose.yaml`, `axllent/mailpit`, pinned). Every email the development server, the Docker `app` container or the e2e tests send lands at **`http://localhost:8025`**, rendered, with its plain text and headers. SMTP is on `127.0.0.1:1025`; both ports listen on localhost only, since the inbox holds working links. It keeps the newest 5000 messages. The e2e tests read it through its API (`/api/v1/search?query=to:"…"`, [../ui/testing.md](../ui/testing.md)).

With `MAIL_DRIVER=log`: `tail -n 40 tmp/mail.log`. To see an email rendered, save its HTML to a `.html` file and open it.

Mailtrap's online sandbox, or any provider, works with the same driver: only the SMTP settings change ([../deployment.md](../deployment.md#email)).
