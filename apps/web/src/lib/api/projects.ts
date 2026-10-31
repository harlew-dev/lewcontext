import { url, type Page } from "@/lib/pagination";
import {
  defaultSort,
  type Project,
  type ProjectInput,
  type ProjectSort,
} from "@/types/projects";
import type { Http } from "./http";
import { path } from "./paths";
// The parameters GET /api/workspaces/{workspace}/projects accepts
// (docs/api/pagination.md).
export type ProjectIndexQuery = {
  page?: number;
  perPage?: number;
  q?: string;
  status?: Project["status"];
  sort?: ProjectSort;
};
// Defaults are left out, so each query has one path (and one SWR key).
function indexParams({ page, perPage, q, status, sort }: ProjectIndexQuery) {
  return {
    page,
    perPage,
    q,
    status,
    sort: sort === defaultSort ? undefined : sort,
  };
}
// Typed paths for one workspace's projects (paths.projects(workspace)).
export function projectRoutes(workspace: string) {
  const base = `/api/workspaces/${workspace}/projects`;
  return {
    index: (query: ProjectIndexQuery = {}) =>
      path<Page<Project>>(url(base, indexParams(query))),
    show: (id: string) => path<Project>(`${base}/${id}`),
    store: () => base,
    update: (id: string) => `${base}/${id}`,
    // Needs DELETE /api/workspaces/{workspace}/projects/{project} in the Go API first.
    // destroy: (id: string) => `${base}/${id}`,
    // Whether an API path (e.g. an SWR key) belongs to this workspace's projects.
    matches: (key: string) =>
      key === base || key.startsWith(`${base}/`) || key.startsWith(`${base}?`),
  };
}
// Only the editable fields are sent; the API rejects unknown ones.
function body({ name, description, status, color }: ProjectInput) {
  return { name, description, status, color };
}
export function projects(http: Http, workspace: string) {
  const routes = projectRoutes(workspace);
  return {
    index: (query: ProjectIndexQuery = {}) => http.get(routes.index(query)),
    show: (id: string) => http.get(routes.show(id)),
    store: (input: ProjectInput) =>
      http.send<Project>("POST", routes.store(), body(input)),
    update: (id: string, input: ProjectInput) =>
      http.send<Project>("PATCH", routes.update(id), body(input)),
    // Needs DELETE /api/workspaces/{workspace}/projects/{project} in the Go API first.
    // destroy: (id: string) => http.send<void>("DELETE", routes.destroy(id)),
  };
}
