# Pagination, search and sort

Lists are paged, filtered and sorted in the API, never in the browser. Code: `internal/store/pagination.go` (shared), `internal/httpapi/pagination.go` (parsing), `internal/store/projects.go` and `internal/httpapi/projects.go` (projects), `internal/store/workspaces.go` and `internal/httpapi/workspaces.go` (workspaces).

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

## Parameters (`GET /api/workspaces`)

`page` and `perPage` only, with the same rules and defaults. Workspaces are always newest first (`created_at DESC, id DESC`), and there is no search or sort. The web app loads them a page at a time as the switcher scrolls ([../ui/pagination.md](../ui/pagination.md#infinite-lists)).

## How it is safe

- **Scoped first:** the base query starts with `WHERE p.workspace_id = ?` (the member's workspace from `requireWorkspace`), and both the count and the rows use it.
- **Search:** `%`, `_` and `\` are escaped (`containsPattern`) and bound with `?`, so `50%` matches literally.
- **Sort:** keys map to fixed SQL in `projectSorts`, and no column name comes from input. Every order ends in `p.id`, so equal values never repeat or go missing across pages. `status` sorts in workflow order (Planned → In progress → Completed).
- **Bounded:** the page cap limits `OFFSET` to about 1M skipped rows, and the 10 s request timeout applies.

## Paginating a new resource

```go
// store: scope, order deterministically, then hand over.
func (s *Store) TaskPage(ctx context.Context, workspaceID int64, f TaskFilter, req PageRequest) (Page[Task], error) {
	q := s.DB.NewSelect().Model((*Task)(nil)).
		Where("t.workspace_id = ?", workspaceID).
		OrderExpr(taskSorts[f.Sort]) // fixed SQL ending in t.id
	return Paginate[Task](ctx, q, req)
}
```

```go
// httpapi: allow-list the parameters, then parse.
params, ok := queryParams(r, "page", "perPage", "q", "sort")
req, ok := pageRequest(params)
```

`Paginate` adds only `LIMIT`/`OFFSET` and runs Bun's `ScanAndCount`. It never adds a `WHERE` clause itself.

If the UI sorts the list, mirror the sort fields in `apps/web/src/types/<resource>.ts` ([../ui/pagination.md](../ui/pagination.md)).
