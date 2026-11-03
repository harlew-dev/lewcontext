import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import EventShow from "@/pages/app/[workspace]/settings/events/[event]";
import { stubApi } from "@/test/api";
import { renderWorkspacePage } from "@/test/workspace-page";
import type { EventDetail, WorkspaceEvent } from "@/types/events";
import type { Role } from "@/types/workspaces";
const ws = "wsp_2yEHYLwiwPVhLW";
const id = "evt_00000000000001";
const base = {
  actor: { id: "usr_00000000000002", name: "Olive Owner" },
  subject: null,
  requestId: "req_00000000000001",
  createdAt: "2026-10-31T14:05:09Z",
};
const removed: EventDetail = {
  ...base,
  id,
  name: "workspace.member.removed",
  subject: { id: "usr_00000000000003", name: "Bob Member" },
  details: { user: "usr_00000000000003", role: "member" },
  related: [
    {
      ...base,
      id: "evt_00000000000002",
      name: "auth.api_key.revoked",
      details: { key: "apk_00000000000001", reason: "member_removed" },
    } as WorkspaceEvent,
  ],
};
function open(role: Role = "owner", event = id) {
  return renderWorkspacePage({
    role,
    path: `/app/${ws}/settings/events/${event}`,
    route: "/app/:workspace/settings/events/:event",
    element: <EventShow />,
  });
}
describe("a change's page", () => {
  it("shows what happened, the request ID and the rest of the request", async () => {
    stubApi({ [`GET /api/workspaces/${ws}/events/${id}`]: { body: removed } });
    open();
    expect(
      await screen.findByRole("heading", {
        name: "Olive Owner removed Bob Member (member) from the workspace",
      }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Logs" })).toHaveAttribute(
      "href",
      `/app/${ws}/settings/events`,
    );
    // Who did it and whom it was about open their pages.
    const details = screen.getByText("Done by").closest("dl") as HTMLElement;
    expect(
      within(details).getByRole("link", { name: "Olive Owner" }),
    ).toHaveAttribute("href", `/app/${ws}/settings/members/usr_00000000000002`);
    expect(
      within(details).getByRole("link", { name: "Bob Member" }),
    ).toHaveAttribute("href", `/app/${ws}/settings/members/usr_00000000000003`);
    expect(within(details).getByText("req_00000000000001")).toBeInTheDocument();
    const related = screen.getByText("Other changes in this request")
      .parentElement as HTMLElement;
    expect(
      within(related).getByRole("link", {
        name: /revoked a personal API key apk_00000000000001/,
      }),
    ).toHaveAttribute("href", `/app/${ws}/settings/events/evt_00000000000002`);
  });
  it("says so when nothing else was recorded with it", async () => {
    stubApi({
      [`GET /api/workspaces/${ws}/events/${id}`]: {
        body: { ...removed, related: [] },
      },
    });
    open();
    expect(await screen.findByText("Nothing else.")).toBeInTheDocument();
  });
  it("says a change isn't found, for an unknown, hidden or foreign ID", async () => {
    stubApi({
      [`GET /api/workspaces/${ws}/events/${id}`]: {
        status: 404,
        body: { error: "Change not found." },
      },
    });
    open();
    expect(await screen.findByText("Change not found")).toBeInTheDocument();
    open("owner", "nope");
    expect(await screen.findAllByText("Change not found")).not.toHaveLength(0);
  });
  it.each(["member", "viewer"] as const)(
    "tells a %s they can't see it, without asking the API",
    (role) => {
      const api = stubApi({});
      open(role);
      expect(
        screen.getByText(
          "Only owners and admins can see this workspace’s logs.",
        ),
      ).toBeInTheDocument();
      expect(api.sent()).toHaveLength(0);
    },
  );
});
