import { fireEvent } from "@testing-library/react";
// Puts the mouse over an element: Base UI's hover handling (tooltips) reads
// pointer events with a pointer type, which a bare mouseenter doesn't carry.
export function hover(element: Element) {
  fireEvent.pointerEnter(element, { pointerType: "mouse" });
  fireEvent.pointerMove(element, { pointerType: "mouse" });
  fireEvent.mouseEnter(element);
  fireEvent.mouseMove(element);
}
