import { url, type Page } from "@/lib/pagination";
import type { Workspace, WorkspaceInput } from "@/types/workspaces";
import type { Http } from "./http";
import { path } from "./paths";
// The parameters GET /api/workspaces accepts: newest first, no search or
// sort. `canLeave: false` lists only the workspaces you're the only owner of,
// which stop you deleting your account; it's the only value the API accepts.
export type WorkspaceIndexQuery = {
  page?: number;
  perPage?: number;
  canLeave?: false;
};
// Deleting a workspace takes its name, typed to confirm, and your password.
export type WorkspaceDeletion = { name: string; password: string };
const base = "/api/workspaces";
// Typed paths for workspaces (paths.workspaces).
export const workspaceRoutes = {
  index: ({ page, perPage, canLeave }: WorkspaceIndexQuery = {}) =>
    path<Page<Workspace>>(
      url(base, {
        page,
        perPage,
        canLeave: canLeave === false ? "false" : undefined,
      }),
    ),
  show: (id: string) => path<Workspace>(`${base}/${id}`),
  store: () => base,
  update: (id: string) => `${base}/${id}`,
  destroy: (id: string) => `${base}/${id}`,
  // Your own membership of the workspace.
  membership: (id: string) => `${base}/${id}/membership`,
};
export function workspaces(http: Http) {
  return {
    index: (query: WorkspaceIndexQuery = {}) =>
      http.get(workspaceRoutes.index(query)),
    show: (id: string) => http.get(workspaceRoutes.show(id)),
    // The creator becomes the workspace's owner.
    store: ({ name }: WorkspaceInput) =>
      http.send<Workspace>("POST", workspaceRoutes.store(), { name }),
    // Owners and admins only.
    update: (id: string, { name }: WorkspaceInput) =>
      http.send<Workspace>("PATCH", workspaceRoutes.update(id), { name }),
    // Owners only. Deletes its projects and memberships too; a wrong name or
    // password is an ApiError with field "name" or "password".
    destroy: (id: string, { name, password }: WorkspaceDeletion) =>
      http.send<void>("DELETE", workspaceRoutes.destroy(id), {
        name,
        password,
      }),
    // Leaves the workspace. Refused (409) for its last owner.
    leave: (id: string) =>
      http.send<void>("DELETE", workspaceRoutes.membership(id)),
  };
}
