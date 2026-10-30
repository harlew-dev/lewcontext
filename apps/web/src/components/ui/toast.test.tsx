import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Toaster, toast } from "./toast";
beforeEach(() => {
  vi.useFakeTimers();
  render(<Toaster />);
});
afterEach(() => {
  vi.useRealTimers();
});
// Long enough for BaseUI's exit animation, after which it removes the toast.
const exit = 600;
function advance(ms: number) {
  act(() => {
    vi.advanceTimersByTime(ms);
  });
}
describe("toast", () => {
  it("announces a success politely and hides it after 5 seconds", () => {
    act(() => toast.success("Workspace renamed."));
    expect(screen.getByRole("status")).toHaveTextContent("Workspace renamed.");
    advance(4999);
    expect(screen.queryByRole("status")).not.toBeNull();
    advance(1 + exit);
    expect(screen.queryByRole("status")).toBeNull();
  });
  it("announces an error as an alert", () => {
    act(() => toast.error("Couldn’t sign out."));
    expect(screen.getByRole("alert")).toHaveTextContent("Couldn’t sign out.");
  });
  it("takes a duration per call, or stays until dismissed", () => {
    act(() => {
      toast.success("Quick", { duration: 1000 });
      toast.success("Sticky", { duration: Infinity });
    });
    advance(1000 + exit);
    expect(screen.queryByText("Quick")).toBeNull();
    advance(60_000);
    expect(screen.getByText("Sticky")).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "Dismiss notification" }),
    );
    advance(exit);
    expect(screen.queryByText("Sticky")).toBeNull();
  });
  it("keeps a toast open while the pointer is over it", () => {
    act(() => toast.success("Project created."));
    advance(0);
    fireEvent.mouseEnter(screen.getByRole("status"));
    advance(10_000);
    expect(screen.getByRole("status")).toBeInTheDocument();
    fireEvent.mouseLeave(screen.getByRole("status"));
    advance(5000 + exit);
    expect(screen.queryByRole("status")).toBeNull();
  });
});
