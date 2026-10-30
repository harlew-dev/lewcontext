# Test environment

How to set up a local environment and check the app by hand. Automated tests are covered in:

- **[API tests](api/testing.md):** Go unit and PostgreSQL integration tests, factories in tests, `FAKER_SEED`.
- **[UI tests](ui/testing.md):** Vitest, React Testing Library and Playwright end-to-end tests.

## Start from a clean checkout

1. Install Go 1.26+, Node 24+, npm, PostgreSQL 15+, and Chromium for browser tests.
2. Make sure the database is running. On Lewagent: `lewagent-services status`.
3. Copy `.env.example` to `.env`. It contains the local test DB URL: `postgresql://lewcontext:password@127.0.0.1:5432/lewcontext?sslmode=disable`.
4. Run `make install`, `make migrate`, and optionally `make seed` (more fake data: [api/seeding.md](api/seeding.md)).
5. Run `make api` and `make web` in separate terminals.
6. Open `http://localhost:3000`, sign in with `hello@forma.test` and the configured `DEMO_PASSWORD`, or register a new account.

`make` loads `.env` for its subprocesses, and its values win over variables set on the command line. If you run Go or Next commands directly, export the variables yourself. Next doesn't read the root `.env` from `apps/web`; the Make targets pass it through. Locally, use `API_URL=http://127.0.0.1:8080` and `APP_ORIGIN=http://localhost:3000`. Restart both servers after changing configuration.

## Before claiming a change works

`make test`, `make check`, `make test-integration` (with `TEST_DATABASE_URL`) for anything touching SQL, auth, middleware or migrations, `make test-e2e` for user flows, and `make build` for build or composition changes. Report failures instead of skipping them.

## Manual demo

1. Sign in and check the overview counts and recent projects.
2. Create a project. The submit button shows a spinner and blocks duplicate submits, and the new project shows first.
3. On Projects, search by name or description, filter by status, sort by clicking column headers, and page through results (`make seed only=volume` gives enough data). The URL keeps the state; reload and back/forward restore it.
4. Click a row to open the project page, edit it, then reload. The change persists.
5. Open Account & security from the account menu, sign out other sessions, then sign out of this browser.
6. Register another account. It starts empty and can't see the demo account's projects (`/app/projects/<their id>` is a 404).
7. At a 390px viewport, navigation, search, paging (Previous/Next) and editing remain usable.

## Failure checks

Stop only the API process you started. A signed-in page reload should return HTTP 503 with a retry screen. During a client refresh, the UI keeps the loaded data and shows an error with Retry. Restart the API to recover. Expired sessions redirect to sign-in. Slow navigation shows a top progress bar, lists show skeletons, and mutations show spinners with submission disabled.

## Migration reversal

See [api/database.md](api/database.md#migrations). Rolling back runs destructive down SQL: don't do it without explicit authorization and a backup.
