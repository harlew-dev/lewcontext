import { Check, CircleDashed, Circle } from "lucide-react";
import { Badge } from "@/components/ui/Badge";
import { cx } from "@/lib/cx";
import type { Project } from "@/lib/types";
import { projectColors, statusVariants } from "./projectStyles";
export function ProjectBadge({ status }: Pick<Project, "status">) {
  const Icon =
    status === "Completed"
      ? Check
      : status === "Planned"
        ? CircleDashed
        : Circle;
  return (
    <Badge variant={statusVariants[status]}>
      <Icon size={11} strokeWidth={2.5} />
      {status}
    </Badge>
  );
}
export function ProjectIcon({
  project,
}: {
  project: Pick<Project, "name" | "color">;
}) {
  return (
    <span
      className={cx(
        "inline-flex size-8.75 shrink-0 items-center justify-center rounded-lg text-label font-semibold tracking-[-0.4px] max-sm:size-7.25 max-sm:text-caption",
        projectColors[project.color],
      )}
      aria-hidden="true"
    >
      {project.name
        .split(" ")
        .map((s) => s[0])
        .slice(0, 2)
        .join("")
        .toUpperCase()}
    </span>
  );
}
