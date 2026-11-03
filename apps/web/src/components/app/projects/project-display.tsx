import { Avatar } from "@/components/ui/avatar";
import { Badge, type BadgeColor } from "@/components/ui/badge";
import { cx } from "@/lib/cx";
import type { Project } from "@/types/projects";
import { projectColors } from "./project-colors";
export const statusColors: Record<Project["status"], BadgeColor> = {
  "In progress": "purple",
  Planned: "zinc",
  Completed: "lime",
};
export function ProjectStatusBadge({ status }: Pick<Project, "status">) {
  return <Badge color={statusColors[status]}>{status}</Badge>;
}
export function ProjectAvatar({
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
