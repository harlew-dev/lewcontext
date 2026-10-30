import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Toaster } from "@/components/ui/toast";
import AccountSettings from "@/pages/app/settings";
import { SessionProvider } from "@/lib/session";
import type { Session } from "@/lib/types";
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
const session: Session = {
  user: {
    id: "usr_00000000000001",
    name: "Ada",
    email: "ada@forma.test",
    createdAt: "2026-10-30T00:00:00Z",
  },
  csrfToken: "csrf-old",
  expiresAt: "",
  signedInAt: "2026-10-30T00:00:00Z",
};
const rotated: Session = { ...session, csrfToken: "csrf-new" };
type Reply = { status: number; body?: unknown };
// Answers each "METHOD /path" with the next reply queued for it.
function stubApi(replies: Record<string, Reply[]>) {
  const fetch = vi.fn().mockImplementation(async (url: string, init) => {
    const reply = replies[`${init?.method ?? "GET"} ${url}`]?.shift();
    if (!reply) return new Response("{}", { status: 404 });
    return reply.body === undefined
      ? new Response(null, { status: reply.status })
      : new Response(JSON.stringify(reply.body), { status: reply.status });
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
function sent(fetch: ReturnType<typeof stubApi>, method: string) {
  return fetch.mock.calls
    .filter(([, init]) => init?.method === method)
    .map(([url, init]) => ({
      url,
      body: init.body ? JSON.parse(init.body) : undefined,
      csrf: init.headers.get("X-CSRF-Token"),
    }));
}
function renderSettings() {
  render(
    <SessionProvider session={session}>
      <AccountSettings />
      <Toaster />
    </SessionProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Change password" }));
}
function type(label: string, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
}
const wrongCurrent = {
  status: 422,
  body: { error: "Current password is incorrect.", field: "currentPassword" },
};
describe("change password", () => {
  it("checks the current password, then saves the new one", async () => {
    const fetch = stubApi({
      "POST /api/auth/password/verify": [{ status: 204 }],
      "PUT /api/auth/password": [{ status: 200, body: rotated }],
      "DELETE /api/auth/sessions": [{ status: 204 }],
    });
    renderSettings();
    type("Current password", "my old passphrase");
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await screen.findByLabelText("New password");
    expect(sent(fetch, "POST")[0].body).toEqual({
      password: "my old passphrase",
    });
    type("New password", "a brand new passphrase");
    type("Confirm new password", "a brand new passphrase!");
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    expect(
      await screen.findByText("The passwords don’t match."),
    ).toBeInTheDocument();
    expect(sent(fetch, "PUT")).toHaveLength(0);
    type("Confirm new password", "a brand new passphrase");
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    expect(
      await screen.findByText(
        "Password updated. Other sessions were signed out.",
      ),
    ).toBeInTheDocument();
    expect(sent(fetch, "PUT")[0]).toEqual({
      url: "/api/auth/password",
      body: {
        currentPassword: "my old passphrase",
        newPassword: "a brand new passphrase",
      },
      csrf: "csrf-old",
    });
    // Later requests use the rotated session's CSRF token.
    fireEvent.click(
      screen.getByRole("button", { name: "Sign out other sessions" }),
    );
    await waitFor(() => expect(sent(fetch, "DELETE")).toHaveLength(1));
    expect(sent(fetch, "DELETE")[0].csrf).toBe("csrf-new");
  });
  it("stays on the first step when the current password is wrong", async () => {
    stubApi({
      "POST /api/auth/password/verify": [
        { status: 422, body: { ...wrongCurrent.body, field: "password" } },
      ],
    });
    renderSettings();
    type("Current password", "not my password");
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Current password is incorrect.",
    );
    expect(screen.queryByLabelText("New password")).toBeNull();
  });
  it("refuses a short new password without asking the API", async () => {
    const fetch = stubApi({
      "POST /api/auth/password/verify": [{ status: 204 }],
    });
    renderSettings();
    type("Current password", "my old passphrase");
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await screen.findByLabelText("New password");
    type("New password", "too short");
    type("Confirm new password", "too short");
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Use at least 12 characters.",
    );
    expect(sent(fetch, "PUT")).toHaveLength(0);
  });
  it("goes back to the first step when the API rejects the current password on save", async () => {
    stubApi({
      "POST /api/auth/password/verify": [{ status: 204 }],
      "PUT /api/auth/password": [wrongCurrent],
    });
    renderSettings();
    type("Current password", "my old passphrase");
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await screen.findByLabelText("New password");
    type("New password", "a brand new passphrase");
    type("Confirm new password", "a brand new passphrase");
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Current password is incorrect.",
    );
    expect(screen.getByLabelText("Current password")).toBeInTheDocument();
  });
  it("shows other failures above the form and keeps the step", async () => {
    stubApi({
      "POST /api/auth/password/verify": [
        {
          status: 429,
          body: { error: "Too many attempts. Try again in 15 minutes." },
        },
      ],
    });
    renderSettings();
    type("Current password", "my old passphrase");
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(
      await screen.findByText("Too many attempts. Try again in 15 minutes."),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Current password")).toBeInTheDocument();
  });
});
