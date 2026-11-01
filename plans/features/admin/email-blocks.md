# Admin: the email block list, and unblocking

Status: **draft 2026-11-01**, decisions to confirm. Plan A9 of the [admin plans](README.md). Split out of [email blocks](../implemented/email-blocks.md) (its decisions 17–21), whose links, page, mailer and command line shipped on 2026-11-01. Needs [admin access](../implemented/admin-access.md) and [events-and-mail](../implemented/admin/events-and-mail.md); brings forward the step-up, reasons, limit and dialog from [actions](actions.md) (A6); waits for [access hardening](access-hardening.md) (A2) only for the `operator` role (decision 3). Docs it will change: [admin.md](../../../docs/api/admin.md), [events.md](../../../docs/api/events.md), [rate-limits.md](../../../docs/api/rate-limits.md), [database.md](../../../docs/api/database.md), [ui/forms-and-actions.md](../../../docs/ui/forms-and-actions.md), [api/testing.md](../../../docs/api/testing.md), [ui/testing.md](../../../docs/ui/testing.md), AGENTS.md.

## Why

An address is blocked by its owner, from the link in an invitation. Today only the command line can see the block list or lift a block (`admin blocks`, `admin unblock`), so only someone with server access can answer "why can't my friend sign up?" or act when someone writes in after their link's year has passed. Admins can already read every account and the audit trail, but not the block list.

**An Email blocks page** in the dashboard lists the blocked addresses, and each row can be unblocked (decision 1). It's the dashboard's first write, so it follows the rules [actions](actions.md) sets for an account-and-access action: a fresh code (step-up) and a written reason, recorded with both (decision 2). This plan builds the shared parts of A6 that unblocking needs; A6 then reuses them for its own actions.

## Design

### Flow

```
/admin/<key>/email-blocks        Email blocks: search, sort, a ⋯ menu per row
  ⋯ → Show activity              → Events ?email=<address>
  ⋯ → Unblock                    → AdminActionDialog (danger): reason, then
       DELETE /api/admin/email-blocks {email, reason}
         403 {stepUp: true}      → the dialog asks for a code → POST /api/admin/step-up {code} → retries
         204                     → unblocked; the list reloads
```

### Endpoints

| Method | Route | Requires | Success |
| --- | --- | --- | --- |
| GET | `/api/admin/email-blocks` | key, admin session | A page of blocks: `email`, `createdAt`, `linkExpiresAt` (the newest live link's expiry; null when none is live, so only an admin or operator can unblock). `q` (address, ignoring case, with the users list's `LIKE` escaping), `sort` `created` (default `-created`), `email`. Recorded as `admin.email_block.listed` (`admin`, `q`, `sort`, `page`) |
| POST | `/api/admin/step-up` | key, admin session, CSRF, Origin, JSON, `mfa:per-account` | 204; `admin_sessions.stepped_up_until` set 5 minutes ahead. Body `{code}`: an authenticator or recovery code, checked as at the door's second step and recorded as `auth.mfa.checked` with step `admin_step_up`. 422 `field: "code"` |
| DELETE | `/api/admin/email-blocks` | key, admin session, CSRF, Origin, JSON, step-up, `admin-action:per-user` | 204; the address is unblocked. Body `{email, reason}`: the address normalised as sign-up does, a reason of 10–500 characters (422 `field: "reason"`). 403 `{"error": "Enter a code to continue.", "stepUp": true}` outside the step-up window. 404 "This address isn't blocked." when it isn't (its owner may have unblocked it meanwhile), recording nothing |

- **The address travels in the body, never the path** (decision 5): an address in a URL lands in access logs and browser history. The user-facing `DELETE /api/email-blocks` takes its token in the body the same way. Blocks keep no public ID.
- **Controllers:** `admin.EmailBlocks` (`endpoints/admin/email_blocks.go`: `Index`, `Destroy`) and `admin.StepUp` (`step_up.go`: `Store`, the session's singleton, as `EmailVerification.Store` is the account's). Both are registered in `adminRoutes`, behind `RequireAdminKey` and `RequireAdminSession` (`TestAdminRoutesStayInTheAdminGroup`).
- **`RequireAdminStepUp`** wraps the unblock route, after the session and CSRF checks. It reads `stepped_up_until` from the admin session already loaded, so it costs no query. A6's other account-and-access actions use it unchanged.
- **Store methods:** `IndexAdminEmailBlocks` (paginated, as `IndexAdminUsers`), `UnblockEmailAddress` (today's by-address unblock, given the admin and the reason), and `StepUpAdminSession`.
- **Rate limits:** `admin-action:per-user` (60 an hour, in the database), new, as A6 defines it; the step-up shares `mfa:per-account` with every other code check, so a stolen admin session guesses no faster there than at the door. Reads count against `admin-read:per-user` as every admin read does.
- **Roles wait for [access hardening](access-hardening.md)** (decision 3). Until it adds `operator`, every admin may unblock. When it does, the route gains `requireAdminRole(operator)` like A6's actions.

### Data

Both edited in place while migrations are squashed:

- `015_create_admin_sessions` gains `stepped_up_until timestamptz` (null: never). A password change, reset or MFA reset already deletes admin sessions, step-up with them.
- `018_create_email_block_links` gains an index on `email`, for `linkExpiresAt`.

### What the action does

`UnblockEmailAddress`, in one transaction with its events: the block row is deleted, and two events are recorded with the admin as actor (A6's decision 6): `admin.email.unblocked` (`admin`, `email`, `reason`) and `email.unblocked` (`email`, `by: "admin"`; the IP in `events.ip`, as for every event). The address can again be invited and used to sign up.

There is **no Block in the dashboard** (decision 4). An address without an account only gets invitations and the sign-up codes it asked for, and every invitation carries its own block link, so someone who wants no email always has a link to use. An admin block would let a stolen admin session stop someone signing up, and would block on an admin's word rather than the inbox owner's.

### Web app

- **Page:** `/admin/:key/email-blocks`, `pages/admin/[key]/email-blocks.tsx`, exporting `AdminEmailBlockIndex`, inside the admin layout; route `adminRoutes.emailBlocks.index(key)` (`lib/admin-routes.ts`). "Email blocks" joins the admin sidebar after Mail (`NoSymbolIcon`).
- **Header:** the heading, then one line: "Addresses whose owners asked for no email from lewcontext, from a link in one of our emails. A blocked address gets no email and can't be used to sign up."
- **List:** `PaginatedTable` with `SearchField` and sortable headers (`admin-list.tsx`). Columns: Address, Blocked (date), Owner's link ("Works until 31 Oct 2027", or "Expired" in zinc when `linkExpiresAt` is null), and a ⋯ `Dropdown` in the last column ([forms-and-actions.md](../../../docs/ui/forms-and-actions.md): table rows). Empty: "No blocked addresses." Search with no match: "No addresses match "…"." Loading, refresh failure with Retry, and the 401 redirect as other admin lists.
- **Row menu:** Show activity (Events `?email=`, where `email.blocked` and `email.unblocked` show who blocked, from which IP, and every unblock's reason), then **Unblock** as `DropdownItem variant="danger"`. An admin can't undo an unblock, since there is no admin block; only the owner's next link can block again.
- **`AdminActionDialog`** (`components/app/admin/admin-action-dialog.tsx`), as A6 describes it: a `ConfirmationDialog` with a required Reason `Textarea` (10–500 characters, counted) and, when the API answers `stepUp`, a code field (`OtpInput`, as the sign-in's `CodeStep` uses) that calls `POST /api/admin/step-up` and retries the action. Danger variant here. Title "Unblock ada@example.com?"; body "This address will get email from lewcontext again and can be used to sign up. Only its owner can block it again, from the link in a new email."; confirm **Unblock**. Pending, the API's error kept in the dialog, then a toast "ada@example.com unblocked." and the list reloads (one more recorded read).
- **Client** (`lib/api/admin.ts`, the admin modules [separate build](../implemented/admin/separate-build.md)'s rule keeps admin code in): typed paths `adminPaths.emailBlocks.index(query)`, `api.emailBlocks().index(query)`, `.destroy({email, reason})`, and `api.stepUp(code)`. Types `AdminEmailBlock`, `AdminEmailBlockIndexQuery`.

## Events

| Name | Payload | Recorded by |
| --- | --- | --- |
| `admin.email_block.listed` | `admin`, `q`, `sort`, `page` | Each read of the Email blocks page, with the admin as actor |
| `admin.email.unblocked` | `admin`, `email`, `reason` | The dashboard's unblock, with the admin as actor |
| `email.unblocked` | gains `by: "admin"` (today `link` or `cli`) | The same unblock, with the admin as actor |

Added to `events.Catalogue` and the catalogues in [events.md](../../../docs/api/events.md) and [admin.md](../../../docs/api/admin.md#audit-trail). `admin.email_block.listed` is an admin read, so the Events page hides it unless Admin reads is on.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| A stolen admin session unblocking someone, so that a harasser's invitations reach them | A fresh code within 5 minutes (step-up, sharing `mfa:per-account`), a required reason, CSRF and the Strict admin cookie, `admin-action:per-user`, and `admin.email.unblocked` naming the admin and reason. The next invitation carries a new link, so the owner can block again at once |
| Admins reading the block list (people who asked not to be contacted) | Only behind the key, an admin session and MFA; every read recorded as `admin.email_block.listed`, searches included. Rows hold the address and two dates, never a token or digest (`decodeAdmin` walks the response) |
| An admin blocking someone to stop them signing up | There is no block in the dashboard (decision 4) |
| An address in an admin URL | The unblock takes it in the body; only the Events link (`?email=`) puts it in a page URL, as Events already does, and admin pages send `Referrer-Policy: no-referrer` |
| Every admin can unblock until roles exist | Accepted until [access hardening](access-hardening.md) adds `operator` (decision 3); admins are few, granted only from the command line, and every unblock is recorded with its reason |

## Tests

- **Go integration:**
  - **The list:** pages, `q` (with `%` and `_` matched literally), both sorts, `linkExpiresAt` (a live link's expiry, null once the cleanup's cutoff passes), one `admin.email_block.listed` per read; `decodeAdmin` finds no token or digest.
  - **Step-up:** a right code opens the window and records `auth.mfa.checked` (`admin_step_up`); a wrong one answers 422 and counts against `mfa:per-account`; a recovery code works once; the window ends after 5 minutes; a new admin session starts without it.
  - **Unblocking:** without step-up 403 with `stepUp`; with it and a reason, 204, the block gone, `admin.email.unblocked` and `email.unblocked` (`by: "admin"`, the admin as actor) recorded in the same transaction, and the address can be invited and signed up with again; no reason or a short one 422; an address that isn't blocked 404 with nothing recorded; the address is normalised; no CSRF token 403; `admin-action:per-user` answers 429.
  - **The group:** `TestAdminRoutesStayInTheAdminGroup` covers the three routes (a wrong key is the unknown-route 404).
- **Vitest (`pages/admin/email-blocks.test.tsx`):**
  - the list's states (loading, empty, no search match, a refresh failure keeping rows with Retry);
  - Unblock: the reason is required, a `stepUp` answer shows the code field and retries after it, pending disables the dialog, an error stays in it, success shows the toast and reloads;
  - the address only in request bodies, never an API URL.
- **Playwright:** an admin finds a blocked address on Email blocks, unblocks it with a reason and a code, and the next invitation arrives (extending `email-blocks.spec.ts`).

## Docs

- `docs/api/admin.md`: the Email blocks page, its three routes, step-up, the audit trail's new events, and the layers: "Read-only" becomes "read-only apart from unblocking an address, behind a fresh code and a reason". "There are no row menus: there are no actions" changes, and Not built drops what this plan builds from "write actions".
- `docs/api/events.md`: the two `admin.*` events and `by: "admin"`.
- `docs/api/rate-limits.md`: `admin-action:per-user`, and step-up on `mfa:per-account`.
- `docs/api/database.md`: `stepped_up_until`, and the `email_block_links` index.
- `docs/ui/forms-and-actions.md`: `AdminActionDialog` (a reason, then a code when asked).
- `docs/api/testing.md` and `docs/ui/testing.md`.
- AGENTS.md: the admin invariant "It stays read-only until a plan adds actions" becomes "Its one action, unblocking an address, needs a fresh code and a reason; further actions come with the admin actions plan"; the email-block invariant "the command line can only unblock" gains the dashboard.
- The admin plans: [actions.md](actions.md) notes that step-up, reasons, `admin-action:per-user` and `AdminActionDialog` ship here; the [README](README.md)'s sidebar shows Email blocks.

## Decisions (to confirm)

Numbered 17–21 in the [email blocks](../implemented/email-blocks.md) plan before the split.

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Blocks in the admin dashboard | An Email blocks page after Mail: the list with search, sort and whether the owner's link still works, each read recorded; Unblock and Show activity in a ⋯ menu. A page of its own rather than the Blocked addresses tab on Mail that [events-and-mail](../implemented/admin/events-and-mail.md) suggested, since it has its own endpoint and action |
| 2 | How the dashboard's unblock is guarded | As an A6 account-and-access action: a 5-minute step-up window opened with a code, a required reason (10–500 characters), `admin-action:per-user`, and `admin.email.unblocked` beside `email.unblocked`, in one transaction. This plan builds A6's step-up, reason, limit and dialog for A6 to reuse. The alternative, a list now and unblocking only when A6 ships, keeps the dashboard read-only for longer |
| 3 | Roles before access hardening | Every admin may unblock until A2 adds `operator`; then the route needs it. The rule "a write needs the `operator` role" ([README](README.md), rule 5) is waived for this one action until then. Waiting for A2 instead would delay the action until A2 ships |
| 4 | Blocking from the dashboard | No, as for the command line: every unwanted email an unregistered address can get carries its own link, and an admin block would let a stolen session stop someone signing up |
| 5 | Identifying a block in the API | By its address in the request body, as the command line does, not a public ID in the path: blocks have no public ID, and an address in a path lands in access logs |

## Not included

- **Blocking from the dashboard** (decision 4).
- **A6's other actions**; the dashboard gains only Unblock.
- **Admin roles.** Every admin may unblock until [access hardening](access-hardening.md) (decision 3).
- **Blocked emails on the Mail page.** A refused `Send` records nothing, so Mail shows them only as asked for without a `.sent`.
