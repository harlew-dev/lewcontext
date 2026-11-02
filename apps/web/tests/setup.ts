import "@testing-library/jest-dom/vitest";
import { cleanup, configure } from "@testing-library/react";
import { afterEach, vi } from "vitest";
// findBy* and waitFor give up after this long. It is a ceiling, not a delay
// (a passing wait returns at once): the default 1s is too short for a busy
// machine, where a heavy page can take longer than that to render.
configure({ asyncUtilTimeout: 5000 });
// jsdom lacks what Base UI's parts use. PointerEvent: a checkbox forwards its
// click to its hidden input as one. ResizeObserver: popups measure their
// anchors.
class TestPointerEvent extends MouseEvent {
  pointerType: string;
  constructor(type: string, init: MouseEventInit & { pointerType?: string }) {
    super(type, init);
    this.pointerType = init?.pointerType ?? "";
  }
}
if (!("PointerEvent" in window))
  Object.defineProperty(window, "PointerEvent", { value: TestPointerEvent });
if (!("ResizeObserver" in window))
  Object.defineProperty(window, "ResizeObserver", {
    value: class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  });
// Every test starts from an empty page and the real globals (fetch,
// navigator), whatever the last one rendered or stubbed.
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
