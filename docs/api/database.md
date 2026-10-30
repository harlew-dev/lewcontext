# Database

PostgreSQL 15+ through Bun (`uptrace/bun`, pgdialect) over the pgx `database/sql` driver. The connection pool holds 10 connections (`internal/database/database.go`).

## Tables

| Table | Purpose | Store model |
| --- | --- | --- |
| `users` | Accounts (`password_hash` never leaves the store) | `User` (+ unexported `account` with the hash) |
| `sessions` | Session token digests, CSRF tokens, expiry | unexported `sessionRecord` |
| `projects` | Projects owned by a user (`ON DELETE CASCADE`) | `Project` |
| `auth_attempts` | Rate-limit counters | unexported `authAttempt` |
| `bun_migrations`, `bun_migration_locks` | Migration history | (Bun) |

## Queries

- All SQL lives in `internal/store`. Handlers never build queries.
- **Scope by user:** every query on user data has `user_id = ?` with the ID from the session. Updates use `WHERE id = ? AND user_id = ?` and return `sql.ErrNoRows` when the row isn't the caller's, which handlers turn into 404.
- **Placeholders only:** values always go through Bun `?` placeholders, and SQL text is never built from input. Dynamic `ORDER BY` uses a fixed map of SQL strings (see [pagination.md](pagination.md)).
- Request contexts carry a 10 s timeout. Pass `r.Context()` into every store call.

## Migrations

- **Files:** `apps/api/migrations/<version>_<name>.tx.up.sql` plus `.tx.down.sql`, created with `make migration name=add_tasks`.
- **Running:** `make migrate` applies pending files, each in its own transaction, as one group, holding a PostgreSQL advisory lock. `make migrate-status` lists them. The API never migrates on startup.
- **Rollback:** `make migrate-rollback` only previews. `CONFIRM=1` runs the down SQL, which can drop data, so it needs explicit authorization.
- **Never edit an applied migration.** Add a new pair instead.
- **Legacy history:** databases from the previous runner have their `schema_migrations` copied into `bun_migrations` once, and the old table renamed `schema_migrations_legacy`.

## Adding a table

1. `make migration name=create_tasks`: the table with `user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE`, `CHECK` constraints matching validation, and an index for the main query.
2. A store model and queries taking `userID`.
3. **A factory in `internal/factory`** with its own model. `TestEveryTableHasAFactory` fails without it ([seeding.md](seeding.md)).
4. Integration tests, including another user's access ([testing.md](testing.md)).

## Rules

- Never drop or reset databases or existing data. Integration tests use their own temporary schema.
- The local test database is `postgresql://lewcontext:password@127.0.0.1:5432/lewcontext?sslmode=disable`. Reuse the running PostgreSQL (`lewagent-services status`).
