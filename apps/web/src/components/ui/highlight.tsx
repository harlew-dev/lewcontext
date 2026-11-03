import { createContext, useContext, useId, useState } from "react";
import { motion } from "motion/react";
import { cx } from "@/lib/cx";
// One background shared by a group of items (sidebar sections, tabs, menu
// items, listbox options) that slides to whichever item is hovered or
// focused. The group sits in its own LayoutGroup, so its `layoutId` never
// meets another group's.
type HighlightGroup = {
  active: string | null;
  setActive: (id: string | null) => void;
};
const HighlightContext = createContext<HighlightGroup | null>(null);
export const HighlightProvider = HighlightContext.Provider;
// The group's state, for the element that provides it (and clears it when
// the pointer leaves).
export function useHighlightGroup(): HighlightGroup {
  const [active, setActive] = useState<string | null>(null);
  return { active, setActive };
}
// One item of the group: whether it has the highlight, and how to take it.
// `grouped` is false outside a HighlightProvider.
export function useHighlight() {
  const group = useContext(HighlightContext);
  const id = useId();
  return {
    grouped: group !== null,
    active: group?.active === id,
    activate: () => group?.setActive(id),
  };
}
export const highlightTransition = {
  type: "spring",
  bounce: 0.15,
  duration: 0.3,
} as const;
// The sliding background. Render it inside the active item, which must be
// `relative`; `className` sets its colour (and stacking, where the item's
// content doesn't come after it). Forced-colours mode uses the system's own
// highlight instead. With `slide={false}` it's a plain background that stays
// put: for a panel still being positioned, where sliding would carry it in
// from wherever the panel first rendered. Switching to sliding remounts it in
// place, without animating.
export function Highlight({
  className,
  slide = true,
}: {
  className: string;
  slide?: boolean;
}) {
  const classes = cx(
    className,
    "absolute inset-0 rounded-lg forced-colors:hidden",
  );
  if (!slide) return <span aria-hidden="true" className={classes} />;
  return (
    <motion.span
      aria-hidden="true"
      layoutId="highlight"
      transition={highlightTransition}
      className={classes}
    />
  );
}
