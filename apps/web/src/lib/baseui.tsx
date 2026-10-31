import {
  forwardRef,
  useEffect,
  useState,
  type ElementType,
  type Ref,
} from "react";
import { cx } from "./cx";
type State = Record<string, unknown>;
type SlotOptions = {
  // Extra DOM props derived from BaseUI state, e.g. aria-expanded or an inline width.
  props?: (state: State) => Record<string, unknown>;
  // State props not to map, e.g. menu options get $isFocused when the whole menu has
  // focus but should only highlight on $isHighlighted.
  ignore?: string[];
};
// BaseUI state prop -> the data attribute the Tailwind classes use.
const stateAttributes: Record<string, string> = {
  $isHovered: "data-hover",
  $isPressed: "data-active",
  $isFocusVisible: "data-focus",
  $isFocused: "data-focus",
  $isHighlighted: "data-focus",
  $disabled: "data-disabled",
  $error: "data-invalid",
  $isOpen: "data-open",
  $selected: "data-selected",
  $isLoading: "data-loading",
};
function dataAttributes(state: State, ignore: string[] = []) {
  const attributes: Record<string, string> = {};
  for (const [prop, attribute] of Object.entries(stateAttributes))
    if (state[prop] && !ignore.includes(prop)) attributes[attribute] = "";
  // Modal and Drawer report $isVisible=false before entering, and keep it true
  // with $isOpen=false while leaving (they unmount 500 ms later). Both are
  // the closed state, so data-closed transitions run in and out.
  if (
    state.$isVisible === false ||
    (state.$isVisible === true && state.$isOpen === false)
  )
    attributes["data-closed"] = "";
  return attributes;
}
// Builds a BaseUI override component that renders `tag` with Tailwind classes.
// BaseUI keeps its behaviour (focus management, keyboard handling, positioning);
// its `$` state props become data attributes and never reach the DOM.
export function slot(
  tag: ElementType,
  className: string,
  options: SlotOptions = {},
) {
  const Slot = forwardRef(function Slot(
    props: Record<string, unknown>,
    ref: Ref<HTMLElement>,
  ) {
    const state: State = {};
    const rest: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(props))
      (key.startsWith("$") ? state : rest)[key] = value;
    const Tag = tag;
    return (
      <Tag
        ref={ref}
        {...dataAttributes(state, options.ignore)}
        {...options.props?.(state)}
        {...rest}
        className={cx(className, rest.className as string | undefined)}
      />
    );
  });
  return Slot;
}
// Neutralises a BaseUI layer's own surface so a Tailwind panel can render inside it.
export const transparentSurface = {
  style: {
    backgroundColor: "transparent",
    boxShadow: "none",
    borderTopLeftRadius: 0,
    borderTopRightRadius: 0,
    borderBottomLeftRadius: 0,
    borderBottomRightRadius: 0,
  },
};
// Popovers (Dropdown, Listbox). BaseUI fades a popover by animating its
// Body's opacity, but an ancestor with opacity below 1 switches off a child's
// backdrop-blur, so the frosted panel turned see-through the moment it
// started to close. Instead the Body stays opaque (it still slides), Inner
// marks the closed state with data-closed, and the panel fades itself with
// `popoverPanel`.
const PopoverInner = slot("div", "", {
  props: (state) =>
    state.$isAnimating && state.$isOpen ? {} : { "data-closed": "" },
});
export const popoverOverrides = {
  Body: { style: { ...transparentSurface.style, opacity: 1 } },
  Inner: { component: PopoverInner },
};
// Classes for the panel inside a popover: fades in and out with it.
export const popoverPanel =
  "transition-opacity duration-100 ease-out in-data-closed:opacity-0 in-data-closed:ease-in motion-reduce:transition-none";
// How long a popover takes to close (pass as animateOutTime); without it
// BaseUI removes it at once.
export const popoverExit = 100;
// BaseUI positions a popover during its first frames, while it fades in. A
// sliding Highlight measured before then would slide in from wherever the
// panel first rendered, so until it has settled the highlight is drawn in
// place (`<Highlight slide={settled}>`).
export const popoverSettle = 150;
export function usePopoverSettled(open: boolean): boolean {
  const [settled, setSettled] = useState(false);
  useEffect(() => {
    if (!open) return;
    const timer = window.setTimeout(() => setSettled(true), popoverSettle);
    return () => {
      window.clearTimeout(timer);
      setSettled(false);
    };
  }, [open]);
  return settled;
}
