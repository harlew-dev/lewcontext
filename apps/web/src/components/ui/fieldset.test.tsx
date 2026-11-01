import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Description, ErrorMessage, Field, Label } from "./fieldset";
import { Input, InputActions, InputGroup } from "./input";
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
  it("locks its input, ends it with the lock and says why", () => {
    const reason = "Your email can’t be changed";
    render(
      <Field locked={reason}>
        <Label>Email</Label>
        <Input value="ada@example.test" />
      </Field>,
    );
    const input = screen.getByLabelText("Email");
    expect(input).toHaveAttribute("readonly");
    expect(input).toHaveAccessibleDescription(reason);
    expect(screen.getByRole("img", { name: reason })).toBeInTheDocument();
  });
  it("puts the lock after the group's own actions", () => {
    render(
      <Field locked>
        <Label>ID</Label>
        <InputGroup>
          <Input value="wsp_1" />
          <InputActions>
            <button type="button">Copy</button>
          </InputActions>
        </InputGroup>
      </Field>,
    );
    const actions = screen.getByRole("button", { name: "Copy" }).parentElement!;
    expect(actions.lastElementChild).toBe(
      within(actions).getByRole("img", { name: "Can’t be changed" }),
    );
  });
  it("shows the lock's tooltip on a try at typing, and Escape hides it", async () => {
    render(
      <Field locked="Only owners can rename this">
        <Label>Name</Label>
        <Input value="Acme" />
      </Field>,
    );
    const input = screen.getByLabelText("Name");
    fireEvent.keyDown(input, { key: "Tab" });
    fireEvent.keyDown(input, { key: "c", ctrlKey: true });
    expect(screen.queryByRole("tooltip")).toBeNull();
    fireEvent.keyDown(input, { key: "a" });
    expect(await screen.findByRole("tooltip")).toHaveTextContent(
      "Only owners can rename this",
    );
    fireEvent.keyDown(input, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("tooltip")).toBeNull());
    fireEvent.paste(input);
    expect(await screen.findByRole("tooltip")).toBeInTheDocument();
  });
});
