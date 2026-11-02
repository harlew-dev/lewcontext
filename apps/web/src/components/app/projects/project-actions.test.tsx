import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { SWRConfig } from "swr";
import { describe, expect, it, vi } from "vitest";
import { Table, TableBody, TableHead } from "@/components/ui/table";
import { Toaster } from "@/components/ui/toast";
import { paths } from "@/lib/api";
import { useResource } from "@/lib/use-resource";
import { WorkspaceProvider } from "@/lib/workspace";
import { stubApi } from "@/test/api";
import { testWorkspace } from "@/test/workspace";
import type { Project } from "@/types/projects";
import type { Role } from "@/types/workspaces";
import { ProjectRow, ProjectTableHead } from "./project-table";
const project: Project = {
  id: "prj_j292s2S2j7Qx4B",
  name: "Research sprint",
  description: "",
  status: "Planned",
  color: "violet",
  createdAt: "2026-10-01T12:00:00Z",
  updatedAt: "2026-10-02T12:00:00Z",
};
const list = (data: Project[]) => ({
  body: {
    data,
    pages: { current: 1, total: 1, perPage: 20, items: data.length },
  },
});
// The workspace's project list, as the projects page loads it; Edit project
// calls `onEdit` (the page opens its ProjectDialog).
function ProjectList({ onEdit }: { onEdit: (project: Project) => void }) {
  const workspace = testWorkspace();
  const projects = useResource(paths.projects(workspace.id).index());
  return (
    <Table>
      <TableHead>
        <ProjectTableHead />
      </TableHead>
      <TableBody>
        {projects.data?.data.map((p) => (
          <ProjectRow key={p.id} project={p} onEdit={() => onEdit(p)} />
        ))}
      </TableBody>
    </Table>
  );
}
function renderAs(role: Role, onEdit: (project: Project) => void = () => {}) {
  const workspace = testWorkspace(role);
  const first = {
    data: [workspace],
    pages: { current: 1, total: 1, perPage: 20, items: 1 },
  };
  render(
    <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>
      <WorkspaceProvider workspace={workspace} first={first}>
        <MemoryRouter>
          <ProjectList onEdit={onEdit} />
        </MemoryRouter>
        <Toaster />
      </WorkspaceProvider>
    </SWRConfig>,
  );
}
const projectsPath = "/api/workspaces/wsp_2yEHYLwiwPVhLW/projects";
describe("ProjectActions", () => {
  it("isn't offered to a viewer", async () => {
    stubApi({ [`GET ${projectsPath}`]: list([project]) });
    renderAs("viewer");
    expect(await screen.findByText("Research sprint")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Actions for Research sprint" }),
    ).toBeNull();
    expect(screen.queryByRole("columnheader", { name: "Actions" })).toBeNull();
  });
  it("offers a member Edit project but not Delete", async () => {
    stubApi({ [`GET ${projectsPath}`]: list([project]) });
    const onEdit = vi.fn();
    renderAs("member", onEdit);
    fireEvent.click(
      await screen.findByRole("button", {
        name: "Actions for Research sprint",
      }),
    );
    const edit = await screen.findByRole("menuitem", { name: "Edit project" });
    expect(
      screen.queryByRole("menuitem", { name: "Delete project" }),
    ).toBeNull();
    fireEvent.click(edit);
    // Items act once the menu has closed and focus is back on its button.
    await vi.waitFor(() => expect(onEdit).toHaveBeenCalledWith(project));
  });
  it("deletes a project from its row, after confirming, and refreshes the list", async () => {
    const api = stubApi({
      [`GET ${projectsPath}`]: [list([project]), list([])],
      [`DELETE ${projectsPath}/${project.id}`]: { status: 204 },
    });
    renderAs("admin");
    fireEvent.click(
      await screen.findByRole("button", {
        name: "Actions for Research sprint",
      }),
    );
    fireEvent.click(
      await screen.findByRole("menuitem", { name: "Delete project" }),
    );
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("heading")).toHaveTextContent(
      "Delete Research sprint?",
    );
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Delete project" }),
    );
    expect(await screen.findByText("Project deleted.")).toBeInTheDocument();
    await screen.findByRole("columnheader", { name: "Actions" });
    await expect.poll(() => screen.queryByText("Research sprint")).toBeNull();
    expect(api.sent("DELETE")).toHaveLength(1);
  });
});
