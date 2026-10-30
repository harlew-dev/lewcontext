import { redirect } from "react-router";
import type { Workspace } from "@/types/workspaces";
import { ApiError, loaderApi } from "./api";
import type { Page } from "./pagination";
import { loginPath, returnPath, routes } from "./routes";
import type { Session } from "./types";
// Route loaders shared by several pages. They run in the browser before a page
// renders, so they only check what a page can't render without: the session
// and where to send the visitor. Pages load their own data through SWR.
//
// Go already sends signed-out requests for /app/* to login before serving any
// HTML; the 401 handling here covers a session that expires afterwards, and
// the Vite dev server, which has no such gate.
export type Unavailable = { unavailable: true };
const unavailable: Unavailable = { unavailable: true };
const unauthorized = (error: unknown) =>
  error instanceof ApiError && error.status === 401;
// The path and query of a loader's request, to return to after signing in.
function here(request: Request): string {
  const url = new URL(request.url);
  return url.pathname + url.search;
}
// Login and register: signed-in visitors go on to ?next= or /app. If the API
// can't be reached the form still renders, and works once it's back.
export async function guestLoader(request: Request): Promise<null> {
  const session = await loaderApi()
    .auth()
    .me()
    .catch(() => null);
  if (!session) return null;
  const next = new URL(request.url).searchParams.get("next");
  throw redirect(returnPath(next) ?? routes.app);
}
export type SignedIn = { session: Session; workspaces: Page<Workspace> };
// The signed-in layout: the session and the first page of the member's
// workspaces, newest first. Accounts without a workspace go to onboarding.
export async function signedInLoader(
  request: Request,
): Promise<SignedIn | Unavailable> {
  const api = loaderApi();
  let loaded: SignedIn;
  try {
    const [session, workspaces] = await Promise.all([
      api.auth().me(),
      api.workspaces().index(),
    ]);
    loaded = { session, workspaces };
  } catch (error) {
    if (unauthorized(error)) throw redirect(loginPath(here(request)));
    return unavailable;
  }
  if (loaded.workspaces.pages.items === 0) throw redirect(routes.onboarding);
  return loaded;
}
// /app: the newest workspace's dashboard, or onboarding for an account
// without one.
export async function workspaceHomeLoader(
  request: Request,
): Promise<Unavailable> {
  let newest: Workspace | undefined;
  try {
    newest = (await loaderApi().workspaces().index({ perPage: 1 })).data[0];
  } catch (error) {
    if (unauthorized(error)) throw redirect(loginPath(here(request)));
    return unavailable;
  }
  throw redirect(newest ? routes.dashboard(newest.id) : routes.onboarding);
}
// Onboarding: signed in, without a workspace yet. Accounts that already have
// one go on to /app.
export async function onboardingLoader(): Promise<
  { session: Session } | Unavailable
> {
  const api = loaderApi();
  let session: Session;
  let items: number;
  try {
    const [me, list] = await Promise.all([
      api.auth().me(),
      api.workspaces().index({ perPage: 1 }),
    ]);
    session = me;
    items = list.pages.items;
  } catch (error) {
    if (unauthorized(error)) throw redirect(routes.login);
    return unavailable;
  }
  if (items > 0) throw redirect(routes.app);
  return { session };
}
