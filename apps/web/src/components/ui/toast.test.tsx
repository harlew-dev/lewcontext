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
// Long enough for the toast's exit transition, after which it is removed.
const exit = 600;
async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}
describe("toast", () => {
  it("announces a success politely and hides it after 5 seconds", async () => {
    act(() => toast.success("Workspace renamed."));
    expect(screen.getByRole("status")).toHaveTextContent("Workspace renamed.");
    await advance(4999);
    expect(screen.queryByRole("status")).not.toBeNull();
    await advance(1 + exit);
    expect(screen.queryByRole("status")).toBeNull();
  });
  it("announces an error as an alert", () => {
    act(() => toast.error("Couldn’t sign out."));
    expect(screen.getByRole("alert")).toHaveTextContent("Couldn’t sign out.");
  });
  it("takes a duration per call, or stays until dismissed", async () => {
    act(() => {
      toast.success("Quick", { duration: 1000 });
      toast.success("Sticky", { duration: Infinity });
    });
    await advance(1000 + exit);
    expect(screen.queryByText("Quick")).toBeNull();
    await advance(60_000);
    expect(screen.getByText("Sticky")).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "Dismiss notification" }),
    );
    await advance(exit);
    expect(screen.queryByText("Sticky")).toBeNull();
  });
  it("stacks several, and keeps them open while the pointer is over them", async () => {
    act(() => {
      toast.success("Project created.");
      toast.success("Member invited.");
    });
    expect(screen.getAllByRole("status")).toHaveLength(2);
    const stack = screen.getByRole("region");
    fireEvent.mouseEnter(stack);
    await advance(10_000);
    expect(screen.getAllByRole("status")).toHaveLength(2);
    fireEvent.mouseLeave(stack);
    await advance(5000 + exit);
    expect(screen.queryByRole("status")).toBeNull();
  });
});
