import { useMemo } from "react";
import { useOptionalSession } from "@/lib/session";
import { browserHttp, type Http } from "./http";
import type { ApiPath } from "./paths";
import { auth, authRoutes, sessionRoutes, sessions } from "./auth";
import { projectRoutes, projects } from "./projects";
import { workspaceRoutes, workspaces } from "./workspaces";
export { ApiError, errorMessage } from "./http";
export type { ApiPath } from "./paths";
export type { ProjectIndexQuery } from "./projects";
export type { WorkspaceIndexQuery } from "./workspaces";
export type {
  Credentials,
  PasswordChange,
  PasswordReset,
  Registration,
  SignedIn,
} from "./auth";
// Typed API paths, for SWR keys and module constants:
//   paths.workspaces.index()   paths.projects(workspace).index({ page: 2 })
export const paths = {
  workspaces: workspaceRoutes,
  projects: projectRoutes,
  auth: authRoutes,
  sessions: sessionRoutes,
};
// The API for components. Mutations send the current session's CSRF token.
// Call it in the component body and use the result in handlers:
//   const api = useApi();
//   await api.projects(workspace.id).create(input);
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
    auth: () => auth(http),
    sessions: () => sessions(http),
  };
}
export type ApiClient = ReturnType<typeof client>;
