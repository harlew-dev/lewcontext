import { describe, expect, it } from "vitest";
import { authPath, loginPath, navigation, returnPath, routes } from "./routes";
const ws = "wsp_2yEHYLwiwPVhLW";
const prj = "prj_j292s2S2j7Qx4B";
describe("routes", () => {
  it("nest workspace pages under the workspace ID", () => {
    expect(routes.dashboard(ws)).toBe(`/app/${ws}/dashboard`);
    expect(routes.projects(ws)).toBe(`/app/${ws}/projects`);
    expect(routes.project(ws, prj)).toBe(`/app/${ws}/projects/${prj}`);
    expect(routes.workspaceSettings(ws)).toBe(`/app/${ws}/settings`);
    expect(navigation(ws).map((item) => item.href)).toEqual([
      routes.dashboard(ws),
      routes.projects(ws),
    ]);
  });
});
describe("returnPath()", () => {
  it("accepts every signed-in page, with its query", () => {
    for (const path of [
      routes.app,
      routes.settings,
      routes.dashboard(ws),
      routes.projects(ws),
      routes.project(ws, prj),
      routes.workspaceSettings(ws),
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
