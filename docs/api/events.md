# Events

Code: `internal/events` (event types, request metadata, `Bus`, `Listen`, `Worker`), `internal/store/events.go` (`record`, `Record`, `EventScope`), `internal/store/deliveries.go` (the delivery queue). Plan: [plans/features/implemented/events.md](../../plans/features/implemented/events.md).

The API records what happens (sign-ins, password changes, workspaces and projects created, changed or deleted, accounts deleted, rate limits reached) as rows in the `events` table: an **audit trail**. **Listeners** react to events (emails, webhooks, indexing) without the handlers knowing about them: each event is queued for its listeners in `event_deliveries`, and a worker in the server delivers it. Two listeners are registered: `mail.password_reset` sends reset links and `mail.workspace_invitation` sends invitations, over SMTP or to the development mailbox ([mail.md](mail.md)); with `MAIL_DRIVER=off`, neither is.

## How an event is recorded

- **Same transaction as the change.** Store methods that change data call `record(ctx, tx, event, scope)` inside their transaction, so a change and its event are saved together or not at all. A change that fails leaves no event; an event that can't be written undoes its change.
- **Events without a data change** (a login attempt, a rate limit reached, a password check) are written on their own with `s.Repo.Record(ctx, event, scope)`. If that write fails, the request fails with 500.
- **No table locks.** Recording is an ordinary insert.
- **Queued with it.** `record` also inserts one `event_deliveries` row per listener of that event (`Store.Listeners`, the server's bus), in the same transaction.

## Rows

| Column | Holds |
| --- | --- |
| `public_id` | `evt_` + 14 base62 |
| `name` | `<resource>.<fact>` ([naming.md](../naming.md#events-internalevents)), e.g. `project.updated`, `workspace.member.removed`; a CHECK allows two or three snake_case segments |
| `actor_id` | Who acted: the signed-in user (`events.WithActor`, set by `RequireSession`), or the user who registered or signed in. Null for login attempts and anonymous rate limits, and once the actor's account is deleted |
| `user_id` | The account the event is about (auth events, and the member in member events). Deleting the account deletes these events |
| `workspace_id` | The workspace (workspace and project events). Deleting the workspace sets it to null; the events stay, readable through the `wsp_` in their payload, until pruned |
| `payload` | The event struct as JSON (`jsonb`) |
| `ip` | The client's address (`inet`) for an event recorded during a request (`events.WithRequest`, set by `ClientAddress` from `httpapi.ClientIP(r)`); null for the CLI and listeners. The one place an event keeps it: no payload has an `ip` field (`TestPayloadsHoldNothingSensitive`) |
| `request_id` | The ID of the request it was recorded in (`req_…`, always the server's own, never a client's or a proxy's), the `requestId` on that request's log lines and in its `X-Request-ID` (`events.WithRequest`, with the IP); empty for the CLI and listeners. `CHECK`ed to be empty or `req_` and 14 base62 ([observability.md](observability.md#request-ids)) |
| `user_agent` | The request's `User-Agent`, valid UTF-8, cut to 256 characters (`events.WithUserAgent`, set by `security`) |
| `created_at` | When |

`actor_id`, `user_id` and `workspace_id` are internal IDs for scoping and joins; they never appear in a payload.

## Catalogue

| Event | Payload | Recorded by |
| --- | --- | --- |
| `auth.registered` | `user`, `name`, `email`, `verifiedBy`: `code` (the sign-up's emailed code) / `none` (`EMAIL_VERIFICATION=off`) | `CompleteRegistration`, `CreateUser` |
| `auth.registration.requested` | `email` (no `user`: the request doesn't look the account up) | `register` and `register/resend` with verification on, for every request past the limits, taken email or not |
| `auth.registration.sent` | `email`, `request` (the requested event's `evt_` ID), `existingAccount`, `user` when it exists | the `mail.registration` listener, after sending a code, or "You already have an account" to an email with one (not when `account-exists:per-account` held it back) |
| `auth.registration.checked` | `email`, `ok`, `attempt` (this sign-up's try, from 1) | `CompleteRegistration`, for every code that reached the check |
| `auth.email_verification.requested` | `user` | `POST /api/auth/email-verification`: a signed-in account asked for a code |
| `auth.email_verification.sent` | `user`, `request` | the `mail.email_verification` listener, after sending the code |
| `auth.email_verified` | `user`, `via`: `code` / `password_reset` (completing a reset link, which went only to that inbox) | `VerifyEmail`, `ResetPassword` (only when the email wasn't verified yet) |
| `auth.login_attempted` | `email`, `user` (if the account exists), `result`: `succeeded` / `mfa_required` (right password, a code still needed) / `wrong_password` / `unknown_account` | `login`, for every attempt that reaches the password check |
| `auth.logged_in` | `user` | `CreateSession`, on login (after the code, with two-factor authentication on; not on registration or a password change) |
| `auth.logged_out` | `user` | `DeleteSession` |
| `auth.other_sessions_revoked` | `user`, `count` | `DeleteOtherSessions` |
| `auth.password_checked` | `user`, `ok`, `step`: `verify` / `update` / `destroy_workspace` / `destroy_account` / `mfa_setup` / `mfa_disable` / `recovery_codes` | both password endpoints (changing the password only without MFA), both deletions that need it, and setting up, turning off and renewing the codes of two-factor authentication, whenever the current password was checked |
| `auth.password_changed` | `user`, `otherSessionsRevoked` | `ChangePassword` |
| `auth.password_reset.requested` | `email` (no `user`: the request doesn't look the account up) | `requestPasswordReset`, for every request that passes the rate limits, whether or not an account uses the email |
| `auth.password_reset.sent` | `user`, `email`, `request` (the requested event's `evt_` ID) | the `mail.password_reset` listener, after writing the link |
| `auth.password_reset.completed` | `user`, `otherSessionsRevoked` (every session the account had) | `ResetPassword` |
| `auth.mfa.enabled` | `user`, `otherSessionsRevoked` | `EnableMFA`: the first code from the app turned two-factor authentication on (the first recovery codes come with it) |
| `auth.mfa.disabled` | `user`, `reason`: `user` / `reset` (the operator's `admin reset-mfa`, with no actor) | `DisableMFA`, `ResetMFA` |
| `auth.mfa.checked` | `user`, `ok`, `method`: `totp` / `recovery_code`, `step`: `login` / `admin_login` / `admin_step_up` (opening an admin session's step-up window) / `enable` (also the admin door's setup) / `disable` / `recovery_codes` / `password_update` (changing the password with MFA on), and `recoveryCodesLeft` when a recovery code worked | every second-factor check that reached the code (not those a rate limit refused) |
| `auth.recovery_codes.created` | `user`, `count` | `CreateRecoveryCodes`: a new set replaced the old |
| `auth.profile_updated` | `user`, `changes.name` (`from`, `to`) | `UpdateProfile`, only when the name changed |
| `auth.account_deleted` | `user` only: no name or email, since that's what's being erased. No `user_id` (it would be deleted with the account), and its actor becomes null | `DeleteAccount` |
| `workspace.created` | `workspace`, `name` (the owner is the actor) | `CreateWorkspace` |
| `workspace.renamed` | `workspace`, `changes.name` (`from`, `to`) | `RenameWorkspace`, only when the name changed |
| `workspace.deleted` | `workspace`, `name`, `projects` and `members` (how many it had); `workspace_id` becomes null with the workspace | `DeleteWorkspace` |
| `workspace.member.updated` | `workspace`, `user`, `changes.role` (`from`, `to`); `user_id` is the member, the actor who changed it | `UpdateMemberRole`, only when the role changed |
| `workspace.member.removed` | `workspace`, `user`, `role` (the role they had); `user_id` is the member | `RemoveMember` |
| `workspace.member.left` | `workspace`, `role`; the member is both actor and `user_id` | `LeaveWorkspace` |
| `workspace.invitation.created` | `invitation`, `workspace`, `email`, `role` (the inviter is the actor) | `CreateInvitation`; queues `mail.workspace_invitation` |
| `workspace.invitation.sent` | `invitation`, `workspace`, `request` (the created event's `evt_` ID) | the `mail.workspace_invitation` listener, after writing the link |
| `workspace.invitation.revoked` | `invitation`, `workspace`, `email` | `RevokeInvitation` |
| `workspace.invitation.accepted` | `invitation`, `workspace`, `role` (the role they now have); the new member is actor and `user_id` | `AcceptInvitation` |
| `project.created` | `project`, `workspace`, `name`, `description`, `status`, `color` | `CreateProject` |
| `project.updated` | `project`, `workspace`, `changes` per changed field (`from`, `to`) | `UpdateProject`, only when something changed |
| `project.deleted` | `project`, `workspace`, `name` | `DeleteProject` |
| `admin.granted` | `user`, `role` (`viewer`, `operator`), `mfaSetupUntil` (null when MFA is already on); `user_id` set, no actor (the CLI) | `GrantAdmin` (`admin grant`): a new admin, or one granted again with the same role (or none) |
| `admin.updated` | `user`, `changes.role` (`from`, `to`); `user_id` set, no actor (the CLI) | `GrantAdmin` (`admin grant -role`), only when an admin's role changed |
| `admin.revoked` | `user`, `adminSessionsRevoked`; `user_id` set, no actor | `RevokeAdmin` (`admin revoke`) |
| `admin.login_attempted` | `email`, `user` (if the account exists), `result`: `mfa_required` / `mfa_setup_required` / `wrong_password` / `unknown_account` / `not_admin` / `mfa_off` (an admin without MFA outside the setup window); no scope | `POST /api/admin/login`, for every attempt that reaches the password check |
| `admin.logged_in` | `user`; the admin is the actor, `user_id` stays empty | `CreateAdminSession`, after the code |
| `admin.logged_out` | `user`; actor only | `DeleteAdminSession` |
| `admin.access_refused` | `reason`: `ip_not_allowed`; no scope, the address in `ip` | a request with the right admin key (or a valid assets pass) from outside `ADMIN_ALLOWED_IPS`, at most once per address (an IPv6 /64) per 15 minutes per server, queued by the request and written by one worker (dropped past 64 queued) |
| `admin.session.moved` | `admin`, `from`, `to` (the sign-in's and the request's networks: IPv4 /24, IPv6 /48), `sessionAge` (seconds); actor only | `FlagAdminSessionMoved`: the first request of an admin session from another network, once per session |
| `admin.session.ended` | `admin`, `reason`: `step_up_failures`; actor only | `StepUpAdminSession`: the session's fifth wrong step-up code deleted it ([admin.md](admin.md#step-up)) |
| `admin.sessions_revoked` | `admin`, `count`; actor only | `DeleteOtherAdminSessions` (`DELETE /api/admin/sessions`) |
| `admin.user.listed`, `admin.workspace.listed` | `admin`, `q`, `sort`, `page`; actor only | every admin list read |
| `admin.user.viewed`, `admin.workspace.viewed` | `admin`, `user` / `workspace` (what was looked at); actor only, so deleting it keeps the record | every admin detail read |
| `admin.event.listed`, `admin.delivery.listed` | `admin`, `filters` (the Events or deliveries filters used, the cursor included; a searched-for IP is `filters.ip`, not the admin's). For Events: `names` (the name tokens as sent), `outcome`, `account`, `workspace`, `email`, `ip`, `window`, `from` and `to` as resolved, `before`, and `summary`, the charts' status when they were asked for with the list (`ok`, `busy`, `too_slow`, `too_wide`) (older rows have a `name` string and `adminReads` instead); actor only | every Events and deliveries list read, once per request, charts included |
| `admin.event.viewed` | `admin`, `event`; actor only | opening an event |
| `admin.mail.viewed` | `admin`, `window` (`24h`, `7d`); actor only | the Mail summary |
| `admin.project.listed` | `admin`, `workspace`, `q`, `status`, `sort`, `page`; actor only | a workspace's projects on its admin page |
| `admin.admin.listed` | `admin`; actor only | the Admins page |
| `admin.email_block.listed` | `admin`, `q`, `sort`, `page`; actor only | every read of the Email blocks page |
| `admin.email.unblocked` | `admin`, `email`, `reason` (10 to 500 characters, as the admin wrote it); actor only | `UnblockEmailAddress` with an admin: the dashboard's Unblock, behind a fresh code, in one transaction with the delete and `email.unblocked` (`by: "admin"`) |
| `email.blocked` | `email` (the address, without an account); no user, workspace or actor. The `ip` column is the request's | `BlockEmail`, from a block link, only when the address wasn't blocked already ([auth.md](auth.md#blocking-an-email-address)) |
| `email.unblocked` | `email`, `by`: `link` (its block link, with the request's `ip`) / `cli` (`admin unblock`, no `ip`) / `admin` (the dashboard's Unblock: the admin as actor, the request's `ip`, beside `admin.email.unblocked`) | `UnblockEmail`, `UnblockEmailAddress`, only when the address was blocked |
| `rate_limit.reached` | `limit`: the limit's name and the level reached, e.g. `auth:per-ip:block`, `project:per-user:block` ([rate-limits.md](rate-limits.md)); `user` and `workspace` when known; `window` (seconds); `resetsAt`. `user_id` and `workspace_id` are set with them | `allow`, only for the first refused request in a window |
| `server.started` | `server` (its `server_instances` ID, `srv_…`), `version` (`observe.Version()`); no user, workspace, actor or `ip` | `RecordServerStart`, as a server starts, with its row ([observability.md](observability.md#servers-and-versions)) |
| `server.stopped` | `server`, `reason`: `signal` (asked to stop) or `failed` (its listener failed, or startup failed after it was recorded). A crash records none | `RecordServerStop`, in a graceful shutdown's final phase ([observability.md](observability.md#shutdown)) |

## Groups

The admin dashboard's Events page picks names in groups (`internal/events/groups.go`, `events.Groups()`, served by `GET /api/admin/catalogue` as `eventGroups`). They're built from the catalogue, so a new event joins its group without anyone remembering:

| Group ID | Label | Holds |
| --- | --- | --- |
| `auth.*` | Accounts and sign-in | every `auth.` name: sub-groups `auth.password_reset.*`, `auth.registration.*`, `auth.email_verification.*`, `auth.mfa.*`, then the single names |
| `workspace.*` | Workspaces | sub-groups `workspace.member.*`, `workspace.invitation.*`, then `workspace.created`, `.renamed`, `.deleted` |
| `project.*`, `rate_limit.*`, `server.*`, `email.*` | Projects, Rate limits, Servers, Email addresses | their names |
| `admin.*` | Admin | every `admin.` name **except the reads**; sub-group `admin.session.*` |
| `admin-reads` | Admin reads | the `admin.<resource>.listed` and `.viewed` names (`events.AdminRead`) |

- **A sub-group** is a three-segment prefix with two or more names; a prefix with one name (`auth.recovery_codes.created`) is just that name. Every name is in exactly one group (`TestEventGroups`). Groups come in catalogue order, sub-groups before single names.
- **`admin-reads`** can't clash with a name: the `events.name` CHECK allows no `-`. With nothing picked, the Events list shows every name but these.

## What's stored, and what never is

- **Never:** passwords (old, new or attempted), password hashes, session tokens or their digests, CSRF tokens, MFA secrets, codes and recovery codes (or their digests), challenge tokens, internal IDs. `TestPayloadsHoldNothingSensitive` checks the event structs' field names and types.
- **Personal data, on purpose:** the client IP of every event recorded during a request (the `ip` column), the email typed on `auth.login_attempted`, `auth.password_reset.requested`, `auth.registration.requested` and `.checked`, the invited address on `workspace.invitation.created` and `.revoked` (someone who may have no account), the address on `email.blocked` and `.unblocked` and on `admin.email.unblocked` (who asked us not to email them), an admin's free-text `reason` on `admin.email.unblocked` (which may name someone: admins are told it's kept in the audit trail), what admins searched for (`q` on `admin.*.listed`, `filters` on the Events and deliveries reads: on `admin.email_block.listed` it is often the address of someone who asked not to be contacted), and the account's name and email on `auth.registered`. The email is only recorded once it parses as an address (people sometimes type a password into the email field), but it can still be someone else's address or a typo. The IP is `httpapi.ClientIP(r)`: the connection's, or behind a proxy configured with `CLIENT_IP_HEADER` and `TRUSTED_PROXIES` the client's ([auth.md](auth.md#rate-limits)); without them, and under Vite's dev proxy, every attempt shows the proxy's IP. It is the full address, even where a limit counts an IPv6 /64.
- **Timing:** `auth.login_attempted` is written whether or not the account exists, so the write doesn't reveal which emails are registered; so are `auth.registration.requested` and `.checked`. Only the listener's `auth.registration.sent`, after the response, says whether the email had an account.
- **Volume:** refused requests aren't recorded one by one. Only the first refusal of a rate-limit window writes `rate_limit.reached`, so a flood writes one row per limit.
- Account, project and workspace names and project descriptions stay in the log after they change. `make prune target=events` deletes them after 90 days by default, keeping any with a pending delivery ([database.md](database.md#pruning)). Deleting an event deletes its deliveries (`ON DELETE CASCADE`).

## Listeners

```go
// cmd/server/main.go, before anything records an event:
events.Listen(bus, "mail.password_reset", mail.PasswordReset(repository, mailer, c.Origin))
// with EMAIL_VERIFICATION on, also mail.registration and mail.email_verification

// or inline:
events.Listen(bus, "mail.password_changed", func(ctx context.Context, e events.PasswordChanged, d events.Delivered) error {
	// e is the decoded payload; d.EventID (evt_…) identifies the event.
	return nil
})
```

- **Typed:** `Listen[E]` decodes the stored payload into `E`, so a listener only ever gets its own event type.
- **Named:** the name (1–100 characters, unique) is stored with each delivery. Renaming a listener makes it a new one; deliveries queued for the old name are marked failed ("no listener").
- **Registered in one place**, `listen` in `cmd/server`, so a search shows everything that reacts to an event.
- **A blocked address** makes `mailer.Send` return `mail.ErrBlocked`; the mail listeners treat it as done (delivered, no `.sent` event, no retry) ([mail.md](mail.md#blocked-addresses)).
- **Write listeners to run more than once.** Delivery is at least once: if a server stops after a listener ran but before it was marked delivered, it runs again. Key side effects on `d.EventID` (e.g. "email for `evt_…` already sent").
- **Respect `ctx`.** Each run has a 30 s timeout; a listener that ignores its context can't be stopped.
- **No ordering.** Events are claimed roughly oldest first, but retries and concurrent workers mean a listener may see a later event before an earlier one.

## Delivery (`events.Worker`)

The server runs one worker (`cmd/server`); every server process runs its own, sharing the queue.

1. **Claim:** a single statement takes up to 10 due deliveries with `FOR UPDATE SKIP LOCKED` (other workers skip those rows instead of waiting), adds 1 to `attempts`, and pushes `next_attempt_at` 5 minutes ahead as a **lease**. The row locks end with the statement, so listeners run with no lock or transaction held.
2. **Run:** the batch's listeners run concurrently, each with a 30 s timeout; a panic counts as a failure, and is logged at error with its stack and reported as a grouped error ([observability.md](observability.md#errors-and-panics)).
3. **Record:** each outcome is counted in `deliveries_total` by listener, and a first attempt's wait in `delivery_lag_ms` ([observability.md](observability.md#metrics)). Success sets `delivered_at`. A failure sets `last_error` (scrubbed, [observability.md](observability.md#scrubbing): an SMTP refusal's recipient is `<email>`; cut to 500 characters), logs it scrubbed, and retries after 1 s, 10 s, 1 min, 10 min, then 1 h; the attempt after that marks it `failed_at`, logs an error and reports it as a grouped error (`app_errors`, source `worker`, the listener's name as its route), once. One listener failing doesn't affect others on the same event. A claim or record that fails (the database) is logged and reported too (source `background`, route `event worker`).
4. **Poll:** when a claim comes back empty, the worker waits 1 s. A full batch is followed straight away by the next.
5. **Shutdown:** the worker stops claiming and finishes its batch within the server's 10 s grace period. A delivery cut off by a crash is claimed again when its lease ends.

## Adding an event

1. A struct in `internal/events/<area>.go` with JSON tags and an `EventName()` returning `<resource>.<fact>` ([naming.md](../naming.md#events-internalevents)), added to `events.Catalogue`. Identify records by public ID; never add secrets.
2. Record it in the store method that makes the change, inside its transaction, with an `EventScope` for the columns. Use `s.Repo.Record` only for events with no data change.
3. An integration test that the endpoint writes exactly one event with the right name, scope and payload (`apitest.EventsNamed`, `apitest.OnlyEvent`), and nothing when the request is refused.
4. Add it to the catalogue above.
