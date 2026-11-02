import { forwardRef } from "react";
import {
  Link as RouterLink,
  type LinkProps as RouterLinkProps,
} from "react-router";
import { useInteractive } from "@/lib/interactive";
export type LinkProps = Omit<RouterLinkProps, "to"> & { href: string };
// React Router client-side link that exposes data-hover/active/focus for the
// Tailwind styles. `preventScrollReset` keeps the scroll position, e.g. when
// re-sorting a table.
export const Link = forwardRef<HTMLAnchorElement, LinkProps>(function Link(
  { href, ...props },
  ref,
) {
  const interactive = useInteractive<HTMLAnchorElement>(props);
  return <RouterLink ref={ref} to={href} {...props} {...interactive} />;
});
