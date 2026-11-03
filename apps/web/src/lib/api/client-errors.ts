import type { Http } from "./http";
// An error the web app hit, as lib/client-errors.ts reports it: the message,
// the stack, the route pattern of the page (never its URL or query) and the
// build it came from. The API groups it with the server's errors.
export type ClientErrorReport = {
  message: string;
  stack: string;
  route: string;
  release: string;
};
export const clientErrorRoutes = {
  store: () => "/api/client-errors",
};
// api.clientErrors(): signed in only (a 401 otherwise), with the session's
// CSRF token; 202 once grouped, 429 past client-error:per-user.
export function clientErrors(http: Http) {
  return {
    store: ({ message, stack, route, release }: ClientErrorReport) =>
      http.send<void>("POST", clientErrorRoutes.store(), {
        message,
        stack,
        route,
        release,
      }),
  };
}
