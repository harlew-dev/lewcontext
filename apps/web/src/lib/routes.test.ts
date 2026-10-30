import { describe, expect, it } from "vitest";
import { authPath, loginPath, returnPath, routes } from "./routes";
describe("returnPath()", () => {
  it("accepts every signed-in page, with its query", () => {
    for (const path of [
      routes.dashboard,
      routes.projects,
      routes.project(42),
      routes.settings,
    ])
      expect(returnPath(path)).toBe(path);
    expect(returnPath("/app/projects?status=Planned&page=2")).toBe(
      "/app/projects?status=Planned&page=2",
    );
  });
  it("rejects other sites, other pages and malformed values", () => {
    for (const next of [
      "https://evil.example/app/dashboard",
      "//evil.example/app/dashboard",
      "/\\evil.example/app/dashboard",
      "javascript:alert(1)",
      "/auth/login",
      "/api/auth/me",
      "/app/projects/0",
      "/app/projects/1x",
      "/app/dashboard/extra",
      "/app/../auth/login",
      "app/dashboard",
      "/" + "a".repeat(600),
      ["/app/dashboard"],
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
    expect(loginPath("/app/projects?page=2")).toBe(
      "/auth/login?next=%2Fapp%2Fprojects%3Fpage%3D2",
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
