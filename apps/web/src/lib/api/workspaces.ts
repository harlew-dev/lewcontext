import { url, type Page } from "@/lib/pagination";
import type { Workspace, WorkspaceInput } from "@/types/workspaces";
import type { Http } from "./http";
import { path } from "./paths";
// The parameters GET /api/workspaces accepts: newest first, no search or sort.
export type WorkspaceIndexQuery = { page?: number; perPage?: number };
const base = "/api/workspaces";
// Typed paths for workspaces (paths.workspaces).
export const workspaceRoutes = {
  index: ({ page, perPage }: WorkspaceIndexQuery = {}) =>
    path<Page<Workspace>>(url(base, { page, perPage })),
  show: (id: string) => path<Workspace>(`${base}/${id}`),
  create: () => base,
  update: (id: string) => `${base}/${id}`,
};
export function workspaces(http: Http) {
  return {
    index: (query: WorkspaceIndexQuery = {}) =>
      http.get(workspaceRoutes.index(query)),
    show: (id: string) => http.get(workspaceRoutes.show(id)),
    // The creator becomes the workspace's owner.
    create: ({ name }: WorkspaceInput) =>
      http.send<Workspace>("POST", workspaceRoutes.create(), { name }),
    // Owners and admins only.
    update: (id: string, { name }: WorkspaceInput) =>
      http.send<Workspace>("PATCH", workspaceRoutes.update(id), { name }),
  };
}
