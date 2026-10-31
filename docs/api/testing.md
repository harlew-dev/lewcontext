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
- To test rate limits, pre-fill counters (`factory.AuthAttempts` with `ForAccount`/`ForIP`, or `ForLimit(name, subject)` for any limit, and `Exhausted`) in a test whose requests haven't created the counter yet.
- Reset links: `factory.PasswordResets` with `ResetFor(userID)` (and `ExpiredReset`) keeps the raw `Token`. To test the listener end to end, `resetServer(t)` registers it with a `mailbox` driver and returns `deliver()`, which runs the worker once; `tokenIn` reads the link from a sent message.
- A failing test prints `fake data seed N`; rerun it with `FAKER_SEED=N`.

## What to cover

- **Validation:** table-driven unit tests for parsing (e.g. `TestPageRequest`, `TestProjectFilter`).
- **Security boundaries:** 401 without a session, 403 without CSRF or with a bad Origin or a role without the permission, a non-member's 404 on every workspace route, another workspace's data returning 404 or empty results, and no numeric `id` in responses.
- **Persistence:** results come back from the database, ordering is stable across pages, and filters combine.
- Don't add tests that only mirror trivial code.

## Notable tests

| Test | Checks |
| --- | --- |
| `TestAuthenticationAndProjectIsolation` | Register (no workspace yet), public IDs, cookies, digests, CSRF, creating a workspace, cross-user and cross-workspace 404, old routes gone, rotation, logout, idle/expiry, session cap, account limit |
| `TestWorkspaceList`, `TestWorkspaceMembershipAndRoles` | Newest-first paging, list isolation, each role's `permissions` and `assignableRoles` in the show, list, rename and create responses, outsiders and malformed IDs 404, name validation |
| `TestMemberList`, `TestChangeMemberRole`, `TestRemoveMember`, `TestLeaveWorkspace` (`members_integration_test.go`) | Role-then-name order, paging, search with `%` and `_`, `addedAt`; admins limited to member ↔ viewer; the last owner can't step down or leave; bad roles 422, extra fields 400, other workspaces' users 404; removing yourself 422; removed users get 404; each event's payload |
| `TestOwnersDemotingEachOther` | Two owners demoting each other at once, five times: one succeeds, one owner always remains (fails without the workspace row lock) |
| `TestWorkspacePermissionMatrix` | Every workspace route × every role: allowed roles succeed, others 403, outsiders 404. Fails when a route is registered but missing from `workspaceRoutesMatrix` |
| `TestWorkspaceMutationsNeedAPermission`, `TestRequirePermission`, `internal/auth` tests | An unguarded `ws` mutation panics; 403 for roles without the permission. The matrix: owner has every declared permission (read from the source), viewer none, each role a subset of the one above, `ParseRole`, `AssignableRoles` |
| `TestAccountRateLimit`, `TestIPRateLimit` | 429 and `Retry-After` from pre-filled counters |
| `TestPasswordReset*` (`password_reset_integration_test.go`) | Identical answers for known and unknown emails; the listener mails only real accounts and stores only the digest; check, reset, every session revoked, sign-in with the new password; a link works once (also under concurrency), dies on expiry, a newer request or a password change; malformed tokens; each reset limit (unknown emails count, 503 for everyone, a refused request spends no later limit); Origin and `MAIL_DRIVER=off` |
| `internal/mail` tests | Every template renders; `<script>` and `<h1>` in a name come out escaped, `javascript:` links are filtered; bad recipients, unknown templates and wrong data send nothing; no `text/template`; the mailbox file is `0600` and appends; the listener (unknown email, digest matches the link, rerun replaces it, stale request dropped) |
| `internal/config` tests | `MAIL_DRIVER` defaults, and `file` refused in production |
| `TestProjectPagination` | Every sort across pages, past-the-end, filters, literal wildcards, isolation, bad parameters |
| `TestEveryTableHasAFactory` | Each migrated table has a factory, and every factory inserts |
| `TestFactoriesMatchValidation` | Fake values pass API validation |
| `internal/database` tests | Migrations up/down, idempotency, `public_id()` uniqueness and format, the `events.name` CHECK (two or three segments), `Fresh` (refused in production, rebuilds the schema) |
| `internal/seed` tests | Demo runs once with its Acme workspace; `volume` and `users` insert exactly what they say |
| `TestSignedInPagesNeedASessionBeforeAnyHTML`, `TestAPIAndPagesStayApart` | `/app/*` without a session (none, unknown, idle, expired) → 302 to login with `next` and no HTML; signed in gets the shell; unknown API paths never get the shell; pages answer GET/HEAD only; no web build serves the API only |
| `internal/web` tests | Per-file CSP hashes (as browsers compute them), fallback vs 404, cache headers, security headers and HSTS, dot-files hidden, a build without the fallback refused |
