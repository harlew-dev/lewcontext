import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { DangerAction, SettingsSection } from "./settings-section";
function renderZone(title = "Danger zone") {
  render(
    <SettingsSection title={title} variant="danger">
      <DangerAction
        title="Delete thing"
        description="Deletes the thing."
        action={<button type="button">Delete thing</button>}
      />
    </SettingsSection>,
  );
}
describe("a danger zone", () => {
  it("starts closed, with its buttons out of reach", () => {
    renderZone();
    const toggle = screen.getByRole("button", { name: "Show danger zone" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByRole("button", { name: "Delete thing" })).toBeNull();
  });
  it("opens and closes from the chevron button", () => {
    renderZone("Actions");
    fireEvent.click(screen.getByRole("button", { name: "Show actions" }));
    expect(
      screen.getByRole("button", { name: "Hide actions" }),
    ).toHaveAttribute("aria-expanded", "true");
    expect(
      screen.getByRole("button", { name: "Delete thing" }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Hide actions" }));
    expect(screen.getByRole("button", { name: "Show actions" })).toBeVisible();
  });
});
describe("a default section", () => {
  it("has no toggle and shows its content", () => {
    render(
      <SettingsSection title="Profile" description="Who you are">
        <p>Content</p>
      </SettingsSection>,
    );
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByText("Content")).toBeVisible();
    expect(screen.getByText("Who you are")).toBeVisible();
  });
});
