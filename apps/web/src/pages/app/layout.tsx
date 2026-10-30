import type { ReactNode } from "react";
import { Outlet, useParams } from "react-router";
import { ApplicationLayout } from "@/components/layout/application-layout";
import { NotFound, ServiceUnavailable } from "@/components/layout/status-page";
import { ListSkeleton } from "@/components/ui/feedback";
import { paths } from "@/lib/api";
import { parsePublicId } from "@/lib/ids";
import { signedInLoader } from "@/lib/loaders";
import type { Page } from "@/lib/pagination";
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
  if ("unavailable" in loaderData) return <ServiceUnavailable />;
  return (
    <SessionProvider session={loaderData.session}>
      <CurrentWorkspace first={loaderData.workspaces}>
        <ApplicationLayout>
          <Outlet />
        </ApplicationLayout>
      </CurrentWorkspace>
    </SessionProvider>
  );
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
