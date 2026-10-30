import { fireEvent, render, screen, cleanup } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ProjectTable } from "./project-table";
import type { Project } from "@/lib/types";
afterEach(cleanup);
const project: Project = {
  id: 1,
  name: "Research sprint",
  description: "Talk to our customers",
  status: "Planned",
  color: "violet",
  createdAt: "2026-10-01T12:00:00Z",
  updatedAt: "2026-10-02T12:00:00Z",
};
describe("ProjectTable", () => {
  it("renders API data and opens the selected project", () => {
    const onEdit = vi.fn();
    render(
      <ProjectTable projects={[project]} onEdit={onEdit} onCreate={vi.fn()} />,
    );
    expect(screen.getByText("Talk to our customers")).toBeInTheDocument();
    expect(screen.getByText("Planned")).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: /Research sprint Talk/ }),
    );
    expect(onEdit).toHaveBeenCalledWith(project);
  });
  it("explains an empty search result", () => {
    render(
      <ProjectTable
        projects={[]}
        onEdit={vi.fn()}
        onCreate={vi.fn()}
        filtered
      />,
    );
    expect(screen.getByText("No matching projects")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });
  it("offers to create the first project in an empty workspace", () => {
    const onCreate = vi.fn();
    render(<ProjectTable projects={[]} onEdit={vi.fn()} onCreate={onCreate} />);
    fireEvent.click(screen.getByRole("button", { name: "Create a project" }));
    expect(onCreate).toHaveBeenCalled();
  });
});
