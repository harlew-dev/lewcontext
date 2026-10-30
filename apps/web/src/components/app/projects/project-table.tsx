import { PencilSquareIcon } from "@heroicons/react/16/solid";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/feedback";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Avatar } from "@/components/ui/avatar";
import { Badge, type BadgeColor } from "@/components/ui/badge";
import { cx } from "@/lib/cx";
import type { Project } from "@/lib/types";
import { projectColors } from "./project-colors";
export function ProjectTable({
  projects,
  onEdit,
  onCreate,
  filtered = false,
  className,
}: {
  projects: Project[];
  onEdit: (project: Project) => void;
  onCreate: () => void;
  filtered?: boolean;
  className?: string;
}) {
  if (!projects.length)
    return filtered ? (
      <EmptyState
        title="No matching projects"
        description="Try another search or a different status."
      />
    ) : (
      <EmptyState
        title="Room for your next big thing"
        description="Create your first project and give your work a place to grow."
        action={<Button onClick={onCreate}>Create a project</Button>}
      />
    );
  return (
    <Table className={className}>
      <TableHead>
        <TableRow>
          <TableHeader>Project</TableHeader>
          <TableHeader>Status</TableHeader>
          <TableHeader className="max-sm:hidden">Last updated</TableHeader>
          <TableHeader className="relative w-0">
            <span className="sr-only">Actions</span>
          </TableHeader>
        </TableRow>
      </TableHead>
      <TableBody>
        {projects.map((project) => (
          <TableRow key={project.id}>
            <TableCell>
              <button
                type="button"
                className="flex max-w-xs cursor-default items-center gap-3 text-left sm:max-w-md"
                onClick={() => onEdit(project)}
              >
                <ProjectAvatar project={project} className="size-8" />
                <span className="min-w-0">
                  <span className="block truncate font-medium">
                    {project.name}
                  </span>
                  <span className="block truncate text-zinc-500 dark:text-zinc-400">
                    {project.description || "No description yet"}
                  </span>
                </span>
              </button>
            </TableCell>
            <TableCell>
              <ProjectStatusBadge status={project.status} />
            </TableCell>
            <TableCell className="text-zinc-500 max-sm:hidden">
              {formatUpdated(project)}
            </TableCell>
            <TableCell>
              <Button
                plain
                aria-label={`Edit ${project.name}`}
                onClick={() => onEdit(project)}
              >
                <PencilSquareIcon />
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
const statusColors: Record<Project["status"], BadgeColor> = {
  "In progress": "purple",
  Planned: "zinc",
  Completed: "lime",
};
function ProjectStatusBadge({ status }: Pick<Project, "status">) {
  return <Badge color={statusColors[status]}>{status}</Badge>;
}
function ProjectAvatar({
  project,
  className,
}: {
  project: Pick<Project, "name" | "color">;
  className?: string;
}) {
  const initials = project.name
    .split(" ")
    .map((word) => word[0])
    .slice(0, 2)
    .join("");
  return (
    <Avatar
      square
      initials={initials}
      className={cx(className, projectColors[project.color])}
    />
  );
}
function formatUpdated(project: Pick<Project, "updatedAt">) {
  return new Date(project.updatedAt).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  });
}
