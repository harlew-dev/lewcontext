# Rename the app to lewdocs

Status: **proposed 2026-11-02**; decisions confirmed 2026-11-02.

## Why

The product is now called **lewdocs**, but the app, its emails, its module path, its cookies, its local database and every current doc still say **lewcontext**. A half-rename leaves two names in the codebase, so the old one keeps getting copied into new code, docs, test fixtures and tooling.

This is a rename, not a feature: no schema change, no new endpoint, no change to behaviour. The only intended visible differences are the product name, the export filename, the authenticator-app issuer shown on new MFA enrolments, and the fact that existing browser sessions end because the cookie names change.

## What the current name touches

`git grep` finds `lewcontext` in 357 tracked files (about 1,160 occurrences). Most are Go import paths (`lewcontext/api/internal/...`) and are mechanical. The rest fall into these groups:

| Area | Current names | Where |
| --- | --- | --- |
| Product name | `APP_NAME=lewcontext`; web fallback in `lib/app.ts`; dev banner; image build arg | `config.go`, `lib/app.ts`, `scripts/dev.sh`, `Dockerfile`, `compose.yaml`, `.env.example`, `.env.testing`, CI, docs |
| Go module | `lewcontext/api` | `apps/api/go.mod` and every Go import |
| Web package | `lewcontext-web` | `apps/web/package.json`, `package-lock.json` |
| Image and deployment | `lewcontext`, `lewcontext:local`, `/etc/lewcontext/prod.env`, `lewcontext_owner`, `lewcontext_app` | `compose.yaml`, `docs/deployment.md`, `docs/incident-response.md` |
| Local database | user and database `lewcontext`, tests' `lewcontext_test`, e2e `lewcontext_test_<port>` | `compose.yaml`, `Makefile`, `.env.example`, `.env.testing`, CI, `dbtest`, e2e scripts and docs |
| Compose project and volume | project and volume `lewcontext`, `lewcontext_postgres` | derived from `compose.yaml` and the checkout directory |
| Cookies | `lewcontext_session`, `__Host-lewcontext_session`, `lewcontext_mfa`, `__Host-lewcontext_mfa`, `lewcontext_admin`, `__Host-lewcontext_admin`, `lewcontext_admin_mfa`, `lewcontext_registration`, `lewcontext_admin_assets`, `__Secure-lewcontext_admin_assets` | `auth/token.go`, `auth/admin.go`, `auth/admin_assets.go`, `auth/email_code.go`, tests |
| Browser storage | `lewcontext:sidebar` | `lib/use-docked-sidebar.ts` |
| Export filename | `lewcontext-export-<date>.json` | `account/data_export.go`, `pages/app/settings/index.tsx`, tests, docs |
| Test addresses and fixtures | `@lewcontext.test`, `http://lewcontext.invalid`, literal `otpauth://totp/lewcontext:...` fixtures | env files, e2e specs, Vitest fixtures |
| Outbound labels | `User-Agent: lewcontext-alerts`, `lewcontext-integration-test/1.0`, `App: "lewcontext"` in test mailers | `alerts/webhook.go`, `httpapi/apitest`, mail tests |
| Assets and history | `lewcontext.png`; old names in implemented plans and findings notes | root README, `plans/features/implemented/`, `plans/features/admin/{findings,read}/` |
| Docs | current README, AGENTS.md, `docs/*` | all current docs |

Two groups are **not** ordinary names and are deliberately excluded below: the HKDF labels in `internal/auth/encrypt.go` and the advisory-lock prefixes in `internal/database/lock.go`.

## Design

### 1. Product name

- `APP_NAME` default becomes `lewdocs` everywhere it is defaulted: `config.Load`, `lib/app.ts`, `scripts/dev.sh`, `Dockerfile` (both stages), `compose.yaml`, CI, `.env.example`, `.env.testing` and the local `.env`.
- Keep the current lowercase style (`lewdocs`, not `Lewdocs`) in prose and code, matching how `lewcontext` is written today.
- `APP_NAME` remains the single source for page titles, the logo text, app copy, email sender name default and the TOTP issuer, so no per-screen copy edits are needed beyond the default and the docs.
- Rename the README image `lewcontext.png` to `lewdocs.png` and update its reference. The artwork and the favicon are generic and need no redraw. The mail template's `logo.png` keeps its name.

### 2. Code and package names

- `apps/api/go.mod`: `module lewcontext/api` becomes `module lewdocs/api`; rewrite every import in the module in the same commit (mechanical `lewcontext/api` → `lewdocs/api`). No external module imports it.
- `apps/web/package.json` and both `name` fields in `package-lock.json`: `lewcontext-web` becomes `lewdocs-web`. Preferred update: change the name and run `npm install --package-lock-only`, then check the lockfile diff is only the name. Do not run a full `npm install` that changes resolved versions.
- Keep the Go binary named `api` (`bin/api`, `/out/api`). Only the Docker image name changes to `lewdocs` / `lewdocs:local`, because the docs use the image name as the command (`docker run ... lewdocs admin ...`).

### 3. Cookies and browser state

Rename all cookie names to the `lewdocs` prefix, keeping the same `__Host-` / `__Secure-` rules in production. There is no dual-read window:

- Every app session, admin session, MFA challenge, admin MFA challenge, pending registration and admin assets pass issued under the old names stops being recognised.
- Users sign in again, and an in-flight sign-up/MFA challenge must restart.
- The sidebar preference key becomes `lewdocs:sidebar`; a docked/undocked choice is forgotten once and returns to the default.
- Update every test that asserts a cookie name or storage key, including the e2e tests that inspect `lewcontext_session` and `lewcontext_admin_assets`.

This is expected and preferable to carrying the old prefix indefinitely.

### 4. User-visible file and issuer names

- Data export: `lewcontext-export-<date>.json` becomes `lewdocs-export-<date>.json` in the server's `Content-Disposition`, the client's download code, the e2e filename regex and docs.
- TOTP: `auth.TOTPURI(s.Config.AppName, ...)` already uses `APP_NAME`, so new enrolments show `lewdocs` as the issuer. Existing authenticator entries keep working because only the display label changes, not the secret or algorithm. Update Vitest fixtures that hard-code `otpauth://totp/lewcontext:...` only where they assert current output.

### 5. Databases, roles and the local stack

- Rename the local names in configuration: PostgreSQL user and database `lewcontext` → `lewdocs`, Go tests' `lewcontext_test` → `lewdocs_test`, e2e databases `lewcontext_test_<port>` → `lewdocs_test_<port>`. Update `compose.yaml`, `Makefile` (`db` target), `.env.example`, `.env.testing`, CI's service, `dbtest`'s schema prefix and comments, and the e2e lock file `/tmp/lewcontext-e2e-<port>.lock` → `/tmp/lewdocs-e2e-<port>.lock`.
- The e2e database patterns (`*_test_<port>`) and URL derivation do not depend on the product prefix, so the scripts only need their names and comments updated.
- Set an explicit Compose project name, `name: lewdocs`, so the project and volume names no longer follow the checkout directory. This matters when the repository folder is eventually renamed: without it a folder rename silently creates a new empty volume.
- **No migration file changes.** No table, column, index or function contains the product name. The database and role names are deployment identifiers, not schema.

There are two ways to handle the existing local volume, and the plan recommends the non-destructive one:

1. **Recommended: rename in place.** With the old stack stopped, connect to the `postgres` database and run `ALTER DATABASE lewcontext RENAME TO lewdocs;`, `ALTER DATABASE lewcontext_test RENAME TO lewdocs_test;` and `ALTER ROLE lewcontext RENAME TO lewdocs;` before starting the new Compose project. Grants and ownership follow the role rename. Docker has no volume-rename command, so copy the old `lewcontext_postgres` volume's files into a new `lewdocs_postgres` volume with a temporary container. Verify with `make migrate-status` and a seed-free `make test`.
2. **Fallback: recreate the local stack.** If local data is disposable, `docker compose down -v` and `make db && make migrate && make seed`. This destroys the local development database and volume, so it needs explicit approval; it is not part of the normal `make migrate-fresh` path.

Production or any database that must be kept uses the same non-destructive `ALTER ROLE` / `ALTER DATABASE` approach and a coordinated `DATABASE_URL` change. If no production deployment exists, this step is documentation only.

### 6. Email, tests and tooling labels

- Test and development addresses `@lewcontext.test` become `@lewdocs.test`; `no-reply@lewcontext.test` and the Compose `MAIL_FROM_ADDRESS` follow. These are reserved `.test` addresses, never real inboxes.
- `User-Agent` strings and integration-test constants become `lewdocs-alerts` / `lewdocs-integration-test/1.0`.
- Test mailer `App: "lewcontext"` literals become `lewdocs`, and mail assertions that expect the old name update with them.
- The account export test, cookie tests, `dbtest` env tests, CI service names and Playwright config comments are updated in the same change.

### 7. Docs and history

- Update every current doc and the root README/AGENTS.md: title, prose, commands, image names, database names, cookie names, export filename, environment defaults and the test-environment guide.
- Do **not** rewrite `plans/features/implemented/**` or the existing `plans/features/admin/{findings,read}/` notes. `plans/README.md` already says implemented plans and their names are history; changing them would falsify the record. The new plan is the place that explains why old strings remain in those files.
- Add this plan's row to `plans/README.md`.
- The following strings intentionally remain `lewcontext`, and each gets a short comment saying why:
  - the five HKDF labels in `internal/auth/encrypt.go`;
  - the five advisory-lock prefixes in `internal/database/lock.go`.

## API and data changes

- No endpoint, request, response, database schema or migration changes.
- User-visible changes: export filename and the TOTP issuer label on new enrolments.
- Credential changes: all cookie-based sessions and challenges end; users sign in again.
- Data changes: none, apart from the local database/role rename described above.

## Security risks and how they are avoided

| Risk | Avoidance |
| --- | --- |
| Renaming the HKDF labels changes every derived key: existing sealed TOTP secrets no longer open, recovery-code and emailed-code digests no longer match, old event email pseudonyms split | Keep all five labels exactly as they are. They are cryptographic domain separators, not branding. Add a comment at the constants saying a rename locks out every MFA account and breaks pseudonym continuity |
| Renaming advisory-lock prefixes lets an old and a new binary run the alerts evaluator or metrics rollup at the same time during a rolling deploy | Keep the five `lewcontext:` prefixes. They coordinate processes, not product identity |
| Sessions are invalidated | Expected for a rename. There is no redirect or dual-cookie shim: a shim would keep the old name alive and add a second session path to test |
| Admin assets pass invalidated | Acceptable; the pass lasts at most 8 hours and is reissued on the next admin page load. The HKDF label behind it also stays unchanged |
| The wrong cookie name drops `__Host-`/`__Secure-` or a protective attribute | Keep the existing production/development branching in `auth/token.go`, `auth/admin.go`, `auth/admin_assets.go` and `auth/email_code.go`; update the tests that assert `HttpOnly`, `Secure`, `SameSite` and `Path` |
| A partial database rename loses production grants or points at an old volume | Use `ALTER ROLE`/`ALTER DATABASE` (grants follow the role), update the connection string in one coordinated change, and set an explicit Compose project name. Recreating the local volume is only the explicitly approved fallback |
| Old names linger in code and get copied | After the mechanical rewrite, `git grep -n lewcontext` must show only the allowlisted HKDF labels, the advisory-lock prefixes and historical plan files. Review that output in the change |
| Secrets are accidentally rewritten | The change edits only non-secret values: `APP_NAME`, `DATABASE_URL`, sender address, cookie names, labels. Never rewrite `APP_KEY`, `ADMIN_ROUTE_KEY`, SMTP credentials or `.env` secrets |

No CSP, rate-limit, permission, auth-flow or header behaviour changes. The web build is rebuilt, so the embedded page CSP hashes are regenerated normally.

## Tests

Run the normal completion checks after the rewrite:

- `make test` (Go unit, PostgreSQL integration, Vitest).
- `make check` (gofmt, vet, docs links, typecheck, lint, Prettier).
- `make build` (both web builds embedded, admin guard, Docker-independent composition).
- `make test-e2e E2E_PORT=auto` (cookies, exports, emails, admin assets, session flows).
- A local `make migrate-status` after the database rename, plus a manual check of sign-in, MFA enrolment, admin sign-in, download-data filename, sidebar preference and a rendered email.

Targeted assertions to update:

- `auth/password_test.go`, `auth/token.go` cookie-name tests and the admin/asset cookie tests.
- `account/data_export_test.go` and the e2e `compliance.spec.ts` export filename regex.
- `config` tests that assert `APP_NAME`/`MAIL_FROM_NAME` defaults.
- `database/dbtest/env_test.go` database names.
- Mail tests expecting "Don’t want email from lewcontext", "Learn about lewcontext", and the test mailer's app name.
- E2E specs' `@lewcontext.test` users and cookie-name lookups.
- `apps/web` fixtures with literal TOTP URIs and the `lewcontext.invalid` path origin.

No test is added merely to assert the rename. The existing behaviour, security and persistence tests are the coverage; the only new checking step is the `git grep` allowlist review above.

## Decisions

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | How far does the rename go? | Full rename of live names: product, module, packages, image, database/roles, cookies, storage key, filenames, tests, tooling and current docs. Historical plans stay untouched |
| 2 | Rename the HKDF labels too? | **No.** Keep the five labels; changing them locks out MFA users, invalidates recovery codes and splits event pseudonyms. This is the main irreversible risk |
| 3 | Rename the advisory-lock prefixes too? | **No.** Keep `lewcontext:` so old and new processes coordinate during a rolling deploy |
| 4 | Keep users signed in across the cookie rename? | **No.** Accept sign-out, MFA/registration challenge resets and one lost sidebar preference. No dual-read shim |
| 5 | Product casing | Keep `lewdocs` lowercase, as `lewcontext` is written now |
| 6 | Existing local database | **Recommended:** non-destructive `ALTER DATABASE`/`ALTER ROLE` plus volume rename/copy. **Fallback:** recreate the local volume and database, only with explicit approval |
| 7 | Compose project/volume names | Set `name: lewdocs` explicitly so they no longer change when the checkout folder is renamed |
| 8 | Go module path | Change `lewcontext/api` to `lewdocs/api`; no external consumer exists |
| 9 | Binary name | Keep `api`; rename only the Docker image to `lewdocs` / `lewdocs:local` |
| 10 | Email domain in fixtures | Use `@lewdocs.test`; these are reserved test addresses |
| 11 | Historical material | Leave implemented plans, findings and read notes unchanged; add this plan and its `plans/README.md` row instead |
| 12 | Branding | Rename `lewcontext.png` to `lewdocs.png`; keep the existing artwork, favicon, tagline and positioning |

## Not included

- Renaming the checkout directory, Git remote, GitHub repository, worktrees or branch names. Those are outside the repository and must be done by the user; a folder rename is safe because Compose now has an explicit project name.
- New logo artwork, a new tagline, a marketing copy pass, a new domain or new email addresses in production.
- A compatibility window for old cookies, old admin passes or old client builds.
- Renaming HKDF labels or advisory-lock prefixes.
- Rewriting implemented plans, findings or read notes as if the old name never existed.
- Any schema migration, endpoint change, event rename or permission rename.
- Moving or rewriting production data beyond the database/role rename described above.
