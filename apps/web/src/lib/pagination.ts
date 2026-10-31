// Mirrors store.Page in the Go API: one page of results plus its position.
export type Pages = {
  current: number;
  total: number;
  perPage: number;
  items: number;
};
export type Page<T> = { data: T[]; pages: Pages };
// A sort key: "field" sorts ascending, "-field" descending.
export type Sort<F extends string> = F | `-${F}`;
export type SortDirection = "ascending" | "descending";
// Matches store.MaxPage; larger page numbers in a URL fall back to page 1.
const maxPage = 10_000;
// Matches the API's search limit (maxSearchLength in httpapi/projects.go).
const maxSearch = 100;
// One query-string value. Repeated keys count as absent.
export function param(query: URLSearchParams, key: string): string {
  const values = query.getAll(key);
  return values.length === 1 ? values[0] : "";
}
// The page number in a URL query; anything but a positive integer is page 1.
export function page(query: URLSearchParams): number {
  const value = param(query, "page");
  const number = /^\d{1,5}$/.test(value) ? Number(value) : 1;
  return number >= 1 && number <= maxPage ? number : 1;
}
// A search term ("q") as the API accepts it: trimmed, without control
// characters, and at most 100 characters (validSearch in httpapi/projects.go).
export function cleanSearch(value: string): string {
  return Array.from(value.replace(/\p{Cc}/gu, "").trim())
    .slice(0, maxSearch)
    .join("")
    .trim();
}
// The sort in a URL query, or the fallback when it isn't an allowed field.
export function sort<F extends string>(
  query: URLSearchParams,
  fields: readonly F[],
  fallback: Sort<F>,
): Sort<F> {
  const value = param(query, "sort");
  const field = value.startsWith("-") ? value.slice(1) : value;
  return (fields as readonly string[]).includes(field)
    ? (value as Sort<F>)
    : fallback;
}
// How `current` sorts `field`, for aria-sort; undefined when it sorts another field.
export function sortDirection<F extends string>(
  current: Sort<F>,
  field: F,
): SortDirection | undefined {
  if (current === field) return "ascending";
  if (current === `-${field}`) return "descending";
  return undefined;
}
// The sort after choosing `field`: reverses it if already sorted by it,
// otherwise starts in the `first` direction.
export function toggleSort<F extends string>(
  current: Sort<F>,
  field: F,
  first: SortDirection = "ascending",
): Sort<F> {
  const direction = sortDirection(current, field);
  const descending = direction
    ? direction === "ascending"
    : first !== "ascending";
  return descending ? `-${field}` : field;
}
// `path` with a query string. Empty values and page 1 are left out, so each
// state has one URL (and one SWR key).
export function url(
  path: string,
  params: Record<string, string | number | null | undefined>,
): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === null || value === undefined || value === "") continue;
    if (key === "page" && value === 1) continue;
    search.set(key, String(value));
  }
  const query = search.toString();
  return query ? `${path}?${query}` : path;
}
// Page numbers for a page selector: the first and last pages, `siblings`
// either side of the current page, and "gap" for skipped runs.
export function pageRange(pages: Pages, siblings = 1): (number | "gap")[] {
  const total = Math.max(1, pages.total);
  const current = Math.min(Math.max(1, pages.current), total);
  const start = Math.max(2, current - siblings);
  const end = Math.min(total - 1, current + siblings);
  const range: (number | "gap")[] = [1];
  // A gap that would hide a single page shows that page instead.
  if (start > 3) range.push("gap");
  else for (let n = 2; n < start; n++) range.push(n);
  for (let n = start; n <= end; n++) range.push(n);
  if (end < total - 2) range.push("gap");
  else for (let n = end + 1; n < total; n++) range.push(n);
  if (total > 1) range.push(total);
  return range;
}
