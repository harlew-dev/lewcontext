# Discoveries

Things found while building the admin plans that aren't part of any one plan's findings, or that are worth knowing beyond them. Your own notes file at the repo root (`discoveries.md`) is untouched. Questions are in [questions.md](questions.md).

## In the code

- **Project names and descriptions accept control characters.** Every other free-text input refuses them. A NUL byte probably answers 500, because PostgreSQL can't store one. This will be fixed with the notes work. See [project-input-validation](../findings/project-input-validation.md).
- **Before A8, every visitor downloaded the admin UI.** The route list every page loads named the admin pages, and the shared API chunk held every `/api/admin/*` path. It was safe (the API checks the key, the account, the password, a code and a session), but it was a map of the admin area. Now the admin build is served only behind the key. See [separate-build findings](../findings/separate-build.md).
- **React Router's type generator can't handle two route lists at once.** It deletes its output folder and registers one global type. So in development both dev servers know every route, and only `make build` splits them. A link between the two builds that's missing a full page reload would only show up on `make serve` or in e2e. See [the A8 plan, spike results](../../implemented/admin/separate-build.md).
- **The admin modules are listed by hand in two places:** the build guard (`apps/web/web-build.ts`) and the ESLint rule. A comment in each says to keep them in sync.
- **Step-up and the admin door share one code-attempt limit** (5 per 15 minutes per account). One stolen admin session can use it up once, locking the owner out of code checks for up to 15 minutes. It can't do so again, because 5 wrong codes end that session. See [email-blocks findings](../findings/email-blocks.md#security-review).
- **A timing difference can tell the previous admin key from a wrong one.** A request carrying a cookie under the previous key costs a database lookup that a wrong key never does. It opens nothing, and it's documented. See [access-hardening findings](../findings/access-hardening.md#risks-that-remain).
- **Free text now reaches the audit trail.** Unblock's reason is written by an admin and kept in event payloads. If events are later shipped to logs (A3, A7), the reasons go with them.

## On this machine

- **Playwright can't find Chromium by default.** E2E needs `CHROMIUM_PATH=/home/homelab/.cache/ms-playwright/chromium-1248/chrome-linux64/chrome`.
- **The local dev database was rebuilt several times** (`make migrate-fresh CONFIRM=1 && make seed`) for migration edits in A2, A9 and A3. The Docker container `lewcontext-app-1` (not started by this work) uses the same database, so its data was reset each time. Run `make seed` after pulling these commits if your own data looks gone.
- **Other sessions are working in this checkout.** `plans/features/page-folders.md` (a proposal to move page files into folders) and `plans/future/` appeared during the run. Both are kept out of these commits. If page-folders is built while the admin pages are being added, the two will collide.
- **One flaky e2e assertion was fixed:** the Mail page's heading matched "Emails sent per hour" too. It now uses `exact: true`. Chromium crashed once ("Target crashed") on an unrelated page; the rerun passed.
