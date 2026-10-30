import NextLink, { type LinkProps as NextLinkProps } from "next/link";
import { forwardRef, type AnchorHTMLAttributes } from "react";
import { useInteractive } from "@/lib/interactive";
export type LinkProps = NextLinkProps &
  Omit<AnchorHTMLAttributes<HTMLAnchorElement>, keyof NextLinkProps>;
// Next.js client-side link that exposes data-hover/active/focus for the Tailwind styles.
export const Link = forwardRef<HTMLAnchorElement, LinkProps>(
  function Link(props, ref) {
    const interactive = useInteractive<HTMLAnchorElement>(props);
    return <NextLink ref={ref} {...props} {...interactive} />;
  },
);
