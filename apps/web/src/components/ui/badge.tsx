import type { ComponentPropsWithoutRef, MouseEvent } from "react";
import { XMarkIcon } from "@heroicons/react/16/solid";
import { cx } from "@/lib/cx";
const colors = {
  zinc: "bg-zinc-600/10 text-zinc-700 group-data-hover:bg-zinc-600/20 dark:bg-white/5 dark:text-zinc-400 dark:group-data-hover:bg-white/10",
  lime: "bg-lime-400/20 text-lime-700 group-data-hover:bg-lime-400/30 dark:bg-lime-400/10 dark:text-lime-300 dark:group-data-hover:bg-lime-400/15",
  purple:
    "bg-purple-500/15 text-purple-700 group-data-hover:bg-purple-500/25 dark:text-purple-400 dark:group-data-hover:bg-purple-500/20",
  pink: "bg-pink-400/15 text-pink-700 group-data-hover:bg-pink-400/25 dark:bg-pink-400/10 dark:text-pink-400 dark:group-data-hover:bg-pink-400/20",
  // Marks something apart from the app, e.g. the admin dashboard.
  red: "bg-red-500/15 text-red-700 group-data-hover:bg-red-500/25 dark:bg-red-500/10 dark:text-red-400 dark:group-data-hover:bg-red-500/20",
  rose: "bg-rose-400/15 text-rose-700 group-data-hover:bg-rose-400/25 dark:bg-rose-400/10 dark:text-rose-400 dark:group-data-hover:bg-rose-400/20",
};
export type BadgeColor = keyof typeof colors;
// A small label. With `onRemove` it ends with a × button (named by
// `removeLabel`, e.g. "Remove filter: IP 203.0.113.0/24"), for something
// applied that can be taken away: a filter, a chip in a Combobox. The ×
// keeps its click to itself, so a badge inside something clickable doesn't
// click that too.
export function Badge({
  color = "zinc",
  className,
  onRemove,
  removeLabel = "Remove",
  children,
  ...props
}: {
  color?: BadgeColor;
  onRemove?: () => void;
  removeLabel?: string;
} & ComponentPropsWithoutRef<"span">) {
  const remove = (e: MouseEvent) => {
    e.stopPropagation();
    onRemove?.();
  };
  return (
    <span
      {...props}
      className={cx(
        className,
        "inline-flex items-center gap-x-1.5 rounded-md px-1.5 py-0.5 text-sm/5 font-medium sm:text-xs/5 forced-colors:outline",
        onRemove && "pr-0.5",
        colors[color],
      )}
    >
      {children}
      {onRemove && (
        <button
          type="button"
          aria-label={removeLabel}
          onClick={remove}
          onMouseDown={(e) => e.stopPropagation()}
          onTouchEnd={(e) => e.stopPropagation()}
          className="relative grid size-5 shrink-0 place-items-center rounded-sm opacity-70 hover:bg-zinc-950/10 hover:opacity-100 focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-blue-500 sm:size-4 dark:hover:bg-white/10"
        >
          <XMarkIcon className="size-4 sm:size-3.5" />
          {/* A 44px target on touch screens, as TouchTarget gives buttons. */}
          <span
            aria-hidden="true"
            className="absolute top-1/2 left-1/2 size-[max(100%,2.75rem)] -translate-x-1/2 -translate-y-1/2 pointer-fine:hidden"
          />
        </button>
      )}
    </span>
  );
}
