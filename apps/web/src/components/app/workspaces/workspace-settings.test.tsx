import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { SWRConfig } from "swr";
import { describe, expect, it, vi } from "vitest";
import { Toaster } from "@/components/ui/toast";
import WorkspaceSettings from "@/pages/app/[workspace]/settings";
import { SessionProvider } from "@/lib/session";
import { WorkspaceProvider } from "@/lib/workspace";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import { testWorkspace } from "@/test/workspace";
import type { Role, Workspace } from "@/types/workspaces";
const session = testSession();
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
                element={<WorkspaceSettings />}
              />
              <Route path="/app" element={<p>Opening your next workspace</p>} />
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
  it("won't let the only owner leave, without loading members", () => {
    const api = stubApi({});
    renderAs("owner");
    expect(
      screen.getByText(
        "You’re its only owner. Make someone else an owner before you leave.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Leave workspace" }),
    ).toBeDisabled();
    expect(api.sent()).toHaveLength(0);
  });
  it("lets an owner leave when there's another owner", () => {
    renderAs("owner", { canLeave: true });
    expect(
      screen.getByRole("button", { name: "Leave workspace" }),
    ).toBeEnabled();
  });
});
