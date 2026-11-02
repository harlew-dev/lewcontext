# Server-side pagination, search and sort

Status: **implemented 2026-10-30.** Decisions are recorded under [Decisions](#decisions). The second round changed parts of the design below:

- **No summary endpoint.** The dashboard uses the single paginated `GET /api/projects`: recent projects with `perPage=5`, and each status card reads `pages.items` from `?status=…&perPage=1`.
- **`perPage` is accepted** (1–100, default 20). There is still no rows-per-page control in the UI.
- **Sort:** `-` prefix keys, sorted by clicking column headers (`aria-sort`). Keys live in Go (`store/projects.go`) and TypeScript (`src/types/projects.ts` `sortable`), and are not published by the API. A Vitest test reads the Go allowlist to keep them in sync.
- **Rows are links** (Catalyst "rows as links") to a new `/app/projects/[id]` page backed by `GET /api/projects/{id}`. Editing moved there.
- **No index migration.** The existing `(user_id, updated_at DESC)` index is kept.
- **`PaginatedTable` leaves load errors to the page's notices**, which already offer Retry, instead of showing a second error itself.

## Why

The projects page and dashboard both load every project with `GET /api/projects`. Search and status filtering then run in the browser (`pages/app/projects.tsx`). The dashboard sorts and slices the list for "Recent projects" and counts statuses on the client. This works for a demo account, but:

- payloads and SSR time grow with every project,
- search only covers what was loaded,
- page/search state isn't in the URL, so it can't be shared, reloaded or reached with the back button.

The plan moves paging, search, filtering and sorting into the Go API. It adds one reusable pagination contract on each side: Go `pagination.go`, TS `pagination.ts`, and a shared `PaginatedTable` with a page selector.

## Decisions

| # | Question | Decision |
| --- | --- | --- |
| 1 | Response naming | `{ data, pages: { current, total, perPage, items } }` |
| 2 | `page` past the last page | `200` with empty `data` and the real `pages.total`. The UI offers "Go to last page" |
| 3 | Unknown query params | Rejected with `400`, matching the strict handling of unknown body fields |
| 4 | After creating a project | Reset to page 1 (the new project sorts first by default) |
| 5 | Page size / sorting | Page size is fixed (20). Sort fields are allowlisted in Go and mirrored in TypeScript |
| 6 | `PaginatedTable` location | `components/ui/paginated-table.tsx` |

## Current state (what changes)

| Area | Today | After |
| --- | --- | --- |
| `GET /api/projects` | Returns `Project[]`, everything the user owns | Returns `Page<Project>`, filtered and sorted by `q`, `status`, `sort`, `page` |
| Dashboard stats | Counted in the browser from the full list | New `GET /api/projects/summary`, SQL `count(*) … GROUP BY status` scoped to the user |
| Dashboard "Recent projects" | Client sort + `slice(0, 5)` | Its own small store call/endpoint for the five most recent (see open question C) |
| Projects page search/filter | `Array.filter` over loaded data | URL query (`/app/projects?q=…&status=…&sort=…&page=2`) → SSR + SWR → API |
| SSR (`lib/server.ts`) | Fetches the full list | Fetches the page the URL asks for, forwarding only parsed, allowlisted params |
| `useProjects` | Optimistic insert into the full array | After create, resets to page 1. After update, revalidates the current page. Both revalidate the summary |

## API contract

```
GET /api/projects?page=2&q=launch&status=In%20progress&sort=-updated
```

```json
{
  "data": [ { "id": 41, "name": "…", "status": "In progress", "…": "…" } ],
  "pages": { "current": 2, "total": 7, "perPage": 20, "items": 131 }
}
```

- `items` is the total number of matching rows. The UI uses it for "131 projects" and "Showing 21–40".
- `total` is at least 1 even when `items` is 0, so "page 1 of 1" is always valid.
- A `page` beyond `total` returns `data: []` with the real `total` (decision 2).
- `GET /api/projects/summary` → `{ "total": 131, "byStatus": { "In progress": 40, "Planned": 50, "Completed": 41 } }`. Dashboard counts stay derived from real project data without loading every row.

### Parameters and validation (all in `httpapi`)

| Param | Rule | Invalid → |
| --- | --- | --- |
| `page` | Base-10 integer, `1 ≤ page ≤ 10 000`, default 1 | 400 `Invalid page.` |
| `q` | Trimmed. ≤ 100 Unicode characters, valid UTF-8, no control characters (Postgres rejects NUL and would otherwise cause a 500). Empty means no filter | 400 `Invalid search.` |
| `status` | Allowlist shared with `validProject` (pulled out into one `projectStatuses` set) | 400 `Invalid status.` |
| `sort` | Key from the backend's sort allowlist, default `-updated` | 400 `Invalid sort.` |
| Any param given twice | Rejected | 400 `Invalid query.` |
| Any param not listed above (including `perPage`) | Rejected (decision 3) | 400 `Invalid query.` |

Page size is fixed at 20 on the server (decision 5) and isn't a request parameter. The page cap bounds `OFFSET` cost to 200 000 skipped rows, and the 10 s query timeout still applies.

### Sort fields (decision 5)

Go is the authority. TypeScript mirrors the same keys so the UI can only offer what the API accepts. Proposed keys use a `-` prefix for descending:

| Key | SQL `ORDER BY` (literal, never built from input) |
| --- | --- |
| `-updated` (default) | `updated_at DESC, id DESC` |
| `updated` | `updated_at ASC, id ASC` |
| `-created` / `created` | `created_at DESC, id DESC` / `created_at ASC, id ASC` |
| `name` / `-name` | `lower(name) ASC, id ASC` / `lower(name) DESC, id DESC` |
| `status` / `-status` | Workflow order via a `CASE` (Planned → In progress → Completed), then `updated_at DESC, id DESC` |

Every order ends in `id`, so rows with equal values keep a fixed position and never repeat or go missing between pages. See open questions A and B.

## Go design

Following the package boundaries in AGENTS.md: `httpapi` owns transport and validation, `store` owns Bun queries.

### `internal/store/pagination.go`: the reusable part, takes a base query

```go
// PageSize is the fixed number of rows per page.
const PageSize = 20

// Pages describes where a page sits in the full result set.
type Pages struct {
	Current int `json:"current"`
	Total   int `json:"total"`
	PerPage int `json:"perPage"`
	Items   int `json:"items"`
}

// Page is one page of results plus its position.
type Page[T any] struct {
	Data  []T   `json:"data"`
	Pages Pages `json:"pages"`
}

// Paginate applies LIMIT/OFFSET for page to q, which must already be scoped to
// the caller and carry a deterministic ORDER BY, and counts the matching rows.
func Paginate[T any](ctx context.Context, q *bun.SelectQuery, page int) (Page[T], error)
```

- It uses Bun's `ScanAndCount`, which runs `count(*)` over the same `WHERE` without `ORDER`/`LIMIT`. `Data` is initialised to `[]T{}` so the JSON is always `[]`, never `null`.
- Generic over `T`, so the next paginated resource reuses it unchanged.
- It never adds `WHERE` clauses itself. Resource methods take `userID` and scope the query before handing it over:

```go
// internal/store/projects.go
type ProjectQuery struct {
	Search string // validated in httpapi
	Status string // "" or an allowlisted status
	Sort   string // an allowlisted sort key
	Page   int
}

// projectSorts is the backend's sort allowlist; values are fixed SQL.
var projectSorts = map[string]string{
	"-updated": "updated_at DESC, id DESC",
	// …
}

func (s *Store) ProjectPage(ctx context.Context, userID int64, pq ProjectQuery) (Page[Project], error) {
	q := s.DB.NewSelect().Model((*Project)(nil)).
		Where("user_id = ?", userID).
		OrderExpr(projectSorts[pq.Sort])
	if pq.Search != "" {
		pattern := like(pq.Search)
		q = q.Where("(name ILIKE ? ESCAPE '\\' OR description ILIKE ? ESCAPE '\\')", pattern, pattern)
	}
	if pq.Status != "" {
		q = q.Where("status = ?", pq.Status)
	}
	return Paginate[Project](ctx, q, pq.Page)
}
```

- `ProjectSortKeys()` exports the allowlist's keys so `httpapi` validates against the same map the store uses, with no second copy in Go.
- `like()` escapes `\`, `%` and `_`, then wraps the term in `%…%`, so a search for `50%` matches literally. Values always go through `?` placeholders.
- `ProjectSummary(ctx, userID)` is a single grouped count query.

### `internal/httpapi/pagination.go`: reusable parsing

```go
// queryValues returns v's single values, rejecting repeated or unlisted keys.
func queryValues(v url.Values, allowed ...string) (map[string]string, error)

// pageNumber parses page with strict bounds; "" means 1.
func pageNumber(raw string) (int, error)
```

`projects.go` gains `projectQuery(v url.Values) (store.ProjectQuery, error)`, and the handler stays short: parse → store → `jsonResponse`.

### Index: no new migration (see "About the index" below)

The existing `projects_user_updated_idx (user_id, updated_at DESC)` already serves the default sort and the per-user count. Decide after measuring with `EXPLAIN ANALYZE` on seeded data (open question D).

## TypeScript design

### `src/lib/pagination.ts`: generic, pure functions, unit-tested

```ts
export type Pages = { current: number; total: number; perPage: number; items: number };
export type Page<T> = { data: T[]; pages: Pages };

/** Read a page number from router.query / ctx.query; bad values → 1. */
export function page(query: ParsedUrlQuery): number;

/** Build "/path?q=…&page=2" with URLSearchParams, omitting empty values and page 1. */
export function url(path: string, params: Record<string, string | number | undefined>): string;

/** Page numbers for the selector with gaps: [1, "gap", 4, 5, 6, "gap", 12]. */
export function pageRange(pages: Pages, siblings?: number): (number | "gap")[];
```

The project-specific parts (`projectSorts`, `projectQuery(query)` normalising `q`/`status`/`sort`) live beside the other project types in `lib/types.ts`, or in `components/app/projects`. `projectSorts` mirrors the Go keys, with labels for the UI:

```ts
export const projectSorts = {
  "-updated": "Recently updated",
  updated: "Least recently updated",
  "-created": "Newest",
  created: "Oldest",
  name: "Name A–Z",
  "-name": "Name Z–A",
  status: "Status",
  "-status": "Status (reversed)",
} as const;
export type ProjectSort = keyof typeof projectSorts;
```

A Go integration test fetches every TS key and expects `200`, and a Vitest test pins the key list. If either side changes alone, CI fails (see open question E for a stronger option).

The same `url()` builds the browser link, the SWR key and the SSR API URL, so they can't drift. The frontend normalises bad URL values to defaults (a hand-edited `?page=abc` shows page 1), while the API stays strict.

### `components/ui/pagination.tsx`: the page selector (family of named exports)

`Pagination`, `PaginationPrevious`, `PaginationNext`, `PaginationList`, `PaginationPage`, `PaginationGap`. These match the existing kit's style (`Button plain href`, zinc, `data-*` states). They are:

- real `<a href>` links, so they work during SSR, before hydration and with middle-click, inside `<nav aria-label="Pagination">`,
- `aria-current="page"` on the current page, with Previous/Next shown disabled at the ends,
- responsive: numbers are hidden below `sm:` and replaced with "Page 2 of 7" text.

### `components/ui/paginated-table.tsx`: `PaginatedTable` (decision 6)

Combines `Table` + `Pagination` with the async rules from AGENTS #8. It has no project knowledge:

```tsx
<PaginatedTable<Project>
  result={state.page}            // Page<Project> | undefined
  loading={state.loading}        // first load → ListSkeleton
  error={state.error}            // keeps the last good page visible and adds a notice with Retry
  onRetry={state.refresh}
  href={(n) => url(routes.projects, { ...query, page: n })}
  header={<ProjectTableHead />}
  row={(project) => <ProjectRow project={project} onEdit={…} />}
  empty={<EmptyState … />}       // caller decides "no projects" vs "no matches"
  label="Projects"
/>
```

When `data` is empty but `pages.current > pages.total`, it renders "This page is empty" with a "Go to last page" link instead of `empty` (decision 2).

`ProjectTable` gets split into `ProjectTableHead` and `ProjectRow`. The dashboard keeps a plain `Table` with those parts and no selector.

### Page state, SWR and SSR

- **URL is the source of truth.** Page links are plain navigations. Typing in search debounces about 300 ms, then `router.replace(…, { shallow: true })` so it doesn't fill up history. Changing search, status or sort resets the page to 1. (Shallow routing still needs checking against this Next version's docs in `node_modules/next/dist/docs`, per `apps/web/AGENTS.md`.)
- **Sort control:** a `Listbox` next to the status filter, built from `projectSorts` (open question B).
- **SWR key** is `[url(apiRoutes.projects, query), user.id]`, so the user ID stays in every key. `keepPreviousData: true` keeps the table on screen while the next page loads, and route progress shows that loading is happening.
- **SSR** (`workspaceProps`) normalises `ctx.query` and then builds the API URL with `url()`. It never passes the raw incoming query string through, so an unknown param in the browser URL is dropped instead of turning into a 400 from the API. It only forwards the cookie to the fixed `API_URL`, and `Cache-Control: private, no-store` stays as is.
- **After create (decision 4):** navigate to page 1 with the current search, filter and sort kept, then show the notice. If the new project doesn't match the current filter, the notice says so (for example, "Project created. It's hidden by your current filter."). **After update:** stay on the current page and revalidate. Both revalidate the summary.

## About the index

An index is a sorted copy of a few columns that points back to the table rows. Postgres keeps it up to date on every write.

**What the existing `(user_id, updated_at DESC)` index does for this feature:**

- **Default page query** (`WHERE user_id = ? ORDER BY updated_at DESC, id DESC LIMIT 20 OFFSET n`): Postgres jumps to the user's entries, which are already in `updated_at` order, and reads only `n + 20` of them. The `id` tiebreak is resolved with a cheap incremental sort among rows that share a timestamp.
- **Count** (`count(*) WHERE user_id = ?`): uses the `user_id` prefix to touch only that user's rows.
- **Summary** (`GROUP BY status`): uses the `user_id` prefix and groups that user's rows.

**What it doesn't help:**

- **Search** (`ILIKE '%term%'`): a b-tree can't match a leading wildcard. The index narrows to the user's rows, then each row is checked. That's fine for hundreds or thousands of projects per user. A `pg_trgm` GIN index would make it indexed, but needs `CREATE EXTENSION`.
- **Other sorts** (`name`, `created`, `status`): the user's rows are fetched via the prefix, then sorted in memory. Again fine at this scale. Each would need its own `(user_id, …)` index to become a "read the first 20 and stop" query.
- **Deep pages:** `OFFSET 199 980` still walks every entry before it. Page numbers inherently cost more the further in you go. The 10 000-page cap and the query timeout bound it.

**What adding an index costs:**

- every insert, and every update that changes an indexed column, also writes the index. `updated_at` changes on every project edit,
- disk space and buffer cache,
- building it in a migration blocks writes on `projects` while it builds. `CREATE INDEX CONCURRENTLY` avoids that, but it can't run inside a transaction, and our migrations are `.tx.` files. That's harmless at demo size and matters on a large production table.

The `(user_id, updated_at DESC, id DESC)` index I first suggested would only remove the tiebreak sort, which is a marginal gain. Keeping both indexes would double the write cost for almost no benefit. So the recommendation is: **no index migration in this feature**. Seed realistic volume, check `EXPLAIN ANALYZE` in the integration test notes, and add an index only for a query that measurably needs one.

## Security checklist

- [ ] Every paged and summary query starts from `Where("user_id = ?", current(r).User.ID)`, and `items`/`total` only count that user's rows.
- [ ] Integration tests cover cross-user access: user B's search for user A's unique project name returns `items: 0`, and B's summary doesn't include A's projects.
- [ ] Search uses `?` placeholders plus LIKE-metacharacter escaping. Tests cover `%`, `_`, `\` and `'` matching literally.
- [ ] Bounds checks on `page` and `q`. Repeated and unknown params are rejected. Control characters and NUL are rejected before they reach PG.
- [ ] `status` and `sort` are checked against allowlists, and `ORDER BY` text only ever comes from the fixed `projectSorts` map values.
- [ ] The existing 10 s request/query timeout and generic 400/500 messages stay, with details only in `slog`.
- [ ] SSR builds the API URL from normalised values only (no forwarding of the raw query string or path injection). The cookie is the only forwarded header.
- [ ] `private, no-store` stays on HTML and API responses, and SWR keys include the user ID.
- [ ] `docs/security.md` endpoint table and limits are updated.

## Tests

**Go unit (table-driven, `-race`)**:

- `pageNumber` cases: missing, `0`, `-1`, `1e3`, ` 2`, `+2`, overflow, cap+1,
- `queryValues` with repeated and unknown keys (including `perPage`),
- `like()` escaping, plus `q`, `status` and `sort` validation.

**Go integration (real PG, temp schema)**: seed 45 projects with some identical `updated_at` values and names. Check:

- page 1/2/3 sizes and `items`/`total`,
- no duplicate or missing IDs across pages for every sort key,
- the past-the-end page returning empty data with the real total,
- search plus the status filter, and literal wildcards,
- cross-user isolation and summary counts,
- every TS sort key accepted.

**Vitest**:

- `page` and project-query normalisation,
- `url` round-trip and default omission,
- `pageRange` (1 page, 7 pages, 50 pages at the start/middle/end),
- the pinned sort keys,
- `PaginatedTable` states: skeleton, empty, past-the-end, error with a kept page and Retry, selector links and `aria-current`.

**Playwright**: create enough projects for two pages. Then check:

- paging changes rows and the URL,
- a search for a project on page 2 finds it,
- changing sort reorders the rows and resets to page 1,
- creating from page 2 returns to page 1 with the new project first,
- back restores the previous page,
- reloading `?page=2&q=…` server-renders the same results.

Then run `make test`, `make check`, `make test-integration` with `TEST_DATABASE_URL`, `make test-e2e` and `make build`.

## Suggested implementation order (separate commits)

1. `feat:` store `Paginate[T]`, `Page[T]` and `ProjectPage` with the sort allowlist and `ProjectSummary`, plus integration tests.
2. `feat:` httpapi `queryValues`/`pageNumber`/`projectQuery`, the paged `GET /api/projects` and `GET /api/projects/summary`, with unit and integration tests. Update `docs/security.md`.
3. `ui:` `lib/pagination.ts` + `ui/pagination.tsx` + `ui/paginated-table.tsx` with Vitest.
4. `ui:` projects page and dashboard moved to URL state, SSR, sort and the summary. Remove client-side filtering and update the e2e tests.

## Open questions

- **A. Sort keys:** are `updated`, `created`, `name` and `status` the right set, each in both directions with a `-` prefix for descending? Or would you prefer `sort=name&order=asc`?
- **B. Sort UI:** a "Sort by" listbox next to the status filter, clickable column headers (`aria-sort`), or both?
- **C. Dashboard "Recent projects":** with page size fixed at 20 and `perPage` rejected, either (a) the dashboard shows the first 5 of page 1, which fetches 20 rows to show 5, or (b) `/api/projects/summary` also returns `recent` (the 5 most recently updated). (b) is one request for the whole dashboard.
- **D. Index:** agree to skip a migration and decide from `EXPLAIN ANALYZE`, or do you want trigram search indexing (`pg_trgm`) planned now?
- **E. Keeping Go and TS sort keys in sync:** are the tests enough (Go accepts every TS key, Vitest pins the list)? Or should the API also publish its keys (for example `pages.sorts` or a `GET /api/projects/options`) so TS checks them at runtime?
