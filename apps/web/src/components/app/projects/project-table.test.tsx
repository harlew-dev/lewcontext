import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it, vi } from "vitest";
import { Table, TableBody, TableHead } from "@/components/ui/table";
import { sortable, type Project } from "@/types/projects";
import { paths } from "@/lib/api";
import { WorkspaceProvider } from "@/lib/workspace";
import type { Workspace } from "@/types/workspaces";
import { indexQuery, projectQuery, projectsPath } from "./project-query";
import {
  ProjectEmptyState,
  ProjectRow,
  ProjectTableHead,
} from "./project-table";
const workspace: Workspace = {
  id: "wsp_2yEHYLwiwPVhLW",
  name: "Acme",
  role: "owner",
  createdAt: "",
  updatedAt: "",
};
const project: Project = {
  id: "prj_j292s2S2j7Qx4B",
  name: "Research sprint",
  description: "Talk to our customers",
  status: "Planned",
  color: "violet",
  createdAt: "2026-10-01T12:00:00Z",
  updatedAt: "2026-10-02T12:00:00Z",
};
describe("project table", () => {
  it("renders API data with rows linking to the project", () => {
    render(
      <WorkspaceProvider
        workspace={workspace}
        first={{
          data: [workspace],
          pages: { current: 1, total: 1, perPage: 20, items: 1 },
        }}
      >
        <Table>
          <TableBody>
            <ProjectRow project={project} />
          </TableBody>
        </Table>
      </WorkspaceProvider>,
      { wrapper: MemoryRouter },
    );
    expect(screen.getByText("Talk to our customers")).toBeInTheDocument();
    expect(screen.getByText("Planned")).toBeInTheDocument();
    for (const link of screen.getAllByRole("link", { name: "Research sprint" }))
      expect(link).toHaveAttribute(
        "href",
        "/app/wsp_2yEHYLwiwPVhLW/projects/prj_j292s2S2j7Qx4B",
      );
  });
  it("links headers to the next sort and reports the current one", () => {
    render(
      <Table>
        <TableHead>
          <ProjectTableHead
            sort="name"
            sortHref={(sort) => `/x?sort=${sort}`}
          />
        </TableHead>
      </Table>,
      { wrapper: MemoryRouter },
    );
    expect(screen.getByRole("link", { name: "Project" })).toHaveAttribute(
      "href",
      "/x?sort=-name",
    );
    expect(screen.getByRole("link", { name: "Last updated" })).toHaveAttribute(
      "href",
      "/x?sort=-updated",
    );
    expect(
      screen.getByRole("columnheader", { name: "Project" }),
    ).toHaveAttribute("aria-sort", "ascending");
    expect(
      screen.getByRole("columnheader", { name: "Status" }),
    ).toHaveAttribute("aria-sort", "none");
  });
  it("explains an empty search result", () => {
    render(<ProjectEmptyState filtered onCreate={vi.fn()} />);
    expect(screen.getByText("No matching projects")).toBeInTheDocument();
  });
  it("offers to create the first project in an empty workspace", () => {
    const onCreate = vi.fn();
    render(<ProjectEmptyState onCreate={onCreate} />);
    fireEvent.click(screen.getByRole("button", { name: "Create a project" }));
    expect(onCreate).toHaveBeenCalled();
  });
});
describe("project query", () => {
  it("normalises URL values the API would reject", () => {
    expect(
      projectQuery(
        new URLSearchParams({
          page: "0",
          q: `  launch\u0000 ${"x".repeat(200)}`,
          status: "Archived",
          sort: "user_id",
        }),
      ),
    ).toEqual({
      page: 1,
      q: `launch ${"x".repeat(93)}`,
      status: "all",
      sort: "-updated",
    });
  });
  it("builds page and API URLs without defaults", () => {
    const query = projectQuery(
      new URLSearchParams({
        q: "web",
        status: "Planned",
        sort: "name",
        page: "2",
      }),
    );
    expect(projectsPath(workspace.id, query)).toBe(
      "/app/wsp_2yEHYLwiwPVhLW/projects?q=web&status=Planned&sort=name&page=2",
    );
    expect(indexQuery(query)).toEqual({
      page: 2,
      q: "web",
      status: "Planned",
      sort: "name",
    });
    expect(
      paths
        .projects(workspace.id)
        .index(indexQuery(projectQuery(new URLSearchParams()))),
    ).toBe("/api/workspaces/wsp_2yEHYLwiwPVhLW/projects");
  });
  it("mirrors the API's sort allowlist", () => {
    const go = readFileSync(
      // Vitest runs from apps/web.
      resolve(process.cwd(), "../api/internal/store/projects.go"),
      "utf8",
    );
    const map = go.match(/var projectSorts = map\[string\]string\{([^}]*)\}/);
    const keys = [...(map?.[1] ?? "").matchAll(/"(-?\w+)":/g)].map((m) => m[1]);
    expect(keys.sort()).toEqual(
      sortable.flatMap((field) => [field, `-${field}`]).sort(),
    );
  });
});
