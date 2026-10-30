import { forwardRef, type ElementType, type Ref } from "react";
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
  // Modal and Drawer report $isVisible=false while entering and leaving.
  if (state.$isVisible === false) attributes["data-closed"] = "";
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
