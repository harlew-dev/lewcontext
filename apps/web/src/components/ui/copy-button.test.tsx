import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CopyButton } from "./copy-button";
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});
// Lets the clipboard's promise settle and React render the answer.
async function settle() {
  await act(async () => {
    await Promise.resolve();
  });
}
describe("CopyButton", () => {
  it("says Copied in a tooltip for 3 seconds, and to screen readers", async () => {
    vi.useFakeTimers();
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    const onCopied = vi.fn();
    render(<CopyButton value="wsp_123" label="Copy ID" onCopied={onCopied} />);
    fireEvent.click(screen.getByRole("button", { name: "Copy ID" }));
    await settle();
    expect(writeText).toHaveBeenCalledWith("wsp_123");
    expect(onCopied).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("status")).toHaveTextContent("Copied");
    expect(screen.getByRole("tooltip")).toHaveTextContent("Copied");
    act(() => vi.advanceTimersByTime(2900));
    expect(screen.getByRole("tooltip")).toHaveTextContent("Copied");
    // Closing starts the tooltip's fade-out, which then removes it.
    act(() => vi.advanceTimersByTime(100));
    act(() => vi.advanceTimersByTime(500));
    expect(screen.queryByRole("tooltip")).toBeNull();
    expect(screen.getByRole("status")).toBeEmptyDOMElement();
  });
  it("says what to do instead when the clipboard isn't available", async () => {
    vi.stubGlobal("navigator", {});
    const onCopied = vi.fn();
    render(
      <CopyButton
        variant="button"
        value="codes"
        label="Copy codes"
        failed="Couldn’t copy. Reveal the codes and copy them by hand instead."
        onCopied={onCopied}
      />,
    );
    // A labelled button has no "Copy" tooltip of its own.
    fireEvent.mouseEnter(screen.getByRole("button", { name: "Copy codes" }));
    expect(screen.queryByRole("tooltip")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Copy codes" }));
    await settle();
    expect(onCopied).not.toHaveBeenCalled();
    expect(screen.getByRole("status")).toHaveTextContent(
      "Couldn’t copy. Reveal the codes and copy them by hand instead.",
    );
    expect(screen.getByRole("tooltip")).toHaveTextContent("Couldn’t copy.");
  });
});
