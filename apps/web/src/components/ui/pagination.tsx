import type { ComponentPropsWithoutRef, ReactNode } from "react";
import {
  ArrowLongLeftIcon,
  ArrowLongRightIcon,
} from "@heroicons/react/16/solid";
import { cx } from "@/lib/cx";
import { Button } from "./button";
export function Pagination({
  "aria-label": ariaLabel = "Page navigation",
  className,
  ...props
}: ComponentPropsWithoutRef<"nav">) {
  return (
    <nav
      aria-label={ariaLabel}
      {...props}
      className={cx(className, "flex gap-x-2")}
    />
  );
}
// Without `href` (on the first page) the link renders disabled.
export function PaginationPrevious({
  href = null,
  className,
  children = "Previous",
}: {
  href?: string | null;
  className?: string;
  children?: ReactNode;
}) {
  return (
    <span className={className}>
      <PageStep href={href} label="Previous page">
        <ArrowLongLeftIcon />
        {children}
      </PageStep>
    </span>
  );
}
export function PaginationNext({
  href = null,
  className,
  children = "Next",
}: {
  href?: string | null;
  className?: string;
  children?: ReactNode;
}) {
  return (
    <span className={className}>
      <PageStep href={href} label="Next page">
        {children}
        <ArrowLongRightIcon />
      </PageStep>
    </span>
  );
}
function PageStep({
  href,
  label,
  children,
}: {
  href: string | null;
  label: string;
  children: ReactNode;
}) {
  return href === null ? (
    <Button plain disabled aria-label={label}>
      {children}
    </Button>
  ) : (
    <Button plain href={href} aria-label={label}>
      {children}
    </Button>
  );
}
export function PaginationList({
  className,
  ...props
}: ComponentPropsWithoutRef<"span">) {
  return (
    <span
      {...props}
      className={cx(className, "hidden items-baseline gap-x-2 sm:flex")}
    />
  );
}
export function PaginationPage({
  href,
  className,
  current = false,
  children,
}: {
  href: string;
  className?: string;
  current?: boolean;
  children: number;
}) {
  return (
    <Button
      href={href}
      plain
      aria-label={`Page ${children}`}
      aria-current={current ? "page" : undefined}
      className={cx(
        className,
        "min-w-9 before:absolute before:-inset-px before:rounded-lg",
        current && "before:bg-zinc-950/5 dark:before:bg-white/10",
      )}
    >
      <span className="-mx-0.5">{children}</span>
    </Button>
  );
}
export function PaginationGap({
  className,
  children = <>&hellip;</>,
  ...props
}: ComponentPropsWithoutRef<"span">) {
  return (
    <span
      aria-hidden="true"
      {...props}
      className={cx(
        className,
        "w-9 text-center text-sm/6 font-semibold text-zinc-950 select-none dark:text-white",
      )}
    >
      {children}
    </span>
  );
}
