import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Disclosure } from "./disclosure";

describe("Disclosure", () => {
  it("is collapsed until its header is pressed, and says so", () => {
    render(
      <Disclosure title="Opened" summary="1 field">
        <p>The payload</p>
      </Disclosure>,
    );
    const header = screen.getByRole("button", { name: /Opened/ });
    expect(header).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(header);
    expect(header).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("The payload")).toBeVisible();
    fireEvent.click(header);
    expect(header).toHaveAttribute("aria-expanded", "false");
  });
  it("can start open", () => {
    render(
      <Disclosure title="Open" defaultOpen>
        <p>Inside</p>
      </Disclosure>,
    );
    expect(screen.getByRole("button", { name: /Open/ })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
  });
});
