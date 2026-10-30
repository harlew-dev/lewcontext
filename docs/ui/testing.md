# UI tests

## Running

```sh
make test       # includes Vitest (cd apps/web && npm test)
make check      # TypeScript, ESLint (zero warnings), Prettier (and the Go checks)
make build      # web build (react-router build), embedded into bin/api
make serve      # run it on :3000, then in another terminal:
TEST_DATABASE_URL='postgresql://lewcontext:password@127.0.0.1:5432/lewcontext?sslmode=disable' make test-e2e
cd apps/web && npm audit
```

## Vitest + React Testing Library

- Files sit next to the code as `*.test.ts(x)` and run in jsdom (`vitest.config.ts`, setup in `tests/setup.ts`). Vitest runs from `apps/web`. It has its own config, without the React Router Vite plugin.
- **Shared setup:** `tests/setup.ts` runs `cleanup()` and `vi.unstubAllGlobals()` after every test, so test files don't repeat them.
- **Faking the API:** `stubApi` from `src/test/api.ts` answers `fetch` from a table of `"METHOD url"` keys (the exact URL, then the path without its query, then `"*"`) and records what was sent:

  ```ts
  const api = stubApi({
    "POST /api/auth/password/verify": { status: 204 },
    "PUT /api/auth/password": [{ status: 422, body: { error, field } }, { body: session }],
    "*": { body: {} },
  });
  api.sent("PUT"); // [{ method, url, body, csrf }]
  ```

  A list of replies is used in order and its last one repeats; anything unmatched gets a 404 naming the missing stub. A reply can hold its response until a `gate()` is released (to check pending states) or fail as if the network were down (`network: true`). `testSession(overrides)` from `src/test/session.ts` is a signed-in session to pass to `SessionProvider`. Only `lib/api/http.test.ts` (the transport, with raw `Response`s) and `lib/use-resource-pages.test.tsx` (an outage that changes mid-test) stub `fetch` themselves.
- **Routing in tests:** components that render links need a router: `render(ui, { wrapper: MemoryRouter })`. Hooks that navigate are tested by mocking `useNavigate`/`useLocation` with `vi.mock("react-router", async (importOriginal) => ({ ...(await importOriginal()), useNavigate: () => navigate }))`.
- **Query by role and accessible name** (`getByRole("link", { name: "Page 2" })`), and assert on behaviour: links, `aria-current`, `aria-sort`, `aria-busy`, disabled state, callbacks. Don't write tests that only mirror styling.
- **Pure helpers get table-style tests:** `lib/pagination.test.ts`, `lib/ids.test.ts` (`parsePublicId`). `lib/use-delayed-loading.test.ts` and `lib/use-action.test.tsx` use fake timers (`vi.useFakeTimers()`) for the 200 ms delay and 400 ms minimum. The API client has `lib/api/http.test.ts` (the transport: CSRF, credentials, errors) and `lib/api/index.test.ts` (paths, methods and bodies, plus `@ts-expect-error` checks that wrong queries and paths fail to type-check). `lib/routes.test.ts` covers the workspace route builders and which `?next=` values are accepted; `lib/session.test.tsx` and `lib/use-resource.test.tsx` cover the session hooks, that `useApi()` sends the CSRF token only when signed in, and that the SWR cache is kept per user. `lib/loaders.test.ts` covers the route loaders: the signed-in layout's session and workspaces, 401 → login with `next`, no workspace → onboarding, an unreachable API, signed-in visitors leaving login for an allowed `next`, and `/app` and onboarding redirects. `pages/home.test.tsx` covers the homepage's "Log in" / "Dashboard" button. `lib/use-resource-pages.test.tsx` covers the infinite list: a given page 1, loading more without repeats, stopping at the end, keeping items on failure with retry, and 401 → login.
- **Components:** `components/layout/sidebar-layout.test.tsx` covers hiding the desktop sidebar and remembering it, opening hidden **from the first render** (a probe records every render, so an effect that flips it after mount fails the test), and working without storage. `paginated-table.test.tsx` covers the skeleton, empty, past-the-end and loading states and the selector links. `project-table.test.tsx` covers row links, sortable headers, empty states, query normalisation and **the sort allowlist matching the Go source** (it reads `apps/api/internal/store/projects.go`). `components/app/workspaces/workspace-form.test.tsx` covers name validation and the create form's pending and error states; `workspace-settings.test.tsx` renders the settings page for each role (rename for owners and admins, read-only for members) and the copy button's status message.

## Playwright end-to-end (`tests/e2e/workspace.spec.ts`)

- **Runs against `make serve`:** the Go binary serving the API and the built web app, with `APP_ORIGIN` as the base URL (default `http://localhost:3000`). It also runs against `make dev`, except the checks of Go's `/app` redirect, CSP and status codes.
- **Self-contained:** it signs up unique `e2e-<timestamp>-…@forma.test` users, creates its own data through the UI and the API (22 projects for the pagination flow, 23 workspaces for the switcher), and afterwards deletes only those users and their workspaces through `TEST_DATABASE_URL`. It doesn't depend on seeded data. The membership test needs `TEST_DATABASE_URL` to add a `member` row (there are no invites yet) and is skipped without it.
- **Browser:** `/usr/bin/chromium` by default; set `CHROMIUM_PATH` (on this computer `/usr/lib/chromium/chromium`).
- **Covers:** registration → onboarding → the new workspace's dashboard, creating with a pending state, editing on the project page, "not found" for unknown projects and workspaces, filters, refresh failure with Retry, paging, sorting, server search, back/forward, opening `?page=2` directly, past-the-end, creating from page 2, CSP script hashes and no CSP violations, security headers, mobile layout, session revocation, logout and login, returning to `?next=` after sign-in (with other sites rejected), the prerendered homepage's HTML (title, description, Styletron styles), signed-out `/app` → 302 to login, the old URLs 404ing, creating and switching workspaces, renaming, copying the ID, infinite scrolling in the switcher, opening the newest workspace after sign-in, "not found" for non-members and members' read-only settings, and hiding the desktop sidebar: remembered after a reload without it ever rendering docked (a `MutationObserver` added with `addInitScript` watches from the first byte), and the mobile sidebar closing from beside the workspace menu.
- Any `pageerror` or CSP violation in the console fails the main flow. API calls made from the test send the page's own Origin and the CSRF token from `/api/auth/me`.
- Repeated runs share the API's per-IP sign-in limit (60 per 15 minutes).

## Tips

- The search box has role `searchbox` (it's `type="search"`), not `textbox`.
- Search applies 300 ms after typing stops. After `fill()`, wait for the URL (`toHaveURL(`${projects}?q=…`)`) before the next step. Otherwise actions such as Refresh run against the previous query, and SWR won't re-fetch a key it loaded in the last 2 seconds, so the test only passes on slow machines.
- `page.route(`**/api/workspaces/${ws}/projects`)` matches only the bare path. Use a regex such as `new RegExp(`/api/workspaces/${ws}/projects(\\?.*)?$`)` to catch list requests with query strings.
- Test emails come from a timestamp taken when each Playwright worker starts, and a failed test restarts the worker. Don't share data between tests through module variables; set up what a test needs inside it.
- To serve a build next to an existing dev server, run `make build`, then `API_ADDR=127.0.0.1:<port> APP_ORIGIN=http://localhost:<port> ./bin/api`, and set the same `APP_ORIGIN` for Playwright.
