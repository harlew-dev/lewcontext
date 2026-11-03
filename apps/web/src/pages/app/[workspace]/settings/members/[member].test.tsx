import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import MemberShow from "@/pages/app/[workspace]/settings/members/[member]";
import { stubApi } from "@/test/api";
import { pick } from "@/test/pick";
import { renderWorkspacePage } from "@/test/workspace-page";
import type { MemberDetail } from "@/types/members";
import type { Role } from "@/types/workspaces";
const ws = "wsp_2yEHYLwiwPVhLW";
const bob = "usr_00000000000003";
const member: MemberDetail = {
  id: bob,
  name: "Bob Member",
  email: "bob@example.test",
  role: "member",
  addedAt: "2026-10-01T12:00:00Z",
  current: true,
  keys: 2,
  changes: 5,
};
const former: MemberDetail = {
  id: "usr_00000000000009",
  name: "Fran Former",
  role: "",
  addedAt: null,
  current: false,
  keys: 0,
  changes: 3,
};
const empty = {
  data: [],
  pages: { current: 1, total: 1, perPage: 20, items: 0 },
};
function open(role: Role, detail: MemberDetail, query = "") {
  return renderWorkspacePage({
    role,
    path: `/app/${ws}/settings/members/${detail.id}${query}`,
    route: "/app/:workspace/settings/members/:member",
    element: <MemberShow />,
  });
}
const show = (detail: MemberDetail) => ({
  [`GET /api/workspaces/${ws}/members/${detail.id}`]: { body: detail },
});
describe("a member's page", () => {
  it("shows the member and, for an owner, the actions in a ⋯ menu", async () => {
    stubApi(show(member));
    open("owner", member);
    expect(
      await screen.findByRole("heading", { name: "Bob Member" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Members" })).toHaveAttribute(
      "href",
      `/app/${ws}/settings/members`,
    );
    const overview = screen
      .getByText("Changes made")
      .closest("dl") as HTMLElement;
    expect(within(overview).getByText("bob@example.test")).toBeInTheDocument();
    expect(within(overview).getByText("5")).toBeInTheDocument();
    expect(
      within(overview).getByText("Personal API keys").nextSibling,
    ).toHaveTextContent("2");
    fireEvent.click(
      screen.getByRole("button", { name: "Actions for Bob Member" }),
    );
    expect(
      await screen.findByRole("menuitem", { name: "Change role" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("menuitem", { name: "Remove from workspace" }),
    ).toBeInTheDocument();
  });
  it("changes their role from the menu", async () => {
    const api = stubApi({
      ...show(member),
      [`PATCH /api/workspaces/${ws}/members/${bob}`]: {
        body: { ...member, role: "viewer" },
      },
    });
    open("owner", member);
    fireEvent.click(
      await screen.findByRole("button", { name: "Actions for Bob Member" }),
    );
    fireEvent.click(
      await screen.findByRole("menuitem", { name: "Change role" }),
    );
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("combobox"));
    pick(await screen.findByRole("option", { name: "Viewer" }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Save role" }));
    expect(
      await screen.findByText("Bob Member is now a viewer."),
    ).toBeInTheDocument();
    expect(api.sent("PATCH")[0].body).toEqual({ role: "viewer" });
  });
  it("removes them and goes back to the list", async () => {
    stubApi({
      ...show(member),
      [`DELETE /api/workspaces/${ws}/members/${bob}`]: { status: 204 },
    });
    open("owner", member);
    fireEvent.click(
      await screen.findByRole("button", { name: "Actions for Bob Member" }),
    );
    fireEvent.click(
      await screen.findByRole("menuitem", { name: "Remove from workspace" }),
    );
    fireEvent.click(await screen.findByRole("button", { name: "Remove" }));
    await waitFor(() =>
      expect(screen.getByTestId("where").textContent).toBe(
        `/app/${ws}/settings/members`,
      ),
    );
  });
  it("offers an admin no actions on an owner, nor anyone on themselves", async () => {
    stubApi(show({ ...member, role: "owner" }));
    open("admin", { ...member, role: "owner" });
    await screen.findByRole("heading", { name: "Bob Member" });
    expect(screen.queryByRole("button", { name: /Actions for/ })).toBeNull();
  });
  it("loads their keys and logs on their own tabs, each in the URL", async () => {
    const api = stubApi({
      ...show(member),
      [`GET /api/workspaces/${ws}/api-keys`]: { body: empty },
      [`GET /api/workspaces/${ws}/events`]: {
        body: { data: [], next: null, refs: {} },
      },
      [`GET /api/workspaces/${ws}/projects`]: { body: empty },
    });
    open("owner", member);
    await screen.findByRole("heading", { name: "Bob Member" });
    fireEvent.click(screen.getByRole("link", { name: "API Keys" }));
    expect(await screen.findByText("No API keys")).toBeInTheDocument();
    expect(api.sent("GET").map((s) => s.url)).toContain(
      `/api/workspaces/${ws}/api-keys?kind=personal&owner=${bob}`,
    );
    fireEvent.click(screen.getByRole("link", { name: "Logs" }));
    expect(
      await screen.findByText("No changes in this period"),
    ).toBeInTheDocument();
    expect(api.sent("GET").map((s) => s.url)).toContain(
      `/api/workspaces/${ws}/events?account=${bob}`,
    );
    expect(screen.getByTestId("where").textContent).toBe(
      `/app/${ws}/settings/members/${bob}?tab=logs`,
    );
  });
  it("shows someone who left by name, with their history and no actions", async () => {
    stubApi(show(former));
    open("owner", former);
    expect(
      await screen.findByRole("heading", { name: "Fran Former" }),
    ).toBeInTheDocument();
    expect(screen.getAllByText("No longer a member").length).toBeGreaterThan(0);
    expect(screen.queryByRole("button", { name: /Actions for/ })).toBeNull();
    expect(screen.queryByText("Email")).toBeNull();
  });
  it("says a member isn't found", async () => {
    stubApi({
      [`GET /api/workspaces/${ws}/members/${bob}`]: {
        status: 404,
        body: { error: "Member not found." },
      },
    });
    open("owner", member);
    expect(await screen.findByText("Member not found")).toBeInTheDocument();
  });
  it.each(["member", "viewer"] as const)(
    "tells a %s they can't see it, without asking the API",
    (role) => {
      const api = stubApi({});
      open(role, member);
      expect(
        screen.getByText(
          "Only owners and admins can see this workspace’s members.",
        ),
      ).toBeInTheDocument();
      expect(api.sent()).toHaveLength(0);
    },
  );
});
