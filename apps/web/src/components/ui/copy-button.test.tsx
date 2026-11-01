import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CopyButton, CopyFailure, useCopy } from "./copy-button";
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
function Copier({
  value,
  onCopied,
  variant,
}: {
  value: string;
  onCopied?: () => void;
  variant?: "icon" | "button";
}) {
  const copy = useCopy(value, { onCopied });
  return (
    <>
      <CopyButton copy={copy} label="Copy ID" variant={variant} />
      <CopyFailure copy={copy}>Couldn’t copy. Select it instead.</CopyFailure>
    </>
  );
}
describe("CopyButton", () => {
  it("says Copied in a tooltip for 3 seconds, announced once", async () => {
    vi.useFakeTimers();
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    const onCopied = vi.fn();
    render(<Copier value="wsp_123" onCopied={onCopied} />);
    const button = screen.getByRole("button", { name: "Copy ID" });
    fireEvent.click(button);
    await settle();
    expect(writeText).toHaveBeenCalledWith("wsp_123");
    expect(onCopied).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("status")).toHaveTextContent("Copied");
    expect(screen.getByRole("tooltip")).toHaveTextContent("Copied");
    // Not read again as the button's description.
    expect(button).not.toHaveAccessibleDescription("Copied");
    act(() => vi.advanceTimersByTime(2900));
    expect(screen.getByRole("tooltip")).toHaveTextContent("Copied");
    // Closing starts the tooltip's fade-out, which then removes it.
    act(() => vi.advanceTimersByTime(100));
    act(() => vi.advanceTimersByTime(500));
    expect(screen.queryByRole("tooltip")).toBeNull();
  });
  it("keeps what to do instead on the page until the next try", async () => {
    vi.useFakeTimers();
    const writeText = vi
      .fn()
      .mockRejectedValueOnce(new Error("denied"))
      .mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    const onCopied = vi.fn();
    render(<Copier value="codes" variant="button" onCopied={onCopied} />);
    // A labelled button has no "Copy" tooltip of its own.
    fireEvent.mouseEnter(screen.getByRole("button", { name: "Copy ID" }));
    expect(screen.queryByRole("tooltip")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Copy ID" }));
    await settle();
    expect(onCopied).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Couldn’t copy. Select it instead.",
    );
    act(() => vi.advanceTimersByTime(60_000));
    expect(screen.getByRole("alert")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Copy ID" }));
    await settle();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(onCopied).toHaveBeenCalledTimes(1);
  });
  it("does nothing once unmounted, however late the clipboard answers", async () => {
    let answer: () => void = () => {};
    const writeText = vi.fn(
      () => new Promise<void>((resolve) => (answer = resolve)),
    );
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    const onCopied = vi.fn();
    const { unmount } = render(<Copier value="x" onCopied={onCopied} />);
    fireEvent.click(screen.getByRole("button", { name: "Copy ID" }));
    unmount();
    answer();
    await settle();
    expect(onCopied).not.toHaveBeenCalled();
  });
});
