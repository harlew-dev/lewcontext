import type { ReactNode } from "react";
import { cx } from "@/lib/cx";
// A page's content fades and rises into place as it opens: short, so moving
// between pages still feels instant. CSS `@starting-style` (`starting:`)
// plays it whenever the element is inserted, so the layouts key it by page.
// Browsers without it, and reduced motion, show the page at once.
export function PageTransition({
  className,
  children,
}: {
  className?: string;
  children: ReactNode;
}) {
  return (
    <div
      className={cx(
        className,
        "transition duration-200 ease-out starting:translate-y-1.5 starting:opacity-0 motion-reduce:transition-none",
      )}
    >
      {children}
    </div>
  );
}
