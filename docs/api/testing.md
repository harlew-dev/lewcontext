# API tests

Go `testing`, `httptest`, the race detector and real PostgreSQL.

## Running

```sh
make test                 # unit tests; integration tests skip without TEST_DATABASE_URL
TEST_DATABASE_URL='postgresql://lewcontext:password@127.0.0.1:5432/lewcontext?sslmode=disable' make test-integration
make check                # gofmt + go vet (and the web checks)
cd apps/api && go run golang.org/x/vuln/cmd/govulncheck@latest ./...   # dependency audit
```

A skipped integration test is not database verification. Set `TEST_DATABASE_URL` whenever you touch auth, SQL, migrations, middleware or authorization.

## Integration tests

- `dbtest.Open(t)` creates a unique `forma_test_<timestamp>` schema, pins `search_path` to it, and drops only that schema at cleanup. Existing data is never touched.
- `integrationServer(t)` (in `httpapi`) migrates the schema (and checks a second migrate is a no-op), then returns a `*Server` and the DB.
- `call(t, handler, method, path, body, csrf, cookie, wantStatus)` sends a request with the trusted Origin and JSON content type, and fails on an unexpected status.
- `signedIn(t, db, faker)` returns a fake user's `factory.Session`. Use `session.UserID`, `session.Cookie()` and `session.CSRFToken`.

## Test data comes from factories

```go
f := factory.TestFaker(t)
session := signedIn(t, db, f)
factory.ProjectsFor(session.UserID).Create(ctx, db, f, 45)
```

- Set only the fields a test asserts on (with `State` or `Sequence`) and leave the rest random. See [seeding.md](seeding.md).
- Don't write rows by hand, and don't set up data through API calls. The exception is when that endpoint is what's being tested: registration, login, rotation, logout, the session cap and the limiters go through the real endpoints.
- To test rate limits, pre-fill counters (`factory.AuthAttempts` with `ForAccount`/`ForIP` and `Exhausted`) in a test whose requests haven't created the counter yet.
- A failing test prints `fake data seed N`; rerun it with `FAKER_SEED=N`.

## What to cover

- **Validation:** table-driven unit tests for parsing (e.g. `TestPageRequest`, `TestProjectFilter`).
- **Security boundaries:** 401 without a session, 403 without CSRF or with a bad Origin, and another user's data returning 404 or empty results.
- **Persistence:** results come back from the database, ordering is stable across pages, and filters combine.
- Don't add tests that only mirror trivial code.

## Notable tests

| Test | Checks |
| --- | --- |
| `TestAuthenticationAndProjectIsolation` | Register, cookies, digests, CSRF, cross-user 404, rotation, logout, idle/expiry, session cap, account limit |
| `TestAccountRateLimit`, `TestIPRateLimit` | 429 and `Retry-After` from pre-filled counters |
| `TestProjectPagination` | Every sort across pages, past-the-end, filters, literal wildcards, isolation, bad parameters |
| `TestEveryTableHasAFactory` | Each migrated table has a factory, and every factory inserts |
| `TestFactoriesMatchValidation` | Fake values pass API validation |
| `internal/database` tests | Migrations up/down, idempotency, adoption of legacy history |
| `internal/seed` tests | Demo runs once; `volume` and `users` insert exactly what they say |
