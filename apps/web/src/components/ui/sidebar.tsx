import {
  forwardRef,
  useContext,
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
import { SidebarToggleContext, useCloseSidebar } from "./sidebar-context";
export function Sidebar({
  className,
  ...props
}: ComponentPropsWithoutRef<"nav">) {
  return (
    <nav {...props} className={cx(className, "flex h-full min-h-0 flex-col")} />
  );
}
export function SidebarHeader({
  className,
  ...props
}: ComponentPropsWithoutRef<"div">) {
  return (
    <div
      {...props}
      className={cx(
        className,
        "flex flex-col border-b border-zinc-950/5 p-4 dark:border-white/5 [&>[data-slot=section]+[data-slot=section]]:mt-2.5",
      )}
    />
  );
}
export function SidebarBody({
  className,
  ...props
}: ComponentPropsWithoutRef<"div">) {
  return (
    <div
      {...props}
      className={cx(
        className,
        "flex flex-1 flex-col overflow-y-auto p-4 [&>[data-slot=section]+[data-slot=section]]:mt-8",
      )}
    />
  );
}
export function SidebarFooter({
  className,
  ...props
}: ComponentPropsWithoutRef<"div">) {
  return (
    <div
      {...props}
      className={cx(
        className,
        "flex flex-col border-t border-zinc-950/5 p-4 dark:border-white/5 [&>[data-slot=section]+[data-slot=section]]:mt-2.5",
      )}
    />
  );
}
// Each section is a LayoutGroup, so its current-page indicator and hover
// background (a Highlight on the item the mouse is over) slide between items
// (the app layout route stays mounted).
export function SidebarSection({
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
          data-slot="section"
          onPointerLeave={(event) => {
            highlight.setActive(null);
            onPointerLeave?.(event);
          }}
          className={cx(className, "flex flex-col gap-0.5")}
        />
      </HighlightProvider>
    </LayoutGroup>
  );
}
export function SidebarDivider({
  className,
  ...props
}: ComponentPropsWithoutRef<"hr">) {
  return (
    <hr
      {...props}
      className={cx(
        className,
        "my-4 border-t border-zinc-950/5 lg:-mx-4 dark:border-white/5",
      )}
    />
  );
}
export function SidebarSpacer({
  className,
  ...props
}: ComponentPropsWithoutRef<"div">) {
  return (
    <div
      aria-hidden="true"
      {...props}
      className={cx(className, "mt-8 flex-1")}
    />
  );
}
export function SidebarHeading({
  className,
  ...props
}: ComponentPropsWithoutRef<"h3">) {
  return (
    <h3
      {...props}
      className={cx(
        className,
        "mb-1 px-2 text-xs/6 font-medium text-zinc-500 dark:text-zinc-400",
      )}
    />
  );
}
const itemClasses = cx(
  // Base
  "flex w-full items-center gap-3 rounded-lg px-2 py-2.5 text-left text-base/6 font-medium text-zinc-950 sm:py-2 sm:text-sm/5",
  // Leading icon/icon-only
  "*:data-[slot=icon]:size-6 *:data-[slot=icon]:shrink-0 *:data-[slot=icon]:fill-zinc-500 sm:*:data-[slot=icon]:size-5",
  // Trailing icon (down chevron or similar), but not the icon of an icon-only item
  "*:not-nth-2:last:data-[slot=icon]:ml-auto *:not-nth-2:last:data-[slot=icon]:size-5 sm:*:not-nth-2:last:data-[slot=icon]:size-4",
  // Avatar
  "*:data-[slot=avatar]:-m-0.5 *:data-[slot=avatar]:size-7 sm:*:data-[slot=avatar]:size-6",
  // Hover (the background comes from the section's sliding highlight, or from
  // `hoverBackground` for items outside a section)
  "data-hover:*:data-[slot=icon]:fill-zinc-950",
  // Active
  "data-active:bg-zinc-950/5 data-active:*:data-[slot=icon]:fill-zinc-950",
  // Current
  "data-current:*:data-[slot=icon]:fill-zinc-950",
  // Dark mode
  "dark:text-white dark:*:data-[slot=icon]:fill-zinc-400",
  "dark:data-hover:*:data-[slot=icon]:fill-white",
  "dark:data-active:bg-white/5 dark:data-active:*:data-[slot=icon]:fill-white",
  "dark:data-current:*:data-[slot=icon]:fill-white",
);
const hoverBackground = "data-hover:bg-zinc-950/5 dark:data-hover:bg-white/5";
type SidebarItemProps = {
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
export const SidebarItem = forwardRef<HTMLElement, SidebarItemProps>(
  function SidebarItem({ current, className, children, ...props }, ref) {
    const closeSidebar = useCloseSidebar();
    const highlight = useHighlight();
    const shared = {
      // `relative` keeps the label above the absolutely positioned highlight.
      className: cx(
        itemClasses,
        highlight.grouped ? "relative" : hoverBackground,
      ),
      "data-current": current ? "" : undefined,
      "aria-current": current ? ("page" as const) : undefined,
    };
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
            className="absolute inset-y-2 -left-4 w-0.5 rounded-full bg-zinc-950 dark:bg-white"
          />
        )}
        {"href" in props && props.href ? (
          <Clickable
            {...props}
            href={props.href}
            {...shared}
            ref={ref}
            onClick={(event) => {
              props.onClick?.(event);
              closeSidebar();
            }}
          >
            <TouchTarget>{children}</TouchTarget>
          </Clickable>
        ) : (
          <Clickable
            {...(props as ComponentPropsWithoutRef<"button">)}
            {...shared}
            ref={ref}
          >
            <TouchTarget>{children}</TouchTarget>
          </Clickable>
        )}
      </span>
    );
  },
);
export function SidebarLabel({
  className,
  ...props
}: ComponentPropsWithoutRef<"span">) {
  return <span {...props} className={cx(className, "truncate")} />;
}
// Icon-only item beside the workspace menu that hides the sidebar (or closes
// it on mobile). A SidebarItem, so it is exactly as tall as the menu's button.
export function SidebarToggle({ className }: { className?: string }) {
  const toggle = useContext(SidebarToggleContext);
  if (!toggle) return null;
  return (
    <SidebarItem
      className={cx(className, toggle.className, "shrink-0")}
      aria-label={toggle.label}
      onClick={toggle.onClick}
    >
      {toggle.icon}
    </SidebarItem>
  );
}
