import type { ReactNode } from "react";
import { cx } from "@/lib/cx";
export type BadgeVariant = "progress" | "planned" | "completed";
const variants: Record<BadgeVariant, string> = {
  progress: "bg-status-progress-soft text-status-progress",
  planned: "bg-status-planned-soft text-status-planned",
  completed: "bg-status-completed-soft text-status-completed",
};
export function Badge({
  variant,
  children,
}: {
  variant: BadgeVariant;
  children: ReactNode;
}) {
  return (
    <span
      className={cx(
        "inline-flex items-center gap-1.25 rounded-sm px-1.75 py-1 text-tiny leading-none font-medium whitespace-nowrap max-sm:gap-0.75 max-sm:px-1.25 max-sm:text-micro",
        variants[variant],
      )}
    >
      {children}
    </span>
  );
}
