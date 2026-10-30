# UI tests

## Running

```sh
make test       # includes Vitest (cd apps/web && npm test)
make check      # TypeScript, ESLint (zero warnings), Prettier (and the Go checks)
make build      # optimized Next build
TEST_DATABASE_URL='postgresql://lewcontext:password@127.0.0.1:5432/lewcontext?sslmode=disable' make test-e2e   # with the app running
cd apps/web && npm audit
```

## Vitest + React Testing Library

- Files sit next to the code as `*.test.ts(x)` and run in jsdom (`vitest.config.ts`, setup in `tests/setup.ts`). Vitest runs from `apps/web`.
- **Query by role and accessible name** (`getByRole("link", { name: "Page 2" })`), and assert on behaviour: links, `aria-current`, `aria-sort`, `aria-busy`, disabled state, callbacks. Don't write tests that only mirror styling.
- **Pure helpers get table-style tests:** `lib/pagination.test.ts`, `lib/ids.test.ts`. The API client has `lib/api/http.test.ts` (transports: CSRF, credentials, errors, the server forwarding only the cookie) and `lib/api/index.test.ts` (paths, methods and bodies, plus `@ts-expect-error` checks that wrong queries and paths fail to type-check). `lib/routes.test.ts` covers which `?next=` values are accepted; `lib/session.test.tsx` and `lib/use-resource.test.tsx` cover the session hooks, that `useApi()` sends the CSRF token only when signed in, and that the SWR cache is kept per user.
- **Components:** `paginated-table.test.tsx` covers the skeleton, empty, past-the-end and loading states and the selector links. `project-table.test.tsx` covers row links, sortable headers, empty states, query normalisation and **the sort allowlist matching the Go source** (it reads `apps/api/internal/store/projects.go`).

## Playwright end-to-end (`tests/e2e/workspace.spec.ts`)

- **Runs against an already running app:** API and web, with `APP_ORIGIN` as the base URL (default `http://localhost:3000`). The API must allow the same origin.
- **Self-contained:** it signs up a unique `e2e-<timestamp>@forma.test` user, creates its own data (22 projects through the API for the pagination flow), and deletes only that user afterwards through `TEST_DATABASE_URL`. It doesn't depend on seeded data.
- **Browser:** `/usr/bin/chromium` by default; set `CHROMIUM_PATH` (on this computer `/usr/lib/chromium/chromium`).
- **Covers:** registration, creating with a pending state, editing on the project page, 404 for unknown projects, filters, refresh failure with Retry, paging, sorting, server search, back/forward, server-rendered `?page=2`, past-the-end, creating from page 2, security headers, mobile layout, session revocation, logout and login, and returning to `?next=` after sign-in (with other sites rejected).
- Any `pageerror` fails the test. API calls made from the test send the page's own Origin and the CSRF token from `/api/auth/me`.
- Repeated runs share the API's per-IP sign-in limit (60 per 15 minutes).

## Tips

- The search box has role `searchbox` (it's `type="search"`), not `textbox`.
- Search applies 300 ms after typing stops. After `fill()`, wait for the URL (`toHaveURL("/app/projects?q=…")`) before the next step. Otherwise actions such as Refresh run against the previous query, and SWR won't re-fetch a key it loaded in the last 2 seconds, so the test only passes on slow machines.
- `page.route("**/api/projects")` matches only the bare path. Use a regex such as `/\/api\/projects(\?.*)?$/` to catch list requests with query strings.
- To serve a build next to an existing dev server, run `next build` and then `next start -p <port>`, with `APP_ORIGIN` set to that port for both the API and Playwright.
