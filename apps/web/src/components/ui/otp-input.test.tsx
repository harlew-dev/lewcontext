import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { Field, Label } from "./fieldset";
import { OtpInput } from "./otp-input";
// A form with only the code, as at sign-in, recording what it submits.
function CodeForm({
  initial = "",
  autoSubmit = true,
  onSubmit,
}: {
  initial?: string;
  autoSubmit?: boolean;
  onSubmit: (code: string) => void;
}) {
  const [code, setCode] = useState(initial);
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit(code);
      }}
    >
      <Field>
        <Label>Authentication code</Label>
        <OtpInput value={code} onChange={setCode} autoSubmit={autoSubmit} />
      </Field>
    </form>
  );
}
const field = () => screen.getByLabelText("Authentication code");
const type = (value: string) =>
  fireEvent.change(field(), { target: { value } });
describe("OtpInput", () => {
  it("is one labelled field for autofill and screen readers", () => {
    render(<CodeForm onSubmit={() => {}} />);
    expect(screen.getAllByRole("textbox")).toHaveLength(1);
    expect(field()).toHaveAttribute("autocomplete", "one-time-code");
    expect(field()).toHaveAttribute("inputmode", "numeric");
  });
  it("keeps only digits, at most six, from typing or a pasted '123 456'", () => {
    const submit = vi.fn();
    render(<CodeForm autoSubmit={false} onSubmit={submit} />);
    type("12a");
    expect(field()).toHaveValue("12");
    type("123 456");
    expect(field()).toHaveValue("123456");
    type("1234567");
    expect(field()).toHaveValue("123456");
  });
  it("submits once, with the whole code, when the sixth digit goes in", () => {
    const submit = vi.fn();
    render(<CodeForm onSubmit={submit} />);
    type("12345");
    expect(submit).not.toHaveBeenCalled();
    type("123456");
    expect(submit).toHaveBeenCalledTimes(1);
    expect(submit).toHaveBeenCalledWith("123456");
    // Fixing a digit completes it again, and so does pasting a new code
    // over a full one.
    type("12345");
    type("123457");
    expect(submit).toHaveBeenLastCalledWith("123457");
    type("654321");
    expect(submit).toHaveBeenLastCalledWith("654321");
    expect(submit).toHaveBeenCalledTimes(3);
  });
  it("doesn't submit a code that was already complete when shown", () => {
    const submit = vi.fn();
    render(<CodeForm initial="123456" onSubmit={submit} />);
    expect(submit).not.toHaveBeenCalled();
  });
});
