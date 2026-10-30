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

- `dbtest.Open(t)` creates a unique `lewcontext_test_<timestamp>` schema, pins `search_path` to it, and drops only that schema at cleanup. Existing data is never touched.
- `integrationServer(t)` (in `httpapi`) migrates the schema (and checks a second migrate is a no-op), then returns a `*Server` and the DB.
- `call(t, handler, method, path, body, csrf, cookie, wantStatus)` sends a request with the trusted Origin and JSON content type, and fails on an unexpected status.
- `signedIn(t, db, faker)` returns a fake user's `factory.Session`. Use `session.UserID`, `session.Cookie()` and `session.CSRFToken`.
- `must(call)(t)` returns a two-result call's value or fails the test: `user := must(factory.Users.CreateOne(ctx, db, f))(t)`. Go only lets a two-result call fill a whole argument list, hence the trailing `(t)`.
- `idOf(t, db, "users" | "workspaces" | "projects", publicID)` and `emailOf(t, db, userID)` look records up instead of one-off SQL. Events have `eventsNamed` and `onlyEvent` (`events_integration_test.go`).
- `ownedWorkspace(t, db, faker, userID)` creates a workspace the user owns; `workspacePath(workspace.PublicID, "/projects")` builds its API paths.

## Test data comes from factories

```go
f := factory.TestFaker(t)
session := signedIn(t, db, f)
workspace := ownedWorkspace(t, db, f, session.UserID)
factory.ProjectsIn(workspace.ID).Create(ctx, db, f, 45)
```

- Set only the fields a test asserts on (with `State` or `Sequence`) and leave the rest random. See [seeding.md](seeding.md).
- Don't write rows by hand, and don't set up data through API calls. The exception is when that endpoint is what's being tested: registration, login, rotation, logout, the session cap and the limiters go through the real endpoints.
- To test rate limits, pre-fill counters (`factory.AuthAttempts` with `ForAccount`/`ForIP` and `Exhausted`) in a test whose requests haven't created the counter yet.
- A failing test prints `fake data seed N`; rerun it with `FAKER_SEED=N`.

## What to cover

- **Validation:** table-driven unit tests for parsing (e.g. `TestPageRequest`, `TestProjectFilter`).
- **Security boundaries:** 401 without a session, 403 without CSRF or with a bad Origin or too low a role, a non-member's 404 on every workspace route, another workspace's data returning 404 or empty results, and no numeric `id` in responses.
- **Persistence:** results come back from the database, ordering is stable across pages, and filters combine.
- Don't add tests that only mirror trivial code.

## Notable tests

| Test | Checks |
| --- | --- |
| `TestAuthenticationAndProjectIsolation` | Register (no workspace yet), public IDs, cookies, digests, CSRF, creating a workspace, cross-user and cross-workspace 404, old routes gone, rotation, logout, idle/expiry, session cap, account limit |
| `TestWorkspaceList`, `TestWorkspaceMembershipAndRoles` | Newest-first paging, list isolation, roles on rename, members on projects, outsiders and malformed IDs 404, name validation |
| `TestAccountRateLimit`, `TestIPRateLimit` | 429 and `Retry-After` from pre-filled counters |
| `TestProjectPagination` | Every sort across pages, past-the-end, filters, literal wildcards, isolation, bad parameters |
| `TestEveryTableHasAFactory` | Each migrated table has a factory, and every factory inserts |
| `TestFactoriesMatchValidation` | Fake values pass API validation |
| `internal/database` tests | Migrations up/down, idempotency, `public_id()` uniqueness and format, `Fresh` (refused in production, rebuilds the schema) |
| `internal/seed` tests | Demo runs once with its Acme workspace; `volume` and `users` insert exactly what they say |
| `TestSignedInPagesNeedASessionBeforeAnyHTML`, `TestAPIAndPagesStayApart` | `/app/*` without a session (none, unknown, idle, expired) → 302 to login with `next` and no HTML; signed in gets the shell; unknown API paths never get the shell; pages answer GET/HEAD only; no web build serves the API only |
| `internal/web` tests | Per-file CSP hashes (as browsers compute them), fallback vs 404, cache headers, security headers and HSTS, dot-files hidden, a build without the fallback refused |
