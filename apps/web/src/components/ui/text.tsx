import {
  forwardRef,
  type ButtonHTMLAttributes,
  type ComponentPropsWithoutRef,
} from "react";
import { cx } from "@/lib/cx";
import { Clickable } from "./clickable";
import { Link, type LinkProps } from "./link";
export function Text({ className, ...props }: ComponentPropsWithoutRef<"p">) {
  return (
    <p
      data-slot="text"
      {...props}
      className={cx(
        className,
        "text-base/6 text-zinc-500 sm:text-sm/6 dark:text-zinc-400",
      )}
    />
  );
}
const textLinkClasses =
  "text-zinc-950 underline decoration-zinc-950/50 data-hover:decoration-zinc-950 dark:text-white dark:decoration-white/50 dark:data-hover:decoration-white";
export function TextLink({ className, ...props }: LinkProps) {
  return <Link {...props} className={cx(className, textLinkClasses)} />;
}
// Looks like TextLink, for an action in running text that doesn't navigate:
// switching a form to another way of answering ("Use a recovery code").
export const TextButton = forwardRef<
  HTMLElement,
  ButtonHTMLAttributes<HTMLButtonElement>
>(function TextButton({ className, ...props }, ref) {
  return (
    <Clickable
      {...props}
      ref={ref}
      className={cx(
        className,
        textLinkClasses,
        "data-focus:outline-2 data-focus:outline-offset-2 data-focus:outline-blue-500",
      )}
    />
  );
});
export function Strong({
  className,
  ...props
}: ComponentPropsWithoutRef<"strong">) {
  return (
    <strong
      {...props}
      className={cx(className, "font-medium text-zinc-950 dark:text-white")}
    />
  );
}
export function Code({
  className,
  ...props
}: ComponentPropsWithoutRef<"code">) {
  return (
    <code
      {...props}
      className={cx(
        className,
        "rounded-sm border border-zinc-950/10 bg-zinc-950/2.5 px-0.5 text-sm font-medium text-zinc-950 sm:text-[0.8125rem] dark:border-white/20 dark:bg-white/5 dark:text-white",
      )}
    />
  );
}
