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
- **Pure helpers get table-style tests:** `lib/pagination.test.ts`, `lib/ids.test.ts`, `lib/api.test.ts`.
- **Components:** `paginated-table.test.tsx` covers the skeleton, empty, past-the-end and loading states and the selector links. `project-table.test.tsx` covers row links, sortable headers, empty states, query normalisation and **the sort allowlist matching the Go source** (it reads `apps/api/internal/store/projects.go`).

## Playwright end-to-end (`tests/e2e/workspace.spec.ts`)

- **Runs against an already running app:** API and web, with `APP_ORIGIN` as the base URL (default `http://localhost:3000`). The API must allow the same origin.
- **Self-contained:** it signs up a unique `e2e-<timestamp>@forma.test` user, creates its own data (22 projects through the API for the pagination flow), and deletes only that user afterwards through `TEST_DATABASE_URL`. It doesn't depend on seeded data.
- **Browser:** `/usr/bin/chromium` by default; set `CHROMIUM_PATH` (on this computer `/usr/lib/chromium/chromium`).
- **Covers:** registration, creating with a pending state, editing on the project page, 404 for unknown projects, filters, refresh failure with Retry, paging, sorting, server search, back/forward, server-rendered `?page=2`, past-the-end, creating from page 2, security headers, mobile layout, session revocation, logout and login.
- Any `pageerror` fails the test. API calls made from the test send the page's own Origin and the CSRF token from `/api/auth/me`.
- Repeated runs share the API's per-IP sign-in limit (60 per 15 minutes).

## Tips

- The search box has role `searchbox` (it's `type="search"`), not `textbox`.
- To serve a build next to an existing dev server, run `next build` and then `next start -p <port>`, with `APP_ORIGIN` set to that port for both the API and Playwright.
