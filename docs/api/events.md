# Events

Code: `internal/events` (event types, request metadata), `internal/store/events.go` (`record`, `Record`, `EventScope`). Plan: [plans/features/events.md](../../plans/features/events.md).

The API records what happens (sign-ins, password changes, workspaces and projects created or changed) as rows in the `events` table. For now the table is an **audit trail**: nothing reads it yet. Listeners that react to events (emails, webhooks) come next, delivered from the same table.

## How an event is recorded

- **Same transaction as the change.** Store methods that change data call `record(ctx, tx, event, scope)` inside their transaction, so a change and its event are saved together or not at all. A change that fails leaves no event; an event that can't be written undoes its change.
- **Events without a data change** (a login attempt, a rate limit reached, a password check) are written on their own with `s.Store.Record(ctx, event, scope)`. If that write fails, the request fails with 500.
- **No table locks.** Recording is an ordinary insert.

## Rows

| Column | Holds |
| --- | --- |
| `public_id` | `evt_` + 14 base62 |
| `name` | `<area>.<past_tense>`, e.g. `project.updated` |
| `actor_id` | Who acted: the signed-in user (`events.WithActor`, set by `requireSession`), or the user who registered or signed in. Null for login attempts and anonymous rate limits |
| `user_id` | The account the event is about (auth events) |
| `workspace_id` | The workspace (workspace and project events) |
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
| `auth.rate_limited` | `scope`: `ip` / `account`, `user` (when signed in) | `allowAuth`, only for the first refused request in a window |
| `auth.logged_out` | `user` | `DeleteSession` |
| `auth.other_sessions_revoked` | `user`, `count` | `DeleteOtherSessions` |
| `auth.password_checked` | `user`, `ok`, `step`: `verify` / `update` | both password endpoints, whenever the current password was checked |
| `auth.password_changed` | `user`, `otherSessionsRevoked` | `ChangePassword` |
| `workspace.created` | `workspace`, `name` (the owner is the actor) | `CreateWorkspace` |
| `workspace.renamed` | `workspace`, `changes.name` (`from`, `to`) | `RenameWorkspace`, only when the name changed |
| `project.created` | `project`, `workspace`, `name`, `description`, `status`, `color` | `CreateProject` |
| `project.updated` | `project`, `workspace`, `changes` per changed field (`from`, `to`) | `UpdateProject`, only when something changed |

## What's stored, and what never is

- **Never:** passwords (old, new or attempted), password hashes, session tokens or their digests, CSRF tokens, internal IDs. `TestPayloadsHoldNothingSensitive` checks the event structs' field names and types.
- **Personal data, on purpose:** the email typed and the connection IP on `auth.login_attempted`, and the account's name and email on `auth.registered`. The email is only recorded once it parses as an address (people sometimes type a password into the email field), but it can still be someone else's address or a typo. The IP is the connection's: behind a proxy (or Vite's dev proxy) every attempt shows the proxy's IP, because forwarded headers aren't trusted.
- **Timing:** `auth.login_attempted` is written whether or not the account exists, so the write doesn't reveal which emails are registered.
- **Volume:** refused requests aren't recorded one by one. Only the first refusal of a rate-limit window writes `auth.rate_limited`, so a flood writes one row.
- Project and workspace names and descriptions stay in the log after they change. Nothing deletes events yet (pruning is planned).

## Adding an event

1. A struct in `internal/events/<area>.go` with JSON tags and an `EventName()` returning `<area>.<past_tense>`, added to `events.Catalogue`. Identify records by public ID; never add secrets.
2. Record it in the store method that makes the change, inside its transaction, with an `EventScope` for the columns. Use `s.Store.Record` only for events with no data change.
3. An integration test that the endpoint writes exactly one event with the right name, scope and payload (`eventsNamed`, `onlyEvent` in `httpapi/events_integration_test.go`), and nothing when the request is refused.
4. Add it to the catalogue above.
