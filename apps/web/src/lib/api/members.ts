import { url, type Page } from "@/lib/pagination";
import type { Member } from "@/types/members";
import type { Role } from "@/types/workspaces";
import type { Http } from "./http";
import { path } from "./paths";
// The parameters GET /api/workspaces/{workspace}/members accepts: by role,
// then name; no sort (docs/api/pagination.md).
export type MemberIndexQuery = { page?: number; perPage?: number; q?: string };
// Typed paths for one workspace's members (paths.members(workspace)).
export function memberRoutes(workspace: string) {
  const base = `/api/workspaces/${workspace}/members`;
  return {
    index: ({ page, perPage, q }: MemberIndexQuery = {}) =>
      path<Page<Member>>(url(base, { page, perPage, q })),
    update: (id: string) => `${base}/${id}`,
    destroy: (id: string) => `${base}/${id}`,
    // Whether an API path (e.g. an SWR key) is one of this workspace's member lists.
    matches: (key: string) => key === base || key.startsWith(`${base}?`),
  };
}
// Owners and admins only (workspace.member.*); `id` is the user's public ID.
export function members(http: Http, workspace: string) {
  const routes = memberRoutes(workspace);
  return {
    index: (query: MemberIndexQuery = {}) => http.get(routes.index(query)),
    update: (id: string, { role }: { role: Role }) =>
      http.send<Member>("PATCH", routes.update(id), { role }),
    // Removes them from the workspace; their account stays.
    destroy: (id: string) => http.send<void>("DELETE", routes.destroy(id)),
  };
}
