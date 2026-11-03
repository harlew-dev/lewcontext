import { fireEvent, screen } from "@testing-library/react";
// Opens the danger zone (a collapsed SettingsSection). A closed zone hides
// its buttons from queries by role, so open it before looking for one, and
// before asserting that one is absent.
export async function openDangerZone(
  name: "Danger zone" | "Actions" = "Danger zone",
) {
  const label = name.toLowerCase();
  fireEvent.click(
    await screen.findByRole("button", {
      name: `Show ${label}`,
      expanded: false,
    }),
  );
  await screen.findByRole("button", { name: `Hide ${label}`, expanded: true });
}
