# Blocking an email address

Status: **decisions 1–16 confirmed and implemented 2026-11-01** (see [Built so far](#built-so-far)). Revised 2026-11-01 for email verification, the existing `cmd/admin` and migrations 011–017, and to make every refusal silent (decisions 2, 3, 14). Builds on [email delivery](implemented/email-delivery.md) (the SMTP driver, the per-recipient rule in [mail.md](../../docs/api/mail.md#who-gets-email-and-how-often)), [workspace invitations](implemented/workspace-invitations.md) and [email verification](implemented/email-verification.md).

## Why

Invitations go to any address an inviter types, including people who have never heard of the app. `invitation:per-email` caps them at 5 an hour per address, but someone being harassed, or who simply doesn't want our email, has no way to make it stop for good.

**Every invitation email gets a "Block this address" link.** It opens a page on our site that explains what blocking does and asks for confirmation. Once blocked, the address gets no more email from us and can't be used to sign up. The link only works for an address **without an account**. An address that has one is told to delete the account first, because blocking a working account's address would cut it off from password reset.

The link is built so other emails can carry it later: a template is marked **blockable**, and the mailer adds the link, the matching `List-Unsubscribe` header and the check against the block list. For now, only invitations are blockable (decision 1):

- **Password reset** and **"You already have an account"** only reach existing accounts, so the link would always answer "delete your account first";
- **sign-up codes** go to someone who just asked for one, and a blocked address never gets them anyway;
- **security mail** is never blockable;
- **workspace notifications** (future) are opt-in and turned off in settings ([mail.md](../../docs/api/mail.md#who-gets-email-and-how-often)).

**A block is visible only to the inbox's owner.** Whoever invites or signs up with a blocked address is told nothing different (decision 14): a harasser who invites someone must not learn that they blocked, which would also confirm the address is read by a person. Only someone holding a link or a code sent to that inbox ever sees the word "blocked".

## Design

### Flow

1. **Sending:** the invitation listener calls `mailer.Send` as today. `WorkspaceInvitation` is blockable, so `Send`:
   1. checks the block list first. A blocked address gets nothing: `Send` returns `mail.ErrBlocked`, and the listener treats that as done (no retry, no `workspace.invitation.sent`). The invitation's `sent_at` is already set when its link is issued, before `Send`, so it shows "Sent …" like any other;
   2. issues a block link for the address: a 256-bit token, with only its SHA-256 digest stored, as for reset and invitation links;
   3. renders the email with the link in both parts' footers, and adds `List-Unsubscribe: <link>`.
2. **The page,** `/auth/email-blocks/<token>`, checks the link (`POST /api/email-blocks/check`) and shows one of four states:

   | State | The page says | Action |
   | --- | --- | --- |
   | Dead link (unknown or expired) | "This link has expired or isn't valid." | none |
   | The address has an account | "**ada@example.com** has a lewcontext account. To stop email from lewcontext, delete your account first: sign in, then Account & security." | Sign in (a link) |
   | Not blocked | "Block **ada@example.com**? You won't receive any more email from lewcontext, and this address can't be used to sign up. You can undo this from this link for a year." | **Block this address** |
   | Blocked | "**ada@example.com** is blocked. You won't receive email from lewcontext, and it can't be used to sign up. Keep this email: its link lets you unblock this address for a year." | **Unblock** |

   The page works signed in or out (no `guestLoader`), like the reset and invitation links. It uses `AuthLayout` with one full-width primary button, per [forms-and-actions.md](../../docs/ui/forms-and-actions.md). Blocking or unblocking switches it to the other state, with a status message.
3. **Blocking** (`POST /api/email-blocks {token}`) and **unblocking** (`DELETE /api/email-blocks {token}`) work only with a live link.

### What a block does

- **No email:** every `Send` to a blocked address is skipped, whatever the template. Security mail goes only to accounts, and a blocked address can't have one, so the rule "security mail is never limited" still holds.
- **No invitations, silently:** `POST …/invitations` to a blocked address answers 201 as for any address, and the invitation is listed as usual, `sentAt` included. It is never emailed (`Send` refuses it), so nobody has its link, and it expires after 7 days. The inviter isn't told (decision 2). Admins see the truth: the Mail page counts it as asked for, not sent.
- **No sign-up:**
  - **Verification on** (the default): `POST /api/auth/register` answers 202 as for every address and saves the pending row as usual; the `mail.registration` listener's `Send` refuses the code, so none arrives (decision 3). Sign-up keeps answering the same, and doing the same work, for every address. The code page gains a line everyone sees: "Didn't get a code? Check your spam folder. If you blocked this address, unblock it from the link in our email first." **Verify** checks the block list again under the lock (below), so an address blocked after its code was sent can't become an account: 422 with no field, "This address has been blocked from lewcontext. To use it, unblock it from the link in an email we sent.", and that email's pending rows are deleted. Only someone holding a code from that inbox can reach this answer.
  - **Verification off:** a blocked address gets the 409 "Unable to create this account…" a taken email already gets there (decision 15). Sign-up with verification off already says whether an email is taken, so a blocked address looking taken adds nothing.
- **Invitations already pending** to the address stay as they are (decision 9). Their emails were already sent, they can't be accepted without an account, and they expire within 7 days.

### Data

Migrations `018_create_email_block_links` and `019_create_email_blocks`, each with a factory (`TestEveryTableHasAFactory`):

| Table | Columns | Why |
| --- | --- | --- |
| `email_block_links` | `id`, `token_hash bytea UNIQUE`, `email text`, `expires_at`, `created_at` | One row per blockable email sent. It holds the address, because the page shows it and checks it against `users`. The server's 15-minute cleanup (`Store.Cleanup`) deletes rows past `expires_at`, one year after sending (decision 4) |
| `email_blocks` | `id`, `email text UNIQUE`, `created_at` | The block list: the normalised address (trimmed, lower-case, as sign-up stores it), kept for as long as it's blocked. Keeping an address in order to honour a request not to be contacted is the case data-protection law expects (a suppression list), and it lets an operator list and unblock addresses (below). Not a log table: never pruned |

Neither table needs a public ID: links are found by token digest, and blocks by address. Nothing else references them, and deleting an account doesn't touch them, since a blocked address can't have one.

### Racing a sign-up

Blocking needs "no account uses this address", and creating an account needs "this address isn't blocked". Two transactions running at once under read committed could each pass their check before the other commits, and both would succeed. So each takes a **transaction-scoped advisory lock keyed by the email** first (`pg_advisory_xact_lock`, with a key taken from the first 8 bytes of the address's SHA-256), through a small helper in `internal/database`, where advisory locks already live (AGENTS.md allows raw SQL only there for them). They then check, insert and commit. The second one waits, then sees the first's row.

The lock goes where a `users` row is created: `BlockEmail`, the sign-up verify transaction (verification on) and `CreateUser` (verification off). Register with verification on creates no account, so it needs neither the lock nor a check. Invitations need neither: `Send` checks every email.

### Endpoints

| Method | Route | Requires | Success |
| --- | --- | --- | --- |
| POST | `/api/email-blocks/check` | Origin, JSON, `auth:per-ip` | 200 `{email, registered, blocked}` for a live `{token}`; 422 with `field: "token"` otherwise. Works signed out |
| POST | `/api/email-blocks` | Origin, JSON, `auth:per-ip` | 204; the address is blocked (blocking an address that's already blocked is also 204). Body `{token}`. 409 "This address has an account. Delete your account first." when one uses it; 422 with `field: "token"` for a dead link |
| DELETE | `/api/email-blocks` | Origin, JSON, `auth:per-ip` | 204; the address is unblocked (unblocking one that isn't blocked is also 204). Body `{token}`. 422 with `field: "token"` for a dead link |

- **Named for the resource, not the page** ([naming.md](../../docs/naming.md)). The controller is `account.EmailBlocks` (`endpoints/account/email_blocks.go`) with `Check`, `Store` and `Destroy`; `Check` is a named action on the link, like `Invitations.Check`. It lives in the `account` area rather than a new area for three routes, the way `/api/invitations/*` lives in `workspace`; naming.md notes the exception. The token is only ever in the body, never in an API URL. The page path carries it, as the reset and invitation pages do (AGENTS.md's single-use-link exception).
- **Store methods:** `IssueEmailBlockLink`, `CheckEmailBlockLink`, `BlockEmail`, `UnblockEmail` (by link, or by address for the command line), `IndexEmailBlocks`, and `CheckEmailBlocked` (the yes/no question).
- **Client:** `api.emailBlocks().check(token)`, `.store(token)` and `.destroy(token)`, with `paths.emailBlocks()`. Page route `routes.emailBlocks.show(token)`; the page file is `pages/auth/email-blocks/[token].tsx`, exporting `EmailBlockShow`.
- **Rate limits:** `auth:per-ip`, counted in the controller as `password-reset/check` does. Tokens are unguessable (256 bits), so the limit only bounds load. No new limit.

### The mailer (`internal/mail`)

```go
// Each template's subject, and whether its emails carry a block link.
var templates = map[Template]templateInfo{
	ForgotPassword:      {subject: "Reset your password"},
	WorkspaceInvitation: {subject: "You’ve been invited to join a workspace", blockable: true},
	…
}

// Blocks is what Send needs from the store.
type Blocks interface {
	CheckEmailBlocked(ctx context.Context, email string) (bool, error)
	IssueEmailBlockLink(ctx context.Context, email string) (token string, err error)
}

mail.New(mail.Options{Driver: driver, App: c.AppName, Origin: c.Origin, Blocks: repository})
```

- **Every** `Send` checks `CheckEmailBlocked`; only blockable templates issue a link.
- **Footer:** both layouts end with an optional line, filled only for blockable email. HTML: "Don't want email from lewcontext? <a>Block this address</a>." Plain text: "Don't want email from lewcontext? Block this address: <link>". The layouts read the link with `{{ blockURL }}`; `Send` clones the parsed templates and sets that function per message (`Clone` keeps the parsed originals untouched).
- **`Message.Unsubscribe`** carries the link, and `SMTPDriver` sends it as `List-Unsubscribe: <https://…/auth/email-blocks/…>` (decision 5). Gmail, Apple Mail and others then show an "Unsubscribe" link by the sender, which opens our page. It's the URL form only, without `List-Unsubscribe-Post`: one-click unsubscribe would block at once, with no confirmation and no account check. The header holds our own URL, not user data.
- **The listeners** treat `ErrBlocked` as done: `mail.workspace_invitation` records no `.sent`, `mail.registration` records no `auth.registration.sent`. Nothing else is recorded, so the audit trail shows a request without its `.sent`.
- **No confirmation email.** The page confirms a block and says to keep the email, whose link unblocks for a year; mailing someone who just asked for no mail would ignore the request, and would need an exception to `Send`'s check.
- **Making another email blockable** is then one word, `blockable: true`, plus a test.

### Command line (`cmd/admin`)

An operator can list and unblock addresses without a link, for example after the year a link lasts, or when someone writes in. Two subcommands join the existing `cmd/admin` (`grant`, `revoke`, `list`, `reset-mfa`, `app-key`, `route-key`), in its style:

```sh
admin blocks                                   # every blocked address, oldest first, with when
admin unblock -email ada@example.com           # preview: "would unblock ada@example.com (blocked 2026-10-31)"
admin unblock -email ada@example.com -confirm  # unblocks; records email.unblocked
```

- `make admin cmd=blocks` and `make admin cmd=unblock email=… CONFIRM=1` locally; `docker run … lewcontext admin unblock …` in production, where it's allowed (it removes a block; it can't add one or read anything but the list).
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
| Probing which addresses are blocked | Nothing anyone can type says so (decision 14). Invitations and sign-up with verification on answer, store and list exactly as for any address; only the email doesn't arrive. "Blocked" would say the inbox is read by a person who has no account, and tell a harasser their invitation was seen. Verification off answers a blocked address as a taken one, which that mode already reveals. Only holders of a link or code from the inbox see the word |
| A block and a sign-up racing | A transaction-scoped advisory lock keyed by the email serialises a block with account creation (verify, or register with verification off) |
| An invitation or code queued before a block still emailed | `Send` checks the block list for every email, so even mail queued before the block isn't sent |
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
    - an invitation to the address answers 201, is listed with `sentAt` like any other, and isn't emailed (no `.sent`);
    - sign-up with it (verification on) answers exactly as for a new address (status, body, cookie), and no code is emailed; verify with a code issued before the block answers 422, creates nothing and deletes the pending rows;
    - sign-up with verification off answers the taken-email 409 and creates nothing;
    - a pending invitation's email isn't sent.
  - **Unblocking:** it lets both through again, and records `email.unblocked` (`by: "link"`). Unblocking by address (the command line's store call) does the same with `by: "cli"`; an address that isn't blocked changes nothing; `IndexEmailBlocks` lists oldest first.
  - **Registered addresses:** `check` says `registered: true`, and `store` answers 409 and blocks nothing.
  - **Dead links:** an expired or unknown token answers 422 for all three endpoints, and the cleanup deletes expired links.
  - **The race:** block and account creation for the same address (verify, and register with verification off), run at once five times; exactly one wins each time (this fails without the lock).
  - **Limits and plumbing:** `auth:per-ip` applies; the Origin and JSON checks apply; the token never appears in an event.
- **Go unit (`smtp`):** the `List-Unsubscribe` header's form.
- **Vitest (`pages/auth/email-blocks.test.tsx`):**
  - the four states;
  - Block and Unblock with pending states, a failure keeping the page, and switching states afterwards;
  - the token only in request bodies;
  - the address rendered as text;
  - the sign-up code page shows the unblock hint.
- **Playwright:**
  - invite an unregistered address, open the block link from Mailpit, block it;
  - inviting it again succeeds but nothing reaches Mailpit; signing up with it reaches the code page but no code arrives;
  - unblock from the same link, and the next invitation arrives;
  - a registered address's link says to delete the account first;

## Docs

- `docs/api/mail.md`: blockable templates, the footer and header, blocked addresses skipped, `mail.New`'s options, and the rule's third column.
- `docs/api/endpoints.md`: the three routes, and an Email blocks section.
- `docs/api/auth.md`: blocking, its silent effects on sign-up and invitations, verify's check, the lock, and the account check.
- `docs/api/admin.md`: `admin blocks` / `admin unblock`, and the Mail page counting a blocked email as asked for, not sent.
- `docs/api/database.md`: both tables, and the cleanup.
- `docs/api/events.md`: the two events.
- `docs/api/rate-limits.md`: `auth:per-ip` on the new routes.
- `docs/naming.md`: `account.EmailBlocks` outside `/api/auth/*`.
- `docs/ui/pages.md`: `/auth/email-blocks/<token>`, and the code page's hint.
- `docs/ui/api.md` if needed.
- `docs/api/testing.md` and `docs/ui/testing.md`.
- `docs/deployment.md`: the Email section mentions blocks.
- `docs/testing.md` and `docs/deployment.md`: `make admin cmd=blocks` / `cmd=unblock`.
- AGENTS.md: rule 5's `cmd/admin` gains unblocking; the security invariants gain "a blocked address gets no email and can't get an account; only addresses without an account can be blocked, and only by their own link; nothing anyone types reveals a block; the command line can only unblock".

## Decisions

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Which emails carry the link | Invitations only for now, through a `blockable` flag on the template, so another email opts in with one word. **Confirmed.** |
| 2 | An invitation to a blocked address | Accepted as usual (201, listed, `sentAt` set as for any invitation) and never emailed; the inviter isn't told. Revised from an honest 422, which told a harasser their invitation had been seen. **Confirmed 2026-11-01.** |
| 3 | Sign-up with a blocked address (verification on) | The usual 202 and code page; no code is sent, and the page's hint mentions unblocking. Verify refuses under the lock. Revised from an honest 422. **Confirmed 2026-11-01.** |
| 4 | How long a link works | A year after the email was sent; then the cleanup deletes it. **Confirmed.** |
| 5 | `List-Unsubscribe` | Yes, the URL form only (no one-click `List-Unsubscribe-Post`), pointing at the page. **Confirmed.** |
| 6 | What the events record | The address and IP, as other events with an email do; pruned after 90 days. **Confirmed.** |
| 7 | Showing the address and "has an account" on the page | Yes. Only a holder of a link sent to that inbox sees it; a forwarded link is the accepted exception. **Confirmed.** |
| 8 | Which emails a block stops | All of them (every `Send` checks), not only blockable ones. Since a blocked address can't have an account, that never includes security mail. **Confirmed.** |
| 9 | Invitations pending when an address is blocked | Left alone: already emailed, can't be accepted without an account, and expire within 7 days. **Confirmed.** |
| 10 | Storing blocks | The address itself, not a digest: what a suppression list needs, and what listing and unblocking by address need. Digests would only have hidden guessable addresses from someone with database access. **Confirmed.** |
| 11 | Serialising a block and a sign-up | A transaction-scoped advisory lock keyed by the email, via an `internal/database` helper, where accounts are created. **Confirmed.** |
| 12 | Wording | As in the tables above: "Block this address" and "Unblock" on the page; "Don't want email from lewcontext? Block this address." in emails; the 409, verify's 422 and the code page's hint as written. **Confirmed.** |
| 13 | The command line | `blocks` and `unblock -email … [-confirm]` join the existing `cmd/admin` (`make admin cmd=…`). No `admin block`. Allowed in production. **Confirmed 2026-11-01.** |
| 14 | Who learns an address is blocked | Only holders of a link or code sent to that inbox. Every other answer is the same as for any address. **Confirmed 2026-11-01.** |
| 15 | Sign-up with a blocked address (verification off) | Refused with the taken-email 409 that mode already gives. Allowing it would leave an account whose mail is blocked (no password reset), or let anyone lift someone's block by typing their address, since that mode doesn't prove ownership. **Confirmed 2026-11-01.** |
| 16 | Where the controller lives | `account.EmailBlocks`, keeping `/api/email-blocks`; noted in naming.md. **Confirmed 2026-11-01.** |

## Built so far

Decisions 1–16 shipped on 2026-11-01. Docs updated: [mail.md](../../docs/api/mail.md#blocked-addresses), [auth.md](../../docs/api/auth.md#blocking-an-email-address), [endpoints.md](../../docs/api/endpoints.md#email-blocks), [events.md](../../docs/api/events.md), [database.md](../../docs/api/database.md), [rate-limits.md](../../docs/api/rate-limits.md), [admin.md](../../docs/api/admin.md) (the command line, the Mail page), [seeding.md](../../docs/api/seeding.md), [api/testing.md](../../docs/api/testing.md), [api/README.md](../../docs/api/README.md), [naming.md](../../docs/naming.md), [ui/pages.md](../../docs/ui/pages.md), [ui/api.md](../../docs/ui/api.md), [ui/testing.md](../../docs/ui/testing.md), [deployment.md](../../docs/deployment.md), [testing.md](../../docs/testing.md), AGENTS.md. Where the build differs from the design above:

- **Verify for an address blocked after its code was sent** answers **401** through the existing "start again" path ("This address has been blocked. To use it, unblock it from the link in an email we sent, then sign up again."), not a 422: the sign-up is over and its rows are deleted, and the code page already handles 401 with the API's reason.
- **The code page's line** is "If you blocked this address, no code can reach it: unblock it from the link in our email first." The shared code step already says to check the spam folder.
- **The mailer** keeps `subjects` and adds a `blockable` set beside it, rather than one `templates` map. `Blocks.IssueEmailBlockLink(ctx, email, hash)` stores a digest the mailer made (`auth.Token` in `Send`, as listeners make the other tokens), and only after both parts have rendered. `mail.New` refuses a mailer without `Blocks` or `Origin`. **Every** mail listener treats `ErrBlocked` as done (`done(err)`), not only the invitation and sign-up ones.
- **Events** carry `email` (and `by`) only: the IP is the `events.ip` column, as for every event (no payload has an `ip` field, `TestPayloadsHoldNothingSensitive`). `email` is a new event area in the name test and naming.md.
- **`email_block_links.token_hash`** is `text`, like the other tables' digests, not `bytea`.
- **Extra store methods:** `FindEmailBlock` (the command line's preview) and `UnblockEmailAddress` (by address, `by: "cli"`), beside `UnblockEmail` (by link).
- **The page's status message** is a success `Notice` ("Address blocked." / "Address unblocked."): sign-in pages have no `Toaster`.

## Not included

- **One-click unsubscribe** (RFC 8058 `List-Unsubscribe-Post`), which blocks without a page or confirmation.
- **A block link on password reset, sign-up codes or security mail.**
- **Telling an inviter an address is blocked** (decision 2).
- **A way for registered users to refuse invitations** ("Don't accept invitations" in settings). `invitation:per-email` caps them meanwhile.
- **Blocks in the admin dashboard.** `admin blocks` lists them; a page can come with the admin plans.
- **Removing pending invitations on block** (decision 9).
- **Workspace notifications and their settings** (their own plan; the opt-in rule is already in mail.md).
