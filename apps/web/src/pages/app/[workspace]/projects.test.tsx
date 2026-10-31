import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { SWRConfig } from "swr";
import { describe, expect, it } from "vitest";
import ProjectIndex from "@/pages/app/[workspace]/projects";
import ProjectShow from "@/pages/app/[workspace]/projects/[project]";
import { SessionProvider } from "@/lib/session";
import { WorkspaceProvider } from "@/lib/workspace";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import { testWorkspace } from "@/test/workspace";
import type { Project } from "@/types/projects";
import type { Role } from "@/types/workspaces";
const project: Project = {
  id: "prj_j292s2S2j7Qx4B",
  name: "Research sprint",
  description: "Talk to our customers",
  status: "Planned",
  color: "violet",
  createdAt: "2026-10-01T12:00:00Z",
  updatedAt: "2026-10-02T12:00:00Z",
};
const projects = "/api/workspaces/wsp_2yEHYLwiwPVhLW/projects";
function page(...data: Project[]) {
  return {
    data,
    pages: { current: 1, total: 1, perPage: 20, items: data.length },
  };
}
// Renders the projects pages at `url` as a member with `role`.
function renderAs(role: Role, url: string) {
  const workspace = testWorkspace(role);
  return render(
    <SWRConfig value={{ provider: () => new Map() }}>
      <SessionProvider session={testSession()}>
        <WorkspaceProvider
          workspace={workspace}
          first={{ data: [workspace], pages: page().pages }}
        >
          <MemoryRouter initialEntries={[url]}>
            <Routes>
              <Route
                path="/app/:workspace/projects"
                element={<ProjectIndex />}
              />
              <Route
                path="/app/:workspace/projects/:project"
                element={<ProjectShow />}
              />
            </Routes>
          </MemoryRouter>
        </WorkspaceProvider>
      </SessionProvider>
    </SWRConfig>,
  );
}
describe("projects for each role", () => {
  it("lets admins create and edit projects", async () => {
    stubApi({ [`GET ${projects}`]: { body: page(project) } });
    renderAs("admin", "/app/wsp_2yEHYLwiwPVhLW/projects");
    expect(
      await screen.findAllByRole("link", { name: "Research sprint" }),
    ).not.toHaveLength(0);
    expect(
      screen.getByRole("button", { name: "New project" }),
    ).toBeInTheDocument();
  });
  it("shows viewers the list without New project", async () => {
    stubApi({ [`GET ${projects}`]: { body: page(project) } });
    renderAs("viewer", "/app/wsp_2yEHYLwiwPVhLW/projects");
    expect(
      await screen.findAllByRole("link", { name: "Research sprint" }),
    ).not.toHaveLength(0);
    expect(screen.queryByRole("button", { name: "New project" })).toBeNull();
  });
  it("shows viewers an empty workspace without Create a project", async () => {
    stubApi({ [`GET ${projects}`]: { body: page() } });
    renderAs("viewer", "/app/wsp_2yEHYLwiwPVhLW/projects");
    expect(
      await screen.findByText("Room for your next big thing"),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Create a project" }),
    ).toBeNull();
  });
  it.each([
    ["member", true],
    ["viewer", false],
  ] as const)("shows a %s the project, editable: %s", async (role, edit) => {
    stubApi({ [`GET ${projects}/${project.id}`]: { body: project } });
    renderAs(role, `/app/wsp_2yEHYLwiwPVhLW/projects/${project.id}`);
    expect(
      await screen.findByRole("heading", { name: "Research sprint" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Talk to our customers")).toBeInTheDocument();
    expect(
      Boolean(screen.queryByRole("button", { name: "Edit project" })),
    ).toBe(edit);
  });
});
