# Test environment

How to set up a local environment, run and build the app, and check it by hand. Automated tests are covered in:

- **[API tests](api/testing.md):** Go unit and PostgreSQL integration tests, factories in tests, `FAKER_SEED`.
- **[UI tests](ui/testing.md):** Vitest, React Testing Library and Playwright end-to-end tests.

## Start from a clean checkout

1. Install Go 1.26+, Node 24+, npm, Docker with Compose, Chromium for browser tests, a C compiler (`gcc`, needed by `go test -race` in `make test`), and the PostgreSQL client (`psql`, used by the end-to-end tests' membership setup and cleanup). On Ubuntu: `sudo apt install gcc postgresql-client`.
2. Start the database: `make db` runs PostgreSQL 17 with pgvector from `compose.yaml` on `127.0.0.1:5432`. To run the whole app in Docker instead, `make docker-dev` starts the database, builds the image, migrates, seeds and starts the app on `:3000`.
3. Copy `.env.example` to `.env`. It contains the local test DB URL: `postgresql://lewcontext:password@127.0.0.1:5432/lewcontext?sslmode=disable`.
4. Run `make install`, `make migrate`, and optionally `make seed` (more fake data: [api/seeding.md](api/seeding.md)). After pulling a schema change, rebuild the local database: `make migrate-fresh CONFIRM=1 && make seed` (migrations are squashed while in development; [api/database.md](api/database.md#migrations)).
5. Start the dev server: `make dev` ([below](#run-the-dev-server)).
6. Open `http://localhost:3000`, sign in with `test@example.com` and the configured `DEMO_PASSWORD` (`password` in `.env.example`) (the Acme workspace), or register a new account.

## Run the dev server

```sh
make dev            # or ./scripts/dev.sh
```

`scripts/dev.sh` first rebuilds the web app the Go server embeds (`scripts/build-web.sh`, the same step as `make build`, about 10 seconds), so the API never serves an old build. Then it runs both halves of the app in one terminal:

| Prefix | Process | Address |
| --- | --- | --- |
| `[build]` | The web build, before the servers start | |
| `[api]` | The Go API (`go run ./cmd/server`) | `API_ADDR`, `127.0.0.1:8080` |
| `[web]` | Vite (`react-router dev`): serves the pages, reloads on save, forwards `/api` to the API | `http://localhost:3000` |

- **Open `http://localhost:3000`**, not `127.0.0.1`: the API only accepts mutations from `APP_ORIGIN`.
- **Ctrl-C stops both.** If either one exits (for example the API fails to start because PostgreSQL is down), the script stops the other and says so.
- If the build fails, nothing starts; fix the error shown under `[build]`.
- It refuses to start if :8080 or :3000 is already taken. Stop the other server first (`make api`, `make web`, `make serve`, or another `make dev`).
- **Front-end changes** reload in the browser. **Go changes** need a restart: Ctrl-C, then `make dev` again. So do changes to `.env`.
- The script reads `.env` itself, so it works without `make`. It needs `.env` and `make install` first.

To run them in separate terminals instead (separate logs, or restarting only the API): `make api` in one and `make web` in the other.

In development there is no Go check before the HTML: signed-out visits to `/app/*` reach the page, whose loader then sends them to login. The CSP and the 404 status for unknown URLs also only come from the Go server. Use `make serve` to check those.

## Build and run the production binary

```sh
make build          # bin/api: the Go server with the web app embedded
make serve          # make build, then run bin/api on http://localhost:3000
```

`make build` runs `make web-build` (`scripts/build-web.sh`): `react-router build` (prerendering `/`), then copies `apps/web/build/client` into `apps/api/internal/web/dist/`, where `go build` embeds it. `make serve` runs the result on `127.0.0.1:3000`, serving the API, the pages and the assets from one process, as in production. Stop `make dev` first; both use :3000.

To run the binary yourself, export the configuration (`DATABASE_URL`, `APP_ORIGIN`, `API_ADDR`, `APP_ENV`) and start `./bin/api`. `APP_ORIGIN` must be the address browsers use. With `APP_ENV=production`, it also needs HTTPS and a database URL with `sslmode=verify-full`, and it refuses to start without the web build embedded.

## Configuration

`make` loads `.env` for its subprocesses, and its values win over variables set on the command line. If you run Go or npm commands directly, export the variables yourself (`scripts/dev.sh` does). Vite doesn't read the root `.env` from `apps/web`; its dev proxy uses `API_URL` (default `http://127.0.0.1:8080`), and `APP_NAME` (default `lewcontext`) is baked into the web build as the product name in page titles, the logo and app copy, so rebuild after changing it. Keep `APP_ORIGIN=http://localhost:3000`: Vite forwards the browser's Origin unchanged, and `make serve` listens on :3000. Restart the servers after changing configuration.

## Before claiming a change works

`make test`, `make check`, `make test-integration` (with `TEST_DATABASE_URL`) for anything touching SQL, auth, middleware or migrations, `make test-e2e` (against `make serve`) for user flows, and `make build` for build or composition changes. Report failures instead of skipping them.

## Manual demo

1. Sign in. You land on your newest workspace's dashboard (`/app/wsp_…/dashboard`); check the overview counts and recent projects.
2. Create a project. The submit button shows a spinner and blocks duplicate submits, and the new project shows first.
3. On Projects, search by name or description, filter by status, sort by clicking column headers, and page through results (`make seed only=volume` gives enough data). The URL keeps the state; reload and back/forward restore it.
4. Click a row to open the project page, edit it, then reload. The change persists.
5. Open Account & security from the account menu, change your name (the account menu shows the new one), sign out other sessions, then sign out of this browser.
6. From the workspace menu, create a second workspace; it opens empty. Switch back from the same menu. In Workspace settings, rename it and copy its ID.
7. Register another account. It starts at onboarding, creates its own workspace and can't see the demo account's (`/app/<their wsp_…>/dashboard` shows "This page has wandered off.").
8. Signed out, open `/`: the homepage offers "Log in". Open `/app/settings`: you land on login, and return to settings after signing in. Back on `/`, the button now says "Dashboard".
9. Hide the sidebar with the button beside the workspace menu; it slides out and the toggle glides into the top bar. Reload: it stays hidden, with no flash of the sidebar. Show it again.
10. At a 390px viewport, navigation, search, paging (Previous/Next) and editing remain usable; the slide-over sidebar closes from the button beside the workspace menu.

## Failure checks

Run `make api` and `make web` in separate terminals, then stop only the API (`make dev` stops both together). A signed-in page reload should show the "A brief pause." retry screen. During a client refresh, the UI keeps the loaded data and shows an error with Retry. Restart the API to recover. Expired sessions redirect to sign-in. Slow navigation shows a top progress bar, lists show skeletons, and mutations show spinners with submission disabled.

## Migration reversal

See [api/database.md](api/database.md#migrations). Rolling back runs destructive down SQL: don't do it without explicit authorization and a backup.
