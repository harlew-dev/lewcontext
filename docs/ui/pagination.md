# Paginated lists

The API pages, searches and sorts ([../api/pagination.md](../api/pagination.md)). The UI keeps that state in the URL and renders it with `PaginatedTable`. The projects page (`pages/app/projects.tsx`) is the reference implementation.

## Helpers (`lib/pagination.ts`)

| Helper | Does |
| --- | --- |
| `Page<T>`, `Pages` | Types for the API response `{ data, pages: { current, total, perPage, items } }` |
| `url(path, params)` | Builds `path?…` with `URLSearchParams`, leaving out empty values and page 1, so each state has exactly one URL and one SWR key |
| `page(query)` | Page number from `router.query` / `context.query`; anything invalid is 1 |
| `param(query, key)` | One query value; repeated keys count as absent |
| `sort(query, fields, fallback)` | Allowed sort from the query, else the fallback |
| `sortDirection(sort, field)` | `"ascending"`, `"descending"` or undefined, for `aria-sort` |
| `toggleSort(sort, field, first)` | Next sort when a header is clicked: reverses the current field, otherwise starts in the `first` direction |
| `pageRange(pages)` | Page numbers with `"gap"` markers for the selector |

## Sort fields live in `types/`

```ts
// src/types/projects.ts — mirrors projectSorts in apps/api/internal/store/projects.go
export const sortable = ["name", "status", "updated", "created"] as const;
export type ProjectSort = Sort<(typeof sortable)[number]>; // "name" | "-name" | …
export const defaultSort: ProjectSort = "-updated";
```

The API doesn't publish its sort keys. A Vitest test reads the Go allowlist and fails if the two lists differ.

## URL state

```ts
const query = projectQuery(router.query);      // URL state; bad values → defaults
// indexQuery() turns it into the typed ProjectIndexQuery ("all" statuses → no filter)
const list = useResource(paths.projects.index(indexQuery(query)), props.projects);
router.push(projectsPath({ ...query, page: 1, status }), undefined, { shallow: true, scroll: false });
```

- **Shallow routing** changes the URL without rerunning `getServerSideProps`, and SWR fetches the new key. The first render is server-rendered from the same URL.
- **Search, filter and sort changes reset to page 1.** Search waits 300 ms after typing and uses `router.replace` so history doesn't fill up. Back and forward restore the state.
- **Normalise the same way the API validates** (`cleanSearch` trims, strips control characters and caps at 100), so the server render never sends a request the API would reject.

## `PaginatedTable` (`components/ui/paginated-table.tsx`)

```tsx
<PaginatedTable
  page={list.data}
  loading={list.loading}
  label="Projects"
  head={<ProjectTableHead sort={query.sort} sortHref={(sort) => projectsPath({ ...query, page: 1, sort })} />}
  row={(project) => <ProjectRow project={project} />}
  rowKey={(project) => project.id}
  href={(page) => projectsPath({ ...query, page })}
  empty={<ProjectEmptyState filtered={filtered} onCreate={…} />}
  shallow
/>
```

- **Loading:** a `ListSkeleton` before the first page. While another page loads, the previous page stays visible, dimmed, with `aria-busy`.
- **Empty and past the end:** `empty` when nothing matches. A page past the end shows "There’s nothing on page N" and a "Go to last page" link.
- **Selector:** below the table when there is more than one page. Previous and Next are disabled at the ends. Page numbers are hidden on small screens.
- **Errors** stay with the page's `Notice` (with Retry), not inside the table.

## Building blocks (Catalyst style)

- `Pagination`, `PaginationPrevious`, `PaginationNext`, `PaginationList`, `PaginationPage` (`current`), `PaginationGap`. They are real links, so they work during server rendering and with middle-click.
- `TableRow href title` makes the whole row a link (each cell gets a link overlay; only the first cell is in the tab order).
- `TableHeader href sort shallow` makes a sortable header with a chevron and `aria-sort`.
