import type { ReactNode } from "react";
import { useLocation, useMatches } from "react-router";
import { cx } from "@/lib/cx";
// Where a tab's content comes in from: the side of the tab chosen.
export type TransitionSide = "left" | "right";
const starting: Record<TransitionSide | "below", string> = {
  below: "starting:translate-y-1.5",
  left: "starting:-translate-x-3",
  right: "starting:translate-x-3",
};
// A page's content fades and rises into place as it opens: short, so moving
// between pages still feels instant. CSS `@starting-style` (`starting:`)
// plays it whenever the element is inserted, so the layouts key it by page
// (`usePageKey`). With `from`, it slides in from that side instead, for a
// tab's content. Browsers without it, and reduced motion, show it at once.
export function PageTransition({
  from,
  className,
  children,
}: {
  from?: TransitionSide;
  className?: string;
  children: ReactNode;
}) {
  return (
    <div
      className={cx(
        className,
        "transition duration-200 ease-out starting:opacity-0 motion-reduce:transition-none",
        starting[from ?? "below"],
      )}
    >
      {children}
    </div>
  );
}
// A route's `handle` marks it as a tab group: its child pages are tabs of
// one page, which slides its own content between them.
export type TransitionHandle = { tabGroup?: boolean };
// What a layout keys PageTransition by: the page's path, or the tab group's,
// so switching tabs isn't a new page.
export function usePageKey(): string {
  const { pathname } = useLocation();
  const group = useMatches().find(
    (match) => (match.handle as TransitionHandle | undefined)?.tabGroup,
  );
  return group?.pathname ?? pathname;
}
