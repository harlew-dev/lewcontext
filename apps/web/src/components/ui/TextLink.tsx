import Link, { type LinkProps } from "next/link";
import type { ReactNode } from "react";
export function TextLink({
  children,
  ...props
}: LinkProps & { children: ReactNode }) {
  return (
    <Link
      className="inline-flex items-center gap-2 text-label font-medium max-sm:gap-1 max-sm:text-tiny"
      {...props}
    >
      {children}
    </Link>
  );
}
