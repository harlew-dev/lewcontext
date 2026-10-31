# Events

Code: `internal/events` (event types, request metadata, `Bus`, `Listen`, `Worker`), `internal/store/events.go` (`record`, `Record`, `EventScope`), `internal/store/deliveries.go` (the delivery queue). Plan: [plans/features/events.md](../../plans/features/events.md).

The API records what happens (sign-ins, password changes, workspaces and projects created, changed or deleted, accounts deleted, rate limits reached) as rows in the `events` table: an **audit trail**. **Listeners** react to events (emails, webhooks, indexing) without the handlers knowing about them: each event is queued for its listeners in `event_deliveries`, and a worker in the server delivers it. Two listeners are registered: `mail.password_reset` sends reset links and `mail.workspace_invitation` sends invitations ([mail.md](mail.md)).

## How an event is recorded

- **Same transaction as the change.** Store methods that change data call `record(ctx, tx, event, scope)` inside their transaction, so a change and its event are saved together or not at all. A change that fails leaves no event; an event that can't be written undoes its change.
- **Events without a data change** (a login attempt, a rate limit reached, a password check) are written on their own with `s.Store.Record(ctx, event, scope)`. If that write fails, the request fails with 500.
- **No table locks.** Recording is an ordinary insert.
- **Queued with it.** `record` also inserts one `event_deliveries` row per listener of that event (`Store.Listeners`, the server's bus), in the same transaction.

## Rows

| Column | Holds |
| --- | --- |
| `public_id` | `evt_` + 14 base62 |
| `name` | `<resource>.<fact>` ([naming.md](../naming.md#events-internalevents)), e.g. `project.updated`, `workspace.member.removed`; a CHECK allows two or three snake_case segments |
| `actor_id` | Who acted: the signed-in user (`events.WithActor`, set by `requireSession`), or the user who registered or signed in. Null for login attempts and anonymous rate limits, and once the actor's account is deleted |
| `user_id` | The account the event is about (auth events, and the member in member events). Deleting the account deletes these events |
| `workspace_id` | The workspace (workspace and project events). Deleting the workspace sets it to null; the events stay, readable through the `wsp_` in their payload, until pruned |
| `payload` | The event struct as JSON (`jsonb`) |
| `user_agent` | The request's `User-Agent`, valid UTF-8, cut to 256 characters (`events.WithUserAgent`, set by `security`) |
| `created_at` | When |

`actor_id`, `user_id` and `workspace_id` are internal IDs for scoping and joins; they never appear in a payload.

## Catalogue

| Event | Payload | Recorded by |
| --- | --- | --- |
| `auth.registered` | `user`, `name`, `email` | `CreateUser` |
| `auth.login_attempted` | `email`, `ip`, `user` (if the account exists), `result`: `succeeded` / `wrong_password` / `unknown_account` | `login`, for every attempt that reaches the password check |
| `auth.logged_in` | `user` | `CreateSession`, on login (not on registration or a password change) |
| `auth.logged_out` | `user` | `DeleteSession` |
| `auth.other_sessions_revoked` | `user`, `count` | `DeleteOtherSessions` |
| `auth.password_checked` | `user`, `ok`, `step`: `verify` / `update` / `destroy_workspace` / `destroy_account` | both password endpoints and both deletions that need it, whenever the current password was checked |
| `auth.password_changed` | `user`, `otherSessionsRevoked` | `ChangePassword` |
| `auth.password_reset.requested` | `email`, `ip` (no `user`: the request doesn't look the account up) | `requestPasswordReset`, for every request that passes the rate limits, whether or not an account uses the email |
| `auth.password_reset.sent` | `user`, `email`, `request` (the requested event's `evt_` ID) | the `mail.password_reset` listener, after writing the link |
| `auth.password_reset.completed` | `user`, `otherSessionsRevoked` (every session the account had) | `ResetPassword` |
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
| `rate_limit.reached` | `limit`: the limit's name and the level reached, e.g. `auth:per-ip:block`, `project:per-user:block` ([rate-limits.md](rate-limits.md)); `user` and `workspace` when known; `ip`; `window` (seconds); `resetsAt`. `user_id` and `workspace_id` are set with them | `allow`, only for the first refused request in a window |

## What's stored, and what never is

- **Never:** passwords (old, new or attempted), password hashes, session tokens or their digests, CSRF tokens, internal IDs. `TestPayloadsHoldNothingSensitive` checks the event structs' field names and types.
- **Personal data, on purpose:** the email typed and the connection IP on `auth.login_attempted` and `auth.password_reset.requested`, the IP on `rate_limit.reached`, the invited address on `workspace.invitation.created` and `.revoked` (someone who may have no account), and the account's name and email on `auth.registered`. The email is only recorded once it parses as an address (people sometimes type a password into the email field), but it can still be someone else's address or a typo. The IP is the connection's: behind a proxy (or Vite's dev proxy) every attempt shows the proxy's IP, because forwarded headers aren't trusted.
- **Timing:** `auth.login_attempted` is written whether or not the account exists, so the write doesn't reveal which emails are registered.
- **Volume:** refused requests aren't recorded one by one. Only the first refusal of a rate-limit window writes `rate_limit.reached`, so a flood writes one row per limit.
- Account, project and workspace names and project descriptions stay in the log after they change. `make prune target=events` deletes them after 90 days by default, keeping any with a pending delivery ([database.md](database.md#pruning)). Deleting an event deletes its deliveries (`ON DELETE CASCADE`).

## Listeners

```go
// cmd/server/main.go, before anything records an event:
events.Listen(bus, "mail.password_reset", mail.PasswordReset(repository, mailer, c.Origin))

// or inline:
events.Listen(bus, "mail.password_changed", func(ctx context.Context, e events.PasswordChanged, d events.Delivered) error {
	// e is the decoded payload; d.EventID (evt_…) identifies the event.
	return nil
})
```

- **Typed:** `Listen[E]` decodes the stored payload into `E`, so a listener only ever gets its own event type.
- **Named:** the name (1–100 characters, unique) is stored with each delivery. Renaming a listener makes it a new one; deliveries queued for the old name are marked failed ("no listener").
- **Registered in one place**, `listen` in `cmd/server`, so a search shows everything that reacts to an event.
- **Write listeners to run more than once.** Delivery is at least once: if a server stops after a listener ran but before it was marked delivered, it runs again. Key side effects on `d.EventID` (e.g. "email for `evt_…` already sent").
- **Respect `ctx`.** Each run has a 30 s timeout; a listener that ignores its context can't be stopped.
- **No ordering.** Events are claimed roughly oldest first, but retries and concurrent workers mean a listener may see a later event before an earlier one.

## Delivery (`events.Worker`)

The server runs one worker (`cmd/server`); every server process runs its own, sharing the queue.

1. **Claim:** a single statement takes up to 10 due deliveries with `FOR UPDATE SKIP LOCKED` (other workers skip those rows instead of waiting), adds 1 to `attempts`, and pushes `next_attempt_at` 5 minutes ahead as a **lease**. The row locks end with the statement, so listeners run with no lock or transaction held.
2. **Run:** the batch's listeners run concurrently, each with a 30 s timeout; a panic counts as a failure.
3. **Record:** success sets `delivered_at`. A failure sets `last_error` (cut to 500 characters) and retries after 1 s, 10 s, 1 min, 10 min, then 1 h; the attempt after that marks it `failed_at` and logs an error. One listener failing doesn't affect others on the same event.
4. **Poll:** when a claim comes back empty, the worker waits 1 s. A full batch is followed straight away by the next.
5. **Shutdown:** the worker stops claiming and finishes its batch within the server's 10 s grace period. A delivery cut off by a crash is claimed again when its lease ends.

## Adding an event

1. A struct in `internal/events/<area>.go` with JSON tags and an `EventName()` returning `<resource>.<fact>` ([naming.md](../naming.md#events-internalevents)), added to `events.Catalogue`. Identify records by public ID; never add secrets.
2. Record it in the store method that makes the change, inside its transaction, with an `EventScope` for the columns. Use `s.Store.Record` only for events with no data change.
3. An integration test that the endpoint writes exactly one event with the right name, scope and payload (`eventsNamed`, `onlyEvent` in `httpapi/events_integration_test.go`), and nothing when the request is refused.
4. Add it to the catalogue above.
