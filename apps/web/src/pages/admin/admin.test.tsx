import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fireEvent, render, screen, within } from "@testing-library/react";
import {
  createRoutesStub,
  isRouteErrorResponse,
  Outlet,
  useRouteError,
} from "react-router";
import { SWRConfig } from "swr";
import { describe, expect, it } from "vitest";
import { NotFound } from "@/components/layout/status-page";
import { adminLoader, adminLoginLoader } from "@/lib/loaders";
import { gate, stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import {
  adminUserSortable,
  adminWorkspaceSortable,
  type AdminUser,
} from "@/types/admin";
import AdminLogin from "./[key]/index";
import AdminUserIndex from "./[key]/users";
import AdminLayoutRoute from "./layout";
const key = "8a1f0c52-6d3e-4b7a-9c2d-5e8f1a3b7c90";
const admin = testSession({
  user: { id: "usr_00000000000009", name: "Olive Admin" },
});
const me = { user: admin.user, csrfToken: "admin-csrf", expiresAt: "" };
const emptyPage = {
  data: [],
  pages: { current: 1, total: 1, perPage: 20, items: 0 },
};
function user(n: number, overrides: Partial<AdminUser> = {}): AdminUser {
  return {
    id: `usr_0000000000000${n}`,
    name: `User ${n}`,
    email: `user${n}@example.com`,
    createdAt: "2026-10-30T12:00:00Z",
    mfaEnabled: false,
    admin: false,
    workspaceCount: 1,
    lastSignedInAt: null,
    ...overrides,
  };
}
// The root's error boundary: a 404 from a loader is the not-found page.
function RootBoundary() {
  const error = useRouteError();
  return isRouteErrorResponse(error) && error.status === 404 ? (
    <NotFound />
  ) : (
    <p>Unexpected error</p>
  );
}
// The admin routes with their real loaders, as routes.ts registers them.
function renderAdmin(path: string) {
  const Stub = createRoutesStub([
    {
      path: "/",
      Component: Outlet,
      ErrorBoundary: RootBoundary,
      children: [
        {
          path: "admin/:key",
          Component: AdminLogin,
          loader: ({ params }) => adminLoginLoader(params.key!),
        },
        {
          Component: AdminLayoutRoute,
          loader: ({ params }) => adminLoader(params.key!),
          children: [{ path: "admin/:key/users", Component: AdminUserIndex }],
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
function type(label: string, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
}
function signIn() {
  type("Email", "olive@example.com");
  type("Password", "correct horse");
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
}
describe("the admin door", () => {
  it("renders the ordinary not-found page for a wrong key, never the form", async () => {
    const api = stubApi({
      "GET /api/admin/me": { status: 404, body: { error: "Not found." } },
    });
    renderAdmin("/admin/not-the-key");
    expect(
      await screen.findByText("This page has wandered off."),
    ).toBeInTheDocument();
    expect(screen.queryByText(/admin/i)).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
    // The key went in the header, never the URL.
    expect(api.sent()).toEqual([
      expect.objectContaining({
        url: "/api/admin/me",
        adminKey: "not-the-key",
      }),
    ]);
  });
  it("signs in with the password, then a code, and opens Users", async () => {
    const api = stubApi({
      "GET /api/admin/me": [
        { status: 401, body: { error: "Please sign in to continue." } },
        { body: me },
      ],
      "POST /api/admin/login": [
        { status: 401, body: { error: "Email or password is incorrect." } },
        { body: { mfaRequired: true } },
      ],
      "POST /api/admin/login/mfa": [
        {
          status: 422,
          body: { error: "That code didn’t work.", field: "code" },
        },
        { body: me },
      ],
      "GET /api/admin/users": { body: emptyPage },
    });
    renderAdmin(`/admin/${key}`);
    expect(
      await screen.findByRole("heading", { name: "Admin sign-in" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: /sign up|forgot/i }),
    ).not.toBeInTheDocument();
    signIn();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Email or password is incorrect.",
    );
    signIn();
    expect(
      await screen.findByRole("heading", { name: "Two-factor authentication" }),
    ).toBeInTheDocument();
    type("Authentication code", "000000");
    fireEvent.click(screen.getByRole("button", { name: "Verify" }));
    expect(
      await screen.findByText("That code didn’t work."),
    ).toBeInTheDocument();
    type("Authentication code", "123456");
    fireEvent.click(screen.getByRole("button", { name: "Verify" }));
    expect(
      await screen.findByRole("heading", { name: "Users" }),
    ).toBeInTheDocument();
    for (const request of api.sent()) {
      expect(request.adminKey).toBe(key);
      expect(request.url).not.toContain(key);
    }
    expect(api.sent("POST").map((r) => r.body)).toEqual([
      { email: "olive@example.com", password: "correct horse" },
      { email: "olive@example.com", password: "correct horse" },
      { code: "000000" },
      { code: "123456" },
    ]);
    expect(document.body.textContent).not.toContain(key);
  });
  it("sets up MFA for an admin without it, with everything secret hidden until revealed", async () => {
    const secret = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP";
    const codes = Array.from({ length: 10 }, (_, i) => `abcde-fgh${i}k`);
    const api = stubApi({
      "GET /api/admin/me": [
        { status: 401, body: { error: "Please sign in." } },
        { body: me },
      ],
      "POST /api/admin/login": { body: { mfaSetupRequired: true } },
      "POST /api/admin/login/mfa/setup": {
        body: {
          secret,
          uri: `otpauth://totp/lewcontext:olive?secret=${secret}`,
        },
      },
      "POST /api/admin/login/mfa": {
        status: 201,
        body: { ...me, recoveryCodes: codes },
      },
      "GET /api/admin/users": { body: emptyPage },
    });
    renderAdmin(`/admin/${key}`);
    await screen.findByRole("heading", { name: "Admin sign-in" });
    signIn();
    expect(
      await screen.findByRole("heading", {
        name: "Set up two-factor authentication",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/Admins need two-factor authentication/),
    ).toBeInTheDocument();
    expect(document.body.innerHTML).not.toContain(secret);
    type("Code from your app", "123456");
    fireEvent.click(screen.getByRole("button", { name: "Verify" }));
    expect(
      await screen.findByRole("heading", { name: "Save your recovery codes" }),
    ).toBeInTheDocument();
    expect(document.body.textContent).not.toContain(codes[0]);
    const done = screen.getByRole("button", { name: "I’ve saved these codes" });
    expect(done).toBeDisabled();
    fireEvent.click(
      screen.getByRole("button", { name: "Reveal recovery codes" }),
    );
    expect(screen.getByText(codes[0])).toBeInTheDocument();
    fireEvent.click(done);
    expect(
      await screen.findByRole("heading", { name: "Users" }),
    ).toBeInTheDocument();
    expect(api.sent("POST").map((r) => r.url)).toEqual([
      "/api/admin/login",
      "/api/admin/login/mfa/setup",
      "/api/admin/login/mfa",
    ]);
  });
  it("goes back to the password when the setup's sign-in has ended", async () => {
    stubApi({
      "GET /api/admin/me": { status: 401, body: { error: "Please sign in." } },
      "POST /api/admin/login": { body: { mfaSetupRequired: true } },
      "POST /api/admin/login/mfa/setup": {
        body: { secret: "JBSWY3DPEHPK3PXP", uri: "otpauth://totp/x" },
      },
      "POST /api/admin/login/mfa": {
        status: 401,
        body: { error: "Your sign-in expired. Enter your password again." },
      },
    });
    renderAdmin(`/admin/${key}`);
    await screen.findByRole("heading", { name: "Admin sign-in" });
    signIn();
    await screen.findByRole("heading", {
      name: "Set up two-factor authentication",
    });
    type("Code from your app", "123456");
    fireEvent.click(screen.getByRole("button", { name: "Verify" }));
    expect(
      await screen.findByText(
        "Your sign-in expired. Enter your password again.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Admin sign-in" }),
    ).toBeInTheDocument();
  });
});
describe("the admin layout", () => {
  it("sends a visitor without an admin session to the admin sign-in page", async () => {
    stubApi({
      "GET /api/admin/me": {
        status: 401,
        body: { error: "Please sign in to continue." },
      },
    });
    renderAdmin(`/admin/${key}/users`);
    expect(
      await screen.findByRole("heading", { name: "Admin sign-in" }),
    ).toBeInTheDocument();
  });
  it("renders not-found for a wrong key on an inner page", async () => {
    stubApi({
      "GET /api/admin/me": { status: 404, body: { error: "Not found." } },
    });
    renderAdmin("/admin/wrong/users");
    expect(
      await screen.findByText("This page has wandered off."),
    ).toBeInTheDocument();
  });
  it("shows who is signed in, with an Admin badge and a way back to the app", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/users": { body: emptyPage },
    });
    renderAdmin(`/admin/${key}/users`);
    const nav = await screen.findByLabelText("Admin navigation");
    expect(within(nav).getByRole("link", { name: "Users" })).toHaveAttribute(
      "href",
      `/admin/${key}/users`,
    );
    expect(screen.getAllByText("Admin").length).toBeGreaterThan(0);
    expect(screen.getByText("Olive Admin")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Admin account menu" }));
    expect(
      await screen.findByRole("menuitem", { name: "Back to app" }),
    ).toHaveAttribute("href", "/app");
    expect(
      screen.getByRole("menuitem", { name: "Account & security" }),
    ).toHaveAttribute("href", "/app/settings");
    expect(screen.getByRole("menuitem", { name: "Sign out" })).toBeVisible();
    expect(screen.queryByText("Signed in as")).toBeNull();
  });
});
describe("the users list", () => {
  it("shows a skeleton, then the rows with links and badges", async () => {
    const held = gate();
    const api = stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/users": {
        gate: held.promise,
        body: {
          data: [user(1, { mfaEnabled: true, admin: true }), user(2)],
          pages: { current: 1, total: 1, perPage: 20, items: 2 },
        },
      },
    });
    renderAdmin(`/admin/${key}/users`);
    expect(await screen.findByLabelText("Loading users")).toBeInTheDocument();
    held.release();
    const [row] = await screen.findAllByRole("link", { name: "Open User 1" });
    expect(row).toHaveAttribute(
      "href",
      `/admin/${key}/users/usr_00000000000001`,
    );
    expect(screen.getByText("MFA")).toBeInTheDocument();
    expect(screen.getByText("No MFA")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Name" })).toHaveAttribute(
      "href",
      `/admin/${key}/users?sort=name`,
    );
    const list = api.sent().find((r) => r.url.startsWith("/api/admin/users"));
    expect(list).toMatchObject({ url: "/api/admin/users", adminKey: key });
  });
  it("says when nothing matches a search", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/users": { body: emptyPage },
    });
    renderAdmin(`/admin/${key}/users?q=nobody`);
    expect(await screen.findByText("No users match")).toBeInTheDocument();
  });
  it("keeps the error on screen with Retry", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/users": [
        {
          status: 500,
          body: { error: "Something went wrong. Please try again." },
        },
        {
          body: {
            data: [user(3)],
            pages: { current: 1, total: 1, perPage: 20, items: 1 },
          },
        },
      ],
    });
    renderAdmin(`/admin/${key}/users`);
    expect(
      await screen.findByText(
        "Something went wrong. Please try again.",
        {},
        { timeout: 15000 },
      ),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(
      (await screen.findAllByRole("link", { name: "Open User 3" }))[0],
    ).toBeInTheDocument();
  }, 20000);
  it("mirrors the API's sort allowlists", () => {
    // Vitest runs from apps/web.
    const go = readFileSync(
      resolve(process.cwd(), "../api/internal/store/admin_views.go"),
      "utf8",
    );
    const keys = (name: string) => {
      const map = go.match(
        new RegExp(`var ${name} = map\\[string\\]string\\{([^}]*)\\}`),
      );
      return [...(map?.[1] ?? "").matchAll(/"(-?\w+)":/g)]
        .map((m) => m[1])
        .sort();
    };
    const both = (fields: readonly string[]) =>
      fields.flatMap((f) => [f, `-${f}`]).sort();
    expect(keys("adminUserSorts")).toEqual(both(adminUserSortable));
    expect(keys("adminWorkspaceSorts")).toEqual(both(adminWorkspaceSortable));
  });
});
