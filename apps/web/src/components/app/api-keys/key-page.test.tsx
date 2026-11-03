import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import UserApiKeyShow from "@/pages/app/settings/api-keys/[key]";
import WorkspaceApiKeyShow from "@/pages/app/[workspace]/settings/api-keys/[key]";
import { stubApi } from "@/test/api";
import { renderWorkspacePage } from "@/test/workspace-page";
import type { ApiKey } from "@/types/api-keys";
import type { Role } from "@/types/workspaces";
const ws = "wsp_2yEHYLwiwPVhLW";
const id = "apk_00000000000001";
const key: ApiKey = {
  id,
  kind: "personal",
  name: "Ada script",
  workspace: { id: ws, name: "Acme" },
  owner: { id: "usr_00000000000003", name: "Ada Lovelace" },
  createdBy: { id: "usr_00000000000003", name: "Ada Lovelace" },
  projectAccess: "selected",
  projects: [{ id: "prj_00000000000001", name: "Website" }],
  scopes: ["project.index", "project.show", "project.update"],
  effectiveScopes: ["project.index", "project.show"],
  createdAt: "2026-10-01T12:00:00Z",
  expiresAt: null,
  lastUsedAt: "2026-10-30T09:30:00Z",
  revokedAt: null,
};
const noEvents = { data: [], next: null, refs: {} };
function openWorkspace(role: Role = "owner", query = "") {
  return renderWorkspacePage({
    role,
    path: `/app/${ws}/settings/api-keys/${id}${query}`,
    route: "/app/:workspace/settings/api-keys/:key",
    element: <WorkspaceApiKeyShow />,
  });
}
function openPersonal(query = "") {
  return renderWorkspacePage({
    path: `/app/settings/api-keys/${id}${query}`,
    route: "/app/settings/api-keys/:key",
    element: <UserApiKeyShow />,
  });
}
describe("an API key's page", () => {
  it("shows a key's overview in words, never a secret", async () => {
    stubApi({ [`GET /api/workspaces/${ws}/api-keys/${id}`]: { body: key } });
    openWorkspace();
    expect(
      await screen.findByRole("heading", { name: "Ada script" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Active")).toBeInTheDocument();
    const list = screen
      .getByText("Project access")
      .closest("dl") as HTMLElement;
    expect(within(list).getByText("Website")).toBeInTheDocument();
    // The API's scopes in words, in order; and what the owner's role allows now.
    expect(within(list).getByText("List, View, Update")).toBeInTheDocument();
    expect(within(list).getByText("List, View")).toBeInTheDocument();
    expect(within(list).getByText("Never")).toBeInTheDocument();
    expect(
      screen.getByText(
        "Some actions are unavailable under the owner’s current role.",
      ),
    ).toBeInTheDocument();
    expect(
      within(list).getByRole("link", { name: "Ada Lovelace" }),
    ).toHaveAttribute("href", `/app/${ws}/settings/members/usr_00000000000003`);
    expect(screen.getByRole("link", { name: "API Keys" })).toHaveAttribute(
      "href",
      `/app/${ws}/settings/api-keys`,
    );
  });
  it("loads a workspace key's logs filtered to the key, in its own tab", async () => {
    const api = stubApi({
      [`GET /api/workspaces/${ws}/api-keys/${id}`]: { body: key },
      [`GET /api/workspaces/${ws}/events`]: { body: noEvents },
      [`GET /api/workspaces/${ws}/projects`]: {
        body: {
          data: [],
          pages: { current: 1, total: 1, perPage: 100, items: 0 },
        },
      },
      [`GET /api/workspaces/${ws}/members`]: {
        body: {
          data: [],
          pages: { current: 1, total: 1, perPage: 100, items: 0 },
        },
      },
    });
    openWorkspace();
    await screen.findByRole("heading", { name: "Ada script" });
    fireEvent.click(screen.getByRole("link", { name: "Logs" }));
    expect(
      await screen.findByText("No changes in this period"),
    ).toBeInTheDocument();
    expect(api.sent("GET").map((s) => s.url)).toContain(
      `/api/workspaces/${ws}/events?key=${id}`,
    );
    expect(screen.getByTestId("where").textContent).toBe(
      `/app/${ws}/settings/api-keys/${id}?tab=logs`,
    );
  });
  it("loads a personal key's own log from the account's endpoint", async () => {
    const api = stubApi({
      [`GET /api/auth/api-keys/${id}`]: { body: key },
      [`GET /api/auth/api-keys/${id}/events`]: { body: noEvents },
    });
    openPersonal("?tab=logs");
    expect(
      await screen.findByText("No changes in this period"),
    ).toBeInTheDocument();
    expect(api.sent("GET").map((s) => s.url)).toContain(
      `/api/auth/api-keys/${id}/events`,
    );
    // No people or project filters on a key's own log.
    fireEvent.click(screen.getByRole("button", { name: /Filters/ }));
    const dialog = await screen.findByRole("dialog", {
      name: "Filter changes",
    });
    expect(within(dialog).queryByText("Person")).toBeNull();
    expect(within(dialog).queryByText("Project")).toBeNull();
  });
  it("offers Revoke, and Edit only where the viewer may edit", async () => {
    stubApi({ [`GET /api/workspaces/${ws}/api-keys/${id}`]: { body: key } });
    openWorkspace();
    fireEvent.click(
      await screen.findByRole("button", { name: "Actions for Ada script" }),
    );
    // A member's personal key can be revoked by a workspace admin, not edited.
    expect(
      await screen.findByRole("menuitem", { name: "Revoke" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Edit" })).toBeNull();
  });
  it("offers an owner Edit on their own key, and none on a revoked one", async () => {
    stubApi({ [`GET /api/auth/api-keys/${id}`]: { body: key } });
    openPersonal();
    fireEvent.click(
      await screen.findByRole("button", { name: "Actions for Ada script" }),
    );
    expect(
      await screen.findByRole("menuitem", { name: "Edit" }),
    ).toBeInTheDocument();
  });
  it("shows a revoked key without actions", async () => {
    stubApi({
      [`GET /api/auth/api-keys/${id}`]: {
        body: { ...key, revokedAt: "2026-10-31T00:00:00Z" },
      },
    });
    openPersonal();
    await screen.findByRole("heading", { name: "Ada script" });
    expect(
      screen.getByText("Revoked", { selector: "span" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Actions for/ })).toBeNull();
  });
  it("says a key isn't found", async () => {
    stubApi({
      [`GET /api/auth/api-keys/${id}`]: {
        status: 404,
        body: { error: "Not found." },
      },
    });
    openPersonal();
    expect(await screen.findByText("API key not found")).toBeInTheDocument();
  });
  it.each(["member", "viewer"] as const)(
    "tells a %s they can't see a workspace's keys, without asking the API",
    (role) => {
      const api = stubApi({});
      openWorkspace(role);
      expect(
        screen.getByText(
          "Only owners and admins can see this workspace’s API keys.",
        ),
      ).toBeInTheDocument();
      expect(api.sent()).toHaveLength(0);
    },
  );
});
