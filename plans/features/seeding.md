# Seeding with factories and fake data

Status: **implemented 2026-10-30.** End-to-end tests stay self-contained (decision 9). Changes from the design below:

- Factory models are exported from `factory` (`factory.User`, `factory.Project`, `factory.Session`, `factory.AuthAttempt`), so tests can write their own states as closures. Named states exist where they carry meaning: `Expired`, `Idle`, `Exhausted`, `ForAccount`, `ForIP`, `WithPasswordHash`.
- `Sequence(func(i, *T))` sits beside `State` for values that depend on position, like Laravel's `sequence`.
- The demo seeder now uses the factories, and `store.CreateDemoAccount` is removed.

## Decisions

| # | Question | Decision |
| --- | --- | --- |
| 1 | Fake data | [`brianvoe/gofakeit/v7`](https://pkg.go.dev/github.com/brianvoe/gofakeit/v7) (MIT, 300+ generators, seedable) |
| 2 | Factories | Our own small generic `Factory[T]` (`State`, `Make`, `Create`) that bulk-inserts through Bun models |
| 3 | Seeders | A seeder list in `cmd/seed`, run as `make seed only=volume count=500` |
| 4 | Coverage | A factory for **every table** in the app: users, sessions, projects, auth attempts |
| 5 | Tests | Integration tests that need data build it with factories, not hand-written rows or setup through API calls |
| 6 | Randomness | Fake values are random on every run. Counts have fixed defaults and can be overridden with flags / Make parameters |
| 7 | Models | `factory` keeps its own Bun models mapped to the same tables, kept separate from `store` on purpose. A schema change means updating the store model, the factory and the seeders together |
| 8 | Seeders | `demo`, `volume` (projects for one account) and `users` (many fake accounts) as proposed |
| 9 | End-to-end tests | Stay self-contained: sign up through the UI and clean up after themselves |

## Today

`make seed` runs `apps/api/cmd/seed`. It refuses to run in production, needs `DEMO_PASSWORD`, and creates `hello@forma.test` with six hand-written projects in one transaction (`store.CreateDemoAccount`). It changes nothing if the account already exists.

Integration tests get data in two ways:

- **Through the API:** they register users via `POST /api/auth/register` (`signUp`, and the `register` closure). Each call goes through the auth rate limiter (60 per IP per 15 minutes) and an Argon2id hash.
- **By hand:** they insert projects directly (`seedProjects`).

## Why not a library for factories or fixtures

| Option | Verdict |
| --- | --- |
| factory_bot ports (`nauyey/factory`, `akaswenwilk/factory`) | Small, reflection-based, maintenance unclear |
| Bun `dbfixture` (YAML) | Not in Bun v1.3.0 (last shipped in v1.2.18); would pin an old Bun |
| Own `Factory[T]` + gofakeit | ~50 lines, type-checked, uses our Bun models |

## Design

### Package layout

```
apps/api/internal/factory/
  factory.go        Factory[T]: Define, State, Make, Create
  users.go          Users      — name, unique email, one shared Argon2id hash per run
  sessions.go       Sessions   — real token + digest; returns the cookie and CSRF token for tests
  projects.go       Projects   — name, description, status, color, spread timestamps
  auth_attempts.go  AuthAttempts — prefilled rate-limit counters (for limiter tests)
apps/api/internal/seed/
  seeders.go        Seeder interface, registry: demo, volume, users
apps/api/cmd/seed/main.go   flags only (-only, -count, -email, -seed); production guard first
```

Neither `factory` nor `seed` is imported by `cmd/server`, so gofakeit and fake data never reach the API binary. Integration tests import `factory`.

### Factory

```go
// Factory builds T values from fake data; states adjust each value in order.
type Factory[T any] struct {
	define func(f *gofakeit.Faker) T
	states []func(*T)
}

func Define[T any](fn func(f *gofakeit.Faker) T) Factory[T]
func (f Factory[T]) State(fn func(*T)) Factory[T]
func (f Factory[T]) Make(faker *gofakeit.Faker, n int) []T
func (f Factory[T]) Create(ctx context.Context, db bun.IDB, faker *gofakeit.Faker, n int) ([]T, error)
```

`Create` bulk-inserts in batches with `RETURNING *`, so IDs and defaults come back. Passing a `bun.Tx` as `bun.IDB` keeps a whole seeder in one transaction.

Usage:

```go
faker := factory.NewFaker(t) // random; see "Randomness" below
users, _ := factory.Users.Create(ctx, db, faker, 2)
owner, other := users[0], users[1]

factory.Projects.
	State(factory.OwnedBy(owner.ID)).
	State(factory.Status("Completed")).
	Create(ctx, db, faker, 45)

cookie, csrf, _ := factory.SessionFor(ctx, db, owner.ID) // signed-in request without the login endpoint
```

### Records for every table

| Table | Factory | Notes |
| --- | --- | --- |
| `users` | `Users` | Unique emails (`gofakeit` email + sequence so 10 000 users never collide). Names 1–80 characters. All fake users share one password, read from `DEMO_PASSWORD` in seeds or a fixed test value in tests, hashed **once** per run (Argon2id costs ~19 MiB and tens of ms per hash). |
| `sessions` | `Sessions` / `SessionFor` | Generates a token with `auth.Token()`, stores only `auth.Digest(token)`, and returns the raw token as a cookie plus the CSRF token. States: `Expired()`, `Idle()` (`last_seen_at` 31 minutes ago) for expiry tests. |
| `projects` | `Projects` | Within the API rules (name ≤ 100, description ≤ 500, allowlisted status/color). `created_at` within the last 90 days and `updated_at` after it, so date sorts mean something. States: `OwnedBy`, `Status`, `Named`. |
| `auth_attempts` | `AuthAttempts` | Prefilled counters keyed like the limiter (`ip:` / account digests), to test the 429 path without making 60 real requests. States: `Exhausted()`, `Reset()`. |

**Models (decision 7):** `factory` has its own Bun models (`userRow`, `sessionRow`, `projectRow`, `authAttemptRow`) mapped to the same tables. The `store` types stay unexported where they are today, so password hashes still can't reach `User` JSON. Factories return values tests can use: `store.User`, `store.Project`, and a session with its cookie.

Keeping the models separate means a migration has to be matched in three places: the store model, the factory and the seeders. Two checks make sure that happens:

- **Insert check:** integration tests insert every factory's output into the freshly migrated schema, so a new `NOT NULL` column without a default fails the build until the factory sets it.
- **Coverage check:** `TestEveryTableHasAFactory` lists the tables in the migrated schema (`information_schema.tables`, except `bun_migrations*`) and fails if one has no registered factory. A new table can't land without fake data for it.

### Seeders

| Seeder | What it does | Idempotent |
| --- | --- | --- |
| `demo` (default) | Today's demo account and projects, built with the factories but keeping the fixed names | Yes: skips if the account exists |
| `volume` | Adds `-count` (default 120) fake projects to an existing account (`-email`, default the demo account) | No: insert-only, refuses unknown accounts |
| `users` | Creates `-count` fake users (default 10), each with a random 0–60 projects | No: insert-only, emails are unique per run |

Commands: `make seed` (demo), `make seed only=volume count=500`, `make seed only=users count=25`. The Makefile passes `only`, `count` and `email` through as flags.

### Integration tests on factories

- `projects_integration_test.go`: `signUp` and `seedProjects` are replaced with `factory.Users`, `factory.SessionFor` and `factory.Projects`.
- `integration_test.go` keeps registering and logging in through the API where those endpoints are what's under test: registration, login, rotation, logout, session cap and rate limits. Users and sessions it only needs as setup come from factories.
- Each test gets its own faker from `factory.NewFaker(t)`.

### Randomness (decision 6)

- **Values:** random on every run, in seeds and tests alike. `factory.NewFaker` picks a random seed and creates `gofakeit.New(seed)` from it. Tests log the seed only when they fail (`t.Cleanup` + `t.Failed()`). Setting `FAKER_SEED` replays one exact run, so random data never makes a failure impossible to reproduce.
- **Counts:** fixed defaults in each seeder, overridable per run:

| Seeder | Default | Override |
| --- | --- | --- |
| `volume` | 120 projects | `make seed only=volume count=500` |
| `users` | 10 users, 0–60 projects each | `make seed only=users count=25 projects=20` |
| `demo` | 6 named projects | (fixed: it's the documented demo account) |

### Safety (per AGENTS.md)

- The production guard stays first in `cmd/seed`, before connecting.
- Seeders never truncate, drop or delete. Tests still run in their temporary schema.
- No passwords in flags, logs or factory output. Session factories store only digests and hand the raw token to the test.
- `go.sum` is updated and `govulncheck` (dependency audit) is run for the new dependency.

### Tests

- **Unit:** the same seed gives the same values. States apply in order. Factory output passes `validProject`, and user values pass registration validation, so factories can't drift from the API rules.
- **Integration:** `demo` run twice creates one account. `volume` adds exactly `count` rows, only to the target user. A `SessionFor` cookie authenticates `GET /api/auth/me`. An `Exhausted()` auth attempt makes login return 429.

