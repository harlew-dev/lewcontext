import { createContext, useContext, type ReactNode } from "react";
import type { Workspace } from "@/types/workspaces";
import { paths } from "./api";
import type { Page } from "./pagination";
import type { Initial, WorkspaceProps } from "./types";
import { useRefreshPages, useResourcePages } from "./use-resource-pages";
// The workspace a signed-in page shows, and the first page of the member's
// workspaces. _app provides them from the page's props, so on the server they
// belong to that one request.
const WorkspaceContext = createContext<WorkspaceProps | null>(null);
export function WorkspaceProvider({
  workspace,
  workspaces,
  children,
}: Partial<WorkspaceProps> & { children: ReactNode }) {
  const value = workspace && workspaces ? { workspace, workspaces } : null;
  return (
    <WorkspaceContext.Provider value={value}>
      {children}
    </WorkspaceContext.Provider>
  );
}
// The current workspace, with the member's role. Only for signed-in pages,
// where appProps guarantees one.
export function useWorkspace(): Workspace {
  const value = useContext(WorkspaceContext);
  if (!value) throw new Error("useWorkspace() needs a signed-in page");
  return value.workspace;
}
// Page `page` of the member's workspaces, newest first.
const workspacePage = (page: number) => paths.workspaces.index({ page });
// All the member's workspaces, loaded a page at a time (the switcher).
export function useWorkspaceList() {
  const initial: Initial<Page<Workspace>> | undefined =
    useContext(WorkspaceContext)?.workspaces;
  return useResourcePages(workspacePage, initial);
}
// Reloads the workspace list after creating or renaming one.
export function useRefreshWorkspaces() {
  return useRefreshPages(workspacePage);
}
