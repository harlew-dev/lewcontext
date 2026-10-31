import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { SWRConfig } from "swr";
import { describe, expect, it } from "vitest";
import { Toaster } from "@/components/ui/toast";
import WorkspaceMembers from "@/pages/app/[workspace]/settings/members";
import { SessionProvider } from "@/lib/session";
import { WorkspaceProvider } from "@/lib/workspace";
import { gate, stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import { testWorkspace } from "@/test/workspace";
import type { Member } from "@/types/members";
import type { Role } from "@/types/workspaces";
const session = testSession();
const members = "/api/workspaces/wsp_2yEHYLwiwPVhLW/members";
function member(id: number, name: string, role: Role): Member {
  return {
    id: `usr_${String(id).padStart(14, "0")}`,
    name,
    email: `${name.split(" ")[0].toLowerCase()}@example.test`,
    role,
    addedAt: "2026-10-01T12:00:00Z",
  };
}
// The signed-in user is usr_00000000000001 (testSession).
const people = [
  member(1, "Ada Lovelace", "admin"),
  member(2, "Olive Owner", "owner"),
  member(3, "Alan Admin", "admin"),
  member(4, "Mia Member", "member"),
  member(5, "Vic Viewer", "viewer"),
];
const page = {
  data: people,
  pages: { current: 1, total: 1, perPage: 20, items: people.length },
};
function renderAs(role: Role) {
  const workspace = testWorkspace(role);
  return render(
    <SWRConfig value={{ provider: () => new Map() }}>
      <SessionProvider session={session}>
        <WorkspaceProvider
          workspace={workspace}
          first={{ data: [workspace], pages: page.pages }}
        >
          <MemoryRouter
            initialEntries={["/app/wsp_2yEHYLwiwPVhLW/settings/members"]}
          >
            <Routes>
              <Route
                path="/app/:workspace/settings/members"
                element={<WorkspaceMembers />}
              />
            </Routes>
          </MemoryRouter>
          <Toaster />
        </WorkspaceProvider>
      </SessionProvider>
    </SWRConfig>,
  );
}
async function rowOf(name: string) {
  return (await screen.findByText(name)).closest("tr") as HTMLElement;
}
async function choose(name: string, item: string) {
  fireEvent.click(
    within(await rowOf(name)).getByRole("button", {
      name: `Actions for ${name}`,
    }),
  );
  fireEvent.click(await screen.findByRole("menuitem", { name: item }));
}
describe("workspace members", () => {
  it.each(["member", "viewer"] as const)(
    "tells a %s they can't see members, without asking the API",
    (role) => {
      const api = stubApi({});
      renderAs(role);
      expect(
        screen.getByText(
          "Only owners and admins can see this workspace’s members.",
        ),
      ).toBeInTheDocument();
      expect(screen.queryByRole("link", { name: "Members" })).toBeNull();
      expect(api.sent()).toHaveLength(0);
    },
  );
  it("offers actions only on rows an admin may manage, never their own", async () => {
    stubApi({ [`GET ${members}`]: { body: page } });
    renderAs("admin");
    expect(await screen.findByText("5 members")).toBeInTheDocument();
    expect(within(await rowOf("Ada Lovelace")).getByText("You")).toBeVisible();
    for (const [name, actions] of [
      ["Ada Lovelace", false],
      ["Olive Owner", false],
      ["Alan Admin", false],
      ["Mia Member", true],
      ["Vic Viewer", true],
    ] as const)
      expect(
        Boolean(
          within(await rowOf(name)).queryByRole("button", {
            name: `Actions for ${name}`,
          }),
        ),
      ).toBe(actions);
    expect(screen.getByRole("link", { name: "Members" })).toHaveAttribute(
      "aria-current",
      "page",
    );
  });
  it("offers only the roles you may give, and saves the new one", async () => {
    const api = stubApi({
      [`GET ${members}`]: { body: page },
      [`PATCH ${members}/usr_00000000000004`]: {
        body: { ...people[3], role: "viewer" },
      },
    });
    renderAs("admin");
    await choose("Mia Member", "Change role");
    const dialog = await screen.findByRole("dialog");
    const save = within(dialog).getByRole("button", { name: "Save role" });
    expect(save).toBeDisabled();
    fireEvent.click(within(dialog).getByRole("combobox"));
    const options = (await screen.findAllByRole("option")).map(
      (option) => option.textContent,
    );
    expect(options).toEqual(["Member", "Viewer"]);
    fireEvent.click(screen.getByRole("option", { name: "Viewer" }));
    fireEvent.click(save);
    expect(
      await screen.findByText("Mia Member is now a viewer."),
    ).toBeInTheDocument();
    expect(api.sent("PATCH")[0].body).toEqual({ role: "viewer" });
  });
  it("shows removing as pending, then the failure, and keeps the dialog", async () => {
    const held = gate();
    stubApi({
      [`GET ${members}`]: { body: page },
      [`DELETE ${members}/usr_00000000000005`]: {
        status: 409,
        body: { error: "A workspace needs at least one owner." },
        gate: held.promise,
      },
    });
    renderAs("owner");
    await choose("Vic Viewer", "Remove from workspace");
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("heading")).toHaveTextContent(
      "Remove Vic Viewer from Acme?",
    );
    expect(dialog).toHaveTextContent(
      "They lose access at once. Their projects stay.",
    );
    const remove = within(dialog).getByRole("button", { name: "Remove" });
    fireEvent.click(remove);
    await waitFor(() => expect(remove).toHaveAttribute("data-loading"));
    held.release();
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "A workspace needs at least one owner.",
    );
    // Loading shows for at least 400 ms (useDelayedLoading).
    await waitFor(() => expect(remove).not.toHaveAttribute("data-loading"));
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });
});
