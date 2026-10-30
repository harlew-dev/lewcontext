# Seeding with factories and fake data

Status: **research and proposal, nothing implemented yet**

## Today

`make seed` runs `apps/api/cmd/seed`. It refuses to run in production, needs `DEMO_PASSWORD`, and creates `hello@forma.test` with six hand-written projects in one transaction (`store.CreateDemoAccount`). It is idempotent: it changes nothing if the account exists. Six projects never reach a second page, so pagination, search and sorting can't be exercised with realistic volume. Integration tests insert rows by hand (`seedProjects` in `httpapi/projects_integration_test.go`).

## Laravel → Go

| Laravel | Go equivalent | Notes |
| --- | --- | --- |
| `php artisan db:seed` | `make seed` → `go run ./cmd/seed` | Already exists; grows subcommands/flags |
| `--class=ProjectSeeder` | `make seed only=projects` → `-only projects` flag | Seeders registered by name |
| Seeder classes | A small `Seeder` interface (`Name()`, `Run(ctx, *store.Store) error`) | One file per seeder |
| Model factories (`definition`, `state`, `count`, `make`/`create`) | A ~50-line generic `Factory[T]` over Bun models | No mature Laravel-style Go library; the factory_bot ports (`nauyey/factory`, `akaswenwilk/factory`) are small, reflection-based and of unclear maintenance |
| Faker (`fake()->sentence()`) | [`brianvoe/gofakeit/v7`](https://pkg.go.dev/github.com/brianvoe/gofakeit/v7) | MIT, widely used, 300+ generators, seedable for reproducible data |
| YAML fixtures | Bun's `dbfixture` | **Not available**: it isn't in Bun v1.3.0 (last shipped in v1.2.18), so using it would pin an old Bun |

**Recommendation:** gofakeit for values, plus our own generic factory and seeder registry. That avoids reflection-heavy factory libraries, stays type-checked, and reuses the Bun models and the store, so all writes keep going through one layer.

## Proposed shape

```
apps/api/internal/seed/
  factory.go        // Factory[T]: Make, Create, State, Count
  projects.go       // ProjectFactory (name, description, status, color, timestamps)
  users.go          // UserFactory (hashes a password once, reuses the hash)
  seeders.go        // Seeder interface + registry: demo, volume
apps/api/cmd/seed/main.go   // flags only: -only, -count, -email, -seed; production guard stays
```

```go
// Factory builds T values from fake data; states adjust them.
type Factory[T any] struct {
	define func(f *gofakeit.Faker) T
	states []func(*T)
}

func (f Factory[T]) State(fn func(*T)) Factory[T]                 // e.g. Completed(), OwnedBy(id)
func (f Factory[T]) Make(faker *gofakeit.Faker, n int) []T          // build without saving
func (f Factory[T]) Create(ctx context.Context, db bun.IDB, faker *gofakeit.Faker, n int) ([]T, error) // bulk insert
```

```go
// usage
projects := seed.Projects.State(seed.OwnedBy(user.ID)).State(seed.Status("Completed"))
_, err := projects.Create(ctx, tx, faker, 50)
```

- **Deterministic by default:** `gofakeit.New(seed)` with a fixed seed (`-seed` flag), so a seeded DB looks the same on every machine and screenshots and bug reports are reproducible.
- **Bulk inserts** in one transaction (`bun.IDB` accepts `bun.Tx`). A 1 000-project seed is one statement per batch, not 1 000 round trips.
- **Timestamps spread over time** (created in the last 90 days, updated after created) so "Last updated" and date sorts are meaningful.
- **Valid by construction:** values respect the API rules (name 1–100 characters, description ≤ 500, allowlisted status and color), and a unit test runs factory output through `validProject` to keep them in step.

### Seeders

| Seeder | What it does | Idempotent |
| --- | --- | --- |
| `demo` (default) | Today's demo account + projects, moved onto the factory with fixed names | Yes: skips if the account exists |
| `volume` | Adds `-count` (default 120) fake projects to an existing account named by `-email` (default the demo account) for paging/search/sort work | No: refuses unless the account exists, prints what it added |

`make seed` keeps today's behaviour. `make seed only=volume count=500` adds volume.

### Safety (per AGENTS.md)

- The production guard stays at the top of `cmd/seed`, before any connection.
- Seeders never truncate or delete. `volume` only inserts rows owned by the named account.
- No passwords in flags or logs. User factories take `DEMO_PASSWORD` from the environment and hash with Argon2id once per run.
- The `internal/seed` package isn't imported by `cmd/server`, so fake data can't reach the API binary. Integration tests may import it.

### Tests

- Unit: factory determinism (same seed → same values), states applied in order, output passes `validProject`.
- Integration (temp schema): `demo` twice creates one account, `volume` adds exactly `count` rows to the right user only.
- Replace `seedProjects` in `projects_integration_test.go` with the factory once it exists.

## Open questions

1. Is **gofakeit** OK as a new dependency? It needs a `go.sum` update and a dependency audit, per AGENTS.md.
2. **Fixed seed** (reproducible) or random each run by default?
3. Should integration tests use the factories too, or keep test data hand-written and explicit?
4. **`volume` target:** add to the demo account, or create N extra fake users each with projects (useful later for testing cross-user isolation at scale)?
5. Any other seeders you want now (e.g. an "empty account", or a project at each status)?
