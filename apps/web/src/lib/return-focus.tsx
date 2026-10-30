import { useEffect } from "react";
// Rendered inside an overlay's content (dialog, menu): when BaseUI removes it
// after its exit transition and focus lock, focus goes back to `opener`, the
// element focused when the overlay opened. BaseUI's own returnFocus loses it,
// and focus lands on the page's first link ("Skip to content") instead.
export function ReturnFocus({ opener }: { opener: HTMLElement | null }) {
  useEffect(
    () => () => {
      window.setTimeout(() => {
        if (opener?.isConnected) opener.focus();
      });
    },
    [opener],
  );
  return null;
}
// The element that has focus now, e.g. an overlay's trigger as it opens.
export function focusedElement(): HTMLElement | null {
  return document.activeElement instanceof HTMLElement
    ? document.activeElement
    : null;
}
