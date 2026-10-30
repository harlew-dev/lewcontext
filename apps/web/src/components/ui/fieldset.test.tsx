import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Description, ErrorMessage, Field, Label } from "./fieldset";
import { Input } from "./input";
describe("Field", () => {
  it("labels the BaseUI input and links its description and error", () => {
    render(
      <Field>
        <Label>Password</Label>
        <Description>Use at least 12 characters.</Description>
        <Input type="password" value="" onChange={() => {}} />
        <ErrorMessage>Too short.</ErrorMessage>
      </Field>,
    );
    const input = screen.getByLabelText("Password");
    const describedBy = input.getAttribute("aria-describedby")?.split(" ");
    expect(describedBy).toHaveLength(2);
    expect(
      describedBy?.map((id) => document.getElementById(id)?.textContent),
    ).toEqual(["Use at least 12 characters.", "Too short."]);
  });
  it("disables its control and marks the label", () => {
    render(
      <Field disabled>
        <Label>Email</Label>
        <Input value="" onChange={() => {}} />
      </Field>,
    );
    expect(screen.getByLabelText("Email")).toBeDisabled();
    expect(screen.getByText("Email")).toHaveAttribute("data-disabled");
  });
});
