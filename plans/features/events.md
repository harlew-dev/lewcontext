# Domain events, listeners and pruning

Status: **steps 1–2 of 3 implemented 2026-10-30** (recording events; listeners and the worker; see [docs/api/events.md](../../docs/api/events.md)). Next: [prune.md](prune.md).

## Why

Things that happen in the API (sign-in, password change, project created) should be able to trigger other work without the handler knowing about it. The first use is an **audit trail** of what happened in an account or workspace. Later uses are a "your password was changed" email, webhooks and search indexing, each added as a listener without changing the handlers.

Go has no built-in event system (nothing like Laravel's `Event::dispatch`). A typed one is small to build with generics, so we write `internal/events` ourselves rather than adopt a library like Watermill, which brings routers, middleware and broker adapters we don't need.

## Decisions

| # | Question | Decision |
| --- | --- | --- |
| 1 | Delivery | Durable: events are rows in PostgreSQL, written in the same transaction as the change (the "outbox", below). |
| 2 | First listeners | None that send anything. The `events` table is the audit trail. The listener machinery is built and tested, so an email listener later is one `Listen` call. |
| 3 | Naming | `<area>.<past_tense>`, snake_case: `auth.password_changed`, `project.created`. |
| 4 | Retention | Kept until pruned with `make prune target=events older-than=90d CONFIRM=1` ([prune.md](prune.md)). |
| 5 | Payload detail | As much as is useful without anything sensitive: e.g. `project.updated` records each changed field with its old and new value. |
| 6 | Order | Change password ships first; `auth.password_changed` is among the first events here. |
| 7 | Login attempts | Every login attempt is recorded as `auth.login_attempted` with the email typed and the connection IP. |
| 8 | Seed events | No: seeders insert directly, and fake history would be misleading. |
| 9 | Show the trail in the UI | Later, as its own plan. v1 is the table only. |
| 10 | Local database | Rebuilding it with `make migrate-fresh CONFIRM=1 && make seed` is fine (test data only). |

## How the durable outbox works

The "outbox" is the `events` table. Instead of calling listeners directly, the code that makes a change also inserts a row describing it, **in the same database transaction**. A worker then reads new rows and hands them to listeners.

```
POST /api/workspaces/wsp_…/projects
  BEGIN
    INSERT INTO projects …                       ← the change
    INSERT INTO events (name='project.created')  ← the event
    INSERT INTO event_deliveries (one per listener, pending)
  COMMIT                                         ← both saved, or neither
  → 201 to the browser (listeners haven't run yet)

worker (goroutine in the server, every second or on NOTIFY)
  claim:   UPDATE event_deliveries SET next_attempt_at = now() + '5 minutes', attempts = attempts + 1
           WHERE (event_id, listener) IN (
             SELECT … WHERE pending AND next_attempt_at <= now()
             ORDER BY next_attempt_at, event_id LIMIT 10 FOR UPDATE SKIP LOCKED)
           RETURNING …                            ← short transaction, commits straight away
  run each listener (30 s timeout each), outside any transaction
  success: SET delivered_at = now()
  failure: SET next_attempt_at = now() + backoff, last_error = …
```

Built as (step 2): batches of **10**, whose listeners run **concurrently**, each with the 30 s timeout. Run one after another, 50 listeners at up to 30 s each could outlast the 5-minute lease and let another worker claim the same rows. Waking is by polling only (1 s); `NOTIFY` is left out until latency matters.

### Locks: rows, not tables

Nothing locks the `events` or `event_deliveries` table. Requests and workers never wait on each other for the whole table.

- **Recording an event** is an ordinary `INSERT` in the change's transaction. Inserts take PostgreSQL's weakest table-level lock (`ROW EXCLUSIVE`), which doesn't conflict with other inserts, updates or reads. All-or-nothing comes from the **transaction**, not a lock: if the project insert fails or the request is cancelled, the event insert rolls back with it.
- **Claiming deliveries** uses **row locks** with `FOR UPDATE SKIP LOCKED`. Each worker locks only the rows it picks, and another worker running the same query skips those rows instead of waiting, so two workers never claim the same delivery.
- **The row lock lasts only for the claim statement.** The claim pushes `next_attempt_at` 5 minutes ahead (a "lease") and commits. Listeners then run outside any transaction, so a slow listener (e.g. sending an email) doesn't hold a lock or a database connection. Other workers ignore the row because it isn't due yet.
- **If a worker dies mid-listener,** the lease simply expires and another worker claims the row after 5 minutes. A listener's 30 s timeout keeps it well inside the lease.

### Delivery guarantees

- **No phantom or lost events.** A rolled-back change leaves no event, and a committed change always has one. A crash or deploy right after commit just means the worker picks the row up when the server restarts.
- **Listeners can't slow down or break requests.** The response doesn't wait for them, and a failing listener is retried with backoff (1 s, 10 s, 1 min, 10 min, 1 h), then marked failed and logged. One listener failing doesn't affect the others on the same event.
- **At least once.** If a server dies after a listener ran but before it was marked delivered, it runs again. Listeners must tolerate that by keying their work on the event's public ID (e.g. "email for `evt_…` already sent").
- **No strict ordering.** Events are claimed roughly in ID order, but retries and concurrent transactions mean a listener may see `project.updated` before an earlier `project.created` was delivered. Listeners shouldn't depend on order.
- **Wake-up:** the worker polls every second; `NOTIFY events` after commit wakes it straight away. Polling alone is correct; `NOTIFY` only cuts latency.
- **Shutdown:** the worker stops claiming and finishes in-flight listeners within the existing 10 s grace period; anything unfinished is retried when its lease expires.

With no listeners registered (v1), events are only inserted and there are no delivery rows, so the worker has nothing to do.

### Tables

Squashed migrations: new create migrations `007_create_events` (step 1) and `008_create_event_deliveries` (step 2), both done, with factories (`TestEveryTableHasAFactory`). Then `make migrate-fresh CONFIRM=1 && make seed` locally.

```sql
CREATE TABLE events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 public_id text NOT NULL UNIQUE DEFAULT public_id('evt') CHECK (public_id ~ '^evt_[0-9A-Za-z]{14}$'),
 name text NOT NULL,
 actor_id bigint REFERENCES users(id) ON DELETE SET NULL,          -- who did it
 user_id bigint REFERENCES users(id) ON DELETE CASCADE,            -- whose account it concerns
 workspace_id bigint REFERENCES workspaces(id) ON DELETE CASCADE,  -- null for account events
 payload jsonb NOT NULL,
 user_agent text NOT NULL DEFAULT '' CHECK (length(user_agent) <= 256),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX events_user_created_idx ON events(user_id, created_at DESC);
CREATE INDEX events_workspace_created_idx ON events(workspace_id, created_at DESC);
CREATE INDEX events_name_created_idx ON events(name, created_at DESC);
CREATE INDEX events_created_idx ON events(created_at);

CREATE TABLE event_deliveries (
 event_id bigint NOT NULL REFERENCES events(id) ON DELETE CASCADE,
 listener text NOT NULL,
 attempts int NOT NULL DEFAULT 0,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),  -- also the claim lease
 delivered_at timestamptz,
 failed_at timestamptz,
 last_error text NOT NULL DEFAULT '' CHECK (length(last_error) <= 500),
 PRIMARY KEY (event_id, listener)
);
CREATE INDEX event_deliveries_pending_idx ON event_deliveries(next_attempt_at)
  WHERE delivered_at IS NULL AND failed_at IS NULL;
```

The `user_id` and `workspace_id` columns are what a later activity page would filter on, and they scope events the same way the rest of the data is scoped. `events_name_created_idx` serves queries like "login attempts for this email in the last day".

## Events in code

### Typed structs

```go
// internal/events/projects.go
type ProjectUpdated struct {
	Project   string            `json:"project"`   // prj_ public ID
	Workspace string            `json:"workspace"` // wsp_
	Changes   Changes `json:"changes"`   // only fields that changed
}
type Change struct {
	From string `json:"from"`
	To   string `json:"to"`
}
func (ProjectUpdated) EventName() string { return "project.updated" }
```

- Built as: the interface method is `EventName()` (not `Name()`, which clashes with payload fields called `Name`), and payload keys are camelCase like API responses (`otherSessionsRevoked`).
- One struct per event type, grouped by area (`auth.go`, `workspaces.go`, `projects.go`), each with `EventName()`. Listeners get typed fields, not a `map[string]any`.
- **Payloads use public IDs** only. Internal IDs live in the table's foreign-key columns and are never in the JSON, so a payload can later go out as a webhook unchanged (AGENTS: never expose bigint IDs).
- **Never in a payload or column:** passwords (old, new or attempted), password hashes, session tokens or their digests, CSRF tokens.
- **Personal data, stored on purpose:** the email and IP on `auth.login_attempted` (below), and the account's own name and email on `auth.registered`. Pruning is what limits how long they're kept.

### Event catalogue (v1)

| Event | Payload | Where it's recorded |
| --- | --- | --- |
| `auth.registered` | user, name, email | `register`, in `CreateUser`'s transaction |
| `auth.login_attempted` | email, ip, user (if the account exists), `result`: `succeeded` / `wrong_password` / `unknown_account` | `login`, on every attempt that reaches the password check |
| `auth.logged_in` | user | `login`, in `CreateSession`'s transaction (the event a future "new sign-in" email listens to) |
| `auth.rate_limited` | `scope`: `ip` / `account`, user (if known) | `limitAuth` / `accountLimit`, **only on the request that first goes over the limit** in a window, so a flood writes one row, not thousands |
| `auth.logged_out` | user | `logout` |
| `auth.other_sessions_revoked` | user, `count` of sessions removed | `revokeOthers` |
| `auth.password_checked` | user, `ok`, `step`: `verify` / `update` | change password, both endpoints |
| `auth.password_changed` | user, `otherSessionsRevoked` count | change password, in `ChangePassword`'s transaction |
| `workspace.created` | workspace, name (the owner is the event's actor) | `CreateWorkspace` |
| `workspace.renamed` | workspace, `changes.name` from/to | `RenameWorkspace` |
| `project.created` | project, workspace, name, description, status, color | `CreateProject` |
| `project.updated` | project, workspace, `changes` per field (from/to) | `UpdateProject` |

- `auth.login_attempted` replaces the earlier `auth.login_failed`: one event per attempt with its `result`. A successful sign-in writes both `login_attempted` (`succeeded`) and `logged_in`.
- `auth.password_reset` is left out until recovery by email exists. Changing it while signed in is `auth.password_changed`.
- `project.updated` needs the old values, so `UpdateProject` becomes a transaction: `SELECT … FOR UPDATE` on that project row, update, compare, record. An update that changes nothing records no event.
- Project names and descriptions are workspace content, not secrets, but they stay in the event log after the project changes until the events are pruned.

### Login attempts: email and IP

- **When:** after the IP limit (`limitAuth`) and the account limit pass and the email is well-formed, i.e. once the password is actually checked. Requests refused earlier aren't recorded one by one (`auth.rate_limited` covers going over a limit), so a flood can't grow the table beyond the rate limits (60 per IP, 10 per account, per 15 minutes).
- **Email:** the address as typed, after the existing trim and lowercase, and only once it parses as an email address. People sometimes type their password into the email field; a password almost never parses as an address, so this keeps stray passwords out of the log. It's stored for unknown accounts too (`user_id` null, `result: unknown_account`), which is useful for spotting credential-stuffing, but means it can be someone else's address or a typo.
- **IP:** the connection IP (`RemoteAddr`), the same value the IP rate limit uses. Forwarded headers are still ignored, so behind a reverse proxy, and locally behind Vite's dev proxy, every attempt shows the proxy's IP (e.g. `127.0.0.1`). Real client IPs need trusted-proxy configuration, a separate change.
- **Timing:** the row is written for every result, so the write doesn't reveal through timing whether an account exists (login already uses a dummy hash for the same reason).
- **Showing it later:** the email is attacker-supplied text. Any UI that lists attempts must render it as text (React does by default) and only show a user the attempts for their own account.

### Who did it

`requireSession` (and `login`/`register` once they know the user) puts `events.Meta{Actor, UserAgent}` in the request context. The store's `record` helper reads it, so store method signatures don't grow an extra argument each. The user agent is truncated to 256 characters.

### Recording

```go
// internal/store/events.go
func record(ctx context.Context, db bun.IDB, e events.Event, scope events.Scope) error
```

- Store methods that change data call `record(ctx, tx, …)` inside their transaction. Methods that weren't transactions (`CreateProject`, `RenameWorkspace`, `UpdateProject`) become one.
- Events without a data change (`auth.login_attempted`, `auth.rate_limited`, `auth.password_checked`) are recorded on their own with `s.Store.Record(ctx, e)`.
- `record` also inserts one `event_deliveries` row per registered listener for that event name (step 2).

### Listeners

```go
// cmd/server/main.go: composition only
bus := events.NewBus()
// events.Listen(bus, "mail.password_changed", mail.PasswordChanged) // later, once email exists

func(ctx context.Context, e events.PasswordChanged, meta events.Delivered) error
```

- `events.Listen[E events.Event](bus, name, fn)` registers a typed listener; the generic decodes the payload into `E`, so a listener can't receive the wrong type. `meta` carries the event's public ID (for idempotency), actor and time.
- Listeners are registered in one place so a search shows everything that reacts to an event. Each has a stable name: renaming one counts as a new listener.
- Package layout avoids an import cycle: `internal/events` holds the event types, `Bus`, `Listen` and the worker loop, and depends on a small `Queue` interface (claim, mark delivered, retry). `internal/store` implements `Queue` and imports `events` for the types. `cmd/server` wires them together.

## Pruning

Events are deleted by the separate prune command: see [prune.md](prune.md). Its `events` target (default 90 days) keeps events that still have a pending delivery.

## Tests

- Unit (`internal/events`): `Listen` decodes into the right type and never gets another event's payload; a failing listener is retried with backoff and doesn't block others; a panicking listener is recovered and counted as a failure.
- Integration (real PostgreSQL, `internal/store` / `httpapi`):
  - each emitting endpoint writes exactly one event with the right name, actor, user/workspace and public IDs only (no bigint IDs in the payload);
  - a rolled-back change writes no event (e.g. a project update that fails a constraint);
  - `project.updated` records only the changed fields, and no event when nothing changed;
  - `auth.login_attempted` is written for success, wrong password and unknown account, with the email and IP and never the password; a malformed email or a request refused by a rate limit writes no attempt; going over a limit writes one `auth.rate_limited` row however many requests follow;
  - two workers claiming at once never get the same delivery; a delivered row isn't claimed again; a claimed row whose lease expired is claimed again.
- A test that no event struct has a field named like `password`, `token`, `hash` or `csrf`, as a guard against sensitive payloads.

## Docs

- New `docs/api/events.md`: the catalogue table, how to add an event or a listener, locks and delivery guarantees (at least once, no ordering, idempotent listeners), what personal data is stored, and a link to pruning. Linked from `docs/README.md`, plus a step in "Adding a resource end to end" (record `<resource>.created`/`.updated`).
- `docs/api/database.md` (tables), `docs/api/seeding.md` (factories), `docs/api/auth.md` (what's recorded for auth, including login attempts' email and IP).

## Later

- An activity page: "Recent account activity" on Account & security, and a workspace activity list for admins. Separate plan.
- Trusted-proxy configuration so login attempts (and the IP rate limit) see real client IPs.
- Email listeners (`auth.password_changed`, `auth.logged_in` from a new device) once email delivery exists.
