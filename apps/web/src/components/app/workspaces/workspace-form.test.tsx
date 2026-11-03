import type { ReactNode } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { SWRConfig } from "swr";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Button } from "@/components/ui/button";
import { ErrorNotice } from "@/components/ui/feedback";
import { SessionProvider } from "@/lib/session";
import { gate, stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import { WorkspaceNameField, useCreateWorkspace } from "./workspace-form";
const navigate = vi.hoisted(() => vi.fn());
vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  useNavigate: () => navigate,
  useLocation: () => ({ pathname: "/app/onboarding", search: "" }),
}));
afterEach(() => {
  navigate.mockReset();
});
const session = testSession();
function wrapper({ children }: { children: ReactNode }) {
  return (
    <SWRConfig value={{ provider: () => new Map() }}>
      <SessionProvider session={session}>{children}</SessionProvider>
    </SWRConfig>
  );
}
// The onboarding page's form, built from the shared pieces.
function CreateForm() {
  const create = useCreateWorkspace();
  return (
    <form onSubmit={create.submit}>
      <ErrorNotice error={create.error} />
      <WorkspaceNameField
        value={create.name}
        onChange={create.setName}
        disabled={create.loading}
      />
      <Button type="submit" loading={create.loading} disabled={!create.valid}>
        Create workspace
      </Button>
    </form>
  );
}
// Holds the POST until `release`, then answers with `status`.
function stubCreate(status: number) {
  const held = gate();
  const body =
    status === 201
      ? { id: "wsp_2yEHYLwiwPVhLW", name: "Acme", role: "owner" }
      : { error: "Use a workspace name of 1–80 characters." };
  const api = stubApi({
    "POST /api/workspaces": { status, body, gate: held.promise },
  });
  return { api, release: held.release };
}
const submit = () => screen.getByRole("button", { name: "Create workspace" });
const name = () => screen.getByLabelText("Workspace name");
describe("creating a workspace", () => {
  it("needs a name of 1–80 characters", () => {
    render(<CreateForm />, { wrapper });
    expect(submit()).toBeDisabled();
    fireEvent.change(name(), { target: { value: "   " } });
    expect(submit()).toBeDisabled();
    fireEvent.change(name(), { target: { value: "é".repeat(81) } });
    expect(submit()).toBeDisabled();
    fireEvent.change(name(), { target: { value: " Acme " } });
    expect(submit()).toBeEnabled();
  });
  it("sends the trimmed name once, then opens the new workspace", async () => {
    const { api, release } = stubCreate(201);
    render(<CreateForm />, { wrapper });
    fireEvent.change(name(), { target: { value: " Acme " } });
    fireEvent.click(submit());
    await waitFor(() => expect(submit()).toHaveAttribute("aria-busy", "true"));
    expect(submit()).toBeDisabled();
    expect(name()).toBeDisabled();
    fireEvent.submit(submit().closest("form")!);
    release();
    await waitFor(() =>
      // The workspace's layout toasts "Acme created." from this state.
      expect(navigate).toHaveBeenCalledWith(
        "/app/wsp_2yEHYLwiwPVhLW/dashboard",
        { state: { createdWorkspace: "Acme" } },
      ),
    );
    const creates = api.sent("POST");
    expect(creates).toHaveLength(1);
    expect(creates[0].body).toEqual({ name: "Acme" });
  });
  it("shows the API's error and lets the user try again", async () => {
    const { release } = stubCreate(422);
    render(<CreateForm />, { wrapper });
    fireEvent.change(name(), { target: { value: "Acme" } });
    fireEvent.click(submit());
    release();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Use a workspace name of 1–80 characters.",
    );
    expect(submit()).toBeEnabled();
    expect(navigate).not.toHaveBeenCalled();
  });
});
