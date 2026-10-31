# Blocking an email address

Status: **decisions confirmed 2026-10-31**, ready to implement (decision 13 to confirm). Builds on [email delivery](implemented/email-delivery.md) (the SMTP driver, the per-recipient rule in [mail.md](../../docs/api/mail.md#who-gets-email-and-how-often)) and [workspace invitations](implemented/workspace-invitations.md).

## Why

Invitations go to any address an inviter types, including people who have never heard of the app. `invitation:per-email` caps them at 5 an hour per address, but someone being harassed, or who simply doesn't want our email, has no way to make it stop for good.

**Every invitation email gets a "Block this address" link.** It opens a page on our site that explains what blocking does and asks for confirmation. Once blocked, the address gets no more email from us and can't be used to sign up. The link only works for an address **without an account**. An address that has one is told to delete the account first, because blocking a working account's address would cut it off from password reset.

The link is built so other emails can carry it later: a template is marked **blockable**, and the mailer adds the link, the matching `List-Unsubscribe` header and the check against the block list. For now, only invitations are blockable (decision 1):

- **Password reset** only reaches existing accounts, so the link would always answer "delete your account first";
- **security mail** is never blockable;
- **workspace notifications** (future) are opt-in and turned off in settings ([mail.md](../../docs/api/mail.md#who-gets-email-and-how-often)).

## Design

### Flow

1. **Sending:** the invitation listener calls `mailer.Send` as today. `WorkspaceInvitation` is blockable, so `Send`:
   1. checks the block list first. A blocked address gets nothing: `Send` returns `mail.ErrBlocked`, and the listener treats that as done (no retry, no `workspace.invitation.sent`);
   2. issues a block link for the address: a 256-bit token, with only its SHA-256 digest stored, as for reset and invitation links;
   3. renders the email with the link in both parts' footers, and adds `List-Unsubscribe: <link>`.
2. **The page,** `/auth/email-blocks/<token>`, checks the link (`POST /api/email-blocks/check`) and shows one of four states:

   | State | The page says | Action |
   | --- | --- | --- |
   | Dead link (unknown or expired) | "This link has expired or isn't valid." | none |
   | The address has an account | "**ada@example.com** has a lewcontext account. To stop email from lewcontext, delete your account first: sign in, then Account & security." | Sign in (a link) |
   | Not blocked | "Block **ada@example.com**? You won't receive any more email from lewcontext, and this address can't be used to sign up. You can undo this from this link for a year." | **Block this address** |
   | Blocked | "**ada@example.com** is blocked. You won't receive email from lewcontext, and it can't be used to sign up." | **Unblock** |

   The page works signed in or out (no `guestLoader`), like the reset and invitation links. It uses `AuthLayout` with one full-width primary button, per [forms-and-actions.md](../../docs/ui/forms-and-actions.md). Blocking or unblocking switches it to the other state, with a status message.
3. **Blocking** (`POST /api/email-blocks {token}`) and **unblocking** (`DELETE /api/email-blocks {token}`) work only with a live link.

### What a block does

- **No email:** every `Send` to a blocked address is skipped, whatever the template. Security mail goes only to accounts, and a blocked address can't have one, so the rule "security mail is never limited" still holds.
- **No invitations:** `POST …/invitations` to a blocked address answers 422, `field: "email"`, "This address has asked not to receive email from lewcontext." It's an honest refusal (decision 2), shown on the Invite dialog's email field.
- **No sign-up:** `POST /api/auth/register` with a blocked address answers 422, `field: "email"`, "This address has been blocked from lewcontext. To use it, unblock it from the link in an email we sent." (decision 3). There's no email verification, so today anyone can register with someone else's address. A block also prevents that.
- **Invitations already pending** to the address stay as they are (decision 9). Their emails were already sent, they can't be accepted without an account, and they expire within 7 days.

### Data

Migrations `011_create_email_block_links` and `012_create_email_blocks`, each with a factory (`TestEveryTableHasAFactory`):

| Table | Columns | Why |
| --- | --- | --- |
| `email_block_links` | `id`, `token_hash bytea UNIQUE`, `email text`, `expires_at`, `created_at` | One row per blockable email sent. It holds the address, because the page shows it and checks it against `users`. The server's 15-minute cleanup (`Store.Cleanup`) deletes rows past `expires_at`, one year after sending (decision 4) |
| `email_blocks` | `id`, `email text UNIQUE`, `created_at` | The block list: the normalised address (trimmed, lower-case, as sign-up stores it), kept for as long as it's blocked. Keeping an address in order to honour a request not to be contacted is the case data-protection law expects (a suppression list), and it lets an operator list and unblock addresses (below). Not a log table: never pruned |

Neither table needs a public ID: links are found by token digest, and blocks by address. Nothing else references them, and deleting an account doesn't touch them, since a blocked address can't have one.

### Racing a sign-up

Blocking needs "no account uses this address", and signing up needs "this address isn't blocked". Two transactions running at once under read committed could each pass their check before the other commits, and both would succeed. So both take a **transaction-scoped advisory lock keyed by the email** first (`pg_advisory_xact_lock`, with a key taken from the first 8 bytes of the address's SHA-256), through a small helper in `internal/database`, where advisory locks already live (AGENTS.md allows raw SQL only there for them). They then check, insert and commit. The second one waits, then sees the first's row.

`CreateInvitation` checks the block list in its transaction without the lock. An invitation slipping in at the same moment as a block is still never emailed, because `Send` checks again.

### Endpoints

| Method | Route | Requires | Success |
| --- | --- | --- | --- |
| POST | `/api/email-blocks/check` | Origin, JSON, `auth:per-ip` | 200 `{email, registered, blocked}` for a live `{token}`; 422 with `field: "token"` otherwise. Works signed out |
| POST | `/api/email-blocks` | Origin, JSON, `auth:per-ip` | 204; the address is blocked (blocking an address that's already blocked is also 204). Body `{token}`. 409 "This address has an account. Delete your account first." when one uses it; 422 with `field: "token"` for a dead link |
| DELETE | `/api/email-blocks` | Origin, JSON, `auth:per-ip` | 204; the address is unblocked (unblocking one that isn't blocked is also 204). Body `{token}`. 422 with `field: "token"` for a dead link |

- **Named for the resource, not the page** ([naming.md](../../docs/naming.md)). `emailBlockHandlers{check, store, destroy}`; `check` is a named action on the link, like `invitations.check`. The token is only ever in the body, never in an API URL. The page path carries it, as the reset and invitation pages do (AGENTS.md's single-use-link exception).
- **Store methods:** `IssueEmailBlockLink`, `CheckEmailBlockLink`, `BlockEmail`, `UnblockEmail` (by link, or by address for the command line), `IndexEmailBlocks`, and `CheckEmailBlocked` (the yes/no question).
- **Client:** `api.emailBlocks().check(token)`, `.store(token)` and `.destroy(token)`, with `paths.emailBlocks()`. Page route `routes.emailBlocks.show(token)`; the page file is `pages/auth/email-blocks/[token].tsx`, exporting `EmailBlockShow`.
- **Rate limits:** `auth:per-ip`, as `invitations/check` uses. Tokens are unguessable (256 bits), so the limit only bounds load. No new limit.

### The mailer (`internal/mail`)

```go
// Each template's subject, and whether its emails carry a block link.
var templates = map[Template]templateInfo{
	ForgotPassword:      {subject: "Reset your password"},
	WorkspaceInvitation: {subject: "You’ve been invited to join a workspace", blockable: true},
}

// Blocks is what Send needs from the store for blockable email.
type Blocks interface {
	CheckEmailBlocked(ctx context.Context, email string) (bool, error)
	IssueEmailBlockLink(ctx context.Context, email string) (token string, err error)
}

mail.New(mail.Options{Driver: driver, App: c.AppName, Origin: c.Origin, Blocks: repository})
```

- **Every** `Send` checks `CheckEmailBlocked`; only blockable templates issue a link.
- **Footer:** both layouts end with an optional line, filled only for blockable email. HTML: "Don't want email from lewcontext? <a>Block this address</a>." Plain text: "Don't want email from lewcontext? Block this address: <link>". The layouts read the link with `{{ blockURL }}`; `Send` clones the parsed templates and sets that function per message (`Clone` keeps the parsed originals untouched).
- **`Message.Unsubscribe`** carries the link, and `SMTPDriver` sends it as `List-Unsubscribe: <https://…/auth/email-blocks/…>` (decision 5). Gmail, Apple Mail and others then show an "Unsubscribe" link by the sender, which opens our page. It's the URL form only, without `List-Unsubscribe-Post`: one-click unsubscribe would block at once, with no confirmation and no account check. The header holds our own URL, not user data.
- **Making another email blockable** is then one word, `blockable: true`, plus a test.

### Command line (`cmd/admin`)

An operator can list and unblock addresses without a link, for example after the year a link lasts, or when someone writes in. The commands follow the [admin access](implemented/admin-access.md) plan's style; this plan creates `cmd/admin` with these two subcommands, and that plan adds `grant`, `revoke` and `key` to it.

```sh
admin blocks                                   # every blocked address, oldest first, with when
admin unblock -email ada@example.com           # preview: "would unblock ada@example.com (blocked 2026-10-31)"
admin unblock -email ada@example.com -confirm  # unblocks; records email.unblocked
```

- `make admin blocks` and `make admin unblock email=… CONFIRM=1` locally; `docker run … lewcontext admin unblock …` in production, where it's allowed (it removes a block; it can't add one or read anything but the list).
- `-email` is normalised as sign-up does. An address that isn't blocked says so and changes nothing.
- There is no `admin block`: blocking takes the inbox owner's link, so it always comes from them.

### Events

| Name | Payload | Recorded by |
| --- | --- | --- |
| `email.blocked` | `email`, `ip` | `BlockEmail`, when the address wasn't already blocked |
| `email.unblocked` | `email`, `ip` (empty from the command line), `by`: `link` or `cli` | `UnblockEmail`, when it was blocked |

The address is in the payload (decision 6), as it is for sign-in attempts, reset requests and invitations, so the audit trail shows who blocked and unblocked. Like those, it's deleted with the event by the 90-day prune. No user, workspace or actor. Both are added to `events.Catalogue` and the catalogue in [events.md](../../docs/api/events.md).

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| Someone blocks another person's address | Only the inbox that received the email has the token (256 bits, stored as a digest). Blocking a *registered* address is impossible, so nobody can lock a user out of password reset. An unregistered address that was blocked by mistake can be unblocked from the same link for a year |
| The page shows whether an address has an account | Only to whoever holds a link sent to that inbox, normally its owner. A forwarded link would show it to someone else: accepted (decision 7). The API's other answers are unchanged (reset, invitations and sign-in reveal nothing new) |
| The refusals show who has blocked | Sign-up and invitations say an address is blocked to anyone who types it. That's low-sensitivity (the person asked us to refuse it), and blocking sign-up is the point. Neither refusal says whether an account exists |
| A block and a sign-up racing | A transaction-scoped advisory lock keyed by the email serialises them |
| An invitation created during a block still emailed | `Send` checks the block list for every email, so even mail queued before the block isn't sent |
| Personal data | `email_blocks` holds each blocked address for as long as it's blocked: the minimum needed to honour the request. `email_block_links` holds addresses for up to a year, deleted by the cleanup. The two events hold the address for 90 days, like other events with an email. A database leak would expose who asked not to be contacted; digests were considered and rejected (decision 10) |
| Misuse of the command line | `admin unblock` only removes a block, needs `-confirm`, and records `email.unblocked` with `by: "cli"`. It runs with database access, which already allows anything |
| A token in a URL | Only in the page path (AGENTS.md's single-use-link exception), never in API URLs or `?next=`. The page sends it in the body |
| Header injection through `List-Unsubscribe` | Its value is built from `APP_ORIGIN` and a base64url token, with no user data |

## Tests

- **Go unit (`internal/mail`):**
  - blockable email carries the footer in both parts and the `List-Unsubscribe` header;
  - non-blockable email has neither;
  - a blocked address gets nothing (`ErrBlocked`) for every template;
  - a link-issuing error sends nothing;
  - the footer link survives `html/template` (an `https` URL, not `#ZgotmplZ`);
  - `TestEveryTemplateRenders` covers both kinds.
- **Go integration (`httpapi`):**
  - **The link:** the invitation email's link checks out (`registered: false`, `blocked: false`, the address); only its digest is stored.
  - **Blocking:** it records `email.blocked` with the address and IP; a second block is a no-op (204, no second event).
  - **Effects of a block:**
    - an invitation to the address answers 422 on `email`;
    - sign-up with it answers 422 on `email` and creates nothing;
    - a pending invitation's email isn't sent.
  - **Unblocking:** it lets both through again, and records `email.unblocked` (`by: "link"`). Unblocking by address (the command line's store call) does the same with `by: "cli"`; an address that isn't blocked changes nothing; `IndexEmailBlocks` lists oldest first.
  - **Registered addresses:** `check` says `registered: true`, and `store` answers 409 and blocks nothing.
  - **Dead links:** an expired or unknown token answers 422 for all three endpoints, and the cleanup deletes expired links.
  - **The race:** block and sign-up for the same address, run at once five times; exactly one wins each time (this fails without the lock).
  - **Limits and plumbing:** `auth:per-ip` applies; the Origin and JSON checks apply; the token never appears in an event.
- **Go unit (`config`/`smtp`):** the `List-Unsubscribe` header's form.
- **Vitest (`pages/auth/email-blocks.test.tsx`):**
  - the four states;
  - Block and Unblock with pending states, a failure keeping the page, and switching states afterwards;
  - the token only in request bodies;
  - the address rendered as text.
- **Playwright:**
  - invite an unregistered address, open the block link from Mailpit, block it;
  - inviting it again is refused on the field, and registering with it is refused;
  - unblock from the same link, and the invitation goes through;
  - a registered address's link says to delete the account first.

## Docs

- `docs/api/mail.md`: blockable templates, the footer and header, blocked addresses skipped, and the rule's third column.
- `docs/api/endpoints.md`: the three routes, and an Email blocks section.
- `docs/api/auth.md`: blocking, its effects on sign-up and invitations, and the account check.
- `docs/api/database.md`: both tables, and the cleanup.
- `docs/api/events.md`: the two events.
- `docs/api/rate-limits.md`: `auth:per-ip` on the new routes.
- `docs/ui/pages.md`: `/auth/email-blocks/<token>`.
- `docs/ui/api.md` if needed.
- `docs/api/testing.md` and `docs/ui/testing.md`.
- `docs/deployment.md`: the Email section mentions blocks.
- `docs/testing.md` and `docs/deployment.md`: `make admin blocks` / `admin unblock`.
- AGENTS.md: rule 5 gains `cmd/admin` (command-line entrypoints); the security invariants gain "a blocked address gets no email and can't sign up; only addresses without an account can be blocked, and only by their own link; the command line can only unblock".

## Decisions

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Which emails carry the link | Invitations only for now, through a `blockable` flag on the template, so another email opts in with one word. **Confirmed.** |
| 2 | An invitation to a blocked address | An honest 422 on the email field. **Confirmed.** |
| 3 | Sign-up with a blocked address | Refused. **Confirmed.** |
| 4 | How long a link works | A year after the email was sent; then the cleanup deletes it. **Confirmed.** |
| 5 | `List-Unsubscribe` | Yes, the URL form only (no one-click `List-Unsubscribe-Post`), pointing at the page. **Confirmed.** |
| 6 | What the events record | The address and IP, as other events with an email do; pruned after 90 days. **Confirmed.** |
| 7 | Showing the address and "has an account" on the page | Yes. Only a holder of a link sent to that inbox sees it; a forwarded link is the accepted exception. **Confirmed.** |
| 8 | Which emails a block stops | All of them (every `Send` checks), not only blockable ones. Since a blocked address can't have an account, that never includes security mail. **Confirmed.** |
| 9 | Invitations pending when an address is blocked | Left alone: already emailed, can't be accepted without an account, and expire within 7 days. **Confirmed.** |
| 10 | Storing blocks | The address itself, not a digest: what a suppression list needs, and what listing and unblocking by address need. Digests would only have hidden guessable addresses from someone with database access. **Confirmed.** |
| 11 | Serialising a block and a sign-up | A transaction-scoped advisory lock keyed by the email, via an `internal/database` helper. **Confirmed.** |
| 12 | Wording | As in the tables above: "Block this address" and "Unblock" on the page; "Don't want email from lewcontext? Block this address." in emails; the 409 and the two 422s as written. **Confirmed.** |
| 13 | The command line | This plan creates `cmd/admin` with `blocks` and `unblock -email … [-confirm]`; [admin access](implemented/admin-access.md) adds its own subcommands later. No `admin block`. Allowed in production. |

## Not included

- **One-click unsubscribe** (RFC 8058 `List-Unsubscribe-Post`), which blocks without a page or confirmation.
- **A block link on password reset or security mail.**
- **A way for registered users to refuse invitations** ("Don't accept invitations" in settings). `invitation:per-email` caps them meanwhile.
- **Blocks in an admin dashboard.** `admin blocks` lists them; a page can come with the admin plans.
- **Removing pending invitations on block** (decision 9).
- **Workspace notifications and their settings** (their own plan; the opt-in rule is already in mail.md).
