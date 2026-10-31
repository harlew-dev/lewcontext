# Pagination, search and sort

Lists are paged, filtered and sorted in the API, never in the browser. Code: `internal/store/pagination.go` (shared), `internal/httpapi/input/query.go` (parsing), `internal/store/projects.go` and `internal/httpapi/endpoints/workspace/projects.go` (projects), `internal/store/workspaces.go` and `internal/httpapi/endpoints/workspace/workspaces.go` (workspaces), `internal/store/members.go` and `internal/httpapi/endpoints/workspace/members.go` (members).

## Response

```json
{
  "data": [ { "id": "prj_j292s2S2j7Qx4B", "name": "…" } ],
  "pages": { "current": 2, "total": 7, "perPage": 20, "items": 131 }
}
```

- `items` counts every match in the workspace (or, for workspaces, every workspace the user belongs to). `total` is at least 1, so an empty list is "page 1 of 1".
- A page past the end returns `200` with `data: []` and the real totals.
- `data` is always an array, never `null`.

## Parameters (`GET /api/workspaces/{workspace}/projects`)

| Param | Rule | Default |
| --- | --- | --- |
| `page` | Plain digits, 1–10 000 | 1 |
| `perPage` | Plain digits, 1–100 | 20 |
| `q` | Trimmed. ≤ 100 characters, valid UTF-8, no control characters. Matches name or description, ignoring case | none |
| `status` | `Planned`, `In progress`, `Completed` | all |
| `sort` | `name`, `status`, `updated`, `created`; `-` prefix for descending | `-updated` |

Unknown, repeated or malformed parameters return 400. Signs, spaces and exponents (`+2`, ` 2`, `1e3`) are rejected, not coerced.

## Parameters (`GET /api/workspaces/{workspace}/members`)

`page`, `perPage` and `q` (name or email, same rules and `LIKE` escaping as projects). There is no `sort`: members are always ordered by role, then name, then ID: `array_position(?::text[], m.role)` with `auth.Roles` passed as a placeholder, so the order comes from Go and no SQL text comes from input.

## Parameters (`GET /api/workspaces`)

`page` and `perPage`, with the same rules and defaults, and `canLeave`. Workspaces are always newest first (`created_at DESC, id DESC`), and there is no search or sort.

- **`canLeave=false`** lists only the workspaces you can't leave: those you're the only owner of, which block deleting your account. It filters on the same SQL expression as the `canLeave` field (`canLeave` in `store/workspaces.go`). It's the only value accepted; `true`, `0` or an empty value is 400. The web app loads them a page at a time as the switcher scrolls ([../ui/pagination.md](../ui/pagination.md#infinite-lists)).

## How it is safe

- **Scoped first:** the base query starts with `WHERE p.workspace_id = ?` (the member's workspace from `RequireWorkspace`), and both the count and the rows use it.
- **Search:** `%`, `_` and `\` are escaped (`containsPattern`) and bound with `?`, so `50%` matches literally.
- **Sort:** keys map to fixed SQL in `projectSorts`, and no column name comes from input. Every order ends in `p.id`, so equal values never repeat or go missing across pages. `status` sorts in workflow order (Planned → In progress → Completed).
- **Bounded:** the page cap limits `OFFSET` to about 1M skipped rows, and the 10 s request timeout applies.

## Cursor pages (log tables)

The admin dashboard's Events and deliveries ([admin.md](admin.md#events)) page by a **cursor**, not a page number: log tables grow to millions of rows, counting them for "page 3 of 4,812" is the slow part, and nobody pages to the middle of an audit log; they filter.

```json
{ "data": [ { "id": "evt_…" } ], "next": "evt_…" }
```

- **`before`** is the cursor: rows strictly older than that row, ordered `created_at DESC, id DESC` (deliveries add `listener DESC`), compared as a row value `(created_at, id) < (?, ?)`, so rows sharing a time never repeat or go missing. `next` is the last row's cursor, or `null` on the last page.
- **No totals:** nothing is counted. The store asks for `perPage + 1` rows to know whether there's a next page.
- A cursor naming a row that's gone (pruned) is 400 "That page is no longer available."; a malformed one is 400 like any bad parameter.
- Only for log tables. Everything a user pages through in the app stays numbered.

## Paginating a new resource

```go
// store: scope, order deterministically, then hand over.
func (s *Store) IndexTasks(ctx context.Context, workspaceID int64, f TaskFilter, req PageRequest) (Page[Task], error) {
	q := s.DB.NewSelect().Model((*Task)(nil)).
		Where("t.workspace_id = ?", workspaceID).
		OrderExpr(taskSorts[f.Sort]) // fixed SQL ending in t.id
	return Paginate[Task](ctx, q, req)
}
```

```go
// httpapi: allow-list the parameters, then parse.
params, ok := input.Query(r, "page", "perPage", "q", "sort")
req, ok := input.Page(params)
```

`Paginate` adds only `LIMIT`/`OFFSET` and runs Bun's `ScanAndCount`. It never adds a `WHERE` clause itself.

If the UI sorts the list, mirror the sort fields in `apps/web/src/types/<resource>.ts` ([../ui/pagination.md](../ui/pagination.md)).
