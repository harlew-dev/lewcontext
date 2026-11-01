import { fireEvent, render, screen, within } from "@testing-library/react";
import { createRoutesStub, Outlet } from "react-router";
import { SWRConfig } from "swr";
import { describe, expect, it } from "vitest";
import type { ComponentType } from "react";
import { adminLoader } from "@/lib/admin";
import { stubApi, type Reply } from "@/test/api";
import { testSession } from "@/test/session";
import type {
  AdminIPBlock,
  AdminRole,
  AdminSession,
  AdminUserDetail,
  AdminWorkspaceDetail,
} from "@/types/admin";
import AdminIPBlockIndex from "./[key]/ip-blocks";
import AdminUserShow from "./[key]/users/[user]";
import AdminWorkspaceShow from "./[key]/workspaces/[workspace]";
import AdminLayoutRoute from "./layout";
// The admin actions (plans/features/admin/actions.md): shown to operators
// only, each through AdminActionDialog (a reason, and a code when the API
// asks), with the action's own fields.
const key = "8a1f0c52-6d3e-4b7a-9c2d-5e8f1a3b7c90";
const admin = testSession({
  user: { id: "usr_00000000000009", name: "Olive Admin" },
});
function me(role: AdminRole): AdminSession {
  return {
    user: admin.user,
    role,
    csrfToken: "admin-csrf",
    expiresAt: "",
    alertEmail: "critical" as const,
    openAlerts: 0,
  };
}
const reason = "Reported for invitation spam, ticket 4521.";
const userId = "usr_00000000000001";
function userDetail(overrides: Partial<AdminUserDetail> = {}): AdminUserDetail {
  return {
    id: userId,
    name: "Ada Lovelace",
    email: "ada@example.com",
    createdAt: "2026-10-30T12:00:00Z",
    emailVerifiedAt: "2026-10-30T12:00:00Z",
    mfaEnabled: true,
    admin: false,
    workspaceCount: 0,
    lastSignedInAt: null,
    suspended: false,
    memberships: [],
    sessionCount: 2,
    suspension: null,
    ...overrides,
  };
}
function renderPage(
  path: string,
  pattern: string,
  Component: ComponentType,
  replies: Record<string, Reply | Reply[]>,
  role: AdminRole = "operator",
) {
  const api = stubApi({ "GET /api/admin/me": { body: me(role) }, ...replies });
  const Stub = createRoutesStub([
    {
      path: "/",
      Component: Outlet,
      children: [
        { path: "admin/:key", Component: () => <p>Admin sign-in page</p> },
        {
          Component: AdminLayoutRoute,
          loader: ({ params }) => adminLoader(params.key!),
          children: [{ path: pattern, Component }],
        },
      ],
    },
  ]);
  render(
    <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>
      <Stub initialEntries={[path]} />
    </SWRConfig>,
  );
  return api;
}
function renderUser(
  detail: AdminUserDetail,
  replies: Record<string, Reply | Reply[]> = {},
  role: AdminRole = "operator",
) {
  return renderPage(
    `/admin/${key}/users/${userId}`,
    "admin/:key/users/:user",
    AdminUserShow,
    { [`GET /api/admin/users/${userId}`]: { body: detail }, ...replies },
    role,
  );
}
function typeReason(dialog: HTMLElement) {
  fireEvent.change(within(dialog).getByLabelText("Reason"), {
    target: { value: reason },
  });
}
describe("a user's actions", () => {
  it("are offered to operators: sign out, reset MFA and suspend", async () => {
    renderUser(userDetail());
    expect(
      await screen.findByRole("button", { name: "Sign out everywhere" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Reset two-factor authentication" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Suspend account" }),
    ).toBeInTheDocument();
  });
  it("are hidden from viewers", async () => {
    renderUser(userDetail(), {}, "viewer");
    expect(await screen.findByText("ada@example.com")).toBeInTheDocument();
    for (const name of [
      "Sign out everywhere",
      "Reset two-factor authentication",
      "Suspend account",
    ])
      expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();
  });
  it("aren't offered on an admin's account: the command line acts on admins", async () => {
    renderUser(userDetail({ admin: true }));
    expect(
      await screen.findByText(/only done from the command line/),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Suspend account" }),
    ).not.toBeInTheDocument();
  });
  it("suspends with the reason, revoking invitations unless unticked", async () => {
    const api = renderUser(userDetail(), {
      [`GET /api/admin/users/${userId}`]: [
        { body: userDetail() },
        {
          body: userDetail({
            suspended: true,
            suspension: {
              reason,
              suspendedBy: {
                id: admin.user.id,
                name: "Olive Admin",
                email: "olive@example.com",
              },
              suspendedAt: "2026-11-01T09:00:00Z",
            },
          }),
        },
      ],
      [`POST /api/admin/users/${userId}/suspension`]: {
        status: 201,
        body: { sessionsRevoked: 2, invitationsRevoked: 0 },
      },
    });
    fireEvent.click(
      await screen.findByRole("button", { name: "Suspend account" }),
    );
    const dialog = await screen.findByRole("dialog");
    const box = within(dialog).getByRole("checkbox", {
      name: /Also revoke invitations/,
    });
    expect(box).toBeChecked();
    fireEvent.click(box);
    expect(box).not.toBeChecked();
    typeReason(dialog);
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Suspend account" }),
    );
    expect(
      await screen.findByText("Ada Lovelace is suspended."),
    ).toBeInTheDocument();
    expect(api.sent("POST")).toEqual([
      expect.objectContaining({
        url: `/api/admin/users/${userId}/suspension`,
        body: { reason, revokeInvitations: false },
        csrf: "admin-csrf",
        adminKey: key,
      }),
    ]);
    // The page reloads: the badge, the notice with the reason, Unsuspend.
    expect(
      await screen.findByRole("button", { name: "Unsuspend account" }),
    ).toBeInTheDocument();
    expect(screen.getByText(/Suspended .* by Olive Admin/)).toHaveTextContent(
      reason,
    );
  });
  it("resets MFA only once who is asking is confirmed outside the app", async () => {
    const api = renderUser(userDetail(), {
      [`DELETE /api/admin/users/${userId}/mfa`]: { status: 204 },
    });
    fireEvent.click(
      await screen.findByRole("button", {
        name: "Reset two-factor authentication",
      }),
    );
    const dialog = await screen.findByRole("dialog");
    typeReason(dialog);
    const confirm = within(dialog).getByRole("button", {
      name: "Reset two-factor authentication",
    });
    expect(confirm).toBeDisabled();
    fireEvent.click(
      within(dialog).getByRole("checkbox", {
        name: /confirmed who is asking/,
      }),
    );
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    expect(
      await screen.findByText(
        "Two-factor authentication is off for Ada Lovelace.",
      ),
    ).toBeInTheDocument();
    expect(api.sent("DELETE")[0]).toEqual(
      expect.objectContaining({ body: { reason, confirmed: true } }),
    );
  });
  it("asks for a code when the API does, then signs out everywhere", async () => {
    const api = renderUser(userDetail(), {
      [`DELETE /api/admin/users/${userId}/sessions`]: [
        {
          status: 403,
          body: { error: "Enter a code to continue.", stepUp: true },
        },
        { status: 204 },
      ],
      "POST /api/admin/step-up": { status: 204 },
    });
    fireEvent.click(
      await screen.findByRole("button", { name: "Sign out everywhere" }),
    );
    const dialog = await screen.findByRole("dialog");
    typeReason(dialog);
    const confirm = within(dialog).getByRole("button", {
      name: "Sign out everywhere",
    });
    fireEvent.click(confirm);
    fireEvent.change(
      await within(dialog).findByLabelText("Authentication code"),
      { target: { value: "123456" } },
    );
    fireEvent.click(confirm);
    expect(
      await screen.findByText("Ada Lovelace is signed out everywhere."),
    ).toBeInTheDocument();
    expect(api.sent().filter((r) => r.method !== "GET")).toHaveLength(3);
  });
});
describe("a workspace's actions", () => {
  const workspace: AdminWorkspaceDetail = {
    id: "wsp_00000000000001",
    name: "Acme",
    createdAt: "2026-10-30T12:00:00Z",
    memberCount: 1,
    projectCount: 0,
    owners: [],
    members: [],
    pendingInvitations: 3,
  };
  function renderWorkspace(
    detail: AdminWorkspaceDetail,
    role: AdminRole = "operator",
  ) {
    return renderPage(
      `/admin/${key}/workspaces/${detail.id}`,
      "admin/:key/workspaces/:workspace",
      AdminWorkspaceShow,
      {
        [`GET /api/admin/workspaces/${detail.id}`]: { body: detail },
        [`GET /api/admin/workspaces/${detail.id}/projects`]: {
          body: {
            data: [],
            pages: { current: 1, total: 1, perPage: 20, items: 0 },
          },
        },
        [`DELETE /api/admin/workspaces/${detail.id}/invitations`]: {
          status: 204,
        },
      },
      role,
    );
  }
  it("revoke pending invitations, with a reason", async () => {
    const api = renderWorkspace(workspace);
    fireEvent.click(
      await screen.findByRole("button", { name: "Revoke pending invitations" }),
    );
    const dialog = await screen.findByRole("dialog");
    typeReason(dialog);
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Revoke invitations" }),
    );
    expect(
      await screen.findByText("Acme’s pending invitations are revoked."),
    ).toBeInTheDocument();
    expect(api.sent("DELETE")[0]).toEqual(
      expect.objectContaining({
        url: `/api/admin/workspaces/${workspace.id}/invitations`,
        body: { reason },
      }),
    );
  });
  it("are disabled with nothing pending", async () => {
    renderWorkspace({ ...workspace, pendingInvitations: 0 });
    expect(
      await screen.findByRole("button", { name: "Revoke pending invitations" }),
    ).toBeDisabled();
  });
  it("are hidden from viewers", async () => {
    renderWorkspace(workspace, "viewer");
    expect(await screen.findByText("Acme")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Revoke pending invitations" }),
    ).not.toBeInTheDocument();
  });
});
describe("the IP blocks page", () => {
  const block: AdminIPBlock = {
    id: "ipb_00000000000001",
    network: "198.51.100.0/24",
    reason,
    createdBy: { id: admin.user.id, name: "Olive Admin", email: "o@x.test" },
    createdAt: "2026-11-01T09:00:00Z",
    expiresAt: "2026-11-02T09:00:00Z",
  };
  function renderBlocks(
    replies: Record<string, Reply | Reply[]>,
    role: AdminRole = "operator",
  ) {
    return renderPage(
      `/admin/${key}/ip-blocks`,
      "admin/:key/ip-blocks",
      AdminIPBlockIndex,
      replies,
      role,
    );
  }
  it("lists the blocks, each with its activity", async () => {
    renderBlocks({ "GET /api/admin/ip-blocks": { body: { data: [block] } } });
    const row = (await screen.findByText("198.51.100.0/24")).closest(
      "tr",
    ) as HTMLElement;
    expect(row).toHaveTextContent("Olive Admin");
    fireEvent.click(
      within(row).getByRole("button", {
        name: "Actions for 198.51.100.0/24",
      }),
    );
    expect(
      await screen.findByRole("menuitem", { name: "Show activity" }),
    ).toHaveAttribute(
      "href",
      `/admin/${key}/events?window=all&ip=198.51.100.0%2F24`,
    );
    expect(
      screen.getByRole("menuitem", { name: "Unblock" }),
    ).toBeInTheDocument();
  });
  it("shows the API's refusal on the address field", async () => {
    const api = renderBlocks({
      "GET /api/admin/ip-blocks": { body: { data: [] } },
      "POST /api/admin/ip-blocks": {
        status: 422,
        body: { error: "That includes your own address.", field: "network" },
      },
    });
    expect(await screen.findByText("No blocked addresses")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Block an address" }));
    const dialog = await screen.findByRole("dialog");
    const confirm = within(dialog).getByRole("button", { name: "Block" });
    typeReason(dialog);
    expect(confirm).toBeDisabled();
    fireEvent.change(within(dialog).getByLabelText("Address or range"), {
      target: { value: "127.0.0.1" },
    });
    fireEvent.click(confirm);
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "That includes your own address.",
    );
    expect(api.sent("POST")[0]).toEqual(
      expect.objectContaining({
        body: { network: "127.0.0.1", expiresIn: "24h", reason },
      }),
    );
  });
  it("warns about an IPv6 range wider than a /48, and suggests a /64", async () => {
    renderBlocks({ "GET /api/admin/ip-blocks": { body: { data: [] } } });
    expect(await screen.findByText("No blocked addresses")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Block an address" }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("usually has a whole /64");
    const field = within(dialog).getByLabelText("Address or range");
    const warning = () => within(dialog).queryByRole("note");
    for (const [value, warned] of [
      ["2001:db8::/32", "A /32 of IPv6"],
      [" 2001:db8:1::/47 ", "A /47 of IPv6"],
      ["2001:db8:1::/48", null],
      ["2001:db8:1:2::/64", null],
      ["2001:db8::1", null],
      ["203.0.0.0/16", null],
    ] as const) {
      fireEvent.change(field, { target: { value } });
      if (warned) {
        expect(warning()).toHaveTextContent(warned);
      } else {
        expect(warning()).toBeNull();
      }
    }
  });
  it("offers a viewer no Block or Unblock", async () => {
    renderBlocks(
      { "GET /api/admin/ip-blocks": { body: { data: [block] } } },
      "viewer",
    );
    const row = (await screen.findByText("198.51.100.0/24")).closest(
      "tr",
    ) as HTMLElement;
    expect(
      screen.queryByRole("button", { name: "Block an address" }),
    ).not.toBeInTheDocument();
    fireEvent.click(
      within(row).getByRole("button", {
        name: "Actions for 198.51.100.0/24",
      }),
    );
    await screen.findByRole("menuitem", { name: "Show activity" });
    expect(
      screen.queryByRole("menuitem", { name: "Unblock" }),
    ).not.toBeInTheDocument();
  });
});
