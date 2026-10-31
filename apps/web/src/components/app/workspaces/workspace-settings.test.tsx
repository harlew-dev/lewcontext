import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { SWRConfig } from "swr";
import { describe, expect, it, vi } from "vitest";
import { Toaster } from "@/components/ui/toast";
import WorkspaceEdit from "@/pages/app/[workspace]/settings";
import WorkspaceSettingsLayout from "@/pages/app/[workspace]/settings/layout";
import { onboardingState } from "@/lib/routes";
import { SessionProvider } from "@/lib/session";
import { WorkspaceProvider } from "@/lib/workspace";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import { testWorkspace } from "@/test/workspace";
import type { Role, Workspace } from "@/types/workspaces";
const session = testSession();
const emptyPage = {
  data: [],
  pages: { current: 1, total: 1, perPage: 20, items: 0 },
};
// Stands in for onboarding, showing what workspace settings told it.
function OnboardingProbe() {
  const { deletedWorkspace, leftWorkspace } = onboardingState(
    useLocation().state,
  );
  if (leftWorkspace) return <p>Onboarding after leaving {leftWorkspace}</p>;
  return <p>Onboarding after deleting {deletedWorkspace}</p>;
}
// Another workspace the member is in, opened after leaving or deleting.
const beta = testWorkspace("member", {
  id: "wsp_3zFIZMxjxQWiMX",
  name: "Beta",
});
function renderAs(
  role: Role,
  overrides: Partial<Workspace> = {},
  others: Workspace[] = [],
) {
  const workspace = testWorkspace(role, overrides);
  const data = [workspace, ...others];
  const first = {
    data,
    pages: { current: 1, total: 1, perPage: 20, items: data.length },
  };
  return render(
    <SWRConfig value={{ provider: () => new Map() }}>
      <SessionProvider session={session}>
        <WorkspaceProvider workspace={workspace} first={first}>
          <MemoryRouter initialEntries={["/app/wsp_2yEHYLwiwPVhLW/settings"]}>
            <Routes>
              <Route
                path="/app/:workspace/settings"
                element={<WorkspaceSettingsLayout />}
              >
                <Route index element={<WorkspaceEdit />} />
                <Route path="members" element={<p>Members tab</p>} />
              </Route>
              <Route
                path="/app/:workspace/dashboard"
                element={<p>Opened the next workspace</p>}
              />
              <Route path="/app/onboarding" element={<OnboardingProbe />} />
            </Routes>
          </MemoryRouter>
          <Toaster />
        </WorkspaceProvider>
      </SessionProvider>
    </SWRConfig>,
  );
}
describe("workspace settings", () => {
  it("lets owners and admins rename the workspace", async () => {
    const api = stubApi({
      "PATCH /api/workspaces/wsp_2yEHYLwiwPVhLW": { body: {} },
      "*": { body: emptyPage },
    });
    renderAs("admin");
    const save = screen.getByRole("button", { name: "Save" });
    expect(save).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Acme Inc " },
    });
    fireEvent.click(save);
    await waitFor(() =>
      expect(screen.getByText("Workspace renamed.")).toBeInTheDocument(),
    );
    const [rename] = api.sent("PATCH");
    expect(rename.url).toBe("/api/workspaces/wsp_2yEHYLwiwPVhLW");
    expect(rename.body).toEqual({ name: "Acme Inc" });
  });
  it.each(["member", "viewer"] as const)(
    "shows a %s the name, locked, without a form",
    (role) => {
      renderAs(role);
      expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
      const name = screen.getByLabelText("Name");
      expect(name).toHaveValue("Acme");
      expect(name).toHaveAttribute("readonly");
      expect(name).not.toBeDisabled();
      expect(
        screen.getByRole("img", {
          name: "Only owners and admins can rename this",
        }),
      ).toBeInTheDocument();
      expect(
        screen.getByRole("img", { name: "Workspace IDs can’t be changed" }),
      ).toBeInTheDocument();
    },
  );
  it("copies the workspace ID and announces it", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    renderAs("member");
    expect(screen.getByLabelText("Workspace ID")).toHaveValue(
      "wsp_2yEHYLwiwPVhLW",
    );
    fireEvent.click(screen.getByRole("button", { name: "Copy Workspace ID" }));
    await waitFor(() =>
      expect(writeText).toHaveBeenCalledWith("wsp_2yEHYLwiwPVhLW"),
    );
    expect(await screen.findByText("Copied")).toHaveAttribute("role", "status");
  });
  it("explains when the clipboard isn't available", async () => {
    vi.stubGlobal("navigator", {});
    renderAs("owner");
    fireEvent.click(screen.getByRole("button", { name: "Copy Workspace ID" }));
    expect(
      await screen.findByText(
        "Couldn’t copy. Select the ID and copy it instead.",
      ),
    ).toBeInTheDocument();
  });
  it("shows General and Members tabs only to owners and admins", () => {
    renderAs("admin");
    expect(screen.getByRole("link", { name: "General" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("link", { name: "Members" })).toHaveAttribute(
      "href",
      "/app/wsp_2yEHYLwiwPVhLW/settings/members",
    );
  });
  it("keeps the heading and tabs mounted when switching tabs", async () => {
    renderAs("admin");
    const tabs = screen.getByRole("navigation", { name: "Workspace settings" });
    fireEvent.click(screen.getByRole("link", { name: "Members" }));
    expect(await screen.findByText("Members tab")).toBeInTheDocument();
    // The same element, so the underline slides rather than reappearing.
    expect(screen.getByRole("navigation", { name: "Workspace settings" })).toBe(
      tabs,
    );
    expect(screen.getByRole("link", { name: "Members" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent(
      "Workspace settings",
    );
  });
  it("lets a viewer leave, then opens their next workspace", async () => {
    const api = stubApi({
      "DELETE /api/workspaces/wsp_2yEHYLwiwPVhLW/membership": { status: 204 },
      // The switcher's list, as it refreshes.
      "*": {
        body: {
          data: [testWorkspace("viewer"), beta],
          pages: { current: 1, total: 1, perPage: 20, items: 2 },
        },
      },
    });
    renderAs("viewer", {}, [beta]);
    expect(screen.queryByRole("link", { name: "Members" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Leave workspace" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("heading")).toHaveTextContent(
      "Leave Acme?",
    );
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Leave workspace" }),
    );
    expect(
      await screen.findByText("Opened the next workspace"),
    ).toBeInTheDocument();
    expect(await screen.findByText("You left Acme.")).toBeInTheDocument();
    expect(api.sent("DELETE")).toHaveLength(1);
  });
  it("tells onboarding when you left your last workspace", async () => {
    stubApi({
      "DELETE /api/workspaces/wsp_2yEHYLwiwPVhLW/membership": { status: 204 },
      "*": { body: emptyPage },
    });
    renderAs("member");
    fireEvent.click(screen.getByRole("button", { name: "Leave workspace" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Leave workspace" }),
    );
    expect(
      await screen.findByText("Onboarding after leaving Acme"),
    ).toBeInTheDocument();
  });
  it("doesn't offer the only owner Leave, without loading members", () => {
    const api = stubApi({});
    renderAs("owner");
    expect(
      screen.queryByRole("button", { name: "Leave workspace" }),
    ).toBeNull();
    expect(
      screen.getByRole("button", { name: "Delete workspace" }),
    ).toBeInTheDocument();
    expect(api.sent()).toHaveLength(0);
  });
  it("lets an owner leave when there's another owner", () => {
    renderAs("owner", { canLeave: true });
    expect(
      screen.getByRole("button", { name: "Leave workspace" }),
    ).toBeEnabled();
  });
  it.each(["admin", "member", "viewer"] as const)(
    "offers a %s Leave but not Delete",
    (role) => {
      renderAs(role);
      expect(screen.getByText("Danger zone")).toBeInTheDocument();
      expect(
        screen.getByRole("button", { name: "Leave workspace" }),
      ).toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: "Delete workspace" }),
      ).toBeNull();
    },
  );
  it("deletes the workspace once its name and the password are given", async () => {
    const path = "DELETE /api/workspaces/wsp_2yEHYLwiwPVhLW";
    const api = stubApi({
      [path]: [
        {
          status: 422,
          body: { error: "Current password is incorrect.", field: "password" },
        },
        { status: 204 },
      ],
      "*": { body: emptyPage },
    });
    renderAs("owner");
    fireEvent.click(screen.getByRole("button", { name: "Delete workspace" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("heading")).toHaveTextContent(
      "Delete “Acme”?",
    );
    expect(within(dialog).getByRole("note")).toHaveTextContent(
      "It can’t be undone.",
    );
    const confirm = within(dialog).getByRole("button", {
      name: "Delete workspace",
    });
    const type = (label: RegExp | string, value: string) =>
      fireEvent.change(within(dialog).getByLabelText(label), {
        target: { value },
      });
    type(/to confirm/, "acme");
    type("Your password", "secret");
    expect(confirm).toBeDisabled();
    type(/to confirm/, "Acme ");
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    expect(
      await within(dialog).findByText("Current password is incorrect."),
    ).toBeInTheDocument();
    type("Your password", "correct horse battery staple");
    fireEvent.click(confirm);
    // It was the account's only workspace.
    expect(
      await screen.findByText("Onboarding after deleting Acme"),
    ).toBeInTheDocument();
    expect(api.sent("DELETE").map((sent) => sent.body)).toEqual([
      { name: "Acme ", password: "secret" },
      { name: "Acme ", password: "correct horse battery staple" },
    ]);
  });
});
