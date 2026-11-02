import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Listbox, ListboxLabel, ListboxOption } from "./listbox";
describe("Listbox", () => {
  it("names the control with the selected option's text", () => {
    render(
      <Listbox
        aria-label="Filter by status"
        value="planned"
        onChange={() => {}}
      >
        <ListboxOption value="all">
          <ListboxLabel>All</ListboxLabel>
        </ListboxOption>
        <ListboxOption value="planned">
          <ListboxLabel>
            Planned <strong>soon</strong>
          </ListboxLabel>
        </ListboxOption>
      </Listbox>,
    );
    expect(
      screen.getByRole("combobox", {
        name: "Selected Planned soon. Filter by status",
      }),
    ).toBeInTheDocument();
  });
});
