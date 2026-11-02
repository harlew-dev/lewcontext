// Shared class strings for Base UI popups. Base UI keeps a popup mounted
// until its exit transition ends and marks the two ends with
// `data-starting-style` (entering) and `data-ending-style` (leaving), so a
// popup transitions between them in CSS.

// A popup panel (menu, listbox, tooltip): fades in and out.
export const popoverPanel =
  "transition-opacity duration-100 ease-out data-starting-style:opacity-0 data-ending-style:opacity-0 data-ending-style:ease-in motion-reduce:transition-none";

// How long a popup takes to close, for work that must wait for it (a menu
// item opening a dialog once focus is back on the menu's button).
export const popoverExit = 100;
