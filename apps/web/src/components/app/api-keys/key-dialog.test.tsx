import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { SWRConfig } from "swr";
import { describe, expect, it, vi } from "vitest";
import { SessionProvider } from "@/lib/session";
import { WorkspaceProvider } from "@/lib/workspace";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import { testWorkspace } from "@/test/workspace";
import { pick } from "@/test/pick";
import type { ApiKey } from "@/types/api-keys";
import { KeyDialog } from "./key-dialog";
import WorkspaceApiKeyIndex from "@/pages/app/[workspace]/settings/api-keys";

const workspace = testWorkspace();
function mount(element: React.ReactNode, mfa = false) {
  return render(
    <SWRConfig value={{ provider: () => new Map(), shouldRetryOnError: false }}>
      <SessionProvider
        session={testSession({
          user: { mfaEnabledAt: mfa ? "2026-11-03T10:00:00Z" : null },
        })}
      >
        <WorkspaceProvider
          workspace={workspace}
          first={{
            data: [workspace],
            pages: { current: 1, total: 1, perPage: 20, items: 1 },
          }}
        >
          <MemoryRouter>{element}</MemoryRouter>
        </WorkspaceProvider>
      </SessionProvider>
    </SWRConfig>,
  );
}

describe("API key creation", () => {
  it("searches projects through the combobox, scrolls API pages and preserves selected names", async () => {
    let intersect = () => {};
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        constructor(
          callback: (entries: { isIntersecting: boolean }[]) => void,
        ) {
          intersect = () => callback([{ isIntersecting: true }]);
        }
        observe() {}
        disconnect() {}
      },
    );
    const project = (id: string, name: string) => ({
      id,
      name,
      description: "remote term",
      color: "blue",
      status: "Planned",
      createdAt: "",
      updatedAt: "",
    });
    const first = project("prj_00000000000001", "Original selection");
    const remote = project("prj_00000000000002", "Description match");
    const later = project("prj_00000000000003", "Next result");
    const base = `/api/workspaces/${workspace.id}/projects`;
    const page = (
      data: ReturnType<typeof project>[],
      current = 1,
      total = 1,
    ) => ({
      data,
      pages: {
        current,
        total,
        perPage: 20,
        items: total === 2 ? 21 : data.length,
      },
    });
    const api = stubApi({
      [`GET ${base}`]: { body: page([first]) },
      [`GET ${base}?q=remote+term`]: { body: page([remote], 1, 2) },
      [`GET ${base}?page=2&q=remote+term`]: { body: page([later], 2, 2) },
      [`GET ${base}?q=none`]: { body: page([]) },
      [`POST /api/workspaces/${workspace.id}/api-keys`]: {
        status: 201,
        body: { secret: "fixture-secret" },
      },
    });
    mount(
      <KeyDialog workspace={workspace} onClose={vi.fn()} onSaved={vi.fn()} />,
    );
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Integration" },
    });
    fireEvent.click(screen.getByRole("combobox", { name: /Project access/ }));
    pick(screen.getByRole("option", { name: "Selected projects" }));
    expect(
      screen.queryByRole("searchbox", { name: "Find projects" }),
    ).not.toBeInTheDocument();
    await waitFor(() =>
      expect(
        screen.queryByRole("option", { name: "Selected projects" }),
      ).not.toBeInTheDocument(),
    );
    const picker = screen.getByRole("combobox", { name: "Projects" });
    fireEvent.click(picker);
    pick(await screen.findByRole("option", { name: /^Original selection/ }));
    expect(screen.getByText("1 selected (maximum 100).")).toBeInTheDocument();
    fireEvent.click(picker);
    fireEvent.change(picker, { target: { value: "remote term" } });
    await waitFor(() =>
      expect(
        api
          .sent("GET")
          .some((request) => request.url === `${base}?q=remote+term`),
      ).toBe(true),
    );
    expect(picker).toHaveAttribute("aria-expanded", "true");
    pick(await screen.findByRole("option", { name: /^Description match/ }));
    expect(
      screen.queryByRole("option", { name: /^Original selection/ }),
    ).not.toBeInTheDocument();
    expect(screen.getByText("2 selected (maximum 100).")).toBeInTheDocument();
    act(() => intersect());
    pick(await screen.findByRole("option", { name: /^Next result/ }));
    expect(
      api
        .sent("GET")
        .some((request) => request.url === `${base}?page=2&q=remote+term`),
    ).toBe(true);
    expect(screen.getByText("3 selected (maximum 100).")).toBeInTheDocument();
    fireEvent.click(picker);
    fireEvent.change(picker, { target: { value: "none" } });
    await screen.findByText("No matching options");
    fireEvent.keyDown(picker, { key: "Escape" });
    expect(
      screen.getByRole("button", { name: "Remove Original selection" }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Remove Description match" }),
    ).toBeVisible();
    expect(screen.getByText("+1")).toBeVisible();
    fireEvent.change(screen.getByLabelText("Current password"), {
      target: { value: "example-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add API Key" }));
    await screen.findByRole("button", { name: "Reveal API key" });
    expect(api.sent("POST")[0].body).toMatchObject({
      projects: [first.id, remote.id, later.id],
    });
  });

  it("opens nested two-factor authentication only on submission, retains the form on cancel and accepts recovery proof", async () => {
    const api = stubApi({
      [`POST /api/workspaces/${workspace.id}/api-keys`]: [
        {
          status: 422,
          body: { error: "Incorrect authentication code.", field: "code" },
        },
        { status: 201, body: { secret: "fixture-secret" } },
      ],
    });
    const saved = vi.fn();
    mount(
      <KeyDialog workspace={workspace} onClose={vi.fn()} onSaved={saved} />,
      true,
    );
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Protected integration" },
    });
    expect(
      screen.queryByLabelText("Authentication code"),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Add API Key" }));
    let nested = within(
      await screen.findByRole("dialog", { name: "Confirm your identity" }),
    );
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(2);
    expect(nested.getByLabelText("Authentication code")).toHaveFocus();
    expect(api.sent("POST")).toHaveLength(0);
    fireEvent.keyDown(nested.getByLabelText("Authentication code"), {
      key: "Escape",
      keyCode: 27,
    });
    await waitFor(() =>
      expect(screen.getByLabelText("Name")).toHaveValue(
        "Protected integration",
      ),
    );
    fireEvent.click(screen.getByRole("button", { name: "Add API Key" }));
    nested = within(
      await screen.findByRole("dialog", { name: "Confirm your identity" }),
    );
    fireEvent.change(nested.getByLabelText("Authentication code"), {
      target: { value: "123456" },
    });
    fireEvent.click(nested.getByRole("button", { name: "Add API Key" }));
    await nested.findAllByText("Incorrect authentication code.");
    expect(nested.getByLabelText("Authentication code")).toHaveValue("");
    fireEvent.click(
      nested.getByRole("button", { name: /Use a recovery code/ }),
    );
    fireEvent.change(nested.getByLabelText("Recovery code"), {
      target: { value: "abcde-fghjk" },
    });
    fireEvent.click(nested.getByRole("button", { name: "Add API Key" }));
    await screen.findByRole("button", { name: "Reveal API key" });
    expect(api.sent("POST")).toHaveLength(2);
    expect(api.sent("POST")[1].body).toMatchObject({
      code: "abcde-fghjk",
      name: "Protected integration",
    });
    expect(api.sent("POST")[1].body).not.toHaveProperty("password");
    expect(saved).toHaveBeenCalledOnce();
  });

  it("renames without proof, but requires proof when adding a permission", async () => {
    const editing: ApiKey = {
      id: "apk_00000000000000",
      kind: "workspace",
      name: "Deployment",
      workspace: { id: workspace.id, name: workspace.name },
      owner: null,
      createdBy: null,
      projectAccess: "all",
      projects: [],
      scopes: ["project.index", "project.show"],
      effectiveScopes: ["project.index", "project.show"],
      createdAt: "2026-11-03T10:00:00Z",
      expiresAt: null,
      lastUsedAt: null,
      revokedAt: null,
    };
    const api = stubApi({
      [`PATCH /api/workspaces/${workspace.id}/api-keys/${editing.id}`]: {
        body: editing,
      },
    });
    mount(
      <KeyDialog
        workspace={workspace}
        editing={editing}
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    );
    expect(screen.queryByLabelText("Current password")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Renamed deployment" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(api.sent("PATCH")).toHaveLength(1));
    expect(api.sent("PATCH")[0].body).toMatchObject({
      name: "Renamed deployment",
      expiresAt: null,
    });
    expect(api.sent("PATCH")[0].body).not.toHaveProperty("password");
    fireEvent.click(screen.getByRole("combobox", { name: "Permissions" }));
    fireEvent.change(screen.getByRole("combobox", { name: "Permissions" }), {
      target: { value: "Update" },
    });
    pick(await screen.findByRole("option", { name: /^Update/ }));
    fireEvent.keyDown(screen.getByRole("combobox", { name: "Permissions" }), {
      key: "Escape",
    });
    expect(await screen.findByLabelText("Current password")).toBeVisible();
  });

  it("shows the creation-only secret behind Reveal, copies while masked, and discards on Done", async () => {
    const secret = "lc_wk_apk_00000000000000_" + "0".repeat(43);
    const clipboard = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: clipboard },
    });
    const api = stubApi({
      [`POST /api/workspaces/${workspace.id}/api-keys`]: {
        status: 201,
        body: { key: { id: "apk_00000000000000" }, secret },
      },
    });
    const close = vi.fn();
    const saved = vi.fn();
    mount(<KeyDialog workspace={workspace} onClose={close} onSaved={saved} />);
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Deployment" },
    });
    fireEvent.change(screen.getByLabelText("Current password"), {
      target: { value: "example-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add API Key" }));
    await screen.findByRole("button", { name: "Reveal API key" });
    expect(document.body.textContent).not.toContain(secret);
    expect(api.sent("POST")).toHaveLength(1);
    expect(api.sent("POST")[0].body).toMatchObject({
      name: "Deployment",
      projectAccess: "all",
      scopes: ["project.index", "project.show"],
    });
    const request = screen.getByRole("button", { name: "Example request" });
    expect(request).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(request);
    expect(request).toHaveAttribute("aria-expanded", "true");
    expect(
      screen.getByRole("link", { name: "API Documentation" }),
    ).toHaveAttribute("href", "/docs");
    expect(
      screen.getByRole("link", { name: "API Documentation" }),
    ).toHaveAttribute("target", "_blank");
    expect(
      screen.getByRole("link", { name: "API Documentation" }),
    ).toHaveAttribute("rel", "noopener noreferrer");
    fireEvent.click(screen.getByRole("button", { name: "Copy API key" }));
    await waitFor(() => expect(clipboard).toHaveBeenCalledWith(secret));
    expect(document.body.textContent).not.toContain(secret);
    fireEvent.click(screen.getByRole("button", { name: "Reveal API key" }));
    expect(document.body.textContent).toContain(secret);
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(close).toHaveBeenCalledOnce();
    expect(saved).toHaveBeenCalledOnce();
    expect(document.body.textContent).not.toContain(secret);
  });

  it("keeps the form and explains uncertain creation after a lost response", async () => {
    stubApi({
      [`POST /api/workspaces/${workspace.id}/api-keys`]: { network: true },
    });
    mount(
      <KeyDialog workspace={workspace} onClose={vi.fn()} onSaved={vi.fn()} />,
    );
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Deployment" },
    });
    fireEvent.change(screen.getByLabelText("Current password"), {
      target: { value: "example-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add API Key" }));
    await screen.findByText(/the key may have been created/);
    expect(screen.getByLabelText("Name")).toHaveValue("Deployment");
    expect(screen.getByLabelText("Current password")).toHaveValue("");
  });

  it("does not fetch workspace keys for a viewer", () => {
    const api = stubApi({});
    render(
      <MemoryRouter>
        <WorkspaceProvider
          workspace={testWorkspace("viewer")}
          first={{
            data: [],
            pages: { current: 1, total: 1, perPage: 20, items: 0 },
          }}
        >
          <WorkspaceApiKeyIndex />
        </WorkspaceProvider>
      </MemoryRouter>,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Only owners and admins",
    );
    expect(api.sent("GET")).toHaveLength(0);
  });
});
