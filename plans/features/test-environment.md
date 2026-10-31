# Test environment (`.env.testing`)

Status: **decisions confirmed 2026-10-31**, ready to implement. Comes before [email delivery](email-delivery.md), which adds Mailpit's settings to `.env.testing` and has the end-to-end tests read mail from it.

## Why

Tests currently run on whatever environment they're started from:

- **Make passes the development `.env` to every target.** The [Makefile](../../Makefile) runs `include .env` then `export`, so `make test` and `make test-e2e` see the development `DATABASE_URL`, `MAIL_*`, `CLIENT_IP_HEADER` and so on. If `.env` ever pointed at a shared or staging database, tests would run against it.
- **Integration tests skip quietly.** They need `TEST_DATABASE_URL` typed on the command line. Without it, `make test` passes having skipped every database test, and AGENTS.md has to warn that this isn't database verification.
- **End-to-end tests share the development app.** They use the development database (their users show up in your app until cleanup), port :3000 (so `make dev` or the Docker `app` container must be stopped first), and the development mailbox. A second server's delivery worker on the same database takes the emails the tests are waiting for ([ui/testing.md](../../docs/ui/testing.md#against-a-server-of-your-own)).
- **Starting the e2e server is manual:** `RATE_LIMITS=off make serve` in one terminal, `TEST_DATABASE_URL=… make test-e2e` in another.

Laravel solves this with test settings that override the app's (`phpunit.xml`, `.env.testing`). This plan gives the project the same: **one committed `.env.testing` that every test reads, however it's started, and separate test databases: `lewcontext_test` for Go tests, and a fresh `lewcontext_test_<port>` for each e2e run.** `make test` then runs everything, including integration tests, with nothing to type.

## Design

### `.env.testing`

Committed at the repository root:

```sh
# Test settings, committed on purpose. Used by make test, make test-e2e,
# go test and Playwright, which ignore .env. Only local, throwaway values:
# never put a real secret here (TestEnvTestingHoldsNoSecrets checks).
# Plain KEY=value lines only: no quotes, no spaces around =, no export.
APP_ENV=development
# The e2e server's port. APP_ORIGIN, API_ADDR and its own database,
# lewcontext_test_<E2E_PORT>, are derived from it (scripts/test-env.sh and
# playwright.config.ts). Override per run: make test-e2e E2E_PORT=auto.
E2E_PORT=3100
# The Go tests' database: each test makes a throwaway schema in it.
DATABASE_URL=postgresql://lewcontext:password@127.0.0.1:5432/lewcontext_test?sslmode=disable
RATE_LIMITS=off
MAIL_DRIVER=file
MAIL_FILE=tmp/mail-test.log
```

- **Safe to commit.** These are the same local Docker credentials already committed in `compose.yaml`, `.env.example` and AGENTS.md, and PostgreSQL listens only on `127.0.0.1`.
- **`.gitignore`** gains `!.env.testing` (it currently ignores `.env.*` except `.env.example`).
- **One format that every reader understands.** `KEY=value`, `#` comments and blank lines. The Go loader, Node's `util.parseEnv`, bash's `source` and Make all read that the same way. Anything else (quotes, `export`, `$` expansion, a repeated key) is refused by the Go loader, and a test runs it over the committed file.
- **`TEST_DATABASE_URL` goes away.** Tests use `DATABASE_URL` from this file, the same name the server uses, as in Laravel.
- **Port 3100** so the e2e server runs beside `make dev`, `make serve` or the Docker `app` container on :3000. A second run at the same time picks another port with `E2E_PORT` ([below](#running-several-at-once-e2e_port)).
- **Its own mailbox file** (`tmp/mail-test.log`) until [email delivery](email-delivery.md) switches it to Mailpit.

### The test database

All in the same PostgreSQL container, which every run shares at its fixed port:

| Database | Used by | Created by | Rebuilt |
| --- | --- | --- | --- |
| `lewcontext_test` | Go integration tests, each in a throwaway schema | `make db` | never; each test drops only its own schema |
| `lewcontext_test_<port>` (`lewcontext_test_3100` by default) | one e2e run on that port: the server and the spec's cleanup | `make serve-test`, on first use | at every `make serve-test` start (`migrate fresh`) |

- **A unique database per e2e run**, named after its port, which the run has reserved ([below](#running-several-at-once-e2e_port)). So no two running e2e servers ever share a database, and `migrate fresh` never touches the Go tests' database.
- **`make db` creates `lewcontext_test` if it's missing**, after the container is healthy, with `docker compose exec postgres` running `psql` and `createdb`. Running it again does nothing. This covers existing volumes, which a `docker-entrypoint-initdb.d` script wouldn't (it only runs on a new volume), so there is one mechanism, not two.
- **`make serve-test` creates its `lewcontext_test_<port>`** the same way when it's missing, with `createdb` from the PostgreSQL client tools, which the e2e tests already need for `psql`.
- **CI** creates `lewcontext_test` through the service container's `POSTGRES_DB`, with the credentials changed to match `.env.testing` (`password` instead of `test-only-password`; it's a throwaway service container). `make serve-test` creates `lewcontext_test_3100` there as it does locally.

### Every way of starting tests loads it

| Started by | How it gets `.env.testing` |
| --- | --- |
| `make test`, `make test-e2e`, `make serve-test` | `scripts/test-env.sh <command>` runs the command with a **clean environment**: `env -i` keeping only `PATH`, `HOME`, `TMPDIR`, `CI`, `CHROMIUM_PATH` and Go's own variables (`GO*`, `CC`), then `.env.testing`. Nothing from `.env` reaches a test. |
| `go test` directly (VS Code, a single package) | `dbtest.Open` reads `DATABASE_URL` from the file itself (below) and **never from the process environment**, so a `DATABASE_URL` exported for development can't be used by mistake. |
| `npx playwright test` directly | `playwright.config.ts` reads the file with `util.parseEnv` and assigns every key to `process.env`, **replacing** what's there (Laravel's `<env force="true">`). The spec and the `webServer` command see the same values. |

### Go: `dbtest`

```go
// internal/database/dbtest/env.go
// testEnv reads .env.testing from the repository root (found by walking up
// from the working directory). Tests use it instead of the process
// environment, so a development DATABASE_URL is never used by mistake.
func testEnv(t *testing.T) map[string]string

// localTestDatabase refuses a URL whose host isn't local or whose database
// name isn't a test database (*_test or *_test_<port>), so an edited
// .env.testing can't aim the tests' schema drops at a real database.
func localTestDatabase(raw string) error
```

- `dbtest.Open(t)` uses `testEnv(t)["DATABASE_URL"]`, checks it with `localTestDatabase`, then works as today (a unique schema, dropped at cleanup).
- **Allowed hosts:** `127.0.0.1`, `::1`, `localhost`, and `postgres` (the compose service name, for running inside a container). The database name must end in `_test`, or `_test_<digits>` (an e2e run's database, which the spec's cleanup also uses).
- **No skipping.** A missing file, a refused URL or an unreachable database **fails** the test, with the fix in the message ("start it with `make db`"). That's decision 3 from our discussion: `make test` needs the database.

### Make targets

```make
test:            # Go (unit and integration) and Vitest, with .env.testing
	./scripts/test-env.sh sh -c 'cd apps/api && go test -race ./... && cd ../web && npm test'
serve-test: build # the e2e server on E2E_PORT, against a freshly migrated lewcontext_test_<port>
	./scripts/test-env.sh ./scripts/serve-test.sh
test-e2e:        # preflight (ports, services), then Playwright, which starts make serve-test
	./scripts/test-env.sh ./scripts/e2e-preflight.sh sh -c 'cd apps/web && npm run test:e2e'
```

- **`test-integration` is removed.** `make test` already runs the integration tests.
- **`scripts/serve-test.sh`** reserves the port (below), checks that the derived `DATABASE_URL` names a `*_test_<digits>` database on a local host (it refuses `lewcontext_test` itself, the Go tests' database), creates the database if it's missing, runs `go run ./cmd/migrate fresh -confirm` against it, then starts `./bin/api`.
- **The run's database is rebuilt on every e2e start** (decision 6). Migrations are squashed during development, so after a create migration changes, a plain `migrate` would leave it stale. A fresh schema also means each run starts empty.

### Running several at once (`E2E_PORT`)

Agents (and people) often start the e2e suite while another run is going, and today they have to find their own workaround: a free port, a second database, the right mailbox. Now each run gets **its own port and database from one variable**, and learns within a second, before anything is built, if it can't have them.

| `E2E_PORT` | Server | Database |
| --- | --- | --- |
| unset or `3100` | `http://localhost:3100` | `lewcontext_test_3100` |
| a port, e.g. `3101` | `http://localhost:3101` | `lewcontext_test_3101`, created on first use |
| `auto` | the first free port from 3101 up | `lewcontext_test_<port>` |

```sh
make test-e2e                  # :3100
make test-e2e E2E_PORT=3101    # a second run beside it
make test-e2e E2E_PORT=auto    # pick a free one (what AGENTS.md tells agents to use)
```

- **`E2E_PORT` is the one value a caller can override in `.env.testing`.** `test-env.sh` keeps the caller's value (or the file's 3100) and derives `API_ADDR`, `APP_ORIGIN` and `DATABASE_URL` (`lewcontext_test_<port>`) for the e2e server and the spec. `playwright.config.ts` does the same for a direct `npx playwright test`. Everything else stays forced.
- **Shared on purpose, at fixed ports:** the PostgreSQL container (with a unique database per run) and Mailpit, once [email delivery](email-delivery.md) ships (each run finds its mail by its own unique recipient addresses). Go and Vitest runs already don't collide: Go tests use throwaway schemas.
- **Preflight** (`scripts/e2e-preflight.sh`, the first step of `make test-e2e`, which takes under a second):
  1. **Services:** PostgreSQL answers at the URL's host and port (and Mailpit, once email delivery ships). Otherwise it stops with "The test database isn't running: start it with `make db`."
  2. **Reserves the port** with an exclusive lock on `/tmp/lewcontext-e2e-<port>.lock` (`flock -n`), held until the run ends, and checks nothing else listens on it. The lock covers the whole machine, so it works across git worktrees, and it's released automatically when the run ends or crashes.
  3. **A taken port stops the run at once**, with a free port already worked out:

     ```
     Port 3100 is in use: another e2e run (or a server) has it.
     Run this one beside it on a free port:
       make test-e2e E2E_PORT=3104
     Or let it pick one: make test-e2e E2E_PORT=auto
     Each port gets its own server and database: docs/ui/testing.md#running-several-at-once
     ```
  4. **With `auto`**, it takes the first port from 3101 up whose lock it can get and where nothing is listening. Getting the lock is the reservation, so two agents starting together get different ports.
  5. **Reports what the run uses**, so the caller knows where to look:

     ```
     E2E run: app http://localhost:3104 · database lewcontext_test_3104 · mail http://127.0.0.1:8025
     ```
- **Why a lock, and not only a port check:** the build takes a while, and `serve-test` rebuilds the database before the server binds the port. With only a port check, two runs starting together would both pass, and the second's `migrate fresh` would wipe the first's database mid-run. The lock is taken before the build and the rebuild.
- **`make serve-test` on its own** (a server for checking by hand) runs the same reservation and prints the same message. Under `make test-e2e` it uses the lock its parent already holds (passed down as `E2E_LOCK_HELD`).
- **`flock`** comes with util-linux on Linux and on CI's Ubuntu runner. On macOS it's `brew install flock`. The preflight says so if it's missing.

### Playwright

- `playwright.config.ts` loads `.env.testing` (above) and gains a `webServer` entry: `command: "make serve-test"` from the repository root, `url: APP_ORIGIN + "/auth/login"`, a 3-minute timeout for the build, and `reuseExistingServer` only when `E2E_REUSE=1`. By default every run starts its own server. With parallel runs, a server already on the port may be another run's, built from another worktree's code, and quietly testing it would test the wrong code. To iterate against a `make serve-test` you started in another terminal, run `make test-e2e E2E_REUSE=1`, which also skips the port reservation.
- **`workspace.spec.ts`:** `TEST_DATABASE_URL` → `DATABASE_URL`. The three `test.skip(!process.env.TEST_DATABASE_URL)` lines go, because the database is always there. The mailbox path comes from `MAIL_FILE`, resolved against the repository root.
- Cleanup in `afterAll` stays. The database is rebuilt on the next server start anyway, but a reused server isn't restarted between runs.

### CI

The workflow's `env:` block loses `DATABASE_URL`, `TEST_DATABASE_URL`, `APP_ORIGIN` and `APP_ENV`, which now come from `.env.testing`. The "Start application" step goes, because Playwright's `webServer` starts it. The steps become `make install`, `make test check build`, install Chromium, `make test-e2e`. The server log on failure comes from Playwright's `webServer` output.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| Credentials in a committed file | Only the local Docker credentials, which are already committed and only reachable on `127.0.0.1`. `TestEnvTestingHoldsNoSecrets` fails if any key named like `*PASSWORD*`, `*SECRET*`, `*TOKEN*` or `*_KEY` has a value, so a real secret can't sneak in. The `DATABASE_URL` it does allow must pass `localTestDatabase`. |
| Tests or `migrate fresh` aimed at a real database | Three independent checks: the file is committed (reviewed like code); `localTestDatabase` in Go and the same check in `serve-test.sh` need a local host and a `_test` or `_test_<port>` name; `Fresh` still refuses `APP_ENV=production`. `E2E_PORT` only changes the port and the database's suffix, never the host. |
| Two runs wiping each other's database | Each port has its own database, and the port is reserved by a lock before the build and the rebuild, so only one run at a time can rebuild a given database. |
| Development settings leaking into tests | `test-env.sh` starts from an empty environment. `dbtest` never reads the process environment. Playwright overwrites every key it loads. |
| Test users mixed into development data | They live in the run's `lewcontext_test_<port>`, never in `lewcontext`. |
| The e2e worker taking the development server's mail, or the reverse | Separate databases, so separate delivery queues. Separate mailbox files until Mailpit (where tests search by their unique recipients). |

## Tests

- **Go unit (`dbtest`):**
  - the parser reads `KEY=value`, comments and blank lines, and refuses quotes, `export`, spaces around `=`, `$` and repeated keys, naming the line;
  - `localTestDatabase` accepts local hosts and the `postgres` service with a `_test` or `_test_3101` name, and refuses another host, a name without `_test`, `_test_` followed by anything but digits, and a URL that doesn't parse;
  - `TestEnvTestingHoldsNoSecrets`: the committed `.env.testing` parses, its `DATABASE_URL` passes `localTestDatabase`, and no secret-like key has a value.
- **Existing integration tests** run unchanged through `dbtest.Open`. They now run in every `make test`.
- **CI** proves the whole path: a clean runner with only the committed file.
- **By hand:**
  - `make test` with `make db` stopped fails with the "start it with `make db`" message;
  - `make test-e2e` with `make dev` running on :3000 passes;
  - a `serve-test.sh` run with `DATABASE_URL` pointing at `lewcontext` is refused before migrating;
  - a second `make test-e2e` while one is running stops within a second with the message and a free port, and passes when rerun with that port, with both runs green;
  - two `make test-e2e E2E_PORT=auto` started together get different ports and databases, and both pass;
  - killing a run releases its port for the next one.

## Docs

- `docs/testing.md` (setup: `make db` creates the test database; "Before claiming a change works"; Configuration: `.env.testing` vs `.env`);
- `docs/api/testing.md` (Running, `dbtest.Open`, no skipping);
- `docs/ui/testing.md` (`make test-e2e` starting the server, port 3100, the mailbox file; "Against a server of your own" is replaced by "Running several at once": `E2E_PORT`, `auto`, the preflight message, `E2E_REUSE`);
- `docs/api/database.md` (the test database, and that it is rebuilt on each e2e start);
- `docs/api/README.md` (environment variables: `.env.testing`);
- `README.md` and AGENTS.md (Commands and validation: drop `TEST_DATABASE_URL` and `make test-integration`, and tell agents to run `make test-e2e E2E_PORT=auto` and use the URLs it reports; Database: local `*_test_<port>` databases may be rebuilt by `make serve-test`);
- `.github/workflows/ci.yml`. Implemented plans that mention `TEST_DATABASE_URL` stay as historical records.

## Decisions

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | File name | `.env.testing`, committed. **Confirmed.** |
| 2 | Separate databases | `lewcontext_test` for Go tests (created by `make db`), and a unique `lewcontext_test_<port>` per e2e run (created by `make serve-test`). **Confirmed.** |
| 3 | `make test` without the database | Fails, with how to start it. **Confirmed.** |
| 4 | Variable name | Drop `TEST_DATABASE_URL`; tests use `DATABASE_URL` from `.env.testing`. |
| 5 | E2E port | 3100 by default, beside `make dev` and Docker on :3000. **Confirmed**, with easy changes for parallel runs: `E2E_PORT=<port>` or `auto` gives a run its own port and database; a preflight reserves the port and stops a clashing run within a second, suggesting a free port. |
| 6 | Rebuilding the test database | `make serve-test` runs `migrate fresh` on it at every start. AGENTS.md's "don't reset databases" rule gains this one exception, limited to local `*_test_<port>` databases. **Confirmed.** |
| 7 | Who starts the e2e server | Playwright's `webServer` (`make serve-test`). **Confirmed**, with one change since: it reuses a running server only with `E2E_REUSE=1`, because with parallel runs the server on a port may be another run's. |
| 11 | Default for agents | Plain `make test-e2e` fails fast on a clash; AGENTS.md tells agents to always pass `E2E_PORT=auto`. People get a predictable :3100; agents never collide. **Confirmed.** |
| 12 | Lock location | `/tmp/lewcontext-e2e-<port>.lock`, the same for every checkout and worktree on the machine (not `$TMPDIR`, which can differ per session). **Confirmed.** |
| 13 | Service ports | PostgreSQL and Mailpit stay on fixed ports, shared by every run; the unique database per run is what keeps runs apart. **Confirmed.** |
| 8 | Clean environment for `make` test targets | Yes, `scripts/test-env.sh` with a short list of kept variables. Without it, `.env` values such as `CLIENT_IP_HEADER` reach tests that call `config.Load`. |
| 9 | Guard on the database URL | Local host (or `postgres`) and a test database name: `_test` or `_test_<port>` in Go, only `_test_<port>` in `serve-test.sh` (which rebuilds it). |
| 10 | An `APP_ENV=testing` value | No. Config only distinguishes production from everything else, and the test server should behave like development. |

## Not included

- **Two runs in the same checkout at once.** They share `bin/api` and the web build, so one build can replace the other's mid-run. Give each agent its own git worktree. Separate worktrees with `E2E_PORT` are fully supported.
- **Changing the PostgreSQL or Mailpit ports** (decision 13). They're shared services, one per machine, at the fixed ports in `.env.testing`. Runs are kept apart by their databases, not by separate services.
- **Dropping old per-port databases.** `lewcontext_test_<port>` databases stay until that port is used again, and are rebuilt then. A cleanup target can come later if they pile up.
- A `.env.testing.local` for personal overrides. Edit `.env` for development instead; tests are meant to run identically everywhere.
- Running Vitest against `.env.testing`. It reads no environment, but runs under `test-env.sh` anyway as part of `make test`.
