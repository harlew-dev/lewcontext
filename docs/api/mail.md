# Mail

Code: `internal/mail`. Plan: [plans/features/forgot-password.md](../../plans/features/forgot-password.md).

The API sends email through `mailer.Send(ctx, to, template, data)`. It renders an HTML template and hands the result to a driver. There is no real email delivery yet. The only driver is a development mailbox file, and production runs with mail off.

## Sending

```go
mailer, _ := mail.New(&mail.FileDriver{Path: c.MailFile}) // cmd/server, at startup
err := mailer.Send(ctx, user.Email, mail.ForgotPassword, mail.ForgotPasswordData{Name: user.Name, URL: link})
```

- **Listeners send mail, handlers don't.** A handler records an event, and a listener on it sends the email ([events.md](events.md#listeners)), so a slow or failing driver never holds up a request and is retried.
- **`to`** must be a bare address (no display name) with no CR or LF, so it can't add headers. Anything else is refused before rendering.
- **Rendered first, then delivered.** If a template fails to render (the wrong data struct, for example), nothing is sent.

## Templates

`internal/mail/templates/<name>.html`, embedded in the binary with `//go:embed` (so they live inside the package, not at the repository root). `mail.New` parses them all at startup, and a missing or broken template stops the server from starting.

| Template | Constant | Data | Subject |
| --- | --- | --- | --- |
| `forgot-password.html` | `mail.ForgotPassword` | `ForgotPasswordData{Name, URL}` | Reset your password |
| `workspace-invitation.html` | `mail.WorkspaceInvitation` | `WorkspaceInvitationData{Workspace, Inviter, InviterEmail, Role, URL}` (`Role` with its article, "a member"; `Inviter` empty once that account is gone) | You’ve been invited to join a workspace |

Every email shares `templates/layout.html`: the document, the grey page and the white card. Each email's file defines only its `title` and `content`, and `mail.New` parses each one with the layout as a set of its own.

Placeholders use Go's syntax, `{{ .Name }}` and `{{ .URL }}`: the dot reads a field of the data struct.

### Adding one

1. Write `templates/<name>.html`, defining `{{ define "title" }}` and `{{ define "content" }}`: inline styles only, no scripts, no remote images (they act as tracking pixels).
2. Add a `Template` constant, its subject in `subjects`, and a `<Name>Data` struct of plain `string` fields.
3. Add example data to `examples` in `mail_test.go`. `TestEveryTemplateRenders` fails without it.

## Keeping templates safe

An account's name is whatever the user typed, such as `<script>alert('XSS')</script>` or `<h1>Hack</h1>`. Mail clients don't run scripts, but they do render injected HTML: a fake button, a different link, a hidden message. The package prevents that:

- **`html/template`, never `text/template`.** It escapes each value for where it appears: text, attribute, URL, CSS or JS. `<h1>Hack</h1>` becomes `&lt;h1&gt;Hack&lt;/h1&gt;` and shows as text. A `javascript:` value in `href` becomes `#ZgotmplZ`; only `http`, `https` and `mailto` links pass. `TestNoTextTemplate` checks the import.
- **Never convert data to `template.HTML`, `template.URL`, `template.HTMLAttr`, `template.JS` or `template.CSS`.** Those types mean "already safe" and switch escaping off.
- **Templates are trusted; data isn't.** That is `html/template`'s security model, so templates come only from the embedded files, never from the database or a request.
- **No user data in headers.** Subjects are fixed strings in Go, not in the HTML file. They're mail headers, and HTML escaping would garble them (`&` → `&amp;`).

## Drivers

| `MAIL_DRIVER` | Driver | Notes |
| --- | --- | --- |
| `file` (default outside production) | `FileDriver` | Appends each message to `MAIL_FILE` (`tmp/mail.log`; `make` uses the repository's `tmp/`): a `====` line, `To`, `Subject`, `Template`, then the HTML. The file is created `0600`, and the server log notes only that mail was written, never the body. **Refused in production:** it holds working reset links. |
| `off` (default in production) | none | Nothing is sent. Neither listener is registered, and requests for a reset link or an invitation answer 503. |

To read the mailbox: `tail -n 40 tmp/mail.log`. To see an email rendered, save its HTML to a `.html` file and open it. With `make docker-dev`, it's inside the container: `docker compose exec app cat /tmp/mail.log`.

A real driver (SMTP or a provider's API) implements `mail.Driver` and adds a `MAIL_DRIVER` value. It should also send a plain-text part, rendered from a `.txt` template.
