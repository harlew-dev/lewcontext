# Fake data: factories and seeders

Factories build fake records for every table, and seeders use them to fill a development database. Tests use the same factories. Values are random [gofakeit v7](https://pkg.go.dev/github.com/brianvoe/gofakeit/v7) data.

## Seeding a database

```sh
make seed                                   # demo account hello@forma.test, its "Acme" workspace + 6 projects (skipped if it exists)
make seed only=volume count=500             # projects in the demo account's newest workspace
make seed only=volume email=you@example.com # …or another existing account's (it needs a workspace)
make seed only=users count=25 projects=20   # 25 fake accounts, each owning a workspace with 0–20 projects
```

| Seeder | Default count | Idempotent |
| --- | --- | --- |
| `demo` | The "Acme" workspace and 6 named projects | Yes |
| `volume` | 120 projects | No (insert-only; refuses unknown accounts and accounts without a workspace) |
| `users` | 10 users, each owning a workspace with up to 60 projects | No (insert-only) |

- Accounts created by `demo` and `users` sign in with `DEMO_PASSWORD` from `.env`.
- Each seeder runs in one transaction and prints its fake data seed. Seeders never delete or truncate. Production is refused before connecting.
- Counts are 0–100 000; 0 means the default. Run `cd apps/api && go run ./cmd/seed -h` to list seeders.
- After a schema change, rebuild the local database first: `make migrate-fresh CONFIRM=1 && make seed` ([database.md](database.md#migrations)).
- **`make` always uses the `DATABASE_URL` from `.env`.** To seed another database or schema, run `cd apps/api && DATABASE_URL=… go run ./cmd/seed -only volume`.

## Factories (`internal/factory`)

| Factory | Model | Notes |
| --- | --- | --- |
| `Users` | `User` | Unique lowercase emails at `example.test`; password `factory.DefaultPassword` (one shared hash per process) |
| `Sessions` | `Session` | Real random token, only the digest stored; `Cookie()`, `CSRFToken`. Needs `UserID`; use `SessionFor` |
| `Workspaces` | `Workspace` | Fake company names (≤ 80 characters), created in the last year. Use `WorkspaceFor` to add an owner |
| `WorkspaceUsers` | `WorkspaceUser` | A membership with a random role. Needs `UserID` and `WorkspaceID`; use `MemberOf` |
| `Projects` | `Project` | Valid name, description, status and color; created in the last 90 days. Needs `WorkspaceID`; use `ProjectsIn` |
| `AuthAttempts` | `AuthAttempt` | Counter in the current 15-minute window, keyed like the IP limiter |

```go
f := factory.TestFaker(t) // or factory.NewFaker() outside tests
user, _ := factory.Users.CreateOne(ctx, db, f)
session, _ := factory.SessionFor(ctx, db, f, user.ID) // session.Cookie(), session.CSRFToken
workspace, _ := factory.WorkspaceFor(ctx, db, f, user.ID) // owned by user; factory.Named("Acme") sets the name
factory.MemberOf(ctx, db, f, other.ID, workspace.ID, "member")

factory.ProjectsIn(workspace.ID).
	State(func(p *factory.Project) { p.Status = "Completed" }).    // every value
	Sequence(func(i int, p *factory.Project) { p.Name = fmt.Sprint("P", i) }). // by position
	Create(ctx, db, f, 45)                                          // batched INSERT … RETURNING
```

- **Methods:** `Make` (build only), `Create`, `CreateOne`, `State`, `Sequence`. Factories are immutable, so each state returns a copy.
- **Named states:** `Expired` and `Idle` (sessions), `ForAccount`, `ForIP` and `Exhausted(limit)` (auth attempts), `WithPasswordHash` (users), `Named` (workspaces).
- **Public IDs** are left empty and filled by PostgreSQL's default; `Create` reads them back (`workspace.PublicID`).
- **Randomness:** fresh every run. When a test fails it logs `fake data seed N`, and `FAKER_SEED=N` replays it. Seeders print the seed too.

## Rules

- **Every table has a factory, with its own Bun model** (separate from `store`'s on purpose). `TestEveryTableHasAFactory` fails for a table without one. Every factory's output is inserted into a freshly migrated schema, so a new required column also needs a factory value.
- Factory values must pass API validation (`TestFactoriesMatchValidation`).
- When a migration changes a table, update the store model, the factory and any seeder in the same change.
- The API server binary doesn't include `factory` or `seed`.
