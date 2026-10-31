# Workspace invitations

Status: **implemented 2026-10-31** (see [How the build differs](#how-the-build-differs)). Docs: [auth.md](../../docs/api/auth.md#invitations), [endpoints.md](../../docs/api/endpoints.md), [events.md](../../docs/api/events.md), [mail.md](../../docs/api/mail.md), [database.md](../../docs/api/database.md), [rate-limits.md](../../docs/api/rate-limits.md), [seeding.md](../../docs/api/seeding.md), [api/testing.md](../../docs/api/testing.md), [ui/pages.md](../../docs/ui/pages.md), [ui/design.md](../../docs/ui/design.md), [ui/api.md](../../docs/ui/api.md), [ui/testing.md](../../docs/ui/testing.md), [ui/README.md](../../docs/ui/README.md), [deployment.md](../../docs/deployment.md) and AGENTS.md. Needs [workspace permissions](workspace-permissions.md) and [workspace members](workspace-members.md), whose page shows pending invitations. Reuses the token, listener and mail design from [forgot password](forgot-password.md).

## Why

A workspace can only ever have one member, its creator. Owners and admins need to invite people by email. The invitee follows a link, signs in or creates an account, and joins with the role they were invited as.

Joining must be deliberate: it happens only by following the link. Registering or signing in with an invited email by any other route never adds the account to a workspace. There is no "on sign-up, accept every invitation for this email". There is also no email verification, so that check would hand any workspace's invitations to whoever registers the address first.

## Flow

```
/app/<wsp>/settings/members  [Invite]  → Dialog: email, role (assignableRoles)
  POST /api/workspaces/{ws}/invitations {email, role}        workspace.invitation.store
    limits; role assignable; not a member; no live invitation; < 50 pending
    one transaction: insert invitation (no token yet),
                     record workspace.invitation.created     ← queues the listener
    → 201 invitation  (shown at once as "Sending…" in Pending invitations)

worker → listener mail.workspace_invitation (≈1 s later)
    invitation gone (revoked/accepted) or expired?  → drop
    new token; store its digest and sent_at on the row (replaces any earlier token)
    mailer.Send(ctx, email, mail.WorkspaceInvitation, {Workspace, Inviter, InviterEmail, Role, URL})
                                     URL = APP_ORIGIN + /auth/invitations/<token>
    record workspace.invitation.sent

/auth/invitations/<token>                                     (works signed in or out)
  POST /api/invitations/check {token}
    → {workspace: {name}, invitedBy: {name, email}, email, role, expiresAt}  | 422 expired
  signed out             → callout "Acme has invited you to join."  (invited by Ada, as Member)
                           [Sign in to accept]   → /auth/login?next=/auth/invitations/<token>
                           [Create an account]   → /auth/register?next=…  (email prefilled)
                           login and register show the same callout above their form while
                           ?next= is an invitation link (they call check themselves)
                           after signing in or registering → back here, accepted at once (decision 6)
  signed in, same email  → [Join Acme]   (or accepted at once when arriving from sign-in)
  signed in, other email → "This invitation is for ada@…; you're signed in as bob@…" [Sign out and switch]
  POST /api/invitations/accept {token}                         session, CSRF
    one transaction: lock workspace; lock invitation by digest (unexpired);
                     email must equal the session user's; add membership with its role
                     (already a member: keep the existing role); delete the invitation;
                     record workspace.invitation.accepted
    → 200 workspace → /app/<wsp>/dashboard, toast "You joined Acme."

/app/<wsp>/settings/members  Pending invitations: [Revoke] → confirm
  DELETE /api/workspaces/{ws}/invitations/{inv}                workspace.invitation.destroy
    delete row, record workspace.invitation.revoked → 204    (an emailed link then stops working)
```

## API

| Endpoint | Requires | Success | Failure |
| --- | --- | --- | --- |
| `GET /api/workspaces/{ws}/invitations` | `workspace.invitation.index` | `{data: [invitation…]}`, newest first, unpaginated (at most 50) | 403 |
| `POST /api/workspaces/{ws}/invitations` | `workspace.invitation.store`, CSRF, Origin, limits | 201 invitation | 403 role not assignable · 422 `field: email` (malformed, already a member, already invited) or `role` · 422 50 pending · 429 · 503 limit for everyone or `MAIL_DRIVER=off` |
| `DELETE /api/workspaces/{ws}/invitations/{invitation}` | `workspace.invitation.destroy`, CSRF, Origin | 204 | 404 |
| `POST /api/invitations/check` | Origin, JSON, `auth:per-ip` | 200 (above) | 422 `field: token` "This invitation has expired or was revoked." |
| `POST /api/invitations/accept` | Session, CSRF, Origin | 200 workspace with `role`, `permissions` | 422 `field: token` (as above) · 403 "This invitation was sent to a different email address." |

```json
{ "id": "inv_8kQ2mZ1xT4bLwN", "email": "ada@example.com", "role": "member",
  "invitedBy": { "id": "usr_…", "name": "Harrison" },  // null if that account is gone
  "sentAt": "2026-10-31T12:00:01Z",                      // null while sending
  "createdAt": "2026-10-31T12:00:00Z", "expiresAt": "2026-11-07T12:00:00Z" }
```

- Tokens travel in JSON bodies only, never in API URLs. The shape is checked before any query (43 base64url characters, `auth.Token()`), and lookup is by SHA-256 digest, as with sessions and resets.
- `accept` and `check` are named actions (CRUD plan): the record is identified by the token in the body, not a path ID.
- **Email** is trimmed and lowercased like sign-up (`validEmail`). `role` must parse and be in the actor's `AssignableRoles()`, otherwise 403. Owner invitations are not allowed (decision 4), so admins can invite member or viewer, and owners can invite admin, member or viewer.
- **An existing expired invitation** for the same email is replaced. A live one is a 422 ("Already invited. Revoke it to send a new one.").
- **The response is the same whether or not the email has an account.** The email and the page offer both sign-in and sign-up, so inviting reveals nothing about which addresses are registered.

### Table: `workspace_invitations` (new create migration `010`)

```sql
CREATE TABLE workspace_invitations (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 public_id text NOT NULL UNIQUE DEFAULT public_id('inv') CHECK (public_id ~ '^inv_[0-9A-Za-z]{14}$'),
 workspace_id bigint NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 email text NOT NULL CHECK (email = lower(email) AND length(email) BETWEEN 3 AND 254),
 role text NOT NULL CHECK (role IN ('admin','member','viewer')),
 invited_by bigint REFERENCES users(id) ON DELETE SET NULL,
 token_hash text UNIQUE,              -- null until the listener sends the email
 sent_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 UNIQUE (workspace_id, email)
);
CREATE INDEX workspace_invitations_expires_at_idx ON workspace_invitations(expires_at);
```

- `inv_` joins the public ID prefixes (`docs/api/database.md`, AGENTS.md). A factory goes in `internal/factory` (`TestEveryTableHasAFactory`).
- Accepted and revoked invitations are deleted. The events keep the history. `Store.Cleanup` deletes invitations that expired more than 30 days ago. Until then the list shows them as "Expired", so whoever sent one can see why it wasn't used.

### Store

- `CreateInvitation(ctx, workspaceID, actorID, email, role, expires) (Invitation, error)`: one transaction. It checks membership by email, the live invitation and the 50-pending cap, replaces an expired row, inserts, and records `workspace.invitation.created`. Typed errors (`ErrAlreadyMember`, `ErrAlreadyInvited`, `ErrTooManyInvitations`) map to the 422s.
- `IssueInvitationToken(ctx, publicID, digest) (InvitationMail, error)`: sets `token_hash` and `sent_at` with `WHERE public_id = ? AND expires_at > now()`, and returns what the email needs (workspace name, inviter name and email). It returns `sql.ErrNoRows` when the invitation is gone or expired, and the listener then treats it as done.
- `CheckInvitation(ctx, digest)` and `AcceptInvitation(ctx, digest, user) (Workspace, error)`. Accept locks the workspace row first, the same lock the members plan uses, so a concurrent removal or role change is serialised. It then locks the invitation `FOR UPDATE`, compares emails, inserts the membership `ON CONFLICT (workspace_id, user_id) DO NOTHING`, deletes the invitation, and records the event. A second concurrent accept waits on the row lock, then finds nothing (422).
- `RevokeInvitation(ctx, workspaceID, publicID)`: scoped by workspace, so another workspace's `inv_` gets 404.

### Events

| Event | Payload | Recorded by |
| --- | --- | --- |
| `workspace.invitation.created` | `invitation`, `workspace`, `email`, `role` | create handler, in its transaction (actor: inviter) |
| `workspace.invitation.sent` | `invitation`, `workspace`, `request` (the created event's `evt_` ID) | the `mail.workspace_invitation` listener, after `Send` |
| `workspace.invitation.revoked` | `invitation`, `workspace`, `email` | revoke handler (actor: revoker) |
| `workspace.invitation.accepted` | `invitation`, `workspace`, `role` | accept, in its transaction (actor: the new member) |

The token and its digest never appear in a payload (`TestPayloadsHoldNothingSensitive`). `email` is personal data about someone who may have no account. It is pruned with the rest after 90 days.

### Listener and mail

- `events.Listen(bus, "mail.workspace_invitation", events.WorkspaceInvitationCreated, …)` is registered in `cmd/server` only when `MAIL_DRIVER` isn't `off`. It drops events older than the invitation lifetime. A rerun after a crash issues a fresh token that replaces the earlier one and sends a new email: the older link stops working, so there is at most one live link per invitation. That is the reset link's design.
- Template `internal/mail/templates/workspace-invitation.html` and data struct `WorkspaceInvitationData{Workspace, Inviter, InviterEmail, Role, URL string}`. The **subject is fixed in Go**: "You've been invited to a workspace on lewcontext". It never includes the workspace or inviter name, because there is no user data in headers (mail rule 5).
- Body: "{{ .Inviter }} ({{ .InviterEmail }}) invited you to join {{ .Workspace }} on lewcontext as {{ .Role }}." Then the link, "It expires in 7 days.", and "If you weren't expecting this, you can ignore it." Showing the inviter's address lets the recipient judge a phishy workspace name.
- This is the second template, so the shared `layout.html` that the forgot-password plan deferred is added now, and `forgot-password.html` moves onto it.

## Rate limits

Every invitation sends an email to an address the inviter chooses. That makes invitations a spam channel for anyone who registers and creates a workspace. The limits follow the `<feature>:<counted by>` naming in `httpapi/limits.go`:

| Name | Limit | Refusal | Purpose |
| --- | --- | --- | --- |
| `invitation:per-user` | 20 / hour (the inviter) | 429 | one account, across all of its workspaces |
| `invitation:global` | challenge above 100, block above 200 / hour ([CAPTCHA challenges](captcha-challenges.md)) | 428 challenge, then 503 "Invitations are busy. Try again later." | caps total mail during an abuse wave |
| pending cap | 50 live invitations per workspace | 422 | bounds the unpaginated list and standing links |

Until the [CAPTCHA plan](captcha-challenges.md) is built, `invitation:global` has only its block level (503 above 200). Both are `rateLimit`s in `httpapi/limits.go`, applied with `s.limit` on the route, as the [rate-limits plan](rate-limits.md) (implemented) does for projects; accepting gets the signed-in write budget.

**Member quota.** This plan also adds the [rate-limits plan](rate-limits.md#quotas)'s quota of **100 members plus pending invitations per workspace** (`store.MaxWorkspaceMembers` in `store/quotas.go`), checked in `CreateInvitation` and `AcceptInvitation` under the workspace lock and returned as a `QuotaError` (422 "A workspace can have up to 100 members, counting pending invitations."). Accepting turns a pending invitation into a member, so it counts members only, excluding the invitation being accepted.

They run in this order after the permission check, so a refused request doesn't spend the global budget. `check` takes `auth:per-ip`, as reset check does. `accept` needs a session and is bounded by the token.

## Web app

### Members page (from the members plan)

- An **Invite** button on the right of the page header ([forms-and-actions.md](../../docs/ui/forms-and-actions.md)) when `can(ws, "workspace.invitation.store")` opens a `Dialog`: email `Input`, role `Listbox` (from `assignableRoles`), and `<Button loading>`. Field errors show inline. A 503 from `MAIL_DRIVER=off` shows "Invitations aren't available on this server." On success: toast "Invitation sent to ada@…", the dialog closes, and the pending list refreshes.
- A **Pending invitations** section above the members table, shown when `can(ws, "workspace.invitation.index")`:
  - columns: email, role badge, invited by, and "Sending…" / "Sent <date>" / an "Expired" badge;
  - **Revoke** (`workspace.invitation.destroy`) as "Revoke invitation" in the row's ⋯ menu, confirmed with a `ConfirmationDialog` ([forms-and-actions.md](../../docs/ui/forms-and-actions.md): row actions live in the menu);
  - while the list is non-empty and has a `Sending…` row, it polls every few seconds so "Sent" appears. Otherwise it refreshes on focus.
- `lib/api/invitations.ts`: `index`, `store`, `destroy` (workspace-scoped), and `check`, `accept` (account-level), with paths in `paths`.

### Invitation page `pages/auth/invitations/[token].tsx`

- Route `auth/invitations/:token`, with no `guestLoader`, so it works signed in and signed out, like the reset link. `lib/routes.ts`: `routes.invitations.show(token)`. The page reads the session if there is one (`useOptionalSession`).
- It checks the token shape locally, then calls `check`, with a skeleton meanwhile. The states are those in the flow. Expired or revoked: `<Notice variant="error">` "Ask whoever invited you to send a new one." with a link home.
- **Returning after sign-in:**
  - `returnRoutes` gains `^/auth/invitations/[A-Za-z0-9_-]{43}$`;
  - the buttons pass `?next=` and navigation state `{invitation: true}`, which marks the arrival as coming from our own page (for accepting straight away). The email is prefilled from `check` (see the callout below);
  - `useAuthSubmit` keeps that state when it navigates back.
- **The invitation callout:** `InvitationCallout` in `components/app/invitations/invitation-callout.tsx`. Three pages use it (the invitation page, login and register), so it goes in `components/` (rule 3). It is a `Notice` with a new `info` variant (`InformationCircleIcon`, `role="status"`, blue, built like the existing `error` and `success` variants):

  > **Acme** has invited you to join.
  > Ada (ada@example.com) invited you as a Member. Sign in or create an account to accept.

  - The first line is the user's wording, with the workspace name in bold. The second line shows who invited you, the role, and what to do; on the invitation page itself it ends "Join below to accept."
  - **Login and register show it** whenever `returnPath(next)` is an invitation link. `useInvitation(next)` (`lib/use-invitation.ts`) takes the token from the path and calls `POST /api/invitations/check`, so the callout survives a reload, the login ⇄ register links (which forward `next` but not state), and a link opened in a new tab.
  - It also prefills the form's email from the invitation when the field is empty. Navigation state isn't needed for that any more.
  - While the check is loading, there is a one-line skeleton of the callout's height, so the form doesn't jump. If the invitation has expired or was revoked, an `error` Notice says "This invitation has expired or was revoked. You can still sign in." The form works either way.
  - All values are rendered as React text, never HTML.
- **Register stops forcing onboarding for invitations:** `useAuthSubmit(register, …)` goes to `returnPath(next)` when it's an invitation path, and to onboarding otherwise. Without an invitation link, a new account still starts with no workspace and goes to onboarding (AGENTS rule 9 holds).
- **Accepting:** with `{invitation: true}` state (only our own buttons can set it), the page accepts as soon as the signed-in email matches, so signing in or registering lands in the workspace. Opened any other way (the email link while already signed in, or a crafted `?next=`), it shows **Join <workspace>**. On success it refreshes the workspace list and goes to the dashboard.
- **Wrong account:** "Sign out and switch" logs out, then opens login with `next` and the invited email.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| **Joining by registering an invited email without the link** (there is no email verification) | Accept needs the token, which only the inbox has. Sign-up and sign-in never look up invitations. |
| **A forwarded or leaked link used by someone else** | Accept also requires the signed-in email to equal the invitation's. A leak plus registering that address is the remaining gap (only when the invitee has no account yet), and it needs the token. It closes for good once email verification exists. |
| **The database, a backup or `events` leaks a usable link** | Only the digest is stored, set by the listener. The raw token exists in listener memory and the mailbox only. |
| **Token in the page URL and in `?next=`** | The same exposure the reset link already accepts: browser history only. `Referrer-Policy: strict-origin-when-cross-origin`, no third-party resources, no access log. Plus a 7-day lifetime, replacement on resend, and deletion on revoke or accept. AGENTS.md's "no auth tokens in query strings" is amended to name this single-use link explicitly (decision 7). |
| **Tricking someone into joining an attacker's workspace** (a crafted link or `?next=`) | Without our own navigation state, the page asks for an explicit Join, showing the workspace and inviter. Accept is a POST with CSRF. |
| **Spam or mail bombing through invitations** | `invitation:per-user` and `invitation:global` limits, the 50-pending cap, and one live invitation per email per workspace. The subject is fixed, and the inviter's email is in the body. |
| **Phishing content in workspace or inviter names** | `html/template` escaping (a script or `<h1>` name renders as text). Names never go in the subject. The inviter's email is shown so the recipient can judge. |
| **The callout on our own login page carries attacker text** (a workspace called "Your account is locked, call…") | It appears only for a live invitation token, which only the invited inbox has. The name is plain React text, at most 80 characters, bold inside fixed wording ("… has invited you to join."), never a link. The inviter's email is shown beside it. |
| **Escalation by invitation** (admin invites an admin or owner) | The role must be in `AssignableRoles()`, and the `CHECK` forbids owner invitations. |
| **An invitation outlives its inviter** (an admin is removed after inviting) | It stays valid: the workspace sent it. Owners and admins see it in Pending and can revoke it. |
| **Revoke races accept** | Both lock the invitation row. Whichever runs second finds nothing. |
| **Host-header poisoning of the link** | Built from `APP_ORIGIN` only. |
| **Enumeration** | Creating an invitation gives the same response whether or not the address has an account. "Already a member" only tells the inviter what the member list already shows them. |
| **Dev mailbox in production** | `MAIL_DRIVER=file` is refused in production. With `off`, the invite endpoint returns 503 and the listener isn't registered. |

## Tests

- **Go integration:**
  - **Create:**
    - an owner invites admin, member and viewer;
    - an admin is refused admin (403);
    - members and viewers are refused (403);
    - an existing member's email gets 422;
    - a live duplicate gets 422 and an expired one is replaced;
    - the 51st pending gets 422;
    - at 100 members plus pending, invite and accept get 422 (the member quota);
    - each limit, in order;
    - `MAIL_DRIVER=off` gets 503;
    - the event is in the same transaction;
    - the response is identical for a registered and an unregistered email.
  - **Listener:**
    - it issues a token and `sent_at`, and the mailbox link works;
    - a revoked invitation before the run sends nothing;
    - a rerun replaces the token, and the old link gets 422;
    - a stale event is dropped;
    - an XSS workspace or inviter name is escaped in the mail;
    - the subject is fixed.
  - **Check:** valid, malformed, expired, revoked, replaced.
  - **Accept:**
    - with the matching email it joins with the invited role and the invitation is gone;
    - another email gets 403 and the invitation is untouched;
    - an existing member keeps their role;
    - two concurrent accepts: one wins, the other gets 422;
    - accept racing revoke;
    - accept racing removal of the inviter;
    - no session gets 401;
    - bad CSRF or Origin gets 403.
  - **Deliberate only:** registering, and signing in, with an email that has a live invitation adds no membership.
  - **Revoke:** another workspace's `inv_` gets 404; the link stops working.
  - **Matrix:** the permissions plan's role × route matrix gains the three workspace routes.
  - **Payloads:** `TestPayloadsHoldNothingSensitive` covers the four new events.
- **Go unit (`internal/mail`):** the new template renders with its struct, and `layout.html` wraps both templates.
- **Vitest:**
  - the invite dialog (assignable roles only, field errors, pending state);
  - the pending list states;
  - the invitation page's states (checking, expired, signed out, matching, other account);
  - login and register show "<workspace> has invited you to join." for an invitation `next`, keep it after switching between them, prefill the email, show the expired notice for a dead token, and show nothing for other `next` values;
  - a workspace name containing `<script>` renders as text;
  - auto-accept only with our navigation state;
  - register returns to an invitation path but otherwise goes to onboarding.
- **Playwright:**
  - an owner invites a new address → read the link from `MAIL_FILE` → register through it (the register page shows "<workspace> has invited you to join.") → land in the workspace as member → the owner sees them in Members;
  - an existing user accepts while signed in;
  - a revoked link shows expired.

## Docs

- `docs/api/endpoints.md`: routes and the invitation shape.
- `docs/api/auth.md`: invitations, limits, the link-token exception, and the "Invites … are not implemented" line removed.
- `docs/api/events.md`: catalogue and listeners.
- `docs/api/mail.md`: the template and the layout.
- `docs/api/database.md`: table and `inv_`.
- `docs/ui/pages.md`: the invitation page, `?next=`, register, and the callout on login and register.
- `docs/ui/design.md`: the `info` variant of `Notice`.
- AGENTS.md:
  - the `inv_` prefix;
  - the link-token wording;
  - new registrations still start with no workspace unless they arrive by an invitation link;
  - don't claim email delivery.

## Decisions (confirmed 2026-10-31, with the AGENTS.md amendments in 7 and for registration)

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Event names | `workspace.invitation.created` (handler) and `workspace.invitation.sent` (listener), following the `<resource>.<fact>` convention ([event-names](event-names.md)), instead of `workspace.invitations.send`. Plus `.revoked` and `.accepted`. |
| 2 | Where the token is made | In the listener, as for password reset, so the raw token is never stored. The row exists without a token until then ("Sending…"). |
| 3 | Bind accept to the invited email | Yes. Signed in as another address shows "Sign out and switch". |
| 4 | Owner invitations | Not allowed: invite as admin, then promote. This keeps a link from ever granting ownership. |
| 5 | Who sees pending invitations | Owners and admins (`workspace.invitation.index`), the same roles that can see members (`workspace.member.index`). Members and viewers see neither list. |
| 6 | After signing in or registering from the link | Accept at once (the "Sign in to accept" click was the deliberate step), but only when arriving from our own buttons (navigation state). Any other arrival shows **Join**. |
| 7 | Token in `/auth/invitations/<token>` and in `?next=` | Keep both, as with the reset link, and amend AGENTS.md's query-string rule to name single-use links. The alternative (navigation state only) loses the invitation on reload or when opening login in a new tab. |
| 8 | Page URL | `/auth/invitations/<token>`, next to the reset link, so it works signed out and outside the app layout. |
| 9 | Lifetime and limits | 7 days; `invitation:per-user` 20/h, `invitation:global` 200/h, 50 pending per workspace. |
| 10 | Production | `MAIL_DRIVER=off` → 503, as reset does, until real email exists. |
| 11 | Expired invitations | Listed as "Expired" for 30 days, then cleaned up. Inviting the same email again replaces one. |
| 12 | The callout on login and register | Shown whenever `?next=` is an invitation link. The page loads it with `check`, rather than from navigation state, so it survives reloads and the login ⇄ register switch. `InvitationCallout` is built on a new `info` variant of `Notice` rather than its own styles. |

## Not included

- Real email delivery (shared with password reset), so invitations are development-only for now.
- A copyable invite link: the raw token would have to come back in the API response.
- Resending, beyond revoke and invite again.
- Declining: an ignored invitation expires.
- Showing an account its pending invitations in the app (no email verification; see Why).
- Email verification, and marking an email verified because an invitation was accepted.
- Invitation links that anyone can use (not tied to one email).

## How the build differs

- **The list is paged** (`{data, pages}` with `page` and `perPage`), as [pagination.md](../../docs/api/pagination.md) asks of every list, rather than a bare `{data}`. The members page loads one page of up to 100, which holds the 50 live invitations plus expired ones.
- **Revoking** needs the invitation's role in your `assignableRoles` too (an admin can't revoke an owner's admin invitation), as removing a member does. It takes the workspace lock before the invitation's row: the race test found that the other order deadlocks with accept (the revoke's event insert needs a key-share lock on the workspace row that accept holds).
- **The member quota is built here** (`store.MaxWorkspaceMembers`, 100). Accepting counts members plus the other live invitations, so it only fails for a workspace already full without this one.
- **Mail:** the subject is "You’ve been invited to join a workspace", without the product name, which only the web build knows (`APP_NAME`). The listener is `mail.SendInvitation`, since `mail.WorkspaceInvitation` names the template. `layout.html` wraps each email, and each is parsed as a set of its own because every file defines `title` and `content`.
- **Client:** `api.invitations(workspace)` for a workspace's invitations and `api.invitations()` for the link's `check` and `accept`, as naming.md shows.
- **"You joined Acme."** is shown by the signed-in layout from navigation state (`arrivalState`): BaseUI's toaster doesn't queue, so a toast from the invitation page would be lost as it unmounts.
- **`useInvitation`** checks the link through SWR, cached for the visit and not refreshed on focus, since each check counts against `auth:per-ip`.
- **Small extensions:** `useResource` takes `refreshInterval` (the pending list polls while one is sending), and `Button` with `href` takes navigation `state`.
- **The e2e membership tests** still add their members through the database: they test roles, not invitations, which have their own e2e test.
