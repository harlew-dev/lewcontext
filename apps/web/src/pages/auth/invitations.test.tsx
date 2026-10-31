import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createRoutesStub, useLocation } from "react-router";
import { SWRConfig } from "swr";
import { describe, expect, it } from "vitest";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import type { Session } from "@/lib/types";
import type { InvitationPreview } from "@/types/invitations";
import InvitationShow from "./invitations/[token]";
import Login from "./login";
import Register from "./register";
const token = "aB3_-".repeat(8) + "xyz"; // 43 base64url characters
const link = `/auth/invitations/${token}`;
const next = `?next=${encodeURIComponent(link)}`;
const preview: InvitationPreview = {
  workspace: { name: "Acme" },
  invitedBy: { name: "Olive Owner", email: "olive@example.com" },
  email: "bob@example.com",
  role: "member",
  expiresAt: "2026-11-07T12:00:00Z",
};
const deadLink = {
  status: 422,
  body: {
    error: "This invitation has expired or was revoked.",
    field: "token",
  },
};
const bob = testSession({ user: { email: "bob@example.com" } });
// Where a page went, with the navigation state it was given.
function Landed({ name }: { name: string }) {
  const state: unknown = useLocation().state;
  return (
    <p>
      {name} {JSON.stringify(state ?? null)}
    </p>
  );
}
// The invitation page (its loader provides `session`), the sign-in pages
// without their guest loaders, and stand-ins for where they lead.
function renderAt(pathname: string, session: Session | null, state?: unknown) {
  const Stub = createRoutesStub([
    {
      path: "/auth/invitations/:token",
      Component: InvitationShow,
      loader: () => ({ session }),
    },
    { path: "/auth/login", Component: Login },
    { path: "/auth/register", Component: Register },
    {
      path: "/app/:workspace/dashboard",
      Component: () => <Landed name="dashboard" />,
    },
    { path: "/app/onboarding", Component: () => <Landed name="onboarding" /> },
  ]);
  const [path, search = ""] = pathname.split("?");
  render(
    <SWRConfig value={{ provider: () => new Map() }}>
      <Stub
        initialEntries={[
          { pathname: path, search: search && `?${search}`, state },
        ]}
      />
    </SWRConfig>,
  );
}
describe("the invitation page", () => {
  it("signed out, shows the invitation and offers signing in or up", async () => {
    const api = stubApi({ "POST /api/invitations/check": { body: preview } });
    renderAt(link, null);
    expect(
      await screen.findByRole("heading", { name: "Join Acme" }),
    ).toBeVisible();
    const callout = screen.getByRole("status");
    expect(callout).toHaveTextContent("Acme has invited you to join.");
    expect(callout).toHaveTextContent(
      "Olive Owner (olive@example.com) invited you as a member. Sign in or create an account to accept.",
    );
    expect(
      screen.getByRole("link", { name: "Sign in to accept" }),
    ).toHaveAttribute("href", `/auth/login${next}`);
    expect(
      screen.getByRole("link", { name: "Create an account" }),
    ).toHaveAttribute("href", `/auth/register${next}`);
    // The token goes only in the check's body.
    expect(api.sent()).toEqual([
      expect.objectContaining({
        url: "/api/invitations/check",
        body: { token },
      }),
    ]);
  });
  it("renders names as text", async () => {
    stubApi({
      "POST /api/invitations/check": {
        body: { ...preview, workspace: { name: "<script>alert(1)</script>" } },
      },
    });
    renderAt(link, null);
    expect(
      await screen.findByRole("heading", {
        name: "Join <script>alert(1)</script>",
      }),
    ).toBeVisible();
    expect(document.querySelector("script")).toBeNull();
  });
  it("says when a link is dead", async () => {
    stubApi({ "POST /api/invitations/check": deadLink });
    renderAt(link, null);
    expect(
      await screen.findByRole("heading", {
        name: "This invitation has expired",
      }),
    ).toBeVisible();
  });
  it("asks nothing for a malformed link", async () => {
    const api = stubApi({});
    renderAt("/auth/invitations/not-a-token", null);
    expect(
      await screen.findByRole("heading", {
        name: "This invitation has expired",
      }),
    ).toBeVisible();
    expect(api.sent()).toHaveLength(0);
  });
  it("signed in as the invitee, joins only when asked", async () => {
    const api = stubApi({
      "POST /api/invitations/check": { body: preview },
      "POST /api/invitations/accept": {
        body: { id: "wsp_2yEHYLwiwPVhLW", name: "Acme" },
      },
    });
    renderAt(link, bob);
    const join = await screen.findByRole("button", { name: "Join Acme" });
    expect(api.sent("POST").map((s) => s.url)).toEqual([
      "/api/invitations/check",
    ]);
    fireEvent.click(join);
    expect(
      await screen.findByText(`dashboard {"joinedWorkspace":"Acme"}`),
    ).toBeVisible();
    expect(api.sent("POST")[1]).toEqual({
      method: "POST",
      url: "/api/invitations/accept",
      body: { token },
      csrf: "csrf",
    });
  });
  it("joins at once when arriving from its own buttons", async () => {
    const api = stubApi({
      "POST /api/invitations/check": { body: preview },
      "POST /api/invitations/accept": {
        body: { id: "wsp_2yEHYLwiwPVhLW", name: "Acme" },
      },
    });
    renderAt(link, bob, { invitation: true });
    expect(await screen.findByText(/^dashboard/)).toBeVisible();
    expect(
      api.sent("POST").filter((s) => s.url.endsWith("/accept")),
    ).toHaveLength(1);
  });
  it("signed in as someone else, offers switching accounts", async () => {
    const api = stubApi({
      "POST /api/invitations/check": { body: preview },
      "POST /api/auth/logout": { status: 204 },
    });
    renderAt(link, testSession(), { invitation: true });
    expect(
      await screen.findByText(
        "This invitation is for bob@example.com; you’re signed in as ada@lewcontext.test.",
      ),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: "Join Acme" })).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: "Sign out and switch" }),
    );
    expect(await screen.findByLabelText("Email")).toHaveValue(
      "bob@example.com",
    );
    expect(api.sent("POST").map((s) => s.url)).toEqual([
      "/api/invitations/check",
      "/api/auth/logout",
      "/api/invitations/check",
    ]);
  });
});
describe("signing in or up from an invitation", () => {
  it("login shows the invitation and fills in its email", async () => {
    stubApi({ "POST /api/invitations/check": { body: preview } });
    renderAt(`/auth/login${next}`, null);
    expect(
      await screen.findByText("has invited you to join.", { exact: false }),
    ).toBeVisible();
    expect(screen.getByLabelText("Email")).toHaveValue("bob@example.com");
    // Switching to register keeps it.
    fireEvent.click(screen.getByRole("link", { name: "Sign up" }));
    expect(
      await screen.findByRole("heading", { name: "Create your account" }),
    ).toBeVisible();
    expect(screen.getByRole("status")).toHaveTextContent(
      "Acme has invited you to join.",
    );
    expect(screen.getByLabelText("Email")).toHaveValue("bob@example.com");
  });
  it("says so when the invitation is dead, and the form still works", async () => {
    stubApi({ "POST /api/invitations/check": deadLink });
    renderAt(`/auth/login${next}`, null);
    expect(
      await screen.findByText(
        "This invitation has expired or was revoked. You can still sign in.",
      ),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "Sign in" })).toBeEnabled();
  });
  it("shows nothing for any other next", async () => {
    const api = stubApi({});
    renderAt(`/auth/login?next=${encodeURIComponent("/app/settings")}`, null);
    expect(
      await screen.findByRole("heading", { name: "Sign in to your account" }),
    ).toBeVisible();
    expect(screen.queryByRole("status")).toBeNull();
    expect(api.sent()).toHaveLength(0);
  });
  it("registering goes back to the invitation, accepting at once", async () => {
    const api = stubApi({
      "POST /api/invitations/check": { body: preview },
      "POST /api/auth/register": { status: 201, body: bob },
    });
    renderAt(`/auth/register${next}`, null, { invitation: true });
    await screen.findByText("has invited you to join.", { exact: false });
    fireEvent.change(screen.getByLabelText("Full name"), {
      target: { value: "Bob" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "a long enough passphrase" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create account" }));
    // The stub's loader still says signed out, so the page offers sign-in;
    // what matters is where register went, and with what state.
    await waitFor(() =>
      expect(api.sent("POST").map((s) => s.url)).toContain(
        "/api/auth/register",
      ),
    );
    expect(
      await screen.findByRole("heading", { name: "Join Acme" }),
    ).toBeVisible();
  });
  it("registering without an invitation goes to onboarding", async () => {
    stubApi({ "POST /api/auth/register": { status: 201, body: bob } });
    renderAt("/auth/register", null);
    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "bob@example.com" },
    });
    fireEvent.change(screen.getByLabelText("Full name"), {
      target: { value: "Bob" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "a long enough passphrase" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create account" }));
    expect(await screen.findByText(/^onboarding/)).toBeVisible();
  });
});
