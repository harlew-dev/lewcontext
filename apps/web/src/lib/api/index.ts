import { useMemo } from "react";
import { useOptionalSession } from "@/lib/session";
import { browserHttp, type Http } from "./http";
import type { ApiPath } from "./paths";
import { auth, authRoutes, sessionRoutes, sessions } from "./auth";
import { emailBlockRoutes, emailBlocks } from "./email-blocks";
import { invitationRoutes, invitations } from "./invitations";
import { memberRoutes, members } from "./members";
import { projectRoutes, projects } from "./projects";
import { workspaceRoutes, workspaces } from "./workspaces";
export { ApiError, errorMessage } from "./http";
export type { ApiPath } from "./paths";
export type { InvitationIndexQuery } from "./invitations";
export type { MemberIndexQuery } from "./members";
export type { ProjectIndexQuery } from "./projects";
export type { WorkspaceDeletion, WorkspaceIndexQuery } from "./workspaces";
export {
  lastOwnerWorkspaces,
  needsMfa,
  needsVerification,
  verificationRequired,
} from "./auth";
export type {
  Credentials,
  MfaConfirmation,
  MfaSetup,
  RecoveryCodes,
  PasswordChange,
  PasswordReset,
  PendingRegistration,
  Registration,
  SignedIn,
  VerificationRequired,
  WorkspaceSummary,
} from "./auth";
// The admin dashboard's client is lib/api/admin.ts, which only the admin
// build imports (plans/features/admin/README.md, rule 7).
//
// Typed API paths, for SWR keys and module constants:
//   paths.workspaces.index()   paths.projects(workspace).index({ page: 2 })
export const paths = {
  workspaces: workspaceRoutes,
  projects: projectRoutes,
  members: memberRoutes,
  invitations: invitationRoutes,
  auth: authRoutes,
  sessions: sessionRoutes,
  emailBlocks: () => emailBlockRoutes,
};
// The API for components. Mutations send the current session's CSRF token.
// Call it in the component body and use the result in handlers:
//   const api = useApi();
//   await api.projects(workspace.id).store(input);
export function useApi(): ApiClient {
  const csrfToken = useOptionalSession()?.csrfToken;
  return useMemo(() => client(browserHttp(csrfToken)), [csrfToken]);
}
// The API for route loaders, which run outside components. Reads only: it has
// no CSRF token, so mutations go through useApi().
export function loaderApi(): ApiClient {
  return client(browserHttp());
}
// Builds the API on a transport (browserHttp, or a stub in tests).
export function client(http: Http) {
  return {
    // Loads any typed path from `paths`, for SWR.
    get: <T>(path: ApiPath<T>) => http.get(path),
    workspaces: () => workspaces(http),
    // Projects belong to a workspace, so their client takes its ID.
    projects: (workspace: string) => projects(http, workspace),
    members: (workspace: string) => members(http, workspace),
    // A workspace's invitations, or with no workspace, the link's check and accept.
    invitations: invitations(http),
    auth: () => auth(http),
    sessions: () => sessions(http),
    // A block link's check, block and unblock, by the token in the body.
    emailBlocks: () => emailBlocks(http),
  };
}
export type ApiClient = ReturnType<typeof client>;
