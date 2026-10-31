import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/feedback";
import { TableCell, TableHeader, TableRow } from "@/components/ui/table";
import {
  sortDirection,
  toggleSort,
  type SortDirection,
} from "@/lib/pagination";
import { routes } from "@/lib/routes";
import { formatDate } from "@/lib/format";
import { useWorkspace } from "@/lib/workspace";
import type { Project, ProjectSort, ProjectSortField } from "@/types/projects";
import { ProjectAvatar, ProjectStatusBadge } from "./project-display";
// Project table columns. Dates sort newest first on the first click.
const columns: {
  field: ProjectSortField;
  label: string;
  first: SortDirection;
  className?: string;
}[] = [
  { field: "name", label: "Project", first: "ascending" },
  { field: "status", label: "Status", first: "ascending" },
  {
    field: "created",
    label: "Created",
    first: "descending",
    className: "max-lg:hidden",
  },
  {
    field: "updated",
    label: "Last updated",
    first: "descending",
    className: "max-sm:hidden",
  },
];
// Header row for project rows. With `sortHref` each column links to its sort.
export function ProjectTableHead({
  sort,
  sortHref,
}: {
  sort?: ProjectSort;
  sortHref?: (sort: ProjectSort) => string;
}) {
  return (
    <TableRow>
      {columns.map(({ field, label, first, className }) => (
        <TableHeader
          key={field}
          className={className}
          href={
            sort && sortHref
              ? sortHref(toggleSort(sort, field, first))
              : undefined
          }
          sort={sort && sortDirection(sort, field)}
        >
          {label}
        </TableHeader>
      ))}
    </TableRow>
  );
}
// A row that links to the project's page in the current workspace.
export function ProjectRow({ project }: { project: Project }) {
  const workspace = useWorkspace();
  return (
    <TableRow
      href={routes.project(workspace.id, project.id)}
      title={project.name}
    >
      <TableCell>
        <div className="flex max-w-xs items-center gap-3 sm:max-w-md">
          <ProjectAvatar project={project} className="size-8" />
          <span className="min-w-0">
            <span className="block truncate font-medium">{project.name}</span>
            <span className="block truncate text-zinc-500 dark:text-zinc-400">
              {project.description || "No description yet"}
            </span>
          </span>
        </div>
      </TableCell>
      <TableCell>
        <ProjectStatusBadge status={project.status} />
      </TableCell>
      <TableCell className="text-zinc-500 max-lg:hidden">
        {formatDate(project.createdAt)}
      </TableCell>
      <TableCell className="text-zinc-500 max-sm:hidden">
        {formatDate(project.updatedAt)}
      </TableCell>
    </TableRow>
  );
}
// No projects yet, or none matching the search and filter.
// Without onCreate (members who can't create projects) there's no button.
export function ProjectEmptyState({
  filtered = false,
  onCreate,
}: {
  filtered?: boolean;
  onCreate?: () => void;
}) {
  return filtered ? (
    <EmptyState
      title="No matching projects"
      description="Try another search or a different status."
    />
  ) : (
    <EmptyState
      title="Room for your next big thing"
      description={
        onCreate
          ? "Create your first project and give your work a place to grow."
          : "Projects will show here once someone creates one."
      }
      action={onCreate && <Button onClick={onCreate}>Create a project</Button>}
    />
  );
}
