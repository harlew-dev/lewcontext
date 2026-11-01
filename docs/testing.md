# Test environment

How to set up a local environment, run and build the app, and check it by hand. Automated tests are covered in:

- **[API tests](api/testing.md):** Go unit and PostgreSQL integration tests, factories in tests, `FAKER_SEED`.
- **[UI tests](ui/testing.md):** Vitest, React Testing Library and Playwright end-to-end tests.

## Start from a clean checkout

1. Install Go 1.26+, Node 24+, npm, Docker with Compose, Chromium for browser tests, a C compiler (`gcc`, needed by `go test -race` in `make test`), the PostgreSQL client (`psql`, used by the end-to-end tests to create their database, set up memberships and clean up), and `flock` (reserves an end-to-end run's port; part of util-linux on Linux, `brew install flock` on macOS). On Ubuntu: `sudo apt install gcc postgresql-client`.
2. Start the database: `make db` runs PostgreSQL 17 with pgvector from `compose.yaml` on `127.0.0.1:5432`, and creates the tests' database, `lewcontext_test`, if it's missing. It also starts Mailpit, a local inbox: every email the app sends (reset links, invitations) lands at **`http://localhost:8025`** instead of a real inbox. `.env.example` points `SMTP_HOST` at it. To work without Docker for mail, set `MAIL_DRIVER=log` in `.env` (email goes to `tmp/mail.log`). To run the whole app in Docker instead, `make docker-dev` starts the database, builds the image, migrates, seeds and starts the app on `:3000`. After changing the code, `make docker-app` rebuilds the image and restarts only the app container: no migrate or seed, and the database keeps running. The web app is embedded in the Go binary, so a UI-only change rebuilds the image too; Docker's cache skips the unchanged steps (dependency installs). Run `make docker-migrate` as well if the change added a migration.
3. Copy `.env.example` to `.env`. It contains the local test DB URL: `postgresql://lewcontext:password@127.0.0.1:5432/lewcontext?sslmode=disable`, an `APP_KEY` for local use (required; it seals two-factor secrets), and an `ADMIN_ROUTE_KEY` for local use (the admin dashboard is at `http://localhost:3000/admin/<that key>`; an `.env` without one has admin off). An `.env` without one: run `make app-key`, which adds a new one (it never replaces an existing key). One still saying `ENCRYPTION_KEY`: rename the line to `APP_KEY`, keeping its value. Without a key the server and every `make` command that loads config refuse to start.
4. Run `make install`, `make migrate`, and optionally `make seed` (more fake data: [api/seeding.md](api/seeding.md)). After pulling a schema change, rebuild the local database: `make migrate-fresh CONFIRM=1 && make seed` (migrations are squashed while in development; [api/database.md](api/database.md#migrations)).
5. Start the dev server: `make dev` ([below](#run-the-dev-server)).
6. Open `http://localhost:3000`, sign in with `test@example.com` and the configured `DEMO_PASSWORD` (`password` in `.env.example`) (the Acme workspace), or register a new account.

## Run the dev server

```sh
make dev            # or ./scripts/dev.sh
```

`scripts/dev.sh` first rebuilds the web app the Go server embeds (`scripts/build-web.sh`, the same step as `make build`: both builds, about 20 seconds), so the API never serves an old build. Then it runs the app in one terminal:

| Prefix | Process | Address |
| --- | --- | --- |
| `[build]` | The web builds, before the servers start | |
| `[api]` | The Go API (`go run ./cmd/server`) | `API_ADDR`, `127.0.0.1:8080` |
| `[web]` | Vite (`react-router dev`): serves the pages, reloads on save, forwards `/api` to the API and `/admin`, `/admin-assets` to the admin Vite (websockets too, for reloading) | `http://localhost:3000` |
| `[admin]` | The admin dashboard's Vite (`WEB_BUILD=admin`): its pages, with their files under `/admin-assets/` as in production. Reached only through `:3000`, which checks neither the admin key nor the assets pass: **in development, anyone who can reach `:3000` (it listens on every interface, so other machines can browse it) can load the admin code**, never its data, since the API still checks the key. Before the separate build, the same code was in the app's dev bundle anyway | `127.0.0.1:3002` |

- **Open `http://localhost:3000`**, not `127.0.0.1`: the API only accepts mutations from `APP_ORIGIN`.
- **Ctrl-C stops all three.** If any one exits (for example the API fails to start because PostgreSQL is down), the script stops the others and says so.
- If the build fails, nothing starts; fix the error shown under `[build]`.
- It refuses to start if :8080, :3000 or :3002 is already taken. Stop the other server first (`make api`, `make web`, `make web-admin`, `make serve`, or another `make dev`).
- **Front-end changes** reload in the browser. **Go changes** need a restart: Ctrl-C, then `make dev` again. So do changes to `.env`.
- The script reads `.env` itself, so it works without `make`. It needs `.env` and `make install` first.

To run them in separate terminals instead (separate logs, or restarting only the API): `make api`, `make web` and, for the admin pages, `make web-admin`.

In development there is no Go check before the HTML: signed-out visits to `/app/*` reach the page, whose loader then sends them to login. The CSP and the 404 status for unknown URLs also only come from the Go server, and so do the admin key check on pages and the assets pass (the API still checks the key). Use `make serve` to check those. Each dev server also knows the other's pages, because React Router's type generation, which each runs, writes one set of types for every page; the split into two builds happens only in `react-router build`, which the output check and the e2e suite cover.

## Build and run the production binary

```sh
make build          # bin/api: the Go server with the web app embedded
make serve          # make build, then run bin/api on http://localhost:3000
```

`make build` runs `make web-build` (`scripts/build-web.sh`): `react-router build` (prerendering `/`) and `WEB_BUILD=admin react-router build` (the admin dashboard), checks that no admin code reached the app build (the app build fails on any admin module, and `scripts/check-app-build.sh` reads its output for admin strings; either fails the build), then copies `apps/web/build/client` into `apps/api/internal/web/dist/` and `apps/web/build/admin/client` into `apps/api/internal/web/admin/`, where `go build` embeds them. `make serve` runs the result on `127.0.0.1:3000`, serving the API, the pages and the assets from one process, as in production. Stop `make dev` first; both use :3000.

To run the binary yourself, export the configuration (`DATABASE_URL`, `APP_ORIGIN`, `API_ADDR`, `APP_ENV`) and start `./bin/api`. `APP_ORIGIN` must be the address browsers use. With `APP_ENV=production`, it also needs HTTPS and a database URL with `sslmode=verify-full`, and it refuses to start without the web build embedded.

- **`bin/api` takes no arguments.** It ignores anything after it and starts the server. So `./bin/api migrate` doesn't migrate: it starts a full server, delivery worker included, against `DATABASE_URL`. Migrate with `make migrate`, or `cd apps/api && DATABASE_URL=… go run ./cmd/migrate` for another database. The Docker image is different: it installs every `cmd/*` as its own command, which is why `docker compose run --rm app migrate` works there.
- **:3000 can also be held by Docker.** `make docker-dev` leaves the `app` container listening on :3000. It runs the code from its last `make docker-app`, not your working tree, and it shares the local database. Stop it (`docker compose stop app`) or serve on another port ([ui/testing.md](ui/testing.md#running-several-at-once)).

## Configuration

**Tests don't use `.env`.** They use the committed `.env.testing`: `make test`, `make test-e2e` and `make serve-test` run with an emptied environment plus that file (`scripts/test-env.sh`), `go test` run directly reads the database from it (`dbtest`), and `playwright.config.ts` loads it over whatever is set. It holds only local, throwaway values (a test checks that nothing named like a secret has a value), with the plain `KEY=value` form bash, make, Go and Node all read alike. Edit `.env` for development; tests behave the same on every machine and in CI.

`make` loads `.env` for its subprocesses, and its values win over variables set on the command line. If you run Go or npm commands directly, export the variables yourself (`scripts/dev.sh` does). Vite doesn't read the root `.env` from `apps/web`; its dev proxy uses `API_URL` (default `http://127.0.0.1:8080`), and `APP_NAME` (default `lewcontext`) is baked into the web build as the product name in page titles, the logo and app copy, so rebuild after changing it. Keep `APP_ORIGIN=http://localhost:3000`: Vite forwards the browser's Origin unchanged, and `make serve` listens on :3000. Restart the servers after changing configuration.

## Before claiming a change works

`make test` (which includes the PostgreSQL integration tests and needs `make db`), `make check` (which also checks the docs' relative links and anchors: fix a dead one rather than ignoring it; [checks.md](checks.md)), `make test-e2e` ([ui/testing.md](ui/testing.md); `E2E_PORT=auto` if another run may be going) for user flows, and `make build` for build or composition changes. Report failures instead of skipping them. Other people's runs may share the machine: check what's running before starting a heavy one.

## Manual demo

1. Sign in. You land on your newest workspace's dashboard (`/app/wsp_…/dashboard`); check the overview counts and recent projects.
2. Create a project. The submit button shows a spinner and blocks duplicate submits, and the new project shows first.
3. On Projects, search by name or description, filter by status, sort by clicking column headers, and page through results (`make seed only=volume` gives enough data). The URL keeps the state; reload and back/forward restore it.
4. Click a row to open the project page, edit it, then reload. The change persists.
5. Open Account & security from the account menu, change your name (the account menu shows the new one), sign out other sessions, then sign out of this browser.
6. From the workspace menu, create a second workspace; it opens empty. Switch back from the same menu. In Workspace settings, rename it and copy its ID.
7. Register another account. It asks for the 6-digit code emailed to it (open Mailpit at `http://localhost:8025`; on a phone the code is offered above the keyboard), then starts at onboarding, creates its own workspace and can't see the demo account's (`/app/<their wsp_…>/dashboard` shows "This page has wandered off."). Signing up again with the same email shows the same code page, while Mailpit gets "You already have an account" instead of a code.
8. Email verification for older accounts: `make seed only=users` makes some unverified ones (password `DEMO_PASSWORD`). Sign in as one: Account & security says "Not verified" with Verify email. With `EMAIL_VERIFICATION_EXISTING=required` in `.env` (restart the API), signing in as another goes to "You must verify your email to continue" first ([api/auth.md](api/auth.md#email-verification)).
9. Signed out, open `/`: the homepage offers "Log in". Open `/app/settings`: you land on login, and return to settings after signing in. Back on `/`, the button now says "Dashboard".
10. Hide the sidebar with the button beside the workspace menu; it slides out and the toggle glides into the top bar. Reload: it stays hidden, with no flash of the sidebar. Show it again.
11. At a 390px viewport, navigation, search, paging (Previous/Next) and editing remain usable; the slide-over sidebar closes from the button beside the workspace menu.

12. Blocking an address: invite a new address from Members, open the invitation in Mailpit and follow "Block this address" at its foot. Block it, then invite it again from another workspace: the invitation shows Sent, but nothing reaches Mailpit; signing up with it shows the code page but no code arrives. `make admin cmd=blocks` lists it, and so does the admin dashboard's Email blocks page; Unblock from the same link (or `make admin cmd=unblock email=… CONFIRM=1`, or as an operator from the row's ⋯ menu on Email blocks, with a reason and then a code) and the next invitation arrives ([api/auth.md](api/auth.md#blocking-an-email-address), [api/admin.md](api/admin.md#email-blocks)).
13. The admin dashboard: `make admin cmd=grant email=test@example.com CONFIRM=1`, then open `http://localhost:3000/admin/<ADMIN_ROUTE_KEY from .env>`. Sign in with the demo account's password; without two-factor authentication it asks you to set it up (the QR code hidden until revealed), then shows Users and Workspaces. `/admin/<anything else>` is the ordinary 404. Admins lists who holds admin, with their role and sessions (`role=operator` on `grant` makes an operator, who sees Unblock on Email blocks). Sign out from the account menu ([api/admin.md](api/admin.md)). Under `make serve`, the admin pages' scripts load from `/admin-assets/`; opening one of those URLs in a private window (no assets pass) shows the 404 page.
14. Admin actions: `make admin cmd=grant email=test@example.com role=operator CONFIRM=1`, sign in at the admin door, and open one of the demo workspace's fake members on Users. Suspend account (a reason, then a code from your authenticator): their sessions end, and signing in as them (`make seed`'s fakes have unusable passwords, so try it with an account you signed up) shows "This account has been suspended. Contact support." only after the right password. Unsuspend. On IP blocks, block `203.0.113.0/24` for an hour, then unblock it; your own address (`127.0.0.1`) is refused on the field. Every action shows in Events under Admin ([api/admin.md](api/admin.md#actions)).

## Failure checks

Run `make api` and `make web` in separate terminals, then stop only the API (`make dev` stops them all together). A signed-in page reload should show the "A brief pause." retry screen. When the API answers but fails (a 500), the error notice and that screen add "Reference: req_…": the API's terminal shows the same ID on the failure's log line and the request's access line (`[api]` prints one line per request; [api/observability.md](api/observability.md)). During a client refresh, the UI keeps the loaded data and shows an error with Retry. Restart the API to recover. Expired sessions redirect to sign-in. Slow navigation shows a top progress bar, lists show skeletons, and mutations show spinners with submission disabled.

## Volume check for the Events page

The admin Events page's list and charts are checked at volume with fake history, on a development database only:

```sh
make seed only=events count=1000000     # about 2½ minutes
```

Then run the queries with `EXPLAIN (ANALYZE, BUFFERS)` in `psql` (the SQL is `IndexAdminEvents` and `SummarizeAdminEvents`). At a million events (2026-11-01, PostgreSQL 17): the default view's list 0.6 ms, its counts 88 ms (an index-only scan of `events_created_idx`), its failed checks 0.3 s; 7 days' counts 1.2 s and failed checks 0.3 s; a group (`auth.mfa.*`, 7 days) 29 ms; `outcome=failed` 2 ms; all time with rare names 0.1–30 ms (the planner picks the name index). Each should stay under 2 s; one that doesn't needs an index or a rollup, not a slower page.

## Migration reversal

See [api/database.md](api/database.md#migrations). Rolling back runs destructive down SQL: don't do it without explicit authorization and a backup.
