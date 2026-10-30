import { page, param, sort, url } from "@/lib/pagination";
import type { ProjectIndexQuery } from "@/lib/api";
import { routes } from "@/lib/routes";
import {
  defaultSort,
  sortable,
  statuses,
  type ProjectQuery,
  type StatusFilter,
} from "@/types/projects";
// Matches the API's search limit (maxSearchLength in httpapi/projects.go).
const maxSearch = 100;
// The projects page state in a URL query. Values the API would reject fall
// back to defaults, so a hand-edited URL still renders a page.
export function projectQuery(query: URLSearchParams): ProjectQuery {
  const status = param(query, "status");
  return {
    page: page(query),
    q: cleanSearch(param(query, "q")),
    status: isStatus(status) ? status : "all",
    sort: sort(query, sortable, defaultSort),
  };
}
// Trims, drops control characters and caps the length like the API does.
export function cleanSearch(value: string): string {
  return Array.from(value.replace(/\p{Cc}/gu, "").trim())
    .slice(0, maxSearch)
    .join("")
    .trim();
}
function isStatus(value: string): value is Exclude<StatusFilter, "all"> {
  return (statuses as readonly string[]).includes(value);
}
// URL parameters for a query, leaving out defaults.
function queryParams({ page, q, status, sort }: ProjectQuery) {
  return {
    q,
    status: status === "all" ? "" : status,
    sort: sort === defaultSort ? "" : sort,
    page,
  };
}
// A workspace's projects page URL for a query.
export function projectsPath(workspace: string, query: ProjectQuery): string {
  return url(routes.projects(workspace), queryParams(query));
}
// The API query for the page state ("all" statuses means no filter); pass it
// to api.projects(workspace).index() or paths.projects(workspace).index().
export function indexQuery({
  page,
  q,
  status,
  sort,
}: ProjectQuery): ProjectIndexQuery {
  return { page, q, sort, status: status === "all" ? undefined : status };
}
