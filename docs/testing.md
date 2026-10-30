# Test environment

## Start from a clean checkout

1. Install Go 1.26+, Node 24+, npm, PostgreSQL 15+, and Chromium for browser tests.
2. Ensure the database is running. On Lewagent: `lewagent-services status`.
3. Copy `.env.example` to `.env`. It contains the user-supplied local test DB URL: `postgresql://lewcontext:password@127.0.0.1:5432/lewcontext?sslmode=disable`.
4. Run `make install`, `make migrate`, and optionally `make seed`.
5. Run `make api` and `make web` in separate terminals.
6. Open `http://localhost:3000`, sign in with `hello@forma.test` / the configured `DEMO_PASSWORD`, or register a new account.

`make` loads `.env` for its subprocesses. If running Go/Next commands directly, export these variables yourself. Next does not read the root `.env` from `apps/web`; the Make targets pass it through explicitly. Use `API_URL=http://127.0.0.1:8080` and `APP_ORIGIN=http://localhost:3000` locally. Restart both servers after changing configuration.

## Automated checks

- `make test`: Argon2id verification, salts, cookie flags, random tokens, input validation, CSRF origin rules, JSON parsing, frontend API responses, and project interactions. DB integration tests explicitly skip if `TEST_DATABASE_URL` is absent.
- `TEST_DATABASE_URL='postgresql://lewcontext:password@127.0.0.1:5432/lewcontext?sslmode=disable' make test-integration`: real SQL/migrations/auth/session/ownership tests. Creates a unique `forma_test_<timestamp>` schema, uses it exclusively, and drops only that schema at cleanup. Requires schema creation permissions in the **test** database. Runs the race detector.
- `make check`: gofmt, Go vet, strict TypeScript, ESLint, formatting.
- `make build`: backend compile and optimized Next build. No database connection is needed during build because private pages render on request.
- `TEST_DATABASE_URL='...' make test-e2e`: tests against the **running** application. Default browser executable `/usr/bin/chromium`; override with `CHROMIUM_PATH`. Creates a unique `e2e-<timestamp>@forma.test` user, then deletes only that user with `psql` using the provided test DB URL. Cascading FKs remove its sessions/projects. Repeated runs use distinct accounts but may encounter the intentional 60 attempts per 15 minutes IP rate limit.
- `cd apps/web && npm audit`: dependency audit including test tools.
- `cd apps/api && go run golang.org/x/vuln/cmd/govulncheck@latest ./...`: Go dependency/stdlib vulnerability check.

## Manual demo

1. Log in and inspect the overview metrics and six seeded projects.
2. Create a project; verify the submit button shows a spinner and prevents duplicate submission.
3. Filter by status and search by name or description. Check the empty-result state.
4. Open the project, update its status, save, then reload. The database-backed change persists.
5. Open Account & security, sign out other sessions, then sign out of this browser.
6. Register another account. It starts empty and cannot view the demo account's projects.
7. At a 390px viewport, navigation, search, project editing, and sign-out remain usable.

## Failure checks

Stop only the API process you started. An authenticated page reload should return HTTP 503 with a retry screen. During client refresh the UI retains already-loaded data and shows an error with Retry. Restart the API to recover. Expired sessions redirect to sign-in. Slow navigation shows a top progress bar; project fetching has skeletons, and mutations show spinners with disabled submission.

## Migration reversal

The migration runner intentionally applies forward migrations only. `001_initial.down.sql` documents the reverse migration and **drops application data**. Do not run it without explicit authorization and a backup. Never edit an applied migration; add a new numbered `.up.sql` and corresponding `.down.sql`. The runner records applied filenames in `schema_migrations`.
