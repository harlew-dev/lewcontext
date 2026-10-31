import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { SWRConfig } from "swr";
import { describe, expect, it } from "vitest";
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
// The workspace's project list, as the projects page loads it.
function ProjectList() {
  const workspace = testWorkspace();
  const projects = useResource(paths.projects(workspace.id).index());
  return (
    <Table>
      <TableHead>
        <ProjectTableHead />
      </TableHead>
      <TableBody>
        {projects.data?.data.map((p) => (
          <ProjectRow key={p.id} project={p} />
        ))}
      </TableBody>
    </Table>
  );
}
function renderAs(role: Role) {
  const workspace = testWorkspace(role);
  const first = {
    data: [workspace],
    pages: { current: 1, total: 1, perPage: 20, items: 1 },
  };
  render(
    <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>
      <WorkspaceProvider workspace={workspace} first={first}>
        <MemoryRouter>
          <ProjectList />
        </MemoryRouter>
        <Toaster />
      </WorkspaceProvider>
    </SWRConfig>,
  );
}
const projectsPath = "/api/workspaces/wsp_2yEHYLwiwPVhLW/projects";
describe("ProjectActions", () => {
  it.each(["member", "viewer"] as const)(
    "isn't offered to a %s",
    async (role) => {
      stubApi({ [`GET ${projectsPath}`]: list([project]) });
      renderAs(role);
      expect(await screen.findByText("Research sprint")).toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: "Actions for Research sprint" }),
      ).toBeNull();
      expect(
        screen.queryByRole("columnheader", { name: "Actions" }),
      ).toBeNull();
    },
  );
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
