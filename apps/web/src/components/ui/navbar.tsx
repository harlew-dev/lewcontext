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
import {
  Highlight,
  HighlightProvider,
  useHighlight,
  useHighlightGroup,
} from "./highlight";
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
// Each section is a LayoutGroup, so its current-page indicator and hover
// background (a Highlight on the item the mouse is over) slide between items.
export function NavbarSection({
  className,
  onPointerLeave,
  ...props
}: ComponentPropsWithoutRef<"div">) {
  const id = useId();
  const highlight = useHighlightGroup();
  return (
    <LayoutGroup id={id}>
      <HighlightProvider value={highlight}>
        <div
          {...props}
          onPointerLeave={(event) => {
            highlight.setActive(null);
            onPointerLeave?.(event);
          }}
          className={cx(className, "flex items-center gap-3")}
        />
      </HighlightProvider>
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
  // Hover (the background comes from the section's sliding highlight, or from
  // `hoverBackground` for items outside a section)
  "data-hover:*:data-[slot=icon]:fill-zinc-950",
  // Active
  "data-active:bg-zinc-950/5 data-active:*:data-[slot=icon]:fill-zinc-950",
  // Dark mode
  "dark:text-white dark:*:data-[slot=icon]:fill-zinc-400",
  "dark:data-hover:*:data-[slot=icon]:fill-white",
  "dark:data-active:bg-white/5 dark:data-active:*:data-[slot=icon]:fill-white",
  // Selected: a menu button whose menu holds the current page (the account
  // menu on account settings)
  "data-selected:bg-zinc-950/5 data-selected:*:data-[slot=icon]:fill-zinc-950",
  "dark:data-selected:bg-white/5 dark:data-selected:*:data-[slot=icon]:fill-white",
);
const hoverBackground = "data-hover:bg-zinc-950/5 dark:data-hover:bg-white/5";
// `current`: this item's link is the current page (the sliding indicator).
// `selected`: a grey background for a menu button whose menu holds the
// current page; the menu marks the page itself.
type NavbarItemProps = {
  current?: boolean;
  selected?: boolean;
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
  function NavbarItem(
    { current, selected, className, children, ...props },
    ref,
  ) {
    const highlight = useHighlight();
    return (
      <span
        className={cx(className, "relative")}
        onPointerEnter={(event) => {
          if (event.pointerType === "mouse") highlight.activate();
        }}
      >
        {highlight.active && (
          <Highlight className="bg-zinc-950/5 dark:bg-white/5" />
        )}
        {current && (
          <motion.span
            layoutId="current-indicator"
            className="absolute inset-x-2 -bottom-2.5 h-0.5 rounded-full bg-zinc-950 dark:bg-white"
          />
        )}
        <Clickable
          {...(props as ComponentPropsWithoutRef<"button">)}
          className={cx(itemClasses, !highlight.grouped && hoverBackground)}
          data-current={current ? "" : undefined}
          data-selected={selected ? "" : undefined}
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
