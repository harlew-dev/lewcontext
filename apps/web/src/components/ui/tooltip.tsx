import type { ReactElement, ReactNode } from "react";
import { PLACEMENT, StatefulTooltip } from "baseui/tooltip";
import { useReducedMotion } from "motion/react";
import { popoverExit, popoverOverrides, popoverPanel } from "@/lib/baseui";
import { cx } from "@/lib/cx";
const placements = {
  top: PLACEMENT.top,
  bottom: PLACEMENT.bottom,
  left: PLACEMENT.left,
  right: PLACEMENT.right,
};
// Tooltip on BaseUI's StatefulTooltip: shows `content` while the pointer is
// over the child or it has keyboard focus, closes on Escape, and describes the
// child (aria-describedby) while open. The child must be focusable, forward
// its ref, and pass on the mouse and focus handlers it's given.
export function Tooltip({
  content,
  placement = "top",
  children,
}: {
  content: ReactNode;
  placement?: keyof typeof placements;
  children: ReactElement;
}) {
  const reducedMotion = useReducedMotion();
  return (
    <StatefulTooltip
      placement={placements[placement]}
      popoverMargin={6}
      animateOutTime={reducedMotion ? 0 : popoverExit}
      overrides={popoverOverrides}
      content={
        <div
          className={cx(
            popoverPanel,
            "rounded-md px-2 py-1 text-xs/5 font-medium",
            "bg-zinc-900 text-white shadow-sm dark:bg-zinc-700 dark:ring-1 dark:ring-white/10 dark:ring-inset",
          )}
        >
          {content}
        </div>
      }
    >
      {children}
    </StatefulTooltip>
  );
}
