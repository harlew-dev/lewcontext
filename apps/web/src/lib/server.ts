import type { GetServerSidePropsContext, GetServerSidePropsResult } from "next";
import { ApiError } from "./api";
import type { AppPageProps, Initial, Session } from "./types";
import { apiRoutes, routes } from "./routes";
const apiURL = process.env.API_URL || "http://127.0.0.1:8080";
// A JSON GET against the private API on behalf of the incoming request.
export type ApiGet = <T>(path: string) => Promise<T>;
// Forwards only the request's cookie, to the fixed API URL, with a fresh
// fetch per request so no authenticated context is shared.
function apiGet(context: GetServerSidePropsContext): ApiGet {
  const cookie = context.req.headers.cookie || "";
  return async <T>(path: string) => {
    const response = await fetch(apiURL + path, {
      headers: { cookie },
      signal: AbortSignal.timeout(8000),
    });
    if (!response.ok) throw new ApiError("API request failed", response.status);
    return (await response.json()) as T;
  };
}
// getServerSideProps for signed-in pages: checks the session, then lets the
// page load its own data. Signed-out visitors go to login, a missing resource
// renders the 404 page, and an unreachable session service a 503.
export async function appProps<P extends object>(
  context: GetServerSidePropsContext,
  load: (get: ApiGet) => Promise<P>,
): Promise<GetServerSidePropsResult<AppPageProps<P>>> {
  context.res.setHeader("Cache-Control", "private, no-store, max-age=0");
  const get = apiGet(context);
  let session: Session;
  try {
    session = await get<Session>(apiRoutes.me);
  } catch (error) {
    if (error instanceof ApiError && error.status === 401)
      return { redirect: { destination: routes.login, permanent: false } };
    context.res.statusCode = 503;
    return { props: { unavailable: true } };
  }
  try {
    return { props: { session, ...(await load(get)) } };
  } catch (error) {
    if (error instanceof ApiError && error.status === 404)
      return { notFound: true };
    if (error instanceof ApiError && error.status === 401)
      return { redirect: { destination: routes.login, permanent: false } };
    throw error;
  }
}
// Server-renders one API path. Other failures become `failure`, so the page
// still renders and can retry; expired sessions and missing resources propagate.
export async function initial<T>(
  get: ApiGet,
  path: string,
  failure: string,
): Promise<Initial<T>> {
  try {
    return { path, data: await get<T>(path), error: null };
  } catch (error) {
    if (error instanceof ApiError && [401, 404].includes(error.status))
      throw error;
    return { path, data: null, error: failure };
  }
}
// For signed-in pages that need only the session.
export function sessionProps(context: GetServerSidePropsContext) {
  return appProps(context, async () => ({}));
}
