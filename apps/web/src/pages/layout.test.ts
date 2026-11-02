import { readdirSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
// Page files are folders: every page lives at <folder>/index.tsx, a record's
// page at <folder>/[<param>].tsx next to it, and a folder with pages beneath
// it holds its bare URL's own page as index.tsx (docs/naming.md#web-app).
// Unlike routes.ts, which registers only pages, this reads the folder tree,
// so it also catches a file that hasn't been registered yet.

// A page file is a .tsx file that isn't a test or a layout route.
function isPageFile(name: string): boolean {
  return (
    name.endsWith(".tsx") &&
    !name.endsWith(".test.tsx") &&
    name !== "layout.tsx"
  );
}

// A folder that holds record pages but no bare-URL page of its own. The
// reason is why the folder exists and why it doesn't need an index.tsx.
const noIndex: Record<string, string> = {
  "auth/invitations":
    "the invitation's link page is [token]; the URL has no page without one",
  "auth/email-blocks":
    "the block's link page is [token]; the URL has no page without one",
};

// The folder's path as shown in a message (pages/ for the tree's root).
function shown(dir: string): string {
  return dir === "" ? "pages" : dir;
}

// What's wrong with a page file's location, or null.
export function badPageFile(dir: string, name: string): string | null {
  const base = name.replace(/\.tsx$/, "");
  if (base === "index" || /^\[[^/]+\]$/.test(base)) return null;
  return `${shown(dir)}/${name}: move it to ${shown(dir)}/${base}/index.tsx — every page is a folder whose index.tsx is the page; see docs/naming.md#web-app`;
}

// What's wrong with a folder's contents, or null.
export function badFolder(dir: string, names: string[]): string | null {
  const pages = names.filter(isPageFile);
  if (pages.length === 0) return null;
  if (names.includes("index.tsx") || noIndex[dir]) return null;
  return `${shown(dir)}/ holds ${pages.join(", ")} but has no index.tsx of its own — give the folder its bare URL's page as index.tsx, or a reason in noIndex; see docs/naming.md#web-app`;
}

// Every problem in the tree (paths relative to pages/), and what it saw.
export function treeProblems(root: string): string[] {
  const problems: string[] = [];
  let seenPages = 0;
  let seenFolders = 0;
  function walk(dir: string) {
    const names = readdirSync(dir, { withFileTypes: true });
    const entries = names.map((entry) => entry.name);
    const folder = dir.slice(root.length + 1);
    if (folder) seenFolders++;
    for (const entry of names) {
      if (entry.isDirectory()) walk(join(dir, entry.name));
      else if (isPageFile(entry.name)) {
        seenPages++;
        const problem = badPageFile(folder, entry.name);
        if (problem) problems.push(problem);
      }
    }
    const problem = badFolder(folder, entries);
    if (problem) problems.push(problem);
  }
  walk(root);
  // A move that empties the tree can't make the check pass quietly.
  expect(seenPages).toBeGreaterThanOrEqual(40);
  expect(seenFolders).toBeGreaterThanOrEqual(5);
  return problems;
}

describe("page folders", () => {
  it("every page is a folder's index.tsx or a record's [<param>].tsx", () => {
    const src = import.meta.dirname;
    expect(treeProblems(src)).toEqual([]);
  });

  it("refuses other locations", () => {
    for (const [dir, name] of [
      ["", "index.tsx"],
      ["app", "index.tsx"],
      ["app/[workspace]", "index.tsx"],
      ["app/[workspace]/projects", "[project].tsx"],
      ["app/[workspace]/settings", "index.tsx"],
      ["admin/[key]", "index.tsx"],
    ]) {
      expect(badPageFile(dir, name)).toBeNull();
    }
    expect(badPageFile("app/[workspace]", "settings.tsx")).toContain(
      "move it to app/[workspace]/settings/index.tsx",
    );
    expect(badPageFile("", "foo.tsx")).toContain(
      "move it to pages/foo/index.tsx",
    );
    expect(badFolder("settings", ["layout.tsx", "members.tsx"])).toContain(
      "has no index.tsx",
    );
    for (const dir of Object.keys(noIndex)) {
      expect(badFolder(dir, ["[token].tsx"])).toBeNull();
    }
    expect(badFolder("projects", ["[project].tsx"])).toContain(
      "has no index.tsx of its own",
    );
    expect(noIndex["auth/invitations"]).toContain("no page");
  });
});
