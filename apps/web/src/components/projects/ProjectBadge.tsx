import { Check, CircleDashed, Circle } from "lucide-react";
import type { Project } from "@/lib/types";
export function ProjectBadge({ status }: Pick<Project, "status">) {
  const Icon =
    status === "Completed"
      ? Check
      : status === "Planned"
        ? CircleDashed
        : Circle;
  return (
    <span className={`badge badge-${status.toLowerCase().replace(" ", "-")}`}>
      <Icon size={11} strokeWidth={2.5} />
      {status}
    </span>
  );
}
export function ProjectIcon({
  project,
}: {
  project: Pick<Project, "name" | "color">;
}) {
  return (
    <span className={`project-icon color-${project.color}`} aria-hidden="true">
      {project.name
        .split(" ")
        .map((s) => s[0])
        .slice(0, 2)
        .join("")
        .toUpperCase()}
    </span>
  );
}
