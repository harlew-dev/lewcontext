import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { createRoutesStub, Outlet } from "react-router";
import { SWRConfig } from "swr";
import { afterEach, describe, expect, it, vi } from "vitest";
import { adminCan, adminLoader } from "@/lib/admin";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import type { AdminAccount, AdminRole, AdminSession } from "@/types/admin";
import AdminAdminIndex from "./[key]/admins";
import AdminUserIndex from "./[key]/users";
import AdminLayoutRoute from "./layout";
const key = "8a1f0c52-6d3e-4b7a-9c2d-5e8f1a3b7c90";
const admin = testSession({
  user: { id: "usr_00000000000009", name: "Olive Admin" },
});
const me: AdminSession = {
  user: admin.user,
  role: "operator",
  csrfToken: "admin-csrf",
  expiresAt: "",
};
const emptyPage = {
  data: [],
  pages: { current: 1, total: 1, perPage: 20, items: 0 },
};
const retiring = { "X-Admin-Key-Status": "previous" };
const banner = /This admin address is being retired/;
const day = 24 * 60 * 60 * 1000;
const ago = (days: number) => new Date(Date.now() - days * day).toISOString();
function account(overrides: Partial<AdminAccount>): AdminAccount {
  return {
    user: { id: "usr_00000000000001", name: "Ada", email: "ada@example.com" },
    role: "viewer",
    mfaEnabled: true,
    mfaSetupUntil: null,
    grantedAt: ago(200),
    lastSignedInAt: ago(2),
    sessions: [],
    ...overrides,
  };
}
// Olive (an operator, this session and one other, the other from before the
// key was rotated), a viewer last seen 120 days ago, and a newcomer still
// inside their setup window.
const admins: AdminAccount[] = [
  account({
    user: { id: me.user.id, name: "Olive Admin", email: "olive@example.com" },
    role: "operator",
    sessions: [
      {
        createdAt: ago(0),
        lastSeenAt: ago(0),
        ip: "203.0.113.5",
        userAgent: "Firefox on Linux",
        current: true,
        previousKey: false,
      },
      {
        createdAt: ago(0.2),
        lastSeenAt: ago(0.1),
        ip: "2001:db8::7",
        userAgent: "Safari on iPhone",
        current: false,
        previousKey: true,
      },
    ],
  }),
  account({
    user: { id: "usr_00000000000002", name: "Vic", email: "=cmd@example.com" },
    lastSignedInAt: ago(120),
  }),
  account({
    user: { id: "usr_00000000000003", name: "Neil", email: "neil@example.com" },
    mfaEnabled: false,
    mfaSetupUntil: new Date(Date.now() + day).toISOString(),
    grantedAt: ago(0),
    lastSignedInAt: null,
  }),
];
function renderAdmin(path: string) {
  const Stub = createRoutesStub([
    {
      path: "/",
      Component: Outlet,
      children: [
        {
          Component: AdminLayoutRoute,
          loader: ({ params }) => adminLoader(params.key!),
          children: [
            { path: "admin/:key/users", Component: AdminUserIndex },
            { path: "admin/:key/admins", Component: AdminAdminIndex },
          ],
        },
      ],
    },
  ]);
  render(
    <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>
      <Stub initialEntries={[path]} />
    </SWRConfig>,
  );
}
afterEach(() => vi.restoreAllMocks());
describe("the retiring key's notice", () => {
  it("shows above every page when the session came under the previous key", async () => {
    stubApi({
      "GET /api/admin/me": { body: me, headers: retiring },
      "GET /api/admin/users": { body: emptyPage, headers: retiring },
    });
    renderAdmin(`/admin/${key}/users`);
    expect(await screen.findByRole("note")).toHaveTextContent(banner);
    expect(screen.getByRole("note")).toHaveTextContent(
      "Ask your operator for the new one before this session ends.",
    );
  });
  it("isn't there under the current key, and appears once any answer says the key is retiring", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/users": { body: emptyPage, headers: retiring },
    });
    renderAdmin(`/admin/${key}/users`);
    await screen.findByRole("heading", { name: "Users" });
    // The users list answered after the rotation.
    expect(await screen.findByText(banner)).toBeInTheDocument();
  });
  it("stays away while nothing says so", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/users": { body: emptyPage },
    });
    renderAdmin(`/admin/${key}/users`);
    await screen.findByText("No users yet");
    expect(screen.queryByText(banner)).not.toBeInTheDocument();
  });
});
describe("the Admins page", () => {
  it("lists every admin with their role, MFA and staleness, and every live session", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/admins": { body: { data: admins } },
    });
    renderAdmin(`/admin/${key}/admins`);
    const [table, sessions] = await screen.findAllByRole("table");
    const [olive, vic, neil] = within(table).getAllByRole("row").slice(1);
    expect(within(olive).getByText("Operator")).toBeInTheDocument();
    expect(within(olive).getByText("MFA")).toBeInTheDocument();
    expect(olive).not.toHaveTextContent("consider revoking");
    expect(within(vic).getByText("Viewer")).toBeInTheDocument();
    expect(vic).toHaveTextContent(
      "no admin sign-in for 90 days: consider revoking",
    );
    expect(within(neil).getByText("No MFA")).toBeInTheDocument();
    expect(within(neil).getByText("Setup window open")).toBeInTheDocument();
    expect(neil).toHaveTextContent("Never");
    // Granted today and never signed in: not stale yet.
    expect(neil).not.toHaveTextContent("consider revoking");

    const [current, other] = within(sessions).getAllByRole("row").slice(1);
    expect(within(current).getByText("This session")).toBeInTheDocument();
    expect(current).toHaveTextContent("203.0.113.5");
    expect(within(other).getByText("Retiring address")).toBeInTheDocument();
    expect(other).toHaveTextContent("Safari on iPhone");
  });
  it("downloads the access review as CSV, from the list already loaded", async () => {
    let saved: Blob | undefined;
    vi.stubGlobal(
      "URL",
      class extends URL {
        static createObjectURL(blob: Blob) {
          saved = blob;
          return "blob:admins";
        }
        static revokeObjectURL() {}
      },
    );
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => {});
    const api = stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/admins": { body: { data: admins } },
    });
    renderAdmin(`/admin/${key}/admins`);
    await screen.findAllByRole("table");
    fireEvent.click(screen.getByRole("button", { name: "Download CSV" }));
    expect(click).toHaveBeenCalled();
    const text = await new Promise<string>((resolve) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result));
      reader.readAsText(saved!);
    });
    const lines = text.trimEnd().split("\r\n");
    expect(lines[0]).toBe('"Email","Role","MFA","Granted","Last sign-in"');
    expect(lines).toHaveLength(4);
    expect(lines[1]).toBe(
      `"olive@example.com","operator","on","${admins[0].grantedAt}","${admins[0].lastSignedInAt}"`,
    );
    // A cell a spreadsheet would run as a formula is defused.
    expect(lines[2]).toMatch(/^"'=cmd@example.com","viewer","on",/);
    expect(lines[3]).toMatch(/^"neil@example.com","viewer","off","[^"]+",""$/);
    // Built in the browser: one read, the page's own.
    expect(
      api.sent().filter((r) => r.url === "/api/admin/admins"),
    ).toHaveLength(1);
  });
  it("signs out my other sessions, with the CSRF token, then reloads the list", async () => {
    const api = stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/admins": [
        { body: { data: admins } },
        {
          body: {
            data: [
              { ...admins[0], sessions: admins[0].sessions.slice(0, 1) },
              ...admins.slice(1),
            ],
          },
        },
      ],
      "DELETE /api/admin/sessions": { status: 204 },
    });
    renderAdmin(`/admin/${key}/admins`);
    await screen.findAllByRole("table");
    fireEvent.click(
      screen.getByRole("button", { name: "Sign out my other sessions" }),
    );
    expect(
      await screen.findByText(
        "Your other admin sessions have been signed out.",
      ),
    ).toBeInTheDocument();
    expect(api.sent("DELETE")).toEqual([
      expect.objectContaining({
        url: "/api/admin/sessions",
        csrf: "admin-csrf",
        adminKey: key,
      }),
    ]);
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Sign out my other sessions" }),
      ).toBeDisabled(),
    );
    expect(screen.queryByText("Retiring address")).not.toBeInTheDocument();
  });
  it("shows a failure as a toast and keeps the list", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/admins": { body: { data: admins } },
      "DELETE /api/admin/sessions": {
        status: 500,
        body: { error: "Something went wrong." },
      },
    });
    renderAdmin(`/admin/${key}/admins`);
    await screen.findAllByRole("table");
    fireEvent.click(
      screen.getByRole("button", { name: "Sign out my other sessions" }),
    );
    expect(
      await screen.findByText("Something went wrong."),
    ).toBeInTheDocument();
    expect(screen.getByText("Retiring address")).toBeInTheDocument();
  });
  it("shows an error with Try again when the list can't load", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/admins": [
        { status: 500, body: { error: "Something went wrong." } },
        { body: { data: admins } },
      ],
    });
    renderAdmin(`/admin/${key}/admins`);
    fireEvent.click(await screen.findByRole("button", { name: "Try again" }));
    expect(await screen.findAllByRole("table")).toHaveLength(2);
  });
});
describe("admin roles", () => {
  it("lets an operator do what a viewer may, never the other way round", () => {
    expect(adminCan("operator", "operator")).toBe(true);
    expect(adminCan("operator", "viewer")).toBe(true);
    expect(adminCan("viewer", "viewer")).toBe(true);
    expect(adminCan("viewer", "operator")).toBe(false);
    // Roles the API might add later, or a bad value, grant nothing.
    const unknown = "root" as AdminRole;
    expect(adminCan(unknown, unknown)).toBe(false);
    expect(adminCan(unknown, "viewer")).toBe(false);
    expect(adminCan("operator", unknown)).toBe(false);
  });
});
