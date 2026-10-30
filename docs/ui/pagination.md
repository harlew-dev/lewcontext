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
const list = useResource(paths.projects(workspace.id).index(indexQuery(query)), props.projects);
router.push(projectsPath(workspace.id, { ...query, page: 1, status }), undefined, { shallow: true, scroll: false });
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
  head={<ProjectTableHead sort={query.sort} sortHref={(sort) => projectsPath(workspace.id, { ...query, page: 1, sort })} />}
  row={(project) => <ProjectRow project={project} />}
  rowKey={(project) => project.id}
  href={(page) => projectsPath(workspace.id, { ...query, page })}
  empty={<ProjectEmptyState filtered={filtered} onCreate={…} />}
  shallow
/>
```

- **Loading:** a `ListSkeleton` before the first page. While another page loads, the previous page stays visible, dimmed, with `aria-busy`.
- **Empty and past the end:** `empty` when nothing matches. A page past the end shows "There’s nothing on page N" and a "Go to last page" link.
- **Selector:** below the table when there is more than one page. Previous and Next are disabled at the ends. Page numbers are hidden on small screens.
- **Errors** stay with the page's `Notice` (with Retry), not inside the table.

## Infinite lists

Lists shown in a menu (the workspace switcher) load a page at a time as they scroll, instead of a page selector. `useResourcePages(pathFor, initial)` (`lib/use-resource-pages.ts`) wraps SWR's `useSWRInfinite`:

```ts
const workspacePage = (page: number) => paths.workspaces.index({ page }); // a module constant
const list = useResourcePages(workspacePage, props.workspaces); // page 1 from the server
list.items        // every loaded page, de-duplicated by id (offset pages shift when records are added)
list.hasMore      // the last loaded page isn't the last page
list.loadMore()   // load the next page (ignored while one is loading, at the end, or after an error)
list.loadingMore  // → a skeleton row
list.error        // keeps the loaded items → a "Couldn’t load more · Retry" item calling list.retry()
```

- **Keys** are `[path, userId]` per page, like `useResource`, and stop after the last page. Page 1 revalidates with the list (mount, focus, refresh), so new records show up.
- **Loading more:** an `IntersectionObserver` on an element after the last item (root: the scrolling list) calls `loadMore()`. A focusable "Show more" item does the same for keyboards and browsers without the observer. Nothing extra renders when everything fits on page 1.
- **Refreshing from elsewhere:** `useRefreshPages(pathFor)` returns a function that revalidates the whole list (`unstable_serialize` of the same keys), e.g. after creating or renaming a workspace (`useRefreshWorkspaces()`).

## Building blocks (Catalyst style)

- `Pagination`, `PaginationPrevious`, `PaginationNext`, `PaginationList`, `PaginationPage` (`current`), `PaginationGap`. They are real links, so they work during server rendering and with middle-click.
- `TableRow href title` makes the whole row a link (each cell gets a link overlay; only the first cell is in the tab order).
- `TableHeader href sort shallow` makes a sortable header with a chevron and `aria-sort`.
