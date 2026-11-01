import { readdirSync } from "node:fs";
import { join, relative } from "node:path";
import { describe, expect, it } from "vitest";
// Component, library, type and test-helper files are kebab-case
// (docs/naming.md#web-app): button.tsx, use-admin-resource.ts,
// confirmation-dialog.test.tsx. pages/ is shaped like the URLs instead
// ([workspace]), so it isn't checked.
const checked = ["components", "lib", "types", "test"];
const kebab = /^[a-z0-9]+(-[a-z0-9]+)*(\.test)?\.(ts|tsx)$/;
// What's wrong with a file's name, or null.
export function badFileName(path: string): string | null {
  const name = path.split("/").at(-1)!;
  return kebab.test(name)
    ? null
    : `${path}: files in ${checked.join(", ")} are kebab-case (.ts, .tsx, .test.ts(x)); see docs/naming.md#web-app`;
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
    ])
      expect(badFileName(ok)).toBeNull();
    for (const bad of [
      "components/ui/ProjectTable.tsx",
      "lib/useResource.ts",
      "components/ui/project_table.tsx",
      "lib/routes.spec.ts",
      "lib/notes.md",
    ])
      expect(badFileName(bad)).toContain("kebab-case");
  });
});
