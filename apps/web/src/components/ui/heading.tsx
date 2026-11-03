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
// A heading with the line under it that describes it, kept close (8px) so
// the line reads as its subheading rather than the next section of a form or
// page laid out with a wider gap: the sign-in pages, status pages. Pass the
// Heading and its Text as children.
export function HeadingGroup({
  className,
  ...props
}: ComponentPropsWithoutRef<"hgroup">) {
  return <hgroup {...props} className={cx(className, "space-y-2")} />;
}
