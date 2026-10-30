import type { BadgeVariant } from "@/components/ui/Badge";
import type { Project } from "@/lib/types";
// Full class strings (not interpolated) so Tailwind can detect them.
export const statusVariants: Record<Project["status"], BadgeVariant> = {
  "In progress": "progress",
  Planned: "planned",
  Completed: "completed",
};
export const statusBars: Record<Project["status"], string> = {
  "In progress": "bg-status-progress-bar",
  Planned: "bg-status-planned-bar",
  Completed: "bg-status-completed-bar",
};
export const projectColors: Record<Project["color"], string> = {
  violet: "bg-tile-violet text-tile-violet-ink",
  blue: "bg-tile-blue text-tile-blue-ink",
  amber: "bg-tile-amber text-tile-amber-ink",
  emerald: "bg-tile-emerald text-tile-emerald-ink",
  rose: "bg-tile-rose text-tile-rose-ink",
};
