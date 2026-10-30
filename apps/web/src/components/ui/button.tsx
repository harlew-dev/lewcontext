import { forwardRef, type ReactNode, type Ref } from "react";
import {
  Button as BaseButton,
  type ButtonProps as BaseButtonProps,
} from "baseui/button";
import { slot } from "@/lib/baseui";
import { cx } from "@/lib/cx";
import { Link } from "./link";
const styles = {
  base: [
    "relative isolate inline-flex items-baseline justify-center gap-x-2 rounded-lg border text-base/6 font-semibold",
    "px-[calc(--spacing(3.5)-1px)] py-[calc(--spacing(2.5)-1px)] sm:px-[calc(--spacing(3)-1px)] sm:py-[calc(--spacing(1.5)-1px)] sm:text-sm/6",
    "focus:not-data-focus:outline-hidden data-focus:outline-2 data-focus:outline-offset-2 data-focus:outline-blue-500",
    "data-disabled:opacity-50",
    "*:data-[slot=icon]:-mx-0.5 *:data-[slot=icon]:my-0.5 *:data-[slot=icon]:size-5 *:data-[slot=icon]:shrink-0 *:data-[slot=icon]:self-center *:data-[slot=icon]:text-(--btn-icon) sm:*:data-[slot=icon]:my-1 sm:*:data-[slot=icon]:size-4 forced-colors:[--btn-icon:ButtonText] forced-colors:data-hover:[--btn-icon:ButtonText]",
    // Loading hides the label and icons; BaseUI renders the spinner on top.
    "data-loading:text-transparent data-loading:[--btn-icon:transparent]",
  ],
  solid: [
    "border-transparent bg-(--btn-border)",
    "dark:bg-(--btn-bg)",
    "before:absolute before:inset-0 before:-z-10 before:rounded-[calc(var(--radius-lg)-1px)] before:bg-(--btn-bg)",
    "before:shadow-sm",
    "dark:before:hidden",
    "dark:border-white/5",
    "after:absolute after:inset-0 after:-z-10 after:rounded-[calc(var(--radius-lg)-1px)]",
    "after:shadow-[inset_0_1px_--theme(--color-white/15%)]",
    "data-active:after:bg-(--btn-hover-overlay) data-hover:after:bg-(--btn-hover-overlay)",
    "dark:after:-inset-px dark:after:rounded-lg",
    "data-disabled:before:shadow-none data-disabled:after:shadow-none",
  ],
  outline: [
    "border-zinc-950/10 text-zinc-950 data-active:bg-zinc-950/2.5 data-hover:bg-zinc-950/2.5",
    "dark:border-white/15 dark:text-white dark:[--btn-bg:transparent] dark:data-active:bg-white/5 dark:data-hover:bg-white/5",
    "[--btn-icon:var(--color-zinc-500)] data-active:[--btn-icon:var(--color-zinc-700)] data-hover:[--btn-icon:var(--color-zinc-700)] dark:data-active:[--btn-icon:var(--color-zinc-400)] dark:data-hover:[--btn-icon:var(--color-zinc-400)]",
  ],
  plain: [
    "border-transparent text-zinc-950 data-active:bg-zinc-950/5 data-hover:bg-zinc-950/5",
    "dark:text-white dark:data-active:bg-white/10 dark:data-hover:bg-white/10",
    "[--btn-icon:var(--color-zinc-500)] data-active:[--btn-icon:var(--color-zinc-700)] data-hover:[--btn-icon:var(--color-zinc-700)] dark:[--btn-icon:var(--color-zinc-500)] dark:data-active:[--btn-icon:var(--color-zinc-400)] dark:data-hover:[--btn-icon:var(--color-zinc-400)]",
  ],
  colors: {
    "dark/zinc": [
      "text-white [--btn-bg:var(--color-zinc-900)] [--btn-border:var(--color-zinc-950)]/90 [--btn-hover-overlay:var(--color-white)]/10",
      "dark:text-white dark:[--btn-bg:var(--color-zinc-600)] dark:[--btn-hover-overlay:var(--color-white)]/5",
      "[--btn-icon:var(--color-zinc-400)] data-active:[--btn-icon:var(--color-zinc-300)] data-hover:[--btn-icon:var(--color-zinc-300)]",
    ],
  },
};
type Style =
  | { color?: keyof typeof styles.colors; outline?: never; plain?: never }
  | { color?: never; outline: true; plain?: never }
  | { color?: never; outline?: never; plain: true };
type ButtonElementProps = Omit<
  BaseButtonProps,
  "kind" | "size" | "shape" | "isLoading" | "overrides" | "children" | "href"
> & { href?: never };
type LinkElementProps = {
  href: string;
  "aria-label"?: string;
  "aria-current"?: "page";
  onClick?: () => void;
  // Next.js Link options, e.g. for query-only navigations such as pagination.
  shallow?: boolean;
  scroll?: boolean;
};
export type ButtonProps = Style & {
  className?: string;
  children: ReactNode;
  loading?: boolean;
} & (ButtonElementProps | LinkElementProps);
function buttonClasses({ color, outline, plain }: Style, className?: string) {
  return cx(
    ...styles.base,
    ...(outline
      ? styles.outline
      : plain
        ? styles.plain
        : [...styles.solid, ...styles.colors[color ?? "dark/zinc"]]),
    className,
  );
}
const ButtonRoot = slot("button", "cursor-default");
// Button on top of BaseUI's Button, which tracks hover, press and
// focus-visible. With `href` it renders a Next.js Link instead. Loading is drawn
// here rather than with BaseUI's isLoading, which collapses the content to 0px.
export const Button = forwardRef<HTMLElement, ButtonProps>(function Button(
  { color, outline, plain, className, children, loading = false, ...props },
  ref,
) {
  const classes = buttonClasses({ color, outline, plain } as Style, className);
  if (props.href !== undefined)
    return (
      <Link
        {...(props as LinkElementProps)}
        className={classes}
        ref={ref as Ref<HTMLAnchorElement>}
      >
        <TouchTarget>{children}</TouchTarget>
      </Link>
    );
  const buttonProps = props as ButtonElementProps;
  return (
    <BaseButton
      {...buttonProps}
      ref={ref as Ref<HTMLButtonElement>}
      type={buttonProps.type ?? "button"}
      disabled={buttonProps.disabled || loading}
      overrides={{
        BaseButton: {
          component: ButtonRoot,
          props: {
            className: classes,
            "data-loading": loading ? "" : undefined,
            "aria-busy": loading || undefined,
          },
        },
      }}
    >
      <TouchTarget>{children}</TouchTarget>
      {loading && (
        <span
          aria-hidden="true"
          className={cx(
            "absolute inset-0 flex items-center justify-center",
            outline || plain
              ? "text-zinc-500 dark:text-zinc-400"
              : "text-white",
          )}
        >
          <span className="size-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
        </span>
      )}
    </BaseButton>
  );
});
// Expands the hit area to at least 44×44px on touch devices.
export function TouchTarget({ children }: { children: ReactNode }) {
  return (
    <>
      <span
        className="absolute top-1/2 left-1/2 size-[max(100%,2.75rem)] -translate-x-1/2 -translate-y-1/2 pointer-fine:hidden"
        aria-hidden="true"
      />
      {children}
    </>
  );
}
