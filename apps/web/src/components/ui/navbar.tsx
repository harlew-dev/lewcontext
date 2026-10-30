import {
  forwardRef,
  useId,
  type ComponentPropsWithoutRef,
  type ReactNode,
} from "react";
import { LayoutGroup, motion } from "motion/react";
import { cx } from "@/lib/cx";
import { TouchTarget } from "./button";
import { Clickable } from "./clickable";
export function Navbar({
  className,
  ...props
}: ComponentPropsWithoutRef<"nav">) {
  return (
    <nav
      {...props}
      className={cx(className, "flex flex-1 items-center gap-4 py-2.5")}
    />
  );
}
export function NavbarDivider({
  className,
  ...props
}: ComponentPropsWithoutRef<"div">) {
  return (
    <div
      aria-hidden="true"
      {...props}
      className={cx(className, "h-6 w-px bg-zinc-950/10 dark:bg-white/10")}
    />
  );
}
export function NavbarSection({
  className,
  ...props
}: ComponentPropsWithoutRef<"div">) {
  const id = useId();
  return (
    <LayoutGroup id={id}>
      <div {...props} className={cx(className, "flex items-center gap-3")} />
    </LayoutGroup>
  );
}
export function NavbarSpacer({
  className,
  ...props
}: ComponentPropsWithoutRef<"div">) {
  return (
    <div
      aria-hidden="true"
      {...props}
      className={cx(className, "-ml-4 flex-1")}
    />
  );
}
const itemClasses = cx(
  // Base (max-w-full: a button keeps its content's width otherwise, so a
  // long label couldn't truncate when the item shrinks)
  "relative flex max-w-full min-w-0 items-center gap-3 rounded-lg p-2 text-left text-base/6 font-medium text-zinc-950 sm:text-sm/5",
  // Leading icon/icon-only
  "*:data-[slot=icon]:size-6 *:data-[slot=icon]:shrink-0 *:data-[slot=icon]:fill-zinc-500 sm:*:data-[slot=icon]:size-5",
  // Trailing icon (down chevron or similar)
  "*:not-nth-2:last:data-[slot=icon]:ml-auto *:not-nth-2:last:data-[slot=icon]:size-5 sm:*:not-nth-2:last:data-[slot=icon]:size-4",
  // Avatar
  "*:data-[slot=avatar]:-m-0.5 *:data-[slot=avatar]:size-7 *:data-[slot=avatar]:[--avatar-radius:var(--radius-md)] sm:*:data-[slot=avatar]:size-6",
  // Hover
  "data-hover:bg-zinc-950/5 data-hover:*:data-[slot=icon]:fill-zinc-950",
  // Active
  "data-active:bg-zinc-950/5 data-active:*:data-[slot=icon]:fill-zinc-950",
  // Dark mode
  "dark:text-white dark:*:data-[slot=icon]:fill-zinc-400",
  "dark:data-hover:bg-white/5 dark:data-hover:*:data-[slot=icon]:fill-white",
  "dark:data-active:bg-white/5 dark:data-active:*:data-[slot=icon]:fill-white",
);
type NavbarItemProps = {
  current?: boolean;
  className?: string;
  children: ReactNode;
} & (
  | ({ href: string } & Omit<
      ComponentPropsWithoutRef<"a">,
      "href" | "className"
    >)
  | ({ href?: never } & Omit<ComponentPropsWithoutRef<"button">, "className">)
);
export const NavbarItem = forwardRef<HTMLElement, NavbarItemProps>(
  function NavbarItem({ current, className, children, ...props }, ref) {
    return (
      <span className={cx(className, "relative")}>
        {current && (
          <motion.span
            layoutId="current-indicator"
            className="absolute inset-x-2 -bottom-2.5 h-0.5 rounded-full bg-zinc-950 dark:bg-white"
          />
        )}
        <Clickable
          {...(props as ComponentPropsWithoutRef<"button">)}
          className={itemClasses}
          data-current={current ? "" : undefined}
          ref={ref}
        >
          <TouchTarget>{children}</TouchTarget>
        </Clickable>
      </span>
    );
  },
);
export function NavbarLabel({
  className,
  ...props
}: ComponentPropsWithoutRef<"span">) {
  return <span {...props} className={cx(className, "truncate")} />;
}
