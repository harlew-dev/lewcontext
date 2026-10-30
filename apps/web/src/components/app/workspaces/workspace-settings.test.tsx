import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { SWRConfig } from "swr";
import { describe, expect, it, vi } from "vitest";
import { Toaster } from "@/components/ui/toast";
import WorkspaceSettings from "@/pages/app/[workspace]/settings";
import { SessionProvider } from "@/lib/session";
import { WorkspaceProvider } from "@/lib/workspace";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import type { Role, Workspace } from "@/types/workspaces";
const session = testSession();
function renderAs(role: Role) {
  const workspace: Workspace = {
    id: "wsp_2yEHYLwiwPVhLW",
    name: "Acme",
    role,
    createdAt: "",
    updatedAt: "",
  };
  const first = {
    data: [workspace],
    pages: { current: 1, total: 1, perPage: 20, items: 1 },
  };
  return render(
    <SWRConfig value={{ provider: () => new Map() }}>
      <SessionProvider session={session}>
        <WorkspaceProvider workspace={workspace} first={first}>
          <WorkspaceSettings />
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
    expect(screen.getByText("Admin")).toBeInTheDocument();
  });
  it("shows members the name without a form", () => {
    renderAs("member");
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    expect(screen.getByText("Acme")).toBeInTheDocument();
    expect(
      screen.getByText("Only owners and admins can rename this workspace."),
    ).toBeInTheDocument();
    expect(screen.getByText("Member")).toBeInTheDocument();
  });
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
});
