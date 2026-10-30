import { ArrowUpRight } from "lucide-react";
import type { Project } from "@/lib/types";
import { ProjectBadge, ProjectIcon } from "./ProjectBadge";
import { EmptyState } from "@/components/ui/Feedback";
import { IconButton } from "@/components/ui/IconButton";
import { cx } from "@/lib/cx";
// Column order: name, status, last updated (hidden on phones), edit action.
const header = "px-2.5 py-2.75 text-caption font-medium";
const cell =
  "border-b border-mauve-100 px-2.5 py-3.75 xl:py-4.5 max-sm:px-1.25 max-sm:py-3.25";
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
    <div className="overflow-x-auto">
      <table className="w-full text-left text-label whitespace-nowrap">
        <thead className="border-y border-line text-mauve-600">
          <tr>
            <th scope="col" className={cx(header, "pl-0")}>
              Project name
            </th>
            <th
              scope="col"
              className={cx(header, "max-sm:pr-0 max-sm:pl-1.25")}
            >
              Status
            </th>
            <th scope="col" className={cx(header, "max-sm:hidden")}>
              Last updated
            </th>
            <th scope="col" className={header}>
              <span className="sr-only">Actions</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {projects.map((project) => (
            <tr key={project.id}>
              <td className={cx(cell, "w-[57%] pl-0 max-sm:pl-0")}>
                <ProjectNameButton project={project} onEdit={onEdit} />
              </td>
              <td className={cx(cell, "max-sm:pr-0")}>
                <ProjectBadge status={project.status} />
              </td>
              <td
                className={cx(
                  cell,
                  "text-caption text-mauve-600 max-sm:hidden",
                )}
              >
                {new Date(project.updatedAt).toLocaleDateString("en-US", {
                  month: "short",
                  day: "numeric",
                  timeZone: "UTC",
                })}
              </td>
              <td
                className={cx(
                  cell,
                  "w-6.25 pr-0 text-right max-sm:w-5 max-sm:pr-0",
                )}
              >
                <IconButton
                  className="max-sm:p-1"
                  aria-label={`Edit ${project.name}`}
                  onClick={() => onEdit(project)}
                >
                  <ArrowUpRight size={17} className="max-sm:w-3.25" />
                </IconButton>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
function ProjectNameButton({
  project,
  onEdit,
}: {
  project: Project;
  onEdit: (project: Project) => void;
}) {
  return (
    <button
      className="flex max-w-full items-center gap-3.25 text-left max-sm:gap-2"
      onClick={() => onEdit(project)}
    >
      <ProjectIcon project={project} />
      <span className="min-w-0">
        <strong className="block text-small font-[550] max-sm:max-w-[150px] max-sm:overflow-hidden max-sm:text-label max-sm:text-ellipsis">
          {project.name}
        </strong>
        <small className="mt-1.25 block max-w-[390px] overflow-hidden text-caption text-ellipsis text-mauve-600 max-lg:max-w-[260px] max-sm:max-w-[145px] max-sm:text-tiny">
          {project.description || "No description yet"}
        </small>
      </span>
    </button>
  );
}
