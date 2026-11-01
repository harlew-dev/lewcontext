import { useMemo } from "react";
import { useOptionalAdmin } from "@/lib/admin";
import { useOptionalSession } from "@/lib/session";
import { adminClient, adminRoutes, type AdminApiClient } from "./admin";
import { adminHttp, browserHttp, type Http } from "./http";
import type { ApiPath } from "./paths";
import { auth, authRoutes, sessionRoutes, sessions } from "./auth";
import { emailBlockRoutes, emailBlocks } from "./email-blocks";
import { invitationRoutes, invitations } from "./invitations";
import { memberRoutes, members } from "./members";
import { projectRoutes, projects } from "./projects";
import { workspaceRoutes, workspaces } from "./workspaces";
export { ApiError, errorMessage } from "./http";
export type { ApiPath } from "./paths";
export type {
  AdminApiClient,
  AdminLoginStep,
  AdminSetupDone,
  AdminSignedIn,
  AdminDeliveryIndexQuery,
  AdminEventIndexQuery,
  AdminProjectIndexQuery,
  AdminUserIndexQuery,
  AdminWorkspaceIndexQuery,
} from "./admin";
export { needsMfaSetup } from "./admin";
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
  admin: adminRoutes,
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
// The admin dashboard's API, for components under the admin pages
// (AdminProvider): every call sends the key from the page's URL, and
// mutations the admin session's CSRF token. It never uses the app session.
//   const api = useAdminApi();
//   await api.logout();
export function useAdminApi(): AdminApiClient {
  const admin = useOptionalAdmin();
  if (!admin) throw new Error("useAdminApi() needs an AdminProvider");
  const { adminKey } = admin;
  const csrfToken = admin.session?.csrfToken;
  return useMemo(
    () => adminClient(adminHttp(adminKey, csrfToken)),
    [adminKey, csrfToken],
  );
}
// The admin API for the admin pages' route loaders: reads only, with the key
// from the route's params.
export function loaderAdminApi(adminKey: string): AdminApiClient {
  return adminClient(adminHttp(adminKey));
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
