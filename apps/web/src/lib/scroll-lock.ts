import { useEffect, useLayoutEffect } from "react";
// How many open overlays hold the lock; the page scrolls again when the last
// one releases it. Browser-only state: effects never run on the server.
let holders = 0;
let restore = () => {};
// The page scrollbar's width. BaseUI's Modal and Drawer set overflow: hidden
// on <body> just before our layout effect runs, which already removes the
// scrollbar, so measure without it.
function scrollbarWidth() {
  const body = document.body.style;
  const overflow = body.overflow;
  body.overflow = "";
  const width = window.innerWidth - document.documentElement.clientWidth;
  body.overflow = overflow;
  return width;
}
// Hides the page's scrollbar and pads the page by its width, so the layout
// doesn't shift when the scrollbar disappears (as Headless UI does). The
// page also keeps its scroll position: overflow: hidden doesn't stop focus
// from scrolling it, and BaseUI focuses a menu before positioning it, at the
// top of the page. Only the page's own scroll events reach `window` here, so
// a dialog's content and a panel's items still scroll.
function lock() {
  if (holders++ === 0) {
    const html = document.documentElement;
    const scrollbar = scrollbarWidth();
    const { overflow, paddingRight } = html.style;
    const { scrollX: x, scrollY: y } = window;
    const keep = () => {
      if (window.scrollX !== x || window.scrollY !== y) window.scrollTo(x, y);
    };
    html.style.overflow = "hidden";
    if (scrollbar > 0) html.style.paddingRight = `${scrollbar}px`;
    window.addEventListener("scroll", keep);
    restore = () => {
      window.removeEventListener("scroll", keep);
      html.style.overflow = overflow;
      html.style.paddingRight = paddingRight;
      keep();
    };
  }
  let released = false;
  return () => {
    if (released) return;
    released = true;
    if (--holders === 0) restore();
  };
}
// Layout effect so the scrollbar goes in the same frame the overlay appears
// (BaseUI's own body lock would otherwise shift the page for a frame).
const useBrowserLayoutEffect =
  typeof window === "undefined" ? useEffect : useLayoutEffect;
// Stops the page scrolling while an overlay (Dialog, mobile sidebar) is
// open; popovers use usePopoverScrollLock.
export function useScrollLock(active: boolean) {
  useBrowserLayoutEffect(() => (active ? lock() : undefined), [active]);
}
// For popovers (Dropdown, Listbox) while open: the page stays put, so the
// panel can't drift away from its trigger as the page scrolls. Besides the
// page lock, wheel and touch scrolling are blocked in other scrolling
// containers too, such as a dialog's content, but not inside the panel,
// which scrolls its own items. Mark the panel with `data-popover-panel`.
export function usePopoverScrollLock(open: boolean) {
  useScrollLock(open);
  useEffect(() => {
    if (!open) return;
    const inPanel = (target: EventTarget | null) =>
      target instanceof Element &&
      target.closest("[data-popover-panel]") !== null;
    const block = (event: Event) => {
      if (!inPanel(event.target)) event.preventDefault();
    };
    const options = { capture: true, passive: false };
    document.addEventListener("wheel", block, options);
    document.addEventListener("touchmove", block, options);
    return () => {
      document.removeEventListener("wheel", block, options);
      document.removeEventListener("touchmove", block, options);
    };
  }, [open]);
}
