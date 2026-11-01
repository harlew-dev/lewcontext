import { describe, expect, it } from "vitest";
import { authPath, loginPath, navigation, returnPath, routes } from "./routes";
const ws = "wsp_2yEHYLwiwPVhLW";
const prj = "prj_j292s2S2j7Qx4B";
describe("routes", () => {
  it("nest workspace pages under the workspace ID", () => {
    expect(routes.dashboard(ws)).toBe(`/app/${ws}/dashboard`);
    expect(routes.projects.index(ws)).toBe(`/app/${ws}/projects`);
    expect(routes.projects.show(ws, prj)).toBe(`/app/${ws}/projects/${prj}`);
    expect(routes.workspaces.edit(ws)).toBe(`/app/${ws}/settings`);
    expect(routes.members.index(ws)).toBe(`/app/${ws}/settings/members`);
    expect(navigation(ws).map((item) => item.href)).toEqual([
      routes.dashboard(ws),
      routes.projects.index(ws),
    ]);
  });
});
describe("returnPath()", () => {
  it("accepts every signed-in page, with its query", () => {
    for (const path of [
      routes.app,
      routes.settings,
      routes.dashboard(ws),
      routes.projects.index(ws),
      routes.projects.show(ws, prj),
      routes.workspaces.edit(ws),
      routes.members.index(ws),
    ])
      expect(returnPath(path)).toBe(path);
    expect(returnPath(`/app/${ws}/projects?status=Planned&page=2`)).toBe(
      `/app/${ws}/projects?status=Planned&page=2`,
    );
  });
  it("rejects other sites, other pages and malformed values", () => {
    for (const next of [
      "https://evil.example/app",
      "//evil.example/app",
      "/\\evil.example/app",
      "javascript:alert(1)",
      "/auth/login",
      "/api/auth/me",
      routes.onboarding,
      "/app/dashboard",
      "/app/projects/42",
      `/app/${ws}/projects/42`,
      `/app/wsp_short/projects`,
      `/app/${prj}/projects`,
      `/app/${ws}/dashboard/extra`,
      "/app/../auth/login",
      "app",
      "/" + "a".repeat(600),
      ["/app"],
      undefined,
    ])
      expect(returnPath(next)).toBeNull();
  });
  it("drops the fragment", () => {
    expect(returnPath("/app/settings#sessions")).toBe("/app/settings");
  });
});
describe("authPath() and loginPath()", () => {
  it("carry an allowed return path", () => {
    expect(loginPath(`/app/${ws}/projects?page=2`)).toBe(
      `/auth/login?next=%2Fapp%2F${ws}%2Fprojects%3Fpage%3D2`,
    );
    expect(authPath(routes.register, "/app/settings")).toBe(
      "/auth/register?next=%2Fapp%2Fsettings",
    );
  });
  it("leave out anything else", () => {
    expect(loginPath("//evil.example")).toBe(routes.login);
    expect(authPath(routes.register, undefined)).toBe(routes.register);
  });
});
// Page route constants (docs/naming.md#web-app): a resource group holds only
// the page actions; every other key is a page that isn't a resource,
// listed here, so a new page is a resource action or a deliberate choice.
const pageActions = ["index", "show", "create", "edit"];
const plainPages = [
  "home",
  "app",
  "onboarding",
  "verifyEmail",
  "settings",
  "dashboard",
  "login",
  "register",
  "registerVerify",
  "forgotPassword",
  // The policy pages: two fixed documents, prerendered, not a resource.
  "termsOfService",
  "privacyPolicy",
];
// What's wrong with a routes object's keys.
function badRouteNames(
  table: Record<string, unknown>,
  plain: string[],
): string[] {
  const problems: string[] = [];
  for (const [key, value] of Object.entries(table)) {
    const group = typeof value === "object" && value !== null;
    if (!group) {
      if (!plain.includes(key))
        problems.push(
          `routes.${key}: a page that isn't a resource is listed in plainPages (lib/routes.test.ts), or it's a resource group with index, show, create or edit; see docs/naming.md#web-app`,
        );
      continue;
    }
    for (const action of Object.keys(value))
      if (!pageActions.includes(action))
        problems.push(
          `routes.${key}.${action}: resource pages use the page actions ${pageActions.join(", ")}; see docs/naming.md#web-app`,
        );
  }
  return problems;
}
describe("page route constant names", () => {
  it("are resource groups of page actions, or listed plain pages", () => {
    expect(badRouteNames(routes, plainPages)).toEqual([]);
    // A listed page that's gone comes off the list.
    expect(plainPages.filter((key) => !(key in routes))).toEqual([]);
  });
  it("refuses other names", () => {
    expect(
      badRouteNames(
        {
          projectList: (ws: string) => ws,
          projects: { list: () => "", show: () => "" },
          home: "/",
        },
        plainPages,
      ),
    ).toEqual([
      "routes.projectList: a page that isn't a resource is listed in plainPages (lib/routes.test.ts), or it's a resource group with index, show, create or edit; see docs/naming.md#web-app",
      "routes.projects.list: resource pages use the page actions index, show, create, edit; see docs/naming.md#web-app",
    ]);
  });
});
