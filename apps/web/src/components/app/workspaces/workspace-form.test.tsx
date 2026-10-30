import type { ReactNode } from "react";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { SWRConfig } from "swr";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { SessionProvider } from "@/lib/session";
import type { Session } from "@/lib/types";
import { WorkspaceNameField, useCreateWorkspace } from "./workspace-form";
const router = vi.hoisted(() => ({ asPath: "/app/onboarding", push: vi.fn() }));
vi.mock("next/router", () => ({ useRouter: () => router }));
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  router.push.mockReset();
});
const session: Session = {
  user: { id: "usr_00000000000001", name: "Ada", email: "", createdAt: "" },
  csrfToken: "csrf",
  expiresAt: "",
  signedInAt: "",
};
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
      {create.error.message && (
        <Notice key={create.error.key} variant="error">
          {create.error.message}
        </Notice>
      )}
      <WorkspaceNameField
        value={create.name}
        onChange={create.setName}
        disabled={create.saving}
      />
      <Button type="submit" loading={create.saving} disabled={!create.valid}>
        Create workspace
      </Button>
    </form>
  );
}
// Holds the POST until `release`, then answers with `status`.
function stubCreate(status: number) {
  let release = () => {};
  const gate = new Promise<void>((resolve) => (release = resolve));
  const fetch = vi.fn().mockImplementation(async () => {
    await gate;
    const body =
      status === 201
        ? { id: "wsp_2yEHYLwiwPVhLW", name: "Acme", role: "owner" }
        : { error: "Use a workspace name of 1–80 characters." };
    return new Response(JSON.stringify(body), { status });
  });
  vi.stubGlobal("fetch", fetch);
  return { fetch, release: () => release() };
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
    const { fetch, release } = stubCreate(201);
    render(<CreateForm />, { wrapper });
    fireEvent.change(name(), { target: { value: " Acme " } });
    fireEvent.click(submit());
    await waitFor(() => expect(submit()).toHaveAttribute("aria-busy", "true"));
    expect(submit()).toBeDisabled();
    expect(name()).toBeDisabled();
    fireEvent.submit(submit().closest("form")!);
    release();
    await waitFor(() =>
      expect(router.push).toHaveBeenCalledWith(
        "/app/wsp_2yEHYLwiwPVhLW/dashboard",
      ),
    );
    const creates = fetch.mock.calls.filter(
      ([, init]) => init?.method === "POST",
    );
    expect(creates).toHaveLength(1);
    expect(JSON.parse(creates[0][1].body)).toEqual({ name: "Acme" });
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
    expect(router.push).not.toHaveBeenCalled();
  });
});
