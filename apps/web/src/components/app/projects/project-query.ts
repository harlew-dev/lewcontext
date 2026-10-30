import type { ParsedUrlQuery } from "querystring";
import { page, param, sort, url } from "@/lib/pagination";
import { apiRoutes, routes } from "@/lib/routes";
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
export function projectQuery(query: ParsedUrlQuery): ProjectQuery {
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
// The projects page URL for a query.
export function projectsPath(query: ProjectQuery): string {
  return url(routes.projects, queryParams(query));
}
// The API URL for a query; also the page's SWR key.
export function projectsApiPath(query: ProjectQuery): string {
  return url(apiRoutes.projects, queryParams(query));
}
