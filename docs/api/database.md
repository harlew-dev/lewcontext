# Database

PostgreSQL 15+ through Bun (`uptrace/bun`, pgdialect) over the pgx `database/sql` driver. The connection pool holds 10 connections (`internal/database/database.go`), each with its `TimeZone` pinned to UTC whatever the server's default, `PGTZ` or the URL say, so date arithmetic (an interval in days, `date_trunc`) and the times PostgreSQL prints are the same on every server. Still write a fixed length of time in hours, not days, in a `CHECK` or a migration's SQL (`interval '720 hours'`): a day is 23 or 25 hours across a DST change in any session that isn't UTC (a `psql` shell, a migration run by hand).

## Tables

| Table | Purpose | Store model |
| --- | --- | --- |
| `users` | Accounts (`password_hash` never leaves the store); `email_verified_at`, null for an account that never verified its email (made while `EMAIL_VERIFICATION` was off); `last_seen_at`, its last signed-in request to the hour (moved by the session check only when over an hour old, never with an event; null before the first), indexed for counting active accounts (`users_last_seen_idx`) and, with `created_at`, for listing those not seen for two years (`users_inactive_idx` on `coalesce(last_seen_at, created_at)`) ([monitoring.md](monitoring.md)) | `User` (+ unexported `account` with the hash) |
| `pending_registrations` | Sign-ups waiting for their emailed code (`EMAIL_VERIFICATION=on`): `email`, `name`, `password_hash`, the browser cookie's `token_hash`, `request_id` (the `evt_` ID of the latest `auth.registration.requested`, by which the listener finds the row; not a request's `req_` ID, which is `events.request_id`), `code_hash` (an HMAC under `APP_KEY`; for a taken email, of a code nobody is sent), `attempts`, `expires_at` (15 minutes). Several per email; completing one creates the user and deletes them all ([auth.md](auth.md#email-verification)) | unexported `pendingRegistration` |
| `email_verifications` | One live code per signed-in account verifying its email: `code_hash` (HMAC), `attempts`, `expires_at` (15 minutes); deleted when used, replaced or after 5 wrong codes | unexported `emailVerification` |
| `sessions` | Session token digests, CSRF tokens, expiry | unexported `sessionRecord` |
| `workspaces` | Workspaces (name) | `Workspace` (+ the member's `Role`, read from `workspace_user`) |
| `workspace_user` | Memberships: user, workspace, `role` (`owner`, `admin`, `member`, `viewer`; `auth.Role`); unique per pair | unexported `membership` |
| `projects` | Projects in a workspace (`ON DELETE CASCADE`) | `Project` |
| `rate_limits` | Rate-limit counters: `key` (`<limit name>:<digest>`), `hits`, `resets_at` (indexed: the cleanup's delete and the Integrity page's check of it) ([rate-limits.md](rate-limits.md)). Reads are counted in memory instead | unexported `rateLimitRecord` |
| `workspace_invitations` | Invitations: `public_id` (`inv_`), workspace, invited `email`, `role` (never owner), `invited_by` (`SET NULL`), the link's `token_hash` and `sent_at` once emailed, `expires_at`; unique per workspace and email ([auth.md](auth.md#invitations)) | unexported `invitationRecord` (+ `Invitation` as listed) |
| `password_resets` | One live reset link per user: the token's SHA-256 digest and `expires_at`; deleted when used, replaced, or the password changes | unexported `passwordReset` |
| `mfa_totp` | One authenticator per user: the secret sealed with `APP_KEY` (never plain), `last_used_step` (a code works once), `confirmed_at` (null while setup is pending) ([auth.md](auth.md#two-factor-authentication)) | unexported `mfaTOTP` |
| `mfa_recovery_codes` | Recovery codes as keyed HMAC digests, `used_at` once used | unexported `recoveryCode` |
| `mfa_challenges` | Sign-ins waiting for their second factor: the cookie token's digest, the user, `purpose` (`login`; `admin` and `admin_setup` for the admin door), `attempts`, `expires_at` | unexported `mfaChallenge` |
| `admins` | Accounts granted admin from the CLI: `user_id` (`CASCADE`), `mfa_setup_until` (the window to set up MFA at the admin door), `role` (`viewer` by default, or `operator`; `CHECK`ed), `created_at` ([admin.md](admin.md)) | unexported `adminRecord` (+ `Admin`, and `AdminAccount` for the Admins page) |
| `admin_sessions` | Admin session token digests, `key_hash` (the `ADMIN_ROUTE_KEY` they were made under, current or previous), CSRF tokens, expiry, the sign-in's `ip` (`inet`) and `user_agent` (≤ 256), `moved_at` (first used from another network), `stepped_up_until` (the end of the 5-minute window a fresh code opens for actions; null: never; cleared on a role change) and `step_up_failures` (its wrong step-up codes; the fifth deletes it, [admin.md](admin.md#step-up)); `user_id` references `admins`, so revoking admin ends them | unexported `adminSessionRecord` |
| `email_block_links` | One per blockable email sent (an invitation): the block link's `token_hash` (SHA-256), the `email` it went to (the page shows it and checks it against `users`), `expires_at` (a year), with an index on `(email, expires_at)` for the admin Email blocks page's newest live link; deleted by the cleanup once expired ([auth.md](auth.md#blocking-an-email-address)) | unexported `emailBlockLink` |
| `email_blocks` | The block list: each blocked `email` (lower-case, `CHECK`ed), `created_at`. Kept while blocked (a suppression list, never pruned); a blocked address never has an account | unexported `emailBlock` (+ `EmailBlock`) |
| `user_suspensions` | One row per suspended account (`user_id` primary key, `CASCADE`): `reason` (10–500 characters, `CHECK`ed), `suspended_by` (the admin, `SET NULL`; null from the command line), `created_at`. `FindSession`, `FindAdminSession` and `IssuePasswordReset` skip a suspended account (`NOT EXISTS` on the primary key), and `CreateSession`, `CreateAdminSession`, `ResetPassword` and `GrantAdmin` refuse one under the user's lock ([admin.md](admin.md#suspending-an-account)) | unexported `userSuspension` (+ `UserSuspension` on the admin user's page) |
| `ip_blocks` | Client addresses and ranges every server refuses ([admin.md](admin.md#ip-blocks)): `public_id` (`ipb_`), `network` (`cidr`, unique, at most a /16 of IPv4 or /32 of IPv6, `CHECK`ed), `reason`, `created_by` (`SET NULL`), `created_at`, `expires_at` (at most 720 hours, 30 days, on, `CHECK`ed in hours so a DST change can't shorten it), with an index on `expires_at`. Each server loads the live ones every 30 seconds; the cleanup deletes expired ones | unexported `ipBlockRecord` (+ `IPBlock`, `LiveIPBlock`) |
| `events` | What happened: name, actor, user, workspace, `jsonb` payload, the client's `ip` (`inet`, null outside a request), the request's `request_id` (`req_…`, its log lines' `requestId`, empty outside a request; unrelated to `pending_registrations.request_id`, an event's `evt_` ID), user agent, `admin_request` (recorded in an admin dashboard request) ([events.md](events.md)). Deleting a user deletes the events about them (`user_id`, `CASCADE`) and nulls `actor_id`; deleting a workspace nulls `workspace_id` (`SET NULL`), so its history survives until pruned | unexported `eventRecord` |
| `event_deliveries` | One row per event and listener: attempts, `next_attempt_at` (also the claim lease), `delivered_at`, `failed_at`, `last_error` (scrubbed, [observability.md](observability.md#scrubbing)) | unexported `deliveryRecord` |
| `alerts` | Alerts ([monitoring.md](monitoring.md#alerts)): `public_id` (`alt_`), the `signal` and its `level` (`warn`, `critical`; raised in place), `opened_at`, `last_value`, `peak_value`, `details` (`jsonb`: the signal's drill-down and an integrity check's sample public IDs, for the dashboard only), `notified` (`mail`, `held`, `digest`, `quiet`), `last_notified_at`, `acknowledged_at`/`_by`, `silenced_until`/`_by` (`SET NULL`), `resolved_at`. A unique partial index allows **one open alert per signal**; others serve the list (`opened_at DESC, id DESC`), a signal's recent alerts, held digests and pruning. Written only by the alert evaluator and the admin actions, each change with its event in one transaction under an advisory lock (`database.LockAlertWrites`); resolved alerts kept a year (`prune alerts`). `admins` gains `alert_email` (`all`, `critical`, `none`) | unexported `alertRecord` (+ `Alert`) |
| `app_errors` | Grouped errors, a row per distinct failure, never per occurrence: `public_id` (`err_`, the admin Errors page's URL), `fingerprint` (unique hex SHA-256 of source, route and scrubbed message), `source` (`request`, `panic`, `worker`, `background`, `client`), `route` (a pattern, label or job, never a path), the scrubbed `message` (≤ 1,000) and `stack` (≤ 8,000), `count`, `first_seen_at`, `last_seen_at`, `last_request_id` (ours only), `last_version`, `resolved_at`; an index on `last_seen_at`. Written only by the server's error collector, every 10 s when something failed (`RecordErrors`, one upsert); pruned 30 days after last seen ([observability.md](observability.md#grouped-errors-app_errors)) | unexported `appErrorRecord` |
| `request_minutes` | Each server's requests per minute and route with traffic, never per request: `minute`, `server` (`srv_…`), `route` (a pattern or page label), `requests`, `status_4xx` (429s included), `status_429`, `status_5xx`, `timeouts`, `aborted`, `duration_sum_ms`, `duration_buckets` (12 counts). Insert only, one statement per server per minute; the rollup deletes rows older than 48 hours ([observability.md](observability.md#metrics-in-the-database)) | unexported `requestMinute` |
| `metric_minutes` | Each server's other series per minute: `metric`, `labels` (`name="value"` pairs, values from fixed sets only), `count`, `sum`, `buckets`. Written and kept like `request_minutes` | unexported `metricMinute` |
| `request_hours`, `metric_hours` | The rollup's hourly sums over every server (unique per hour and route, or hour, metric and labels; a bigint `id` for `make prune`) | unexported `requestHour`, `metricHour` |
| `server_instances` | One row per running server: `id` (`srv_…`, new at every start, made by the server), `hostname`, `version`, `started_at`, `last_seen_at` (each minute's heartbeat), `config` (the configuration's summary: which settings are set, never a secret's value or digest). Deleted by the rollup a day after a server is last seen ([observability.md](observability.md#servers-and-versions)) | `ServerInstance` |
| `bun_migrations`, `bun_migration_locks` | Migration history | (Bun) |

## Public IDs

`users`, `workspaces`, `projects`, `workspace_invitations` and `events` have `public_id text NOT NULL UNIQUE DEFAULT public_id('usr' | 'wsp' | 'prj' | 'inv' | 'evt')`, with a `CHECK` on the format. The plpgsql `public_id(prefix)` function (migration 001) turns bytes from the built-in `gen_random_uuid()` into 14 base62 characters, skipping the UUID's fixed version/variant bytes and bytes ≥ 248 so every character is equally likely (about 83 random bits). No extension is needed.

- Every insert gets one, from the API, factories or seeders. Models map it as `PublicID string` with `bun:",nullzero"`, so Bun sends `DEFAULT`, and read it back with `Returning("*")`. Go never generates IDs.
- JSON uses the public ID as `"id"`; the bigint `id` is `json:"-"` and stays internal (keys, joins, sort tie-breaks).
- `input.PublicID(prefix, id)` checks the shape before any query.

## Queries

Store methods are named verb first ([../naming.md](../naming.md#store-methods-internalstore)):

| Verb | For | Examples |
| --- | --- | --- |
| `Index<Resources>` | one page of a list, with its search and filters | `IndexProjects`, `IndexMembers`, `IndexWorkspaces` |
| `Find<Resource>` | one record; `sql.ErrNoRows` when it's missing or out of scope | `FindProject`, `FindSession`, `FindWorkspace` (through the user's membership), `FindUser` |
| `Create` / `Update` / `Delete<Resource>` | plain writes | `CreateProject`, `UpdateProject`, `DeleteProject`, `DeleteWorkspace`, `DeleteAccount` |
| a domain verb | when it says more | `RenameWorkspace`, `UpdateMemberRole`, `RemoveMember`, `LeaveWorkspace`, `IssuePasswordReset`, `IssueInvitationToken`, `AcceptInvitation`, `RevokeInvitation`, `Allow` (a hit against a rate limit), `RecordErrors` (the error collector's flush), `RecordMetrics` (a minute and the heartbeat), `SummarizeMetrics` (the hourly rollup) |
| `Check<Thing>` | a yes/no question, or what a link shows | `CheckPasswordReset`, `CheckInvitation`, `CheckEmailBlockLink`, `CheckEmailBlocked` |

Methods implementing an interface from elsewhere keep its names (`Claim`, `Delivered`, `Fail`, `Retry` for `events.Queue`).

- All SQL lives in `internal/store`. Handlers never build queries.
- **Deleting** relies on the foreign keys' `ON DELETE CASCADE` (and `SET NULL` for `events.actor_id` and `events.workspace_id`), in one transaction with the event. `DeleteWorkspace` and `DeleteAccount` take the workspace lock first, as every membership change does; `DeleteAccount` locks the user's row first, then every workspace the user owns in `id` order, so two deletions can't deadlock, nor one with an invitation (`CreateInvitation` also takes the user's row before the workspace's) ([endpoints.md](endpoints.md#deleting)).
- **Scope by membership:** workspaces are only read through a `workspace_user` row for the session's user (`memberWorkspaces`). The one exception is the admin dashboard's reads (`admin_views.go`: `IndexAdminUsers`, `FindAdminUser`, `IndexAdminWorkspaces`, `FindAdminWorkspace`, `IndexAdminProjects`; `admin_events.go`: `IndexAdminEvents`, `FindAdminEvent`, `FindAdminEventRefs`; `admin_event_summary.go`: `SummarizeAdminEvents`; `admin_mail.go`: `IndexAdminDeliveries`, `FindAdminMailSummary`; `admin_admins.go`: `IndexAdminAdmins`; `admin_email_blocks.go`: `IndexAdminEmailBlocks`; `monitor.go` and `monitor_<page>.go`: the Monitoring pages' counts and review lists, the IP addresses, Accounts, Workspaces and Usage pages included, [monitoring.md](monitoring.md)), and the admin actions' writes (`admin_actions.go`, `suspensions.go`, `ip_blocks.go`, the requeues in `deliveries.go`), which see across workspaces on purpose and are reachable only behind the admin key and an admin session ([admin.md](admin.md)). Workspace data (projects) is scoped by the workspace's internal ID from `RequireWorkspace`: `WHERE p.public_id = ? AND p.workspace_id = ?`, returning `sql.ErrNoRows` (404) when the row isn't in that workspace.
- **Bun's builder only:** write queries with `NewSelect`/`NewInsert`/`NewUpdate`/`NewDelete` (and `With`, `ColumnExpr`, `WhereGroup` for the harder ones), in tests too; no SQL strings through `NewRaw`, `QueryContext` or `ExecContext`. Only `internal/database` (schemas, advisory locks) and migration files write SQL directly.
- **Placeholders only:** values always go through Bun `?` placeholders, and SQL text is never built from input. Dynamic `ORDER BY` uses a fixed map of SQL strings (see [pagination.md](pagination.md)).
- **The rollup's lock:** `database.TryLockMetricsRollup(ctx, tx)` tries the metrics rollup's transaction-scoped advisory lock (`pg_try_advisory_xact_lock`, through the builder), never waiting: one server at a time sums the hours and deletes old minutes ([observability.md](observability.md#metrics-in-the-database)). Its key hashes the current schema, so test schemas don't share it.
- **The email lock:** `database.LockEmail(ctx, tx, email)` takes a transaction-scoped advisory lock on an address (the one SQL string outside the builder besides schemas and the migration lock). Blocking an address and creating an account take it before checking each other's table (`BlockEmail`, `CompleteRegistration`, `CreateUser`), so the two can't both pass ([auth.md](auth.md#blocking-an-email-address)).
- **The IP blocks lock:** `database.LockIPBlocks(ctx, tx)` takes a transaction-scoped advisory lock (through the builder: `pg_advisory_xact_lock` on a hash of a name and the current schema, so integration tests in their own schemas don't share it) that `CreateIPBlock` holds while it counts the live blocks and inserts one, so blocks made at once can't take the list past 1,000 ([admin.md](admin.md#ip-blocks)).
- **Quotas** (`quotas.go`): `CreateWorkspace`, `CreateProject`, `CreateInvitation` and `AcceptInvitation` lock a row (the user's, or the workspace's) and count before inserting, returning `QuotaError` past the cap ([rate-limits.md](rate-limits.md#quotas)).
- Request contexts carry a 10 s timeout. Pass `r.Context()` into every store call.

## Migrations

- **Files:** `apps/api/migrations/<version>_<name>.tx.up.sql` plus `.tx.down.sql`, one create migration per table, in dependency order: `001_create_public_id_function`, `002_create_users`, `003_create_sessions`, `004_create_workspaces`, `005_create_projects`, `006_create_rate_limits`, `007_create_events`, `008_create_event_deliveries`, `009_create_password_resets`, `010_create_workspace_invitations`, `011_create_mfa_totp`, `012_create_mfa_recovery_codes`, `013_create_mfa_challenges`, `014_create_admins`, `015_create_admin_sessions`, `016_create_pending_registrations`, `017_create_email_verifications`, `018_create_email_block_links`, `019_create_email_blocks`, `020_create_app_errors`, `021_create_request_minutes`, `022_create_request_hours`, `023_create_metric_minutes`, `024_create_metric_hours`, `025_create_server_instances`, then `060_create_user_suspensions`, `061_create_ip_blocks` and `062_create_alerts` (the admin actions plan's and the alerts plan's; numbered from 060 because they were built beside other work, so the numbers wouldn't collide). `events` has a partial index on each user's `auth.logged_in` events (holding the address, `INCLUDE (ip)`, for the Monitoring takeover check), for the admin dashboard's last sign-in, one on each admin's `admin.logged_in` (by actor), for the Admins page's, and one per Events filter ([admin.md](admin.md#events)): `ip`, the payload's lower-cased `email` and its `workspace`, `actor_id`, and the payload's `request` (an event's related events); two for erasure, on an admin's Events search for an address (`lower(payload #>> '{filters,email}')`, where there is one) and an admin's list search (`lower(payload->>'q')`), so deleting an account finds them without reading every admin read (`TestErasureUsesItsIndexes`); and a partial index on `created_at` of the events not yet minimised (`minimised_at IS NULL`), so the daily `pseudonymise` reads only what's left to do. `events_created_idx` is `ON events (created_at) INCLUDE (name)`, so the Events page's summary counts a window by bucket and name from the index alone (an index-only scan), and it still serves pruning's `created_at < cutoff`. `event_deliveries` has a partial index on failed deliveries.
- **Squashed while in development.** There is no database whose data must be kept, so a table changes by **editing its create migration**, not by adding an alter migration. Then rebuild the local database: `make migrate-fresh CONFIRM=1 && make seed`. Once a database holds data that must be kept, migrations become append-only again.
- **`make migrate-fresh`** lists the tables and row counts it would drop. `CONFIRM=1` drops the current schema (`DROP SCHEMA … CASCADE`), recreates it and migrates. It is refused when `APP_ENV=production`. Only run it on the local development database. Other checkouts sharing that database need it too after pulling a schema change: Bun only runs migrations it hasn't recorded, so an edited file won't re-run.
- **Running:** `make migrate` applies pending files, each in its own transaction, as one group, holding a PostgreSQL advisory lock. `make migrate-status` lists them. The API never migrates on startup.
- **Rollback:** `make migrate-rollback` only previews. `CONFIRM=1` runs the down SQL, which drops tables.

## Adding a table

1. A create migration (`make migration name=create_tasks`, or a numbered pair after the tables it references): the table with `public_id … DEFAULT public_id('tsk')` if users open or search its records, `workspace_id bigint NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE` for workspace data, `CHECK` constraints matching validation, and an index for the main query.
2. A store model (`PublicID` with `nullzero`, internal `ID` as `json:"-"`) and queries taking the workspace ID.
3. **A factory in `internal/factory`** with its own model. `TestEveryTableHasAFactory` fails without it ([seeding.md](seeding.md)).
4. **A row in [compliance.md](../compliance.md#data-map)'s data map:** its personal data, why it's kept, for how long and who sees it, and a retention rule (a prune target, the cleanup, or its owner deleting it). `TestEveryTableIsInTheDataMap` fails without it.
5. **With a `workspace_id`**, a row in `store.WorkspaceTables` (`workspace_tables.go`) and an index leading with `workspace_id`, so the admin Monitoring page counts its rows per workspace. `TestEveryWorkspaceTableIsMonitored` fails without them ([monitoring.md](monitoring.md#workspaces)).
6. Integration tests, including another user's access ([testing.md](testing.md)).

## Pruning

Log tables grow without limit, so `make prune` deletes their old rows on demand (`events`, `audit_events`, `app_errors`, `alerts`, `request_hours`, `metric_hours`), and minimises events past 30 days (`pseudonymise`): the retention schedule of [compliance.md](../compliance.md#retention) (`cmd/prune`; one file per target in `internal/prune`, its queries in `internal/store/prune_<target>.go`, and the shared count and batched delete in `internal/store/prune.go`):

```sh
make prune                                          # previews every target at its default age; deletes nothing
make prune target=events                            # preview: "events: would delete 12408 older than 2026-08-01 20:29 UTC (90d)"
make prune target=events older-than=7d CONFIRM=1    # deletes
cd apps/api && go run ./cmd/prune events -older-than 36h -confirm
cd apps/api && go run ./cmd/prune -h                # lists targets and their default ages
```

| Target | Default age | Deletes | Keeps |
| --- | --- | --- | --- |
| `events` | 90 days | events created before the cutoff, **other than audit events**, with their delivered or failed `event_deliveries` (cascade) | events with a pending delivery, counted as "kept" |
| `audit_events` | 1 year | audit events (`events.Audit`: every `auth.*`, `admin.*`, `alert.*`, `server.*` and `prune.*` name) created before the cutoff, as `events` deletes the others | the same |
| `pseudonymise` | 30 days | **deletes nothing:** rewrites each event created before the cutoff and not minimised yet: its `ip` cut to the IPv4 /24 or IPv6 /48, the emails in its payload replaced by keyed pseudonyms, `minimised_at` set; 1,000 a transaction, oldest first. Needs `APP_KEY` ([compliance.md](../compliance.md#retention)) | events with a pending delivery (a listener may need the address) |
| `alerts` | 1 year | alerts resolved before the cutoff (open ones are never touched) | nothing |
| `app_errors` | 30 days | error groups last seen before the cutoff (however long ago they first happened); one that recurs later starts a new row | nothing |
| `request_hours`, `metric_hours` | 90 days | hourly metric sums older than the cutoff | nothing |

- **Evidence:** a confirmed run that finishes records `prune.completed` (`target`, `deleted`, `kept`, `cutoff`; for `pseudonymise`, `deleted` counts the rows minimised), an audit event kept a year. A preview, or a run cut short, records nothing.
- **Previews unless confirmed**, like `migrate-rollback`. Without a target it previews every target and never deletes, even with `CONFIRM=1`.
- **Ages are per target.** `older-than` overrides one only with a target (a run over every target uses each default), and takes whole days or hours (`90d`, `36h`), at least `1h`, so a typo like `0d` can't empty a table.
- **The cutoff is fixed** when the run starts; rows created meanwhile are never in scope.
- **Batches** of 5000 rows, each one short `DELETE … FOR UPDATE SKIP LOCKED` statement, so a large prune holds no long transaction and two runs don't wait on each other. Ctrl-C (or the 30-minute run timeout) stops between batches; the report says how many were deleted.
- **Allowed in production**, unlike `seed`; a deployment would run it from cron. `request_minutes` and `metric_minutes` aren't targets: the server's hourly rollup keeps them to 48 hours. `server_instances` isn't either: the server deletes servers silent for a day after each rollup and at startup. `sessions`, `admin_sessions`, `rate_limits`, `password_resets`, `mfa_challenges`, `email_block_links`, `ip_blocks` and `workspace_invitations` aren't targets: the server's 15-minute cleanup already deletes them (IP blocks once expired, invitations 30 days after they expire), and MFA setups left unconfirmed for 15 minutes.
- **Adding a target** is two files, and nothing shared changes. The table needs a bigint `id` primary key.

  ```go
  // internal/store/prune_<target>.go: which table, which age column, which old rows to keep
  var PrunableEvents = Prunable{
  	Model: (*eventRecord)(nil),
  	Age:   "created_at",
  	Keep:  func(q *bun.SelectQuery) *bun.SelectQuery { return q.Where("EXISTS (?)", pendingDeliveries) },
  }

  // internal/prune/<target>.go: how the command lists it, and its default age
  func init() {
  	register(Pruner{Name: "events", Description: "…", DefaultAge: 90 * day, Table: store.PrunableEvents, KeptReason: "with pending deliveries"})
  }
  ```

  `store.CountPrunable` and `store.Prune` build the queries with Bun from the declaration: older than the cutoff, minus `Keep` (`WhereGroup(" AND NOT ", Keep)`), deleted by `id` in batches.

## Rules

- Never drop or reset a database other than the local development one, and that one only with `make migrate-fresh`. The exception is an e2e run's local `lewcontext_test_<port>`, which `make serve-test` rebuilds (`migrate fresh`) on every start and refuses to do for any other name or host. Integration tests use their own temporary schema in `lewcontext_test`.
- The local development database is `postgresql://lewcontext:password@127.0.0.1:5432/lewcontext?sslmode=disable`. It runs in Docker: `make db` (`compose.yaml`, PostgreSQL 17 with pgvector).

## Test databases

In the same container, named in the committed `.env.testing` ([../testing.md](../testing.md#configuration)):

| Database | Used by | Created by | Rebuilt |
| --- | --- | --- | --- |
| `lewcontext_test` | Go integration tests, each in a throwaway schema (`dbtest.Open`) | `make db` | never; each test drops only its own schema |
| `lewcontext_test_<port>` (`lewcontext_test_3100` by default) | one e2e run on that port: its server and the spec's setup and cleanup | `make serve-test`, on first use | at every `make serve-test` start, so each run begins empty and a changed create migration applies |

Old `lewcontext_test_<port>` databases stay until that port is used again; drop them by hand if they pile up.
