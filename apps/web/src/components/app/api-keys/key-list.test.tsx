import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import UserApiKeyIndex from "@/pages/app/settings/api-keys/index";
import WorkspaceApiKeyIndex from "@/pages/app/[workspace]/settings/api-keys/index";
import { stubApi } from "@/test/api";
import { renderWorkspacePage } from "@/test/workspace-page";
import type { ApiKey } from "@/types/api-keys";
import { accessText } from "./key-display";
const ws = "wsp_2yEHYLwiwPVhLW";
const base: ApiKey = {
  id: "apk_00000000000001",
  kind: "workspace",
  name: "Deploy bot",
  workspace: { id: ws, name: "Acme" },
  owner: null,
  createdBy: { id: "usr_00000000000002", name: "Olive Owner" },
  projectAccess: "all",
  projects: [],
  scopes: ["project.destroy", "project.show", "project.index", "project.store"],
  effectiveScopes: [],
  createdAt: "2026-10-01T12:00:00Z",
  expiresAt: null,
  lastUsedAt: null,
  revokedAt: null,
};
const selected: ApiKey = {
  ...base,
  id: "apk_00000000000002",
  kind: "personal",
  name: "Ada script",
  owner: { id: "usr_00000000000003", name: "Ada Lovelace" },
  projectAccess: "selected",
  projects: [{ id: "prj_00000000000001", name: "Website" }],
  scopes: ["project.index", "project.show"],
  lastUsedAt: "2026-10-30T09:30:00Z",
};
const revoked: ApiKey = {
  ...base,
  id: "apk_00000000000003",
  name: "Old key",
  revokedAt: "2026-10-02T00:00:00Z",
};
const expired: ApiKey = {
  ...base,
  id: "apk_00000000000004",
  name: "Stale key",
  expiresAt: "2026-10-05T00:00:00Z",
};
const page = (data: ApiKey[]) => ({
  body: {
    data,
    pages: { current: 1, total: 1, perPage: 20, items: data.length },
  },
});
describe("the API keys table", () => {
  it("reads in words: a name, a green status and one grouped access badge", async () => {
    stubApi({
      [`GET /api/workspaces/${ws}/api-keys?kind=workspace`]: page([
        base,
        revoked,
        expired,
      ]),
      [`GET /api/workspaces/${ws}/api-keys?kind=personal`]: page([selected]),
    });
    renderWorkspacePage({
      path: `/app/${ws}/settings/api-keys`,
      route: "/app/:workspace/settings/api-keys",
      element: <WorkspaceApiKeyIndex />,
    });
    const row = (
      await screen.findByRole("link", { name: "Deploy bot" })
    ).closest("tr") as HTMLElement;
    // No identifier line: the name links to the key's page.
    expect(within(row).queryByText(/apk_/)).toBeNull();
    expect(screen.getByRole("link", { name: "Deploy bot" })).toHaveAttribute(
      "href",
      `/app/${ws}/settings/api-keys/apk_00000000000001`,
    );
    // The API's scopes in words and in order.
    expect(
      within(row).getByText("All projects: List, View, Create, Delete"),
    ).toBeInTheDocument();
    expect(within(row).getByText("Active").className).toMatch(/green/);
    const pick = (name: string) =>
      screen.getByRole("link", { name }).closest("tr") as HTMLElement;
    expect(
      within(pick("Ada script")).getByText("Selected projects: List, View"),
    ).toBeInTheDocument();
    expect(within(pick("Old key")).getByText("Revoked").className).not.toMatch(
      /green/,
    );
    expect(
      within(pick("Stale key")).getByText("Expired").className,
    ).not.toMatch(/green/);
    // A member's name opens their page.
    expect(
      within(pick("Ada script")).getByRole("link", { name: "Ada Lovelace" }),
    ).toHaveAttribute("href", `/app/${ws}/settings/members/usr_00000000000003`);
  });
  it("keeps long text from widening a row, and hides secondary columns on small screens", async () => {
    const long = "a very long key name ".repeat(12);
    stubApi({
      [`GET /api/workspaces/${ws}/api-keys?kind=workspace`]: page([
        { ...base, name: long },
      ]),
      [`GET /api/workspaces/${ws}/api-keys?kind=personal`]: page([]),
    });
    renderWorkspacePage({
      path: `/app/${ws}/settings/api-keys`,
      route: "/app/:workspace/settings/api-keys",
      element: <WorkspaceApiKeyIndex />,
    });
    const name = await screen.findByRole("link", { name: long.trim() });
    expect(name.className).toMatch(/truncate/);
    expect(name.closest("td")?.className).toMatch(/max-w-0/);
    const row = name.closest("tr") as HTMLElement;
    const access = within(row)
      .getByText(/All projects/)
      .closest("td");
    expect(access?.className).toMatch(/max-md:hidden/);
  });
  it("offers View on every row, Edit and Revoke only on a live key", async () => {
    stubApi({
      [`GET /api/workspaces/${ws}/api-keys?kind=workspace`]: page([
        base,
        revoked,
      ]),
      [`GET /api/workspaces/${ws}/api-keys?kind=personal`]: page([]),
    });
    renderWorkspacePage({
      path: `/app/${ws}/settings/api-keys`,
      route: "/app/:workspace/settings/api-keys",
      element: <WorkspaceApiKeyIndex />,
    });
    fireEvent.click(
      await screen.findByRole("button", { name: "Actions for Deploy bot" }),
    );
    expect(
      await screen.findByRole("menuitem", { name: "View" }),
    ).toHaveAttribute(
      "href",
      `/app/${ws}/settings/api-keys/apk_00000000000001`,
    );
    expect(screen.getByRole("menuitem", { name: "Edit" })).toBeInTheDocument();
    expect(
      screen.getByRole("menuitem", { name: "Revoke" }),
    ).toBeInTheDocument();
  });
  it("links personal keys to the account's key page", async () => {
    stubApi({ "GET /api/auth/api-keys": page([selected]) });
    renderWorkspacePage({
      path: "/app/settings/api-keys",
      route: "/app/settings/api-keys",
      element: <UserApiKeyIndex />,
    });
    expect(
      await screen.findByRole("link", { name: "Ada script" }),
    ).toHaveAttribute("href", "/app/settings/api-keys/apk_00000000000002");
  });
});
describe("accessText", () => {
  it("names the reach and the actions", () => {
    expect(accessText(base)).toBe("All projects: List, View, Create, Delete");
    expect(accessText(selected)).toBe("Selected projects: List, View");
    expect(accessText({ ...selected, projects: [] })).toBe(
      "No selected projects remain",
    );
  });
});
