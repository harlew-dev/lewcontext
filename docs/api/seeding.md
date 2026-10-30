# Fake data: factories and seeders

Factories build fake records for every table, and seeders use them to fill a development database. Tests use the same factories. Values are random [gofakeit v7](https://pkg.go.dev/github.com/brianvoe/gofakeit/v7) data.

## Seeding a database

```sh
make seed                                   # demo account hello@forma.test + 6 projects (skipped if it exists)
make seed only=volume count=500             # projects for the demo account
make seed only=volume email=you@example.com # …or for another existing account
make seed only=users count=25 projects=20   # 25 fake accounts, 0–20 projects each
```

| Seeder | Default count | Idempotent |
| --- | --- | --- |
| `demo` | 6 named projects | Yes |
| `volume` | 120 projects | No (insert-only; refuses unknown accounts) |
| `users` | 10 users, up to 60 projects each | No (insert-only) |

- Accounts created by `demo` and `users` sign in with `DEMO_PASSWORD` from `.env`.
- Each seeder runs in one transaction and prints its fake data seed. Seeders never delete or truncate. Production is refused before connecting.
- Counts are 0–100 000; 0 means the default. Run `cd apps/api && go run ./cmd/seed -h` to list seeders.
- **`make` always uses the `DATABASE_URL` from `.env`.** To seed another database or schema, run `cd apps/api && DATABASE_URL=… go run ./cmd/seed -only volume`.

## Factories (`internal/factory`)

| Factory | Model | Notes |
| --- | --- | --- |
| `Users` | `User` | Unique lowercase emails at `example.test`; password `factory.DefaultPassword` (one shared hash per process) |
| `Sessions` | `Session` | Real random token, only the digest stored; `Cookie()`, `CSRFToken`. Needs `UserID`; use `SessionFor` |
| `Projects` | `Project` | Valid name, description, status and color; created in the last 90 days. Needs `UserID`; use `ProjectsFor` |
| `AuthAttempts` | `AuthAttempt` | Counter in the current 15-minute window, keyed like the IP limiter |

```go
f := factory.TestFaker(t) // or factory.NewFaker() outside tests
user, _ := factory.Users.CreateOne(ctx, db, f)
session, _ := factory.SessionFor(ctx, db, f, user.ID) // session.Cookie(), session.CSRFToken

factory.ProjectsFor(user.ID).
	State(func(p *factory.Project) { p.Status = "Completed" }).    // every value
	Sequence(func(i int, p *factory.Project) { p.Name = fmt.Sprint("P", i) }). // by position
	Create(ctx, db, f, 45)                                          // batched INSERT … RETURNING
```

- **Methods:** `Make` (build only), `Create`, `CreateOne`, `State`, `Sequence`. Factories are immutable, so each state returns a copy.
- **Named states:** `Expired` and `Idle` (sessions), `ForAccount`, `ForIP` and `Exhausted(limit)` (auth attempts), `WithPasswordHash` (users).
- **Randomness:** fresh every run. When a test fails it logs `fake data seed N`, and `FAKER_SEED=N` replays it. Seeders print the seed too.

## Rules

- **Every table has a factory, with its own Bun model** (separate from `store`'s on purpose). `TestEveryTableHasAFactory` fails for a table without one. Every factory's output is inserted into a freshly migrated schema, so a new required column also needs a factory value.
- Factory values must pass API validation (`TestFactoriesMatchValidation`).
- When a migration changes a table, update the store model, the factory and any seeder in the same change.
- The API server binary doesn't include `factory` or `seed`.
