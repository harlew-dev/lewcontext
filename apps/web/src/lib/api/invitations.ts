import { url, type Page } from "@/lib/pagination";
import type {
  Invitation,
  InvitationInput,
  InvitationPreview,
} from "@/types/invitations";
import type { Workspace } from "@/types/workspaces";
import type { Http } from "./http";
import { path } from "./paths";
// The parameters GET /api/workspaces/{workspace}/invitations accepts: newest
// first, no search or sort. A workspace has at most 50 live invitations.
export type InvitationIndexQuery = { page?: number; perPage?: number };
// Typed paths for one workspace's invitations (paths.invitations(workspace)).
export function invitationRoutes(workspace: string) {
  const base = `/api/workspaces/${workspace}/invitations`;
  return {
    index: ({ page, perPage }: InvitationIndexQuery = {}) =>
      path<Page<Invitation>>(url(base, { page, perPage })),
    store: () => base,
    destroy: (id: string) => `${base}/${id}`,
    // Whether an API path (e.g. an SWR key) is one of this workspace's lists.
    matches: (key: string) => key === base || key.startsWith(`${base}?`),
  };
}
// The invitation link's own endpoints, by token rather than ID.
export const invitationLinkRoutes = {
  check: () => "/api/invitations/check",
  accept: () => "/api/invitations/accept",
};
// Owners and admins (workspace.invitation.*).
function workspaceInvitations(http: Http, workspace: string) {
  const routes = invitationRoutes(workspace);
  return {
    index: (query: InvitationIndexQuery = {}) => http.get(routes.index(query)),
    // Sends the invitation; the email goes out a moment later ("Sending…").
    store: ({ email, role }: InvitationInput) =>
      http.send<Invitation>("POST", routes.store(), { email, role }),
    // Revokes it: the emailed link stops working.
    destroy: (id: string) => http.send<void>("DELETE", routes.destroy(id)),
  };
}
// The link's page. The token goes in the body, never in an API URL.
function invitationLinks(http: Http) {
  return {
    // Works signed out; a dead link is an ApiError with field "token".
    check: (token: string) =>
      http.send<InvitationPreview>("POST", invitationLinkRoutes.check(), {
        token,
      }),
    // Joins the invitation's workspace, signed in as the invited email.
    accept: (token: string) =>
      http.send<Workspace>("POST", invitationLinkRoutes.accept(), { token }),
  };
}
// api.invitations(workspace) for a workspace's invitations, and
// api.invitations() for the link's check and accept.
export function invitations(http: Http) {
  function forWorkspace(): ReturnType<typeof invitationLinks>;
  function forWorkspace(
    workspace: string,
  ): ReturnType<typeof workspaceInvitations>;
  function forWorkspace(workspace?: string) {
    return workspace === undefined
      ? invitationLinks(http)
      : workspaceInvitations(http, workspace);
  }
  return forWorkspace;
}
