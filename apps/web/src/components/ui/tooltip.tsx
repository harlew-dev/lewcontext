import {
  cloneElement,
  useId,
  useState,
  type ReactElement,
  type ReactNode,
} from "react";
import { Tooltip as BaseTooltip } from "@base-ui/react/tooltip";
import { cx } from "@/lib/cx";
import { popoverPanel } from "@/lib/transitions";
const sides = {
  top: "top",
  bottom: "bottom",
  left: "left",
  right: "right",
} as const;
// Tooltip on Base UI's Tooltip: shows `content` while the pointer is over the
// child or it has keyboard focus, closes on Escape, and labels the child's
// description while open. The child must be focusable, forward its ref, and
// pass on the props it's given. With `onClick` the tooltip itself is
// clickable too, as a shortcut for the pointer ("Click to reveal"); it stays
// open while the pointer is over it. Keyboard users need the same action
// elsewhere. `disabled` keeps it closed without changing the child's place in
// the tree. `open` holds it open whatever the pointer and focus do (a locked
// field's lock, after a try at typing).
export function Tooltip({
  content,
  placement = "top",
  onClick,
  disabled = false,
  open = false,
  children,
}: {
  content: ReactNode;
  placement?: keyof typeof sides;
  onClick?: () => void;
  disabled?: boolean;
  open?: boolean;
  children: ReactElement<{ "aria-describedby"?: string }>;
}) {
  const id = useId();
  const [shown, setShown] = useState(false);
  const visible = !disabled && (shown || open);
  const classes = cx(
    popoverPanel,
    "rounded-md px-2 py-1 text-xs/5 font-medium",
    "bg-zinc-900 text-white shadow-sm dark:bg-zinc-700 dark:ring-1 dark:ring-white/10 dark:ring-inset",
  );
  return (
    <BaseTooltip.Root
      open={visible}
      onOpenChange={setShown}
      disabled={disabled}
    >
      <BaseTooltip.Trigger
        render={cloneElement(children, {
          "aria-describedby":
            [children.props["aria-describedby"], visible ? id : undefined]
              .filter(Boolean)
              .join(" ") || undefined,
        })}
      />
      <BaseTooltip.Portal>
        <BaseTooltip.Positioner
          side={sides[placement]}
          sideOffset={6}
          className="z-[70]"
        >
          <BaseTooltip.Popup
            id={id}
            role="tooltip"
            className={onClick ? popoverPanel : classes}
          >
            {onClick ? (
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
              content
            )}
          </BaseTooltip.Popup>
        </BaseTooltip.Positioner>
      </BaseTooltip.Portal>
    </BaseTooltip.Root>
  );
}
