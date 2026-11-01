import {
  Children,
  createContext,
  forwardRef,
  isValidElement,
  useContext,
  useEffect,
  useId,
  useRef,
  useState,
  type ComponentPropsWithoutRef,
  type ElementType,
  type FocusEvent,
  type KeyboardEvent,
  type PointerEvent,
  type MouseEvent,
  type ReactElement,
  type ReactNode,
} from "react";
import { PLACEMENT, StatefulPopover } from "baseui/popover";
import { LayoutGroup, useReducedMotion } from "motion/react";
import {
  popoverExit,
  popoverOverrides,
  popoverPanel,
  usePopoverSettled,
} from "@/lib/baseui";
import { cx } from "@/lib/cx";
import { ReturnFocus, focusedElement } from "@/lib/return-focus";
import { usePopoverScrollLock } from "@/lib/scroll-lock";
import { Button } from "./button";
import {
  Highlight,
  HighlightProvider,
  useHighlight,
  useHighlightGroup,
} from "./highlight";
import { Link } from "./link";
const placements = {
  bottom: PLACEMENT.bottom,
  "bottom start": PLACEMENT.bottomLeft,
  "bottom end": PLACEMENT.bottomRight,
  top: PLACEMENT.top,
  "top start": PLACEMENT.topLeft,
  "top end": PLACEMENT.topRight,
};
type Anchor = keyof typeof placements;
// Long enough for the menu's exit animation and focus return.
const afterClose = popoverExit + 50;
// `settled`: the open menu has settled where BaseUI positions it, so its
// highlight may slide (`usePopoverSettled`).
const DropdownContext = createContext<{ close: () => void; settled: boolean }>({
  close: () => {},
  settled: false,
});
export type DropdownMenuProps = {
  anchor?: Anchor;
  className?: string;
  children: ReactNode;
};
// Dropdown on BaseUI's StatefulPopover (click trigger, positioning,
// focus lock, Escape and outside-click dismissal, menu ARIA on the trigger).
// Children: the trigger (a DropdownButton) and then the menu
// (a DropdownMenu, or a component rendering one that accepts `anchor`).
export function Dropdown({ children }: { children: ReactNode }) {
  const [trigger, menu] = Children.toArray(children).filter(isValidElement) as [
    ReactElement,
    ReactElement<Pick<DropdownMenuProps, "anchor">>,
  ];
  const reducedMotion = useReducedMotion();
  const [open, setOpen] = useState(false);
  // The trigger, focused when the menu opens; focus returns to it on close.
  const [opener, setOpener] = useState<HTMLElement | null>(null);
  usePopoverScrollLock(open);
  const settled = usePopoverSettled(open);
  if (!trigger || !menu) return null;
  return (
    <StatefulPopover
      accessibilityType="menu"
      triggerType="click"
      placement={placements[menu.props.anchor ?? "bottom"]}
      popoverMargin={8}
      animateOutTime={reducedMotion ? 0 : popoverExit}
      onOpen={() => {
        setOpener(focusedElement());
        setOpen(true);
      }}
      onClose={() => setOpen(false)}
      focusLock
      autoFocus
      returnFocus={false}
      overrides={popoverOverrides}
      content={({ close }) => (
        <DropdownContext.Provider value={{ close, settled }}>
          {menu}
          <ReturnFocus opener={opener} />
        </DropdownContext.Provider>
      )}
    >
      {trigger}
    </StatefulPopover>
  );
}
// Renders the trigger as `as` (Button by default), forwarding the ref and the
// popover's click/ARIA props.
type DropdownButtonProps = {
  as?: ElementType;
  children?: ReactNode;
  className?: string;
  "aria-label"?: string;
  // Button styles, when `as` is the default Button (e.g. a plain ⋯ trigger).
  plain?: true;
  outline?: true;
  // When `as` is a SidebarItem or NavbarItem: its menu holds the current page.
  selected?: boolean;
};
export const DropdownButton = forwardRef<HTMLElement, DropdownButtonProps>(
  function DropdownButton({ as, ...props }, ref) {
    const Component: ElementType = as ?? Button;
    return <Component ref={ref} {...props} />;
  },
);
function moveFocus(event: KeyboardEvent<HTMLDivElement>) {
  const keys = ["ArrowDown", "ArrowUp", "Home", "End"];
  if (!keys.includes(event.key)) return;
  event.preventDefault();
  const items = Array.from(
    event.currentTarget.querySelectorAll<HTMLElement>('[role="menuitem"]'),
  );
  const index = items.indexOf(document.activeElement as HTMLElement);
  const next =
    event.key === "Home"
      ? 0
      : event.key === "End"
        ? items.length - 1
        : (index + (event.key === "ArrowDown" ? 1 : -1) + items.length) %
          items.length;
  items[next]?.focus();
}
// Where focus starts when the menu opens, and returns when the mouse leaves
// it: the `selected` item (the page you're on), or else the menu itself, so no
// item is active until the pointer or arrow keys move to one (as Headless UI
// does).
function focusHome(menu: HTMLElement) {
  const selected = menu.querySelector<HTMLElement>(
    '[role="menuitem"][data-selected]',
  );
  (selected ?? menu).focus({ preventScroll: true });
}
// The focused item's background is a Highlight that slides from item to item;
// it goes when focus leaves the items.
export function DropdownMenu({ className, children }: DropdownMenuProps) {
  const id = useId();
  const highlight = useHighlightGroup();
  const panel = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const menu = panel.current;
    if (menu?.querySelector("[data-selected]")) focusHome(menu);
  }, []);
  function leaveItems(event: FocusEvent<HTMLDivElement>) {
    const next = event.relatedTarget;
    const toItem =
      next instanceof Element &&
      event.currentTarget.contains(next) &&
      next.closest('[role="menuitem"]') !== null;
    if (!toItem) highlight.setActive(null);
  }
  return (
    <LayoutGroup id={id}>
      <HighlightProvider value={highlight}>
        <MenuPanel
          ref={panel}
          className={className}
          onBlur={leaveItems}
          onPointerLeave={(event) => {
            if (event.pointerType === "mouse") focusHome(event.currentTarget);
          }}
        >
          {children}
        </MenuPanel>
      </HighlightProvider>
    </LayoutGroup>
  );
}
const MenuPanel = forwardRef<
  HTMLDivElement,
  {
    className?: string;
    onBlur: (event: FocusEvent<HTMLDivElement>) => void;
    onPointerLeave: (event: PointerEvent<HTMLDivElement>) => void;
    children: ReactNode;
  }
>(function MenuPanel({ className, onBlur, onPointerLeave, children }, ref) {
  return (
    <div
      ref={ref}
      role="menu"
      // The popover's focus lock focuses the menu itself (data-autofocus); then
      // DropdownMenu moves focus to a `selected` item, if there is one.
      tabIndex={-1}
      data-autofocus
      data-popover-panel
      onKeyDown={moveFocus}
      onBlur={onBlur}
      onPointerLeave={onPointerLeave}
      className={cx(
        className,
        "isolate w-max rounded-xl p-1",
        popoverPanel,
        "outline outline-transparent focus:outline-hidden",
        "overflow-y-auto",
        "bg-white/75 backdrop-blur-xl dark:bg-zinc-800/75",
        "shadow-lg ring-1 ring-zinc-950/10 dark:ring-white/10 dark:ring-inset",
        "supports-[grid-template-columns:subgrid]:grid supports-[grid-template-columns:subgrid]:grid-cols-[auto_1fr_1.5rem_0.5rem_auto]",
      )}
    >
      {children}
    </div>
  );
});
// Headless UI marks the hovered or keyboard-active item with data-focus; here the
// item takes real focus on hover, so `focus:` gives the same highlight. Its
// background is the menu's sliding grey Highlight (-z-10 in the menu's
// `isolate` stacking context), so while it slides it passes under every
// item's text.
const itemClasses = cx(
  "group relative cursor-default rounded-lg px-3.5 py-2.5 focus:outline-hidden sm:px-3 sm:py-1.5",
  "text-left text-base/6 sm:text-sm/6 forced-colors:text-[CanvasText]",
  "disabled:opacity-50",
  "forced-color-adjust-none forced-colors:focus:bg-[Highlight] forced-colors:focus:text-[HighlightText] forced-colors:focus:*:data-[slot=icon]:text-[HighlightText]",
  "col-span-full grid grid-cols-[auto_1fr_1.5rem_0.5rem_auto] items-center supports-[grid-template-columns:subgrid]:grid-cols-subgrid",
  "*:data-[slot=icon]:col-start-1 *:data-[slot=icon]:row-start-1 *:data-[slot=icon]:mr-2.5 *:data-[slot=icon]:-ml-0.5 *:data-[slot=icon]:size-5 sm:*:data-[slot=icon]:mr-2 sm:*:data-[slot=icon]:size-4",
  "*:data-[slot=avatar]:mr-2.5 *:data-[slot=avatar]:-ml-1 *:data-[slot=avatar]:size-6 sm:*:data-[slot=avatar]:mr-2 sm:*:data-[slot=avatar]:size-5",
);
// Text and icon colours for each item variant (a focused default item's icon
// darkens, as in the sidebar); "danger" is for destructive actions (Delete),
// never for Sign out, which isn't one.
const itemVariants = {
  default:
    "text-zinc-950 *:data-[slot=icon]:text-zinc-500 focus:*:data-[slot=icon]:text-zinc-950 dark:text-white dark:*:data-[slot=icon]:text-zinc-400 dark:focus:*:data-[slot=icon]:text-white",
  danger:
    "text-red-600 *:data-[slot=icon]:text-red-500 dark:text-red-400 dark:*:data-[slot=icon]:text-red-400",
};

// Items close the menu when chosen, then act once it has closed and returned
// focus to its trigger, so a dialog an item opens returns focus there too
// (the item itself is gone by then). `keepOpen` items (e.g. "Show more") act
// at once, inside the menu.
type DropdownItemProps = {
  className?: string;
  children: ReactNode;
  onClick?: (event: MouseEvent<HTMLElement>) => void;
  "aria-current"?: "page";
  variant?: keyof typeof itemVariants;
  // The page you're on (Workspace settings on workspace settings): the menu
  // opens with it focused (and highlighted), and returns focus to it when the
  // mouse leaves.
  selected?: boolean;
} & (
  | { href: string; keepOpen?: never }
  | { href?: never; disabled?: boolean; keepOpen?: boolean }
);
export function DropdownItem({
  className,
  onClick,
  keepOpen = false,
  variant = "default",
  selected = false,
  children,
  ...props
}: DropdownItemProps) {
  const { close, settled } = useContext(DropdownContext);
  const highlight = useHighlight();
  const content = (
    <>
      {highlight.active && (
        <Highlight
          slide={settled}
          className="-z-10 bg-zinc-950/5 dark:bg-white/10"
        />
      )}
      {children}
    </>
  );
  const shared = {
    role: "menuitem",
    "data-selected": selected ? "" : undefined,
    className: cx(className, itemClasses, itemVariants[variant]),
    children: content,
    onFocus: () => highlight.activate(),
    onPointerEnter: (event: React.PointerEvent<HTMLElement>) =>
      event.currentTarget.focus(),
    onClick: (event: MouseEvent<HTMLElement>) => {
      if (keepOpen) return onClick?.(event);
      close();
      if (onClick) window.setTimeout(() => onClick(event), afterClose);
    },
  };
  if ("href" in props && props.href)
    return <Link {...props} href={props.href} {...shared} />;
  return <button type="button" {...props} {...shared} />;
}
export function DropdownHeader({
  className,
  ...props
}: ComponentPropsWithoutRef<"div">) {
  return (
    <div
      {...props}
      className={cx(className, "col-span-5 px-3.5 pt-2.5 pb-1 sm:px-3")}
    />
  );
}
export function DropdownLabel({
  className,
  ...props
}: ComponentPropsWithoutRef<"div">) {
  return (
    <div
      data-slot="label"
      {...props}
      className={cx(className, "col-start-2 row-start-1")}
    />
  );
}
export function DropdownDivider({
  className,
  ...props
}: ComponentPropsWithoutRef<"div">) {
  return (
    <div
      role="separator"
      {...props}
      className={cx(
        className,
        "col-span-full mx-3.5 my-1 h-px border-0 bg-zinc-950/5 sm:mx-3 dark:bg-white/10 forced-colors:bg-[CanvasText]",
      )}
    />
  );
}
