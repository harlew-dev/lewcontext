import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useScrollLock } from "./scroll-lock";
const html = document.documentElement;
beforeEach(() => {
  // A 15px scrollbar: the viewport is wider than the page's content box.
  Object.defineProperty(window, "innerWidth", {
    value: 1280,
    configurable: true,
  });
  Object.defineProperty(html, "clientWidth", {
    value: 1265,
    configurable: true,
  });
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
describe("useScrollLock()", () => {
  it("hides the scrollbar and pads the page by its width", () => {
    const lock = renderHook(({ active }) => useScrollLock(active), {
      initialProps: { active: true },
    });
    expect([html.style.overflow, html.style.paddingRight]).toEqual([
      "hidden",
      "15px",
    ]);
    lock.rerender({ active: false });
    expect([html.style.overflow, html.style.paddingRight]).toEqual(["", ""]);
  });
  it("measures past BaseUI's lock on <body>", () => {
    document.body.style.overflow = "hidden";
    renderHook(() => useScrollLock(true));
    expect(html.style.paddingRight).toBe("15px");
    expect(document.body.style.overflow).toBe("hidden");
    document.body.style.overflow = "";
  });
  it("stays locked until the last overlay closes", () => {
    const dialog = renderHook(() => useScrollLock(true));
    const listbox = renderHook(() => useScrollLock(true));
    listbox.unmount();
    expect(html.style.overflow).toBe("hidden");
    dialog.unmount();
    expect(html.style.overflow).toBe("");
  });
  it("keeps the page's scroll position while locked", () => {
    const scrollTo = vi.fn();
    vi.stubGlobal("scrollTo", scrollTo);
    Object.defineProperty(window, "scrollY", {
      value: 600,
      configurable: true,
    });
    renderHook(() => useScrollLock(true));
    // Focus moving into an overlay scrolls the page to the top.
    Object.defineProperty(window, "scrollY", { value: 0, configurable: true });
    window.dispatchEvent(new Event("scroll"));
    expect(scrollTo).toHaveBeenCalledWith(0, 600);
  });
});
