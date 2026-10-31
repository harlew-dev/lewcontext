import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Reveal } from "./reveal";
function renderReveal(onReveal = vi.fn()) {
  render(
    <Reveal
      label="Reveal secret"
      hideLabel="Hide secret"
      announcement="Secret shown."
      placeholder={<p>decoy</p>}
      onReveal={onReveal}
    >
      <p>the real secret</p>
    </Reveal>,
  );
  return onReveal;
}
describe("Reveal", () => {
  it("leaves the secret out of the page until asked for", () => {
    renderReveal();
    expect(document.body).not.toHaveTextContent("the real secret");
    // The blurred stand-in is hidden from screen readers.
    expect(screen.getByText("decoy").closest("[aria-hidden]")).toHaveAttribute(
      "aria-hidden",
      "true",
    );
    expect(screen.getByRole("status")).toHaveTextContent("");
  });
  it("reveals it, moves focus to it and says so, then hides it again", () => {
    const onReveal = renderReveal();
    fireEvent.click(screen.getByRole("button", { name: "Reveal secret" }));
    const secret = screen.getByText("the real secret");
    expect(onReveal).toHaveBeenCalledTimes(1);
    expect(secret.parentElement).toHaveFocus();
    expect(screen.getByRole("status")).toHaveTextContent("Secret shown.");
    expect(screen.queryByText("decoy")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Hide secret" }));
    expect(screen.queryByText("the real secret")).toBeNull();
    expect(screen.getByRole("button", { name: "Reveal secret" })).toHaveFocus();
  });
});
