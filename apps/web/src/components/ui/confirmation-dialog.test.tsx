import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { ApiError } from "@/lib/api";
import { ConfirmationDialog } from "./confirmation-dialog";
import { Field, Label } from "./fieldset";
import { Input } from "./input";
// A dialog with a type-to-confirm field, open from the start.
function Harness({
  onConfirm,
  onConfirmed = () => {},
}: {
  onConfirm: () => Promise<unknown>;
  onConfirmed?: () => void;
}) {
  const [open, setOpen] = useState(true);
  const [typed, setTyped] = useState("");
  return (
    <>
      <p>{open ? "Open" : "Closed"}</p>
      <ConfirmationDialog
        open={open}
        onClose={() => setOpen(false)}
        title="Delete Acme?"
        description="It can’t be undone."
        confirm="Delete workspace"
        variant="danger"
        disabled={typed !== "Acme"}
        onConfirm={onConfirm}
        onConfirmed={onConfirmed}
      >
        <Field>
          <Label>Name</Label>
          <Input value={typed} onChange={(e) => setTyped(e.target.value)} />
        </Field>
      </ConfirmationDialog>
    </>
  );
}
const confirmButton = () =>
  screen.getByRole("button", { name: "Delete workspace" });
describe("ConfirmationDialog", () => {
  it("keeps confirm off while disabled", () => {
    const onConfirm = vi.fn(() => Promise.resolve());
    render(<Harness onConfirm={onConfirm} />);
    expect(screen.getByRole("heading")).toHaveTextContent("Delete Acme?");
    expect(confirmButton()).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Acm" },
    });
    expect(confirmButton()).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Acme" },
    });
    expect(confirmButton()).toBeEnabled();
    expect(onConfirm).not.toHaveBeenCalled();
  });
  it("can't be cancelled or closed while confirming, and confirms once", async () => {
    let finish = () => {};
    const onConfirm = vi.fn(
      () => new Promise<void>((resolve) => (finish = resolve)),
    );
    const onConfirmed = vi.fn();
    render(<Harness onConfirm={onConfirm} onConfirmed={onConfirmed} />);
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Acme" },
    });
    fireEvent.click(confirmButton());
    fireEvent.click(confirmButton());
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled(),
    );
    expect(screen.getByLabelText("Name")).toBeDisabled();
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    expect(screen.getByText("Open")).toBeInTheDocument();
    expect(onConfirm).toHaveBeenCalledTimes(1);
    finish();
    await waitFor(() => expect(screen.getByText("Closed")).toBeInTheDocument());
    expect(onConfirmed).toHaveBeenCalledTimes(1);
  });
  it("stays open with the error and the typed fields when it fails", async () => {
    const onConfirmed = vi.fn();
    render(
      <Harness
        onConfirm={() => Promise.reject(new ApiError("Try again later.", 503))}
        onConfirmed={onConfirmed}
      />,
    );
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Acme" },
    });
    fireEvent.click(confirmButton());
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Try again later.",
    );
    expect(screen.getByText("Open")).toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toHaveValue("Acme");
    expect(onConfirmed).not.toHaveBeenCalled();
  });
});
