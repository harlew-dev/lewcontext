import { useEffect, useRef, type ReactNode } from "react";
import { Outlet, useLocation, useParams } from "react-router";
import { ApplicationLayout } from "@/components/layout/application-layout";
import {
  PageTransition,
  usePageKey,
} from "@/components/layout/page-transition";
import { NotFound, ServiceUnavailable } from "@/components/layout/status-page";
import { ListSkeleton } from "@/components/ui/feedback";
import { Toaster, toast } from "@/components/ui/toast";
import { paths } from "@/lib/api";
import { parsePublicId } from "@/lib/ids";
import { signedInLoader } from "@/lib/loaders";
import type { Page } from "@/lib/pagination";
import { arrivalState, type ArrivalState } from "@/lib/routes";
import { SessionProvider } from "@/lib/session";
import { useResource } from "@/lib/use-resource";
import { WorkspaceProvider, useWorkspaceList } from "@/lib/workspace";
import type { Workspace } from "@/types/workspaces";
import type { Route } from "./+types/layout";
// The signed-in chrome around account settings and every workspace page. It
// stays mounted between them, so the sidebar keeps its state and the
// current-page indicator can slide.
//
// The session loads once, when the layout first opens. Moving between pages
// never waits on a loader: pages render at once and fetch their own data.
export function clientLoader({ request }: Route.ClientLoaderArgs) {
  return signedInLoader(request);
}
export function shouldRevalidate() {
  return false;
}
export default function AppLayout({ loaderData }: Route.ComponentProps) {
  const pageKey = usePageKey();
  if ("unavailable" in loaderData) return <ServiceUnavailable />;
  return (
    <SessionProvider session={loaderData.session}>
      <CurrentWorkspace first={loaderData.workspaces}>
        <ApplicationLayout>
          {/* Each page fades in; a new query (search, page) isn't a new
              page, and nor is another tab of the same page. */}
          <PageTransition key={pageKey}>
            <Outlet />
          </PageTransition>
        </ApplicationLayout>
      </CurrentWorkspace>
      <Toaster />
      <ArrivalToast />
    </SessionProvider>
  );
}
// Announces arriving at a workspace, from pages outside the layout that
// couldn't toast themselves (their Toaster unmounts with them): the workspace
// just joined from an invitation, or just created (from onboarding, or the
// switcher, which uses the same form). Once per navigation, even when React
// runs effects twice in development.
function ArrivalToast() {
  const location = useLocation();
  const message = arrivalMessage(arrivalState(location.state));
  const shown = useRef<string | null>(null);
  useEffect(() => {
    if (!message || shown.current === location.key) return;
    shown.current = location.key;
    toast.success(message);
  }, [message, location.key]);
  return null;
}
function arrivalMessage({ joinedWorkspace, createdWorkspace }: ArrivalState) {
  if (joinedWorkspace) return `You joined ${joinedWorkspace}.`;
  if (createdWorkspace) return `${createdWorkspace} created.`;
  return null;
}
// Provides the workspace in the URL, or the newest one on pages outside a
// workspace (account settings). A workspace already in the switcher's list
// shows at once while its record refreshes, so switching doesn't wait. One
// that doesn't exist or isn't the member's renders the not-found page.
function CurrentWorkspace({
  first,
  children,
}: {
  first: Page<Workspace>;
  children: ReactNode;
}) {
  const param = useParams().workspace;
  const id = param === undefined ? undefined : parsePublicId("wsp", param);
  const list = useWorkspaceList(first);
  const listed = id ? list.items.find((item) => item.id === id) : undefined;
  const record = useResource(id ? paths.workspaces.show(id) : null, listed);
  if (id === null || record.missing) return <NotFound />;
  const workspace =
    id === undefined ? (list.items[0] ?? first.data[0]) : record.data;
  if (!workspace)
    return record.error ? <ServiceUnavailable /> : <WorkspaceLoading />;
  return (
    <WorkspaceProvider workspace={workspace} first={first}>
      {children}
    </WorkspaceProvider>
  );
}
// Opening a workspace by URL that the loaded list doesn't include yet.
function WorkspaceLoading() {
  return (
    <main className="mx-auto max-w-6xl p-6 lg:p-10">
      <ListSkeleton label="Loading workspace" />
    </main>
  );
}
