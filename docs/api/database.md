# Database

PostgreSQL 15+ through Bun (`uptrace/bun`, pgdialect) over the pgx `database/sql` driver. The connection pool holds 10 connections (`internal/database/database.go`).

## Tables

| Table | Purpose | Store model |
| --- | --- | --- |
| `users` | Accounts (`password_hash` never leaves the store) | `User` (+ unexported `account` with the hash) |
| `sessions` | Session token digests, CSRF tokens, expiry | unexported `sessionRecord` |
| `workspaces` | Workspaces (name) | `Workspace` (+ the member's `Role`, read from `workspace_user`) |
| `workspace_user` | Memberships: user, workspace, `role` (`owner`, `admin`, `member`); unique per pair | unexported `membership` |
| `projects` | Projects in a workspace (`ON DELETE CASCADE`) | `Project` |
| `auth_attempts` | Rate-limit counters | unexported `authAttempt` |
| `events` | What happened: name, actor, user, workspace, `jsonb` payload, user agent ([events.md](events.md)) | unexported `eventRecord` |
| `event_deliveries` | One row per event and listener: attempts, `next_attempt_at` (also the claim lease), `delivered_at`, `failed_at`, `last_error` | unexported `deliveryRecord` |
| `bun_migrations`, `bun_migration_locks` | Migration history | (Bun) |

## Public IDs

`users`, `workspaces`, `projects` and `events` have `public_id text NOT NULL UNIQUE DEFAULT public_id('usr' | 'wsp' | 'prj' | 'evt')`, with a `CHECK` on the format. The plpgsql `public_id(prefix)` function (migration 001) turns bytes from the built-in `gen_random_uuid()` into 14 base62 characters, skipping the UUID's fixed version/variant bytes and bytes ≥ 248 so every character is equally likely (about 83 random bits). No extension is needed.

- Every insert gets one, from the API, factories or seeders. Models map it as `PublicID string` with `bun:",nullzero"`, so Bun sends `DEFAULT`, and read it back with `Returning("*")`. Go never generates IDs.
- JSON uses the public ID as `"id"`; the bigint `id` is `json:"-"` and stays internal (keys, joins, sort tie-breaks).
- `httpapi.validPublicID(prefix, id)` checks the shape before any query.

## Queries

- All SQL lives in `internal/store`. Handlers never build queries.
- **Scope by membership:** workspaces are only read through a `workspace_user` row for the session's user (`memberWorkspaces`). Workspace data (projects) is scoped by the workspace's internal ID from `requireWorkspace`: `WHERE p.public_id = ? AND p.workspace_id = ?`, returning `sql.ErrNoRows` (404) when the row isn't in that workspace.
- **Placeholders only:** values always go through Bun `?` placeholders, and SQL text is never built from input. Dynamic `ORDER BY` uses a fixed map of SQL strings (see [pagination.md](pagination.md)).
- Request contexts carry a 10 s timeout. Pass `r.Context()` into every store call.

## Migrations

- **Files:** `apps/api/migrations/<version>_<name>.tx.up.sql` plus `.tx.down.sql`, one create migration per table, in dependency order: `001_create_public_id_function`, `002_create_users`, `003_create_sessions`, `004_create_workspaces`, `005_create_projects`, `006_create_auth_attempts`, `007_create_events`, `008_create_event_deliveries`.
- **Squashed while in development.** There is no database whose data must be kept, so a table changes by **editing its create migration**, not by adding an alter migration. Then rebuild the local database: `make migrate-fresh CONFIRM=1 && make seed`. Once a database holds data that must be kept, migrations become append-only again.
- **`make migrate-fresh`** lists the tables and row counts it would drop. `CONFIRM=1` drops the current schema (`DROP SCHEMA … CASCADE`), recreates it and migrates. It is refused when `APP_ENV=production`. Only run it on the local development database. Other checkouts sharing that database need it too after pulling a schema change: Bun only runs migrations it hasn't recorded, so an edited file won't re-run.
- **Running:** `make migrate` applies pending files, each in its own transaction, as one group, holding a PostgreSQL advisory lock. `make migrate-status` lists them. The API never migrates on startup.
- **Rollback:** `make migrate-rollback` only previews. `CONFIRM=1` runs the down SQL, which drops tables.

## Adding a table

1. A create migration (`make migration name=create_tasks`, or a numbered pair after the tables it references): the table with `public_id … DEFAULT public_id('tsk')` if users open or search its records, `workspace_id bigint NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE` for workspace data, `CHECK` constraints matching validation, and an index for the main query.
2. A store model (`PublicID` with `nullzero`, internal `ID` as `json:"-"`) and queries taking the workspace ID.
3. **A factory in `internal/factory`** with its own model. `TestEveryTableHasAFactory` fails without it ([seeding.md](seeding.md)).
4. Integration tests, including another user's access ([testing.md](testing.md)).

## Rules

- Never drop or reset a database other than the local development one, and that one only with `make migrate-fresh`. Integration tests use their own temporary schema.
- The local test database is `postgresql://lewcontext:password@127.0.0.1:5432/lewcontext?sslmode=disable`. Reuse the running PostgreSQL (`lewagent-services status`).
