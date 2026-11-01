import type { ReactElement, ReactNode } from "react";
import {
  PLACEMENT,
  StatefulContainer,
  Tooltip as BaseTooltip,
} from "baseui/tooltip";
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
// its ref, and pass on the mouse and focus handlers it's given. With
// `onClick` the tooltip itself is clickable too, as a shortcut for the
// pointer ("Click to reveal"); it stays open while the pointer is over it.
// Keyboard users need the same action elsewhere. `disabled` keeps it closed
// without changing the child's place in the tree. `open` holds it open whatever
// the pointer and focus do (a locked field's lock, after a try at typing). This
// is StatefulTooltip with its container's open state widened by `open`.
export function Tooltip({
  content,
  placement = "top",
  onClick,
  disabled = false,
  open = false,
  children,
}: {
  content: ReactNode;
  placement?: keyof typeof placements;
  onClick?: () => void;
  disabled?: boolean;
  open?: boolean;
  children: ReactElement;
}) {
  const reducedMotion = useReducedMotion();
  const classes = cx(
    popoverPanel,
    "rounded-md px-2 py-1 text-xs/5 font-medium",
    "bg-zinc-900 text-white shadow-sm dark:bg-zinc-700 dark:ring-1 dark:ring-white/10 dark:ring-inset",
    // Already open when disabled (an enabled Save under the pointer).
    disabled && "hidden",
  );
  return (
    <StatefulContainer
      placement={placements[placement]}
      stateReducer={(_type, next) =>
        disabled ? { ...next, isOpen: false } : next
      }
      popoverMargin={6}
      animateOutTime={reducedMotion ? 0 : popoverExit}
      overrides={popoverOverrides}
      content={
        onClick ? (
          <button
            type="button"
            tabIndex={-1}
            onClick={onClick}
            className={cx(
              classes,
              "block cursor-pointer hover:bg-zinc-800 dark:hover:bg-zinc-600",
            )}
          >
            {content}
          </button>
        ) : (
          <div className={classes}>{content}</div>
        )
      }
    >
      {(props) => (
        <BaseTooltip {...props} isOpen={!disabled && (props.isOpen || open)}>
          {children}
        </BaseTooltip>
      )}
    </StatefulContainer>
  );
}
