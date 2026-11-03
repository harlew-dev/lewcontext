import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import { RadioGroup } from "./radio-group";

function Harness() {
  const [value, setValue] = useState<"warn" | "critical">("warn");
  return (
    <>
      <RadioGroup
        label="Level"
        value={value}
        onChange={setValue}
        options={[
          { value: "warn", label: "Warnings and critical" },
          {
            value: "critical",
            label: "Critical only",
            description: "No warnings",
          },
        ]}
      />
      <p>chosen: {value}</p>
    </>
  );
}

describe("RadioGroup", () => {
  it("picks one option by its label and names the group", () => {
    render(<Harness />);
    expect(screen.getByRole("radiogroup", { name: "Level" })).toBeTruthy();
    const warn = screen.getByRole("radio", { name: /Warnings and critical/ });
    expect(warn.getAttribute("aria-checked")).toBe("true");
    fireEvent.click(screen.getByText("Critical only"));
    expect(screen.getByText("chosen: critical")).toBeTruthy();
    expect(
      screen
        .getByRole("radio", { name: /Critical only/ })
        .getAttribute("aria-checked"),
    ).toBe("true");
  });
  it("reads a description with its radio", () => {
    render(<Harness />);
    const critical = screen.getByRole("radio", { name: /Critical only/ });
    expect(critical.getAttribute("aria-describedby")).toBeTruthy();
  });
});
