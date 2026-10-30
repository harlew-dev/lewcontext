import type { GetServerSidePropsContext, GetServerSidePropsResult } from "next";
import { ApiError, client, type ApiClient, type ApiPath } from "./api";
import { serverHttp } from "./api/http";
import type { AppPageProps, GuestProps, Initial, Session } from "./types";
import { loginPath, returnPath, routes } from "./routes";
const apiURL = process.env.API_URL || "http://127.0.0.1:8080";
// The API for one incoming request: the fixed API URL and only that request's
// cookie. Built per request so no authenticated context is shared.
export function serverApi(context: GetServerSidePropsContext): ApiClient {
  return client(serverHttp(apiURL, context.req.headers.cookie || ""));
}
// getServerSideProps for signed-in pages: checks the session, then lets the
// page load its own data. Signed-out visitors go to login and come back here
// afterwards, a missing resource renders the 404 page, and an unreachable
// session service a 503.
export async function appProps<P extends object>(
  context: GetServerSidePropsContext,
  load: (api: ApiClient) => Promise<P>,
): Promise<GetServerSidePropsResult<AppPageProps<P>>> {
  context.res.setHeader("Cache-Control", "private, no-store, max-age=0");
  const api = serverApi(context);
  const login = {
    redirect: { destination: loginPath(context.resolvedUrl), permanent: false },
  };
  let session: Session;
  try {
    session = await api.auth().me();
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) return login;
    context.res.statusCode = 503;
    return { props: { unavailable: true } };
  }
  try {
    return { props: { session, ...(await load(api)) } };
  } catch (error) {
    if (error instanceof ApiError && error.status === 404)
      return { notFound: true };
    if (error instanceof ApiError && error.status === 401) return login;
    throw error;
  }
}
// Server-renders one typed API path. Other failures become `failure`, so the
// page still renders and can retry; expired sessions and missing resources propagate.
export async function initial<T>(
  api: ApiClient,
  path: ApiPath<T>,
  failure: string,
): Promise<Initial<T>> {
  try {
    return { path, data: await api.get(path), error: null };
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
// getServerSideProps for guest pages: the session, or null when signed out.
// With `redirectSignedIn` (login, register), signed-in visitors go on to
// ?next= or the dashboard. If the session service is unreachable the page
// renders signed out, so the form is still there once it's back.
export async function guestProps(
  context: GetServerSidePropsContext,
  { redirectSignedIn = false } = {},
): Promise<GetServerSidePropsResult<GuestProps>> {
  context.res.setHeader("Cache-Control", "private, no-store, max-age=0");
  const session = await serverApi(context)
    .auth()
    .me()
    .catch(() => null);
  if (session && redirectSignedIn) {
    const destination = returnPath(context.query.next) ?? routes.dashboard;
    return { redirect: { destination, permanent: false } };
  }
  return { props: { session } };
}
