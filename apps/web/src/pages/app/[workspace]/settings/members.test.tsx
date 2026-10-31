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
import MemberIndex from "@/pages/app/[workspace]/settings/members";
import { SessionProvider } from "@/lib/session";
import { WorkspaceProvider } from "@/lib/workspace";
import { gate, stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import { testWorkspace } from "@/test/workspace";
import type { Invitation } from "@/types/invitations";
import type { Member } from "@/types/members";
import type { Role } from "@/types/workspaces";
const session = testSession();
const members = "/api/workspaces/wsp_2yEHYLwiwPVhLW/members";
const invitations = "/api/workspaces/wsp_2yEHYLwiwPVhLW/invitations";
const future = "2099-01-01T00:00:00Z";
function invitation(
  id: number,
  email: string,
  overrides: Partial<Invitation> = {},
): Invitation {
  return {
    id: `inv_${String(id).padStart(14, "0")}`,
    email,
    role: "member",
    invitedBy: { id: "usr_00000000000002", name: "Olive Owner" },
    sentAt: "2026-10-31T12:00:00Z",
    createdAt: "2026-10-31T12:00:00Z",
    expiresAt: future,
    ...overrides,
  };
}
function invitationPage(items: Invitation[]) {
  return {
    body: {
      data: items,
      pages: { current: 1, total: 1, perPage: 100, items: items.length },
    },
  };
}
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
                element={<MemberIndex />}
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
    expect(await screen.findByText("Showing 1–5 of 5")).toBeInTheDocument();
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
describe("invitations", () => {
  it.each(["member", "viewer"] as const)(
    "offers a %s no Invite",
    async (role) => {
      stubApi({});
      renderAs(role);
      expect(screen.queryByRole("button", { name: "Invite" })).toBeNull();
    },
  );
  it("invites as a role you may give, showing a refused email on its field", async () => {
    const api = stubApi({
      [`GET ${members}`]: { body: page },
      [`GET ${invitations}`]: [
        invitationPage([]),
        invitationPage([invitation(1, "bob@example.com", { sentAt: null })]),
      ],
      [`POST ${invitations}`]: [
        {
          status: 422,
          body: {
            error: "Already invited. Revoke it to send a new one.",
            field: "email",
          },
        },
        { status: 201, body: invitation(1, "bob@example.com") },
      ],
    });
    renderAs("admin");
    fireEvent.click(await screen.findByRole("button", { name: "Invite" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("heading")).toHaveTextContent(
      "Invite someone to Acme",
    );
    fireEvent.click(within(dialog).getByRole("combobox"));
    expect(
      (await screen.findAllByRole("option")).map((o) => o.textContent),
    ).toEqual(["Member", "Viewer"]);
    fireEvent.click(screen.getByRole("option", { name: "Viewer" }));
    fireEvent.change(within(dialog).getByLabelText("Email"), {
      target: { value: " bob@example.com " },
    });
    const send = within(dialog).getByRole("button", {
      name: "Send invitation",
    });
    fireEvent.click(send);
    expect(
      await within(dialog).findByText(
        "Already invited. Revoke it to send a new one.",
      ),
    ).toBeVisible();
    expect(within(dialog).getByLabelText("Email")).toHaveAttribute(
      "aria-invalid",
      "true",
    );
    fireEvent.click(send);
    expect(
      await screen.findByText("Invitation sent to bob@example.com."),
    ).toBeInTheDocument();
    expect(api.sent("POST").map((s) => s.body)).toEqual([
      { email: "bob@example.com", role: "viewer" },
      { email: "bob@example.com", role: "viewer" },
    ]);
    // The pending list refreshes and shows the new invitation.
    expect(await screen.findByText("Sending…")).toBeVisible();
  });
  it("lists pending invitations, revoking only roles you may give", async () => {
    const api = stubApi({
      [`GET ${members}`]: { body: page },
      [`GET ${invitations}`]: invitationPage([
        invitation(1, "sending@example.com", { sentAt: null }),
        invitation(2, "sent@example.com", { role: "viewer" }),
        invitation(3, "lapsed@example.com", {
          expiresAt: "2026-01-01T00:00:00Z",
        }),
        invitation(4, "admin@example.com", { role: "admin", invitedBy: null }),
      ]),
      [`DELETE ${invitations}/inv_00000000000002`]: { status: 204 },
    });
    renderAs("admin");
    const section = await screen.findByRole("region", {
      name: "Pending invitations",
    });
    const row = (email: string) =>
      within(section).getByText(email).closest("tr") as HTMLElement;
    expect(row("sending@example.com")).toHaveTextContent("Sending…");
    expect(row("sent@example.com")).toHaveTextContent("Sent");
    expect(row("lapsed@example.com")).toHaveTextContent("Expired");
    expect(row("admin@example.com")).toHaveTextContent("A former member");
    // An admin can't give admin, so can't revoke an admin invitation.
    expect(
      within(row("admin@example.com")).queryByRole("button", {
        name: /Actions for/,
      }),
    ).toBeNull();
    fireEvent.click(
      within(row("sent@example.com")).getByRole("button", {
        name: "Actions for the invitation to sent@example.com",
      }),
    );
    fireEvent.click(
      await screen.findByRole("menuitem", { name: "Revoke invitation" }),
    );
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("heading")).toHaveTextContent(
      "Revoke the invitation to sent@example.com?",
    );
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Revoke invitation" }),
    );
    expect(
      await screen.findByText(
        "The invitation to sent@example.com was revoked.",
      ),
    ).toBeInTheDocument();
    expect(api.sent("DELETE")).toHaveLength(1);
  });
  it("shows no section without pending invitations", async () => {
    stubApi({
      [`GET ${members}`]: { body: page },
      [`GET ${invitations}`]: invitationPage([]),
    });
    renderAs("owner");
    expect(await screen.findByText("Showing 1–5 of 5")).toBeInTheDocument();
    expect(
      screen.queryByRole("region", { name: "Pending invitations" }),
    ).toBeNull();
  });
});
