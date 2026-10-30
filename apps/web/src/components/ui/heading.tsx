import type { ComponentPropsWithoutRef } from "react";
import { cx } from "@/lib/cx";
type HeadingProps = {
  level?: 1 | 2 | 3 | 4 | 5 | 6;
} & ComponentPropsWithoutRef<"h1">;
export function Heading({ className, level = 1, ...props }: HeadingProps) {
  const Element = `h${level}` as const;
  return (
    <Element
      {...props}
      className={cx(
        className,
        "text-2xl/8 font-semibold text-zinc-950 sm:text-xl/8 dark:text-white",
      )}
    />
  );
}
export function Subheading({ className, level = 2, ...props }: HeadingProps) {
  const Element = `h${level}` as const;
  return (
    <Element
      {...props}
      className={cx(
        className,
        "text-base/7 font-semibold text-zinc-950 sm:text-sm/6 dark:text-white",
      )}
    />
  );
}
