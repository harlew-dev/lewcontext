import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { SWRConfig } from "swr";
import { Toaster } from "@/components/ui/toast";
import { SessionProvider } from "@/lib/session";
import { WorkspaceProvider } from "@/lib/workspace";
import { testSession } from "@/test/session";
import { testWorkspace } from "@/test/workspace";
import type { Role } from "@/types/workspaces";
// Renders a signed-in workspace page at `path`, routed by `route`, as a
// member with `role` (usr_00000000000001, from testSession). A `where`
// output shows the current path and query, for what a click navigates to.
function Where() {
  const location = useLocation();
  return (
    <output data-testid="where">{location.pathname + location.search}</output>
  );
}
export function renderWorkspacePage({
  role = "owner",
  path,
  route,
  element,
}: {
  role?: Role;
  path: string;
  route: string;
  element: ReactElement;
}) {
  const workspace = testWorkspace(role);
  return render(
    <SWRConfig value={{ provider: () => new Map() }}>
      <SessionProvider session={testSession()}>
        <WorkspaceProvider
          workspace={workspace}
          first={{
            data: [workspace],
            pages: { current: 1, total: 1, perPage: 20, items: 1 },
          }}
        >
          <MemoryRouter initialEntries={[path]}>
            <Where />
            <Routes>
              <Route path={route} element={element} />
            </Routes>
          </MemoryRouter>
          <Toaster />
        </WorkspaceProvider>
      </SessionProvider>
    </SWRConfig>,
  );
}
