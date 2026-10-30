import { createContext, useContext, type ReactNode } from "react";
import { useSWRConfig } from "swr";
import type { Workspace } from "@/types/workspaces";
import { paths } from "./api";
import type { Page } from "./pagination";
import { useOptionalSession } from "./session";
import { useRefreshPages, useResourcePages } from "./use-resource-pages";
// The workspace a signed-in page shows, and the first page of the member's
// workspaces from the layout's route loader.
type WorkspaceValue = { workspace: Workspace; first: Page<Workspace> };
const WorkspaceContext = createContext<WorkspaceValue | null>(null);
export function WorkspaceProvider({
  workspace,
  first,
  children,
}: WorkspaceValue & { children: ReactNode }) {
  return (
    <WorkspaceContext.Provider value={{ workspace, first }}>
      {children}
    </WorkspaceContext.Provider>
  );
}
// The current workspace, with the member's role. Only for signed-in pages,
// where the layout provides one.
export function useWorkspace(): Workspace {
  const value = useContext(WorkspaceContext);
  if (!value) throw new Error("useWorkspace() needs a signed-in page");
  return value.workspace;
}
// Page `page` of the member's workspaces, newest first.
const workspacePage = (page: number) => paths.workspaces.index({ page });
// All the member's workspaces, loaded a page at a time (the switcher). Page 1
// starts from `first` (the layout's loader) when no provider is above yet.
export function useWorkspaceList(first?: Page<Workspace>) {
  const provided = useContext(WorkspaceContext)?.first;
  return useResourcePages(workspacePage, first ?? provided);
}
// Reloads the workspace list and `id`'s record after creating or renaming one.
export function useRefreshWorkspaces() {
  const { mutate } = useSWRConfig();
  const userId = useOptionalSession()?.user.id;
  const refreshList = useRefreshPages(workspacePage);
  return (id?: string) => {
    void refreshList();
    if (id) void mutate([paths.workspaces.show(id), userId]);
  };
}
