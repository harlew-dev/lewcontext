import { url, type Page } from "@/lib/pagination";
import {
  defaultSort,
  type Project,
  type ProjectInput,
  type ProjectSort,
} from "@/types/projects";
import type { Http } from "./http";
import { path } from "./paths";
// The parameters GET /api/projects accepts (docs/api/pagination.md).
export type ProjectIndexQuery = {
  page?: number;
  perPage?: number;
  q?: string;
  status?: Project["status"];
  sort?: ProjectSort;
};
const base = "/api/projects";
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
// Typed paths for each method (paths.projects).
export const projectRoutes = {
  index: (query: ProjectIndexQuery = {}) =>
    path<Page<Project>>(url(base, indexParams(query))),
  show: (id: number) => path<Project>(`${base}/${id}`),
  create: () => base,
  update: (id: number) => `${base}/${id}`,
  // Needs DELETE /api/projects/{id} in the Go API first.
  // destroy: (id: number) => `${base}/${id}`,
  // Whether an API path (e.g. an SWR key) belongs to projects.
  matches: (key: string) =>
    key === base || key.startsWith(`${base}/`) || key.startsWith(`${base}?`),
};
// Only the editable fields are sent; the API rejects unknown ones.
function body({ name, description, status, color }: ProjectInput) {
  return { name, description, status, color };
}
export function projects(http: Http) {
  return {
    index: (query: ProjectIndexQuery = {}) =>
      http.get(projectRoutes.index(query)),
    show: (id: number) => http.get(projectRoutes.show(id)),
    create: (input: ProjectInput) =>
      http.send<Project>("POST", projectRoutes.create(), body(input)),
    update: (id: number, input: ProjectInput) =>
      http.send<Project>("PATCH", projectRoutes.update(id), body(input)),
    // Needs DELETE /api/projects/{id} in the Go API first.
    // destroy: (id: number) =>
    //   http.send<void>("DELETE", projectRoutes.destroy(id)),
  };
}
