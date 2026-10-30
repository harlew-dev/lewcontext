import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { SWRConfig } from "swr";
import { afterEach, describe, expect, it, vi } from "vitest";
import WorkspaceSettings from "@/pages/app/[workspace]/settings";
import { SessionProvider } from "@/lib/session";
import type { Session } from "@/lib/types";
import { WorkspaceProvider } from "@/lib/workspace";
import type { Role, Workspace } from "@/types/workspaces";
const router = vi.hoisted(() => ({
  asPath: "/app/wsp_2yEHYLwiwPVhLW/settings",
  replace: vi.fn(),
}));
vi.mock("next/router", () => ({ useRouter: () => router }));
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  router.replace.mockReset();
});
const session: Session = {
  user: { id: "usr_00000000000001", name: "Ada", email: "", createdAt: "" },
  csrfToken: "csrf",
  expiresAt: "",
  signedInAt: "",
};
function renderAs(role: Role) {
  const workspace: Workspace = {
    id: "wsp_2yEHYLwiwPVhLW",
    name: "Acme",
    role,
    createdAt: "",
    updatedAt: "",
  };
  const workspaces = { path: "/api/workspaces", data: null, error: null };
  return render(
    <SWRConfig value={{ provider: () => new Map() }}>
      <SessionProvider session={session}>
        <WorkspaceProvider workspace={workspace} workspaces={workspaces}>
          <WorkspaceSettings
            session={session}
            workspace={workspace}
            workspaces={workspaces}
          />
        </WorkspaceProvider>
      </SessionProvider>
    </SWRConfig>,
  );
}
describe("workspace settings", () => {
  it("lets owners and admins rename the workspace", async () => {
    const fetch = vi
      .fn()
      .mockImplementation(
        async () => new Response(JSON.stringify({}), { status: 200 }),
      );
    vi.stubGlobal("fetch", fetch);
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
    const [url, init] = fetch.mock.calls.find(
      ([, init]) => init?.method === "PATCH",
    )!;
    expect(url).toBe("/api/workspaces/wsp_2yEHYLwiwPVhLW");
    expect(JSON.parse(init.body)).toEqual({ name: "Acme Inc" });
    expect(router.replace).toHaveBeenCalled();
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
