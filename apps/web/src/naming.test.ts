import { readdirSync } from "node:fs";
import { join, relative } from "node:path";
import { describe, expect, it } from "vitest";
// Component, library, type and test-helper files are kebab-case
// (docs/naming.md#web-app): button.tsx, use-admin-resource.ts,
// confirmation-dialog.test.tsx. A `.server` suffix is React Router's mark
// for a module kept out of the browser's bundles (policies.server.ts, which
// runs only at build time), so it may come before `.test` and the
// extension. pages/ has its own check (pages/layout.test.ts): every page
// is a folder with the page in index.tsx.
const checked = ["components", "lib", "types", "test"];
const kebab = /^[a-z0-9]+(-[a-z0-9]+)*(\.server)?(\.test)?\.(ts|tsx)$/;
// What's wrong with a file's name, or null.
export function badFileName(path: string): string | null {
  const name = path.split("/").at(-1)!;
  return kebab.test(name)
    ? null
    : `${path}: files in ${checked.join(", ")} are kebab-case (.ts, .tsx, .test.ts(x), .server before them); see docs/naming.md#web-app`;
}
function files(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) =>
    entry.isDirectory()
      ? files(join(dir, entry.name))
      : [join(dir, entry.name)],
  );
}
describe("file names", () => {
  it("are kebab-case outside pages/", () => {
    const src = import.meta.dirname;
    const all = checked.flatMap((dir) => files(join(src, dir)));
    expect(all.length).toBeGreaterThan(50);
    const problems = all
      .map((file) => badFileName(relative(src, file)))
      .filter((problem) => problem !== null);
    expect(problems).toEqual([]);
  });
  it("refuses other names", () => {
    for (const ok of [
      "components/ui/button.tsx",
      "lib/use-admin-resource.ts",
      "components/ui/confirmation-dialog.test.tsx",
      "lib/api/index.ts",
      "lib/policies.server.ts",
      "lib/policies.server.test.ts",
    ])
      expect(badFileName(ok)).toBeNull();
    for (const bad of [
      "components/ui/ProjectTable.tsx",
      "lib/useResource.ts",
      "components/ui/project_table.tsx",
      "lib/routes.spec.ts",
      "lib/notes.md",
      "lib/Policies.server.ts",
      "lib/policies.client.ts",
    ])
      expect(badFileName(bad)).toContain("kebab-case");
  });
});
