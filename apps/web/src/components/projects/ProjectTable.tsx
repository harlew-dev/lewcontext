import { ArrowUpRight } from "lucide-react";
import type { Project } from "@/lib/types";
import { ProjectBadge, ProjectIcon } from "./ProjectBadge";
import { EmptyState } from "@/components/ui/Feedback";
export function ProjectTable({
  projects,
  onEdit,
  onCreate,
  filtered = false,
}: {
  projects: Project[];
  onEdit: (project: Project) => void;
  onCreate: () => void;
  filtered?: boolean;
}) {
  if (!projects.length)
    return <EmptyState filtered={filtered} onCreate={onCreate} />;
  return (
    <div className="table-scroll">
      <table className="project-table">
        <thead>
          <tr>
            <th scope="col">Project name</th>
            <th scope="col">Status</th>
            <th scope="col">Last updated</th>
            <th scope="col">
              <span className="sr-only">Actions</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {projects.map((project) => (
            <tr key={project.id}>
              <td>
                <button
                  className="project-name-button"
                  onClick={() => onEdit(project)}
                >
                  <ProjectIcon project={project} />
                  <span>
                    <strong>{project.name}</strong>
                    <small>{project.description || "No description yet"}</small>
                  </span>
                </button>
              </td>
              <td>
                <ProjectBadge status={project.status} />
              </td>
              <td className="date-cell">
                {new Date(project.updatedAt).toLocaleDateString("en-US", {
                  month: "short",
                  day: "numeric",
                  timeZone: "UTC",
                })}
              </td>
              <td>
                <button
                  className="icon-button"
                  aria-label={`Edit ${project.name}`}
                  onClick={() => onEdit(project)}
                >
                  <ArrowUpRight size={17} />
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
