import { useMemo } from "react";
import { useOptionalSession } from "@/lib/session";
import { browserHttp, type Http } from "./http";
import type { ApiPath } from "./paths";
import { auth, authRoutes, sessionRoutes, sessions } from "./auth";
import { projectRoutes, projects } from "./projects";
export { ApiError, errorMessage } from "./http";
export type { ApiPath } from "./paths";
export type { ProjectIndexQuery } from "./projects";
export type { Credentials, Registration, SignedIn } from "./auth";
// Typed API paths, for SWR keys, module constants and server rendering:
//   paths.projects.index({ page: 2 })   paths.projects.show(42)
export const paths = {
  projects: projectRoutes,
  auth: authRoutes,
  sessions: sessionRoutes,
};
// The API for components. Mutations send the current session's CSRF token.
// Call it in the component body and use the result in handlers:
//   const api = useApi();
//   await api.projects().create(input);
export function useApi(): ApiClient {
  const csrfToken = useOptionalSession()?.csrfToken;
  return useMemo(() => client(browserHttp(csrfToken)), [csrfToken]);
}
// Builds the API on a transport: useApi with browserHttp, lib/server.ts
// with serverHttp.
export function client(http: Http) {
  return {
    // Loads any typed path from `paths`, for SWR and server rendering.
    get: <T>(path: ApiPath<T>) => http.get(path),
    projects: () => projects(http),
    auth: () => auth(http),
    sessions: () => sessions(http),
  };
}
export type ApiClient = ReturnType<typeof client>;
