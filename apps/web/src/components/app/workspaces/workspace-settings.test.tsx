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
import { onboardingState } from "@/lib/routes";
import { SessionProvider } from "@/lib/session";
import { WorkspaceProvider } from "@/lib/workspace";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import { testWorkspace } from "@/test/workspace";
import type { Role, Workspace } from "@/types/workspaces";
const session = testSession();
// Stands in for onboarding, showing what workspace settings told it.
function OnboardingProbe() {
  const { deletedWorkspace } = onboardingState(useLocation().state);
  return <p>Onboarding after deleting {deletedWorkspace}</p>;
}
function renderAs(role: Role, overrides: Partial<Workspace> = {}) {
  const workspace = testWorkspace(role, overrides);
  const first = {
    data: [workspace],
    pages: { current: 1, total: 1, perPage: 20, items: 1 },
  };
  return render(
    <SWRConfig value={{ provider: () => new Map() }}>
      <SessionProvider session={session}>
        <WorkspaceProvider workspace={workspace} first={first}>
          <MemoryRouter initialEntries={["/app/wsp_2yEHYLwiwPVhLW/settings"]}>
            <Routes>
              <Route
                path="/app/:workspace/settings"
                element={<WorkspaceEdit />}
              />
              <Route path="/app" element={<p>Opening your next workspace</p>} />
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
    const api = stubApi({ "*": { body: {} } });
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
      expect(
        screen.getByText("Only owners and admins can rename this workspace."),
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
  it("lets a viewer leave, then opens their next workspace", async () => {
    const api = stubApi({
      "DELETE /api/workspaces/wsp_2yEHYLwiwPVhLW/membership": { status: 204 },
      "*": {
        body: {
          data: [],
          pages: { current: 1, total: 1, perPage: 20, items: 0 },
        },
      },
    });
    renderAs("viewer");
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
      await screen.findByText("Opening your next workspace"),
    ).toBeInTheDocument();
    expect(api.sent("DELETE")).toHaveLength(1);
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
      "*": {
        body: {
          data: [],
          pages: { current: 1, total: 1, perPage: 20, items: 0 },
        },
      },
    });
    renderAs("owner");
    fireEvent.click(screen.getByRole("button", { name: "Delete workspace" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("heading")).toHaveTextContent(
      "Delete Acme?",
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
