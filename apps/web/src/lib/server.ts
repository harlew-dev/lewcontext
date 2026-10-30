import type { GetServerSidePropsContext, GetServerSidePropsResult } from "next";
import type { Workspace } from "@/types/workspaces";
import { ApiError, client, paths, type ApiClient, type ApiPath } from "./api";
import { serverHttp } from "./api/http";
import { parsePublicId } from "./ids";
import type { Page } from "./pagination";
import type { AppPageProps, GuestProps, Initial, Session } from "./types";
import { loginPath, returnPath, routes } from "./routes";
const apiURL = process.env.API_URL || "http://127.0.0.1:8080";
// The API for one incoming request: the fixed API URL and only that request's
// cookie. Built per request so no authenticated context is shared.
export function serverApi(context: GetServerSidePropsContext): ApiClient {
  return client(serverHttp(apiURL, context.req.headers.cookie || ""));
}
// getServerSideProps for signed-in pages: checks the session, loads the
// member's workspaces and the one in the URL ([workspace] pages; others show
// the newest), then lets the page load its own data for that workspace.
// Signed-out visitors go to login and come back here afterwards, accounts
// without a workspace go to onboarding, a workspace that isn't the member's
// or a missing resource renders the 404 page, and an unreachable API a 503.
export async function appProps<P extends object>(
  context: GetServerSidePropsContext,
  load: (api: ApiClient, workspace: Workspace) => Promise<P>,
): Promise<GetServerSidePropsResult<AppPageProps<P>>> {
  context.res.setHeader("Cache-Control", "private, no-store, max-age=0");
  const api = serverApi(context);
  const login = {
    redirect: { destination: loginPath(context.resolvedUrl), permanent: false },
  };
  const param = context.params?.workspace;
  const id = param === undefined ? null : parsePublicId("wsp", param);
  if (param !== undefined && id === null) return { notFound: true };
  const listPath = paths.workspaces.index();
  let session: Session, list: Page<Workspace>, current: Workspace | undefined;
  try {
    [session, list, current] = await Promise.all([
      api.auth().me(),
      api.get(listPath),
      id ? api.workspaces().show(id) : undefined,
    ]);
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) return login;
    if (error instanceof ApiError && error.status === 404)
      return { notFound: true };
    context.res.statusCode = 503;
    return { props: { unavailable: true } };
  }
  const workspace = current ?? list.data[0];
  if (!workspace)
    return { redirect: { destination: routes.onboarding, permanent: false } };
  const workspaces = { path: listPath, data: list, error: null };
  try {
    return {
      props: {
        session,
        workspace,
        workspaces,
        ...(await load(api, workspace)),
      },
    };
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
// For signed-in pages that need only the session and workspaces.
export function sessionProps(context: GetServerSidePropsContext) {
  return appProps(context, async () => ({}));
}
// getServerSideProps for guest pages: the session, or null when signed out.
// With `redirectSignedIn` (login, register), signed-in visitors go on to
// ?next= or /app (their newest workspace, or onboarding). If the session service is unreachable the page
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
    const destination = returnPath(context.query.next) ?? routes.app;
    return { redirect: { destination, permanent: false } };
  }
  return { props: { session } };
}
// getServerSideProps for /app: the newest workspace's dashboard, or
// onboarding for an account without one.
export async function workspaceHomeProps(
  context: GetServerSidePropsContext,
): Promise<GetServerSidePropsResult<{ unavailable: true }>> {
  context.res.setHeader("Cache-Control", "private, no-store, max-age=0");
  try {
    const newest = (await serverApi(context).workspaces().index({ perPage: 1 }))
      .data[0];
    const destination = newest
      ? routes.dashboard(newest.id)
      : routes.onboarding;
    return { redirect: { destination, permanent: false } };
  } catch (error) {
    if (error instanceof ApiError && error.status === 401)
      return {
        redirect: {
          destination: loginPath(context.resolvedUrl),
          permanent: false,
        },
      };
    context.res.statusCode = 503;
    return { props: { unavailable: true } };
  }
}
// getServerSideProps for onboarding: signed in, without a workspace yet.
// Accounts that already have one go on to /app.
export async function onboardingProps(
  context: GetServerSidePropsContext,
): Promise<
  GetServerSidePropsResult<{ session: Session } | { unavailable: true }>
> {
  context.res.setHeader("Cache-Control", "private, no-store, max-age=0");
  const api = serverApi(context);
  try {
    const [session, list] = await Promise.all([
      api.auth().me(),
      api.workspaces().index({ perPage: 1 }),
    ]);
    if (list.pages.items > 0)
      return { redirect: { destination: routes.app, permanent: false } };
    return { props: { session } };
  } catch (error) {
    if (error instanceof ApiError && error.status === 401)
      return { redirect: { destination: routes.login, permanent: false } };
    context.res.statusCode = 503;
    return { props: { unavailable: true } };
  }
}
