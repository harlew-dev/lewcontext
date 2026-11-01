import { fireEvent, render, screen, within } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { Combobox, compressPicked, type ComboboxNode } from "./combobox";
// A tree like the Events picker's: a group holding a sub-group and names.
const leaf = (value: string): ComboboxNode => ({
  value,
  text: value,
  label: value,
});
const options: ComboboxNode[] = [
  {
    value: "auth.*",
    text: "auth.* Accounts",
    chip: "auth.*",
    label: "auth.*",
    children: [
      {
        value: "auth.mfa.*",
        text: "auth.mfa.* Two-factor",
        chip: "auth.mfa.*",
        label: "auth.mfa.*",
        children: [
          leaf("auth.mfa.enabled"),
          leaf("auth.mfa.disabled"),
          leaf("auth.mfa.checked"),
        ],
      },
      leaf("auth.logged_in"),
    ],
  },
  {
    value: "project.*",
    text: "project.* Projects",
    chip: "project.*",
    label: "project.*",
    children: [leaf("project.created"), leaf("project.deleted")],
  },
];
function Picker({
  initial = [],
  onChange = () => {},
}: {
  initial?: string[];
  onChange?: (value: string[]) => void;
}) {
  const [value, setValue] = useState(initial);
  return (
    <Combobox
      aria-label="Events"
      value={value}
      onChange={(next) => {
        setValue(next);
        onChange(next);
      }}
      options={options}
      placeholder="All events"
    />
  );
}
function open() {
  fireEvent.click(screen.getByRole("combobox"));
}
// An option by the start of its spoken text.
function option(name: string) {
  return screen.getByRole("option", { name: new RegExp(`^${escape(name)},`) });
}
function escape(text: string) {
  return text.replace(/[.*]/g, (c) => `\\${c}`);
}
describe("Combobox", () => {
  it("picks a group's names, shows a partly picked group mixed, and clears a group picked again", () => {
    const changes = vi.fn();
    render(<Picker onChange={changes} />);
    open();
    fireEvent.click(option("auth.mfa.* Two-factor"));
    expect(changes).toHaveBeenLastCalledWith([
      "auth.mfa.enabled",
      "auth.mfa.disabled",
      "auth.mfa.checked",
    ]);
    expect(
      screen.getByText("auth.mfa.* Two-factor, group, 3 of 3 selected"),
    ).toBeInTheDocument();
    // The parent group is partly picked: 3 of its 4 names.
    expect(
      screen.getByText("auth.* Accounts, group, 3 of 4 selected"),
    ).toBeInTheDocument();
    // The picked state is aria-checked; aria-selected is BaseUI's highlight.
    expect(option("auth.mfa.* Two-factor")).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(option("auth.* Accounts")).toHaveAttribute("aria-checked", "mixed");
    expect(option("auth.logged_in")).toHaveAttribute("aria-checked", "false");
    fireEvent.click(option("auth.mfa.checked"));
    expect(
      screen.getByText("auth.mfa.* Two-factor, group, 2 of 3 selected"),
    ).toBeInTheDocument();
    // A partly picked group, chosen, picks the rest; chosen again, clears.
    fireEvent.click(option("auth.mfa.* Two-factor"));
    expect(changes).toHaveBeenLastCalledWith([
      "auth.mfa.enabled",
      "auth.mfa.disabled",
      "auth.mfa.checked",
    ]);
    fireEvent.click(option("auth.mfa.* Two-factor"));
    expect(changes).toHaveBeenLastCalledWith([]);
    expect(
      screen.getByText("auth.* Accounts, group, 0 of 4 selected"),
    ).toBeInTheDocument();
  });
  it("searches names and groups, keeping a match in its group", () => {
    render(<Picker />);
    open();
    fireEvent.change(screen.getByRole("combobox"), {
      target: { value: "CHECKED" },
    });
    expect(
      screen.getAllByRole("option").map((o) => o.textContent?.split(",")[0]),
    ).toEqual(["auth.* Accounts", "auth.mfa.* Two-factor", "auth.mfa.checked"]);
    // A matching group lists everything in it.
    fireEvent.change(screen.getByRole("combobox"), {
      target: { value: "projects" },
    });
    expect(
      screen.getAllByRole("option").map((o) => o.textContent?.split(",")[0]),
    ).toEqual(["project.* Projects", "project.created", "project.deleted"]);
  });
  it("toggles with Enter, and Backspace removes nothing", () => {
    const changes = vi.fn();
    render(<Picker initial={["project.created"]} onChange={changes} />);
    const input = screen.getByRole("combobox");
    fireEvent.click(input);
    fireEvent.keyDown(input, { key: "Backspace", keyCode: 8 });
    expect(changes).not.toHaveBeenCalled();
    // The picked row is highlighted on opening: Enter toggles it off, and
    // on again.
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 });
    expect(changes).toHaveBeenLastCalledWith([]);
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 });
    expect(changes).toHaveBeenLastCalledWith(["project.created"]);
  });
  it("shows the choice as chips, compressed by group, two and then +N, each removable", () => {
    const changes = vi.fn();
    render(
      <Picker
        initial={[
          "auth.mfa.enabled",
          "auth.mfa.disabled",
          "auth.mfa.checked",
          "auth.logged_in",
          "project.created",
          "project.deleted",
        ]}
        onChange={changes}
      />,
    );
    // Every auth.* name: one chip; the project.* names: another.
    expect(screen.getByText("auth.*")).toBeInTheDocument();
    expect(screen.getByText("project.*")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Remove auth.*" }));
    expect(changes).toHaveBeenLastCalledWith([
      "project.created",
      "project.deleted",
    ]);
    const leaves = render(
      <Picker
        initial={["auth.mfa.enabled", "auth.logged_in", "project.created"]}
      />,
    );
    const chips = within(leaves.container);
    expect(chips.getByText("auth.mfa.enabled")).toBeInTheDocument();
    expect(chips.getByText("auth.logged_in")).toBeInTheDocument();
    expect(chips.getByText("+1")).toBeInTheDocument();
    expect(chips.queryByText("project.created")).toBeNull();
  });
  it("compresses to the highest fully picked group", () => {
    const all = new Set([
      "auth.mfa.enabled",
      "auth.mfa.disabled",
      "auth.mfa.checked",
      "project.created",
    ]);
    expect(compressPicked(options, all)).toEqual([
      "auth.mfa.*",
      "project.created",
    ]);
    all.add("auth.logged_in");
    expect(compressPicked(options, all)).toEqual(["auth.*", "project.created"]);
  });
});
