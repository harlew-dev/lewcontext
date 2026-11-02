import { fireEvent } from "@testing-library/react";
// Chooses a listbox or combobox option the way a mouse does: Base UI only
// commits a click that began with a press on the option (it ignores one that
// didn't, e.g. an item that slid under a still pointer), so a bare click is
// not enough.
export function pick(option: Element) {
  fireEvent.pointerDown(option);
  fireEvent.click(option);
}
