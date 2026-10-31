# UI tests

## Running

```sh
make test       # includes Vitest (cd apps/web && npm test)
make check      # TypeScript, ESLint (zero warnings), Prettier (and the Go checks)
make build      # web build (react-router build), embedded into bin/api
make serve      # run it on :3000, then in another terminal:
TEST_DATABASE_URL='postgresql://lewcontext:password@127.0.0.1:5432/lewcontext?sslmode=disable' make test-e2e
# …with the server started as RATE_LIMITS=off make serve (see below)
cd apps/web && npm audit
```

## Vitest + React Testing Library

- Files sit next to the code as `*.test.ts(x)` and run in jsdom (`vitest.config.ts`, setup in `tests/setup.ts`). Vitest runs from `apps/web`. It has its own config, without the React Router Vite plugin.
- **Shared setup:** `tests/setup.ts` runs `cleanup()` and `vi.unstubAllGlobals()` after every test, so test files don't repeat them.
- **Faking the API:** `stubApi` from `src/test/api.ts` answers `fetch` from a table of `"METHOD url"` keys (the exact URL, then the path without its query, then `"*"`) and records what was sent:

  ```ts
  const api = stubApi({
    "POST /api/auth/password/verify": { status: 204 },
    "PATCH /api/auth/password": [{ status: 422, body: { error, field } }, { body: session }],
    "*": { body: {} },
  });
  api.sent("PATCH"); // [{ method, url, body, csrf }]
  ```

  A list of replies is used in order and its last one repeats; anything unmatched gets a 404 naming the missing stub. A reply can hold its response until a `gate()` is released (to check pending states) or fail as if the network were down (`network: true`). `testSession(overrides)` from `src/test/session.ts` is a signed-in session to pass to `SessionProvider`. Only `lib/api/http.test.ts` (the transport, with raw `Response`s) and `lib/use-resource-pages.test.tsx` (an outage that changes mid-test) stub `fetch` themselves.
- **Routing in tests:** components that render links need a router: `render(ui, { wrapper: MemoryRouter })`. Hooks that navigate are tested by mocking `useNavigate`/`useLocation` with `vi.mock("react-router", async (importOriginal) => ({ ...(await importOriginal()), useNavigate: () => navigate }))`.
- **Query by role and accessible name** (`getByRole("link", { name: "Page 2" })`), and assert on behaviour: links, `aria-current`, `aria-sort`, `aria-busy`, disabled state, callbacks. Don't write tests that only mirror styling.
- **Pure helpers get table-style tests:** `lib/pagination.test.ts`, `lib/ids.test.ts` (`parsePublicId`). `pages/auth/invitations.test.tsx` renders the invitation page through `createRoutesStub` (its loader gives the session): signed out (the callout, links carrying `next`, the token only in the check's body), names rendered as text, dead and malformed links (no request), Join only when asked or at once with our navigation state, the wrong account switching, and login and register showing the invitation, prefilling its email, keeping it when switching, and register returning to the link instead of onboarding. `members.test.tsx` also covers Invite (only your assignable roles, never owner; a refused email on its field; the toast and the refreshed list) and the pending list (Sending…, Sent, Expired, revoking only roles you may give). `lib/swr.test.tsx` checks that a 4xx read isn't retried (a 429 is fetched once, Retry asks again) while a 500 is retried twice, and that a refused mutation shows the API's message. `lib/use-delayed-loading.test.ts` and `lib/use-action.test.tsx` use fake timers (`vi.useFakeTimers()`) for the 200 ms delay and 400 ms minimum. The API client has `lib/api/http.test.ts` (the transport: CSRF, credentials, errors) and `lib/api/index.test.ts` (paths, methods and bodies, plus `@ts-expect-error` checks that wrong queries and paths fail to type-check). `lib/routes.test.ts` covers the workspace route builders and which `?next=` values are accepted; `lib/session.test.tsx` and `lib/use-resource.test.tsx` cover the session hooks, that `useApi()` sends the CSRF token only when signed in, and that the SWR cache is kept per user. `lib/loaders.test.ts` covers the route loaders: the signed-in layout's session and workspaces, 401 → login with `next`, no workspace → onboarding, an unreachable API, signed-in visitors leaving login for an allowed `next`, and `/app` and onboarding redirects. `pages/home.test.tsx` covers the homepage's "Log in" / "Dashboard" button. `pages/auth/forgot-password.test.tsx` renders login and both reset pages in a `MemoryRouter`: the "Forgot password?" link carrying the email in navigation state, the same confirmation for every email, refusals, malformed and dead links (no password asked), retry when the check can't reach the API, password checks, the token sent only in bodies, and arriving at login with the email and a success notice. `lib/use-resource-pages.test.tsx` covers the infinite list: a given page 1, loading more without repeats, stopping at the end, keeping items on failure with retry, and 401 → login.
- **Components:** `components/layout/sidebar-layout.test.tsx` covers hiding the desktop sidebar and remembering it, opening hidden **from the first render** (a probe records every render, so an effect that flips it after mount fails the test), and working without storage. `paginated-table.test.tsx` covers the skeleton, empty, past-the-end and loading states, the "Showing … of …" count and the selector links. `project-table.test.tsx` covers row links, sortable headers, empty states, query normalisation and **the sort allowlist matching the Go source** (it reads `apps/api/internal/store/projects.go`). `components/app/workspaces/workspace-form.test.tsx` covers name validation and the create form's pending and error states; `workspace-settings.test.tsx` renders the settings page for each role (rename for owners and admins, read-only for members and viewers), the copy button's status message, the tabs, leaving (the next workspace with "You left …", or onboarding after the last one), the only owner seeing no Leave, Delete workspace only for owners, its warning, and its type-to-confirm and password errors; `components/ui/listbox.test.tsx` checks that the control is named with the selected option's text, not "[object Object]"; `components/ui/confirmation-dialog.test.tsx` covers `disabled`, no closing and one confirm while pending, and a failure keeping the dialog, its error and its fields; `components/app/projects/project-actions.test.tsx` covers no ⋯ menu for viewers, members getting Edit project but not Delete, and deleting from a row refreshing the list; `pages/app/settings.test.tsx` covers deleting the account: the last-owner warning with links and no delete button, the password confirmation ending at login's notice, and a 409 switching to the warning; `lib/use-clear-cache.test.tsx` covers emptying the cache, infinite lists included; `pages/app/[workspace]/settings/members.test.tsx` covers the notice (and no request) for members and viewers, the ⋯ menu only on rows you may manage and never your own, the role list offering only `assignableRoles`, and the remove dialog's pending and failure states; `pages/app/[workspace]/projects.test.tsx` checks viewers get no New project, Create a project or ⋯ menu (members find Edit project in it), and that New project opens empty again after creating one; `types/workspaces.test.ts` checks the permission and role names match the Go source. Build workspaces with `testWorkspace(role)` (`src/test/workspace.ts`).

## Playwright end-to-end (`tests/e2e/workspace.spec.ts`)

- **Runs against `RATE_LIMITS=off make serve`:** the Go binary serving the API and the built web app, with `APP_ORIGIN` as the base URL (default `http://localhost:3000`). One run signs up about a dozen users from one IP, past `register:per-ip` (10 an hour), so the server it runs against needs its rate limits off ([../api/rate-limits.md](../api/rate-limits.md#switching-limits-off-for-tests-rate_limits)); quotas still apply. CI starts the server that way. It also runs against `make dev`, except the checks of Go's `/app` redirect, CSP and status codes.
- **Self-contained:** it signs up unique `e2e-<timestamp>-…@lewcontext.test` users, creates its own data through the UI and the API (22 projects for the pagination flow; 19 workspaces through the API, the 21st refused by the quota, and 4 memberships in the database for the switcher), and afterwards deletes only those users and their workspaces through `TEST_DATABASE_URL`. It doesn't depend on seeded data. The membership tests (read-only members and viewers; an owner changing a member to viewer, then removing them) add their `member` row through `TEST_DATABASE_URL` and are skipped without it, and so is the switcher test: a user can own 20 workspaces, so it fills the rest of the switcher's second page with memberships.
- **Browser:** `/usr/bin/chromium` by default. Without a system Chromium, use the one Playwright installs (`npx playwright install chromium`): `CHROMIUM_PATH=~/.cache/ms-playwright/chromium-<revision>/chrome-linux64/chrome`. Write the whole path: a glob in the variable isn't expanded. If the browser can't launch, every browser test fails within seconds; `test-results/<test>/error-context.md` gives the reason.
- **Covers:** registration → onboarding → the new workspace's dashboard, creating with a pending state, editing on the project page (from its ⋯ menu), "not found" for unknown projects and workspaces, filters, refresh failure with Retry, paging, sorting, server search, back/forward, opening `?page=2` directly, past-the-end, creating from page 2, CSP script hashes and no CSP violations, security headers, mobile layout, session revocation, logout and login, returning to `?next=` after sign-in (with other sites rejected), the prerendered homepage's HTML (title, description, Styletron styles), signed-out `/app` → 302 to login, the old URLs 404ing, creating ("… created.") and switching workspaces, the menus marking Workspace settings and Account & security as the current page, renaming, copying the ID, the owned-workspace quota, infinite scrolling in the switcher, opening the newest workspace after sign-in, "not found" for non-members and members' read-only settings, and hiding the desktop sidebar: remembered after a reload without it ever rendering docked (a `MutationObserver` added with `addInitScript` watches from the first byte), and the mobile sidebar closing from beside the workspace menu, deleting a project from its row, then the account's only workspace from the last-owner warning (landing in onboarding), then the account from onboarding's menu (login says so, and signing in fails), invitations (an owner invites, the list shows "Sending…" until the worker has emailed each, revokes one; a new account opens its link from the mailbox, signs up through it with the callout and the email filled in, and lands in the workspace with "You joined …"; the link then shows expired; an existing account joins while signed in; the revoked link shows expired; the owner sees both members), and forgot password: requesting a link from login, reading it from the server's mailbox file, setting a new password (the token never in an API URL), signing in with it, and the link refused the second time.
- **Mailbox:** the forgot-password and invitation tests read links from `MAIL_FILE` (`mailedLink`), by default `<repo>/tmp/mail.log`, where `make serve` writes them. A server started another way must use the same file (set `MAIL_FILE` for both).
- Any `pageerror` or CSP violation in the console fails the main flow. API calls made from the test send the page's own Origin and the CSRF token from `/api/auth/me`.
- Against a server with rate limits on, a run fails at the eleventh sign-up (429 "Too many requests"): restart it with `RATE_LIMITS=off`.

## Tips

- The search box has role `searchbox` (it's `type="search"`), not `textbox`.
- Search applies 300 ms after typing stops. After `fill()`, wait for the URL (`toHaveURL(`${projects}?q=…`)`) before the next step. Otherwise a refresh (a `focus` event) runs against the previous query. SWR ignores focus for 5 s after a list mounts, so retry the `focus` event with `expect(…).toPass()` rather than sleeping.
- `page.route(`**/api/workspaces/${ws}/projects`)` matches only the bare path. Use a regex such as `new RegExp(`/api/workspaces/${ws}/projects(\\?.*)?$`)` to catch list requests with query strings.
- Test emails come from a timestamp taken when each Playwright worker starts, and a failed test restarts the worker. Don't share data between tests through module variables; set up what a test needs inside it.

## Against a server of your own

`make serve` + `make test-e2e` assumes :3000 is free and the database is yours. Often it isn't: `make dev` or the Docker `app` container (`make docker-dev`, an older build) may hold :3000, and another server on the same database runs its own delivery worker. That worker claims the invitation and reset emails, so they go to *its* mailbox, and the mailbox tests time out waiting for a link. In that case, run a build on its own port against its own database:

```sh
make build
# Once: a second database in the same PostgreSQL container, migrated.
psql 'postgresql://lewcontext:password@127.0.0.1:5432/lewcontext?sslmode=disable' -c 'CREATE DATABASE lewcontext_e2e'
E2E_DB='postgresql://lewcontext:password@127.0.0.1:5432/lewcontext_e2e?sslmode=disable'
(cd apps/api && DATABASE_URL=$E2E_DB go run ./cmd/migrate)   # not ./bin/api migrate

# Terminal 1, from the repository root (MAIL_FILE defaults to tmp/mail.log here):
ss -ltn | grep :3100   # must print nothing
set -a; . ./.env; set +a
DATABASE_URL=$E2E_DB API_ADDR=127.0.0.1:3100 APP_ORIGIN=http://localhost:3100 RATE_LIMITS=off ./bin/api

# Terminal 2:
cd apps/web
APP_ORIGIN=http://localhost:3100 TEST_DATABASE_URL=$E2E_DB CHROMIUM_PATH=… npx playwright test
```

- **Check the port is free first.** If `bin/api` can't bind, it exits, but whatever already listens there keeps answering, and Playwright tests that server instead of yours.
- **Call Playwright directly, not `make test-e2e`.** `make` loads `.env`, and its `APP_ORIGIN` wins over one set on the command line ([../testing.md](../testing.md#configuration)), so the tests would go to :3000.
- **The server and Playwright must agree** on `APP_ORIGIN` and on the mailbox. The server writes `MAIL_FILE` relative to its working directory, so start it from the repository root, or give both the same absolute `MAIL_FILE`.
- **One server per database.** Don't start a second server (or `./bin/api` with a stray argument, which also starts one) against the database a run is using. If someone else may use `lewcontext_e2e` at the same time, create your own database.
- **Clean up accounts you add by hand.** Accounts you make against this server (for screenshots or a manual check) aren't removed by the suite, which deletes only its own run's users. Give them an `e2e-…@lewcontext.test` address and delete them with the suite's cleanup SQL (`test.afterAll` in `workspace.spec.ts`).
