import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { testWorkspace } from "@/test/workspace";
import { can, permissions, roles } from "./workspaces";
// Vitest runs from apps/web.
const go = readFileSync(
  resolve(process.cwd(), "../api/internal/auth/permissions.go"),
  "utf8",
);
describe("workspace permissions", () => {
  it("mirrors the API's permission names", () => {
    const names = [...go.matchAll(/\w+\s+Permission = "([a-z.]+)"/g)].map(
      (m) => m[1],
    );
    expect(names.sort()).toEqual([...permissions].sort());
  });
  it("mirrors the API's roles, most access first", () => {
    const names = [...go.matchAll(/Role\w+\s+Role = "(\w+)"/g)].map(
      (m) => m[1],
    );
    expect(names).toEqual([...roles]);
  });
  it("reads permissions from the workspace, not the role", () => {
    expect(can(testWorkspace("member"), "project.store")).toBe(true);
    expect(can(testWorkspace("viewer"), "project.store")).toBe(false);
    expect(
      can(testWorkspace("owner", { permissions: [] }), "workspace.update"),
    ).toBe(false);
  });
});
