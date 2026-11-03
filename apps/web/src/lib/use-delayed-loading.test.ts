import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useDelayedLoading } from "./use-delayed-loading";
function renderBusy(busy: boolean) {
  return renderHook(({ busy }) => useDelayedLoading(busy), {
    initialProps: { busy },
  });
}
function wait(ms: number) {
  act(() => {
    vi.advanceTimersByTime(ms);
  });
}
describe("useDelayedLoading()", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });
  it("shows nothing for work quicker than the delay", () => {
    const { result, rerender } = renderBusy(true);
    wait(180);
    rerender({ busy: false });
    wait(1000);
    expect(result.current).toBe(false);
  });
  it("shows once the work has lasted 200 ms", () => {
    const { result } = renderBusy(true);
    wait(199);
    expect(result.current).toBe(false);
    wait(1);
    expect(result.current).toBe(true);
  });
  it("stays shown for at least 400 ms", () => {
    const { result, rerender } = renderBusy(true);
    wait(220);
    rerender({ busy: false });
    wait(379);
    expect(result.current).toBe(true);
    wait(1);
    expect(result.current).toBe(false);
  });
  it("hides as soon as longer work ends", () => {
    const { result, rerender } = renderBusy(true);
    wait(1000);
    rerender({ busy: false });
    wait(0);
    expect(result.current).toBe(false);
  });
  it("stays shown when new work starts during the minimum", () => {
    const { result, rerender } = renderBusy(true);
    wait(220);
    rerender({ busy: false });
    wait(100);
    rerender({ busy: true });
    wait(1000);
    expect(result.current).toBe(true);
  });
});
