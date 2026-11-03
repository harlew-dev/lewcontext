import { basename } from "node:path";
import { describe, expect, it } from "vitest";
// A page's default export is named from its file (docs/naming.md#web-app):
// index.tsx is Index, [<param>].tsx is Show and layout.tsx is Layout.
// Every page and layout route under src/pages is checked, including the admin
// build's, by walking the filesystem rather than a list of resource groups, so
// a new page is enforced automatically.

// Every page module: the admin dashboard's pages are imported here at test
// time only (the app build still excludes them).
const pages = import.meta.glob(["./**/*.tsx", "!./**/*.test.tsx"]);

// The default export a page file's name requires.
export function pageExportName(file: string): string | null {
  const base = basename(file).replace(/\.tsx$/, "");
  if (base === "index") return "Index";
  if (base === "layout") return "Layout";
  if (/^\[[^/]+\]$/.test(base)) return "Show";
  return null;
}

// What's wrong with a page's export name, or null.
export function badPageExport(
  file: string,
  exported: string | undefined,
): string | null {
  const want = pageExportName(file);
  if (!want) {
    return `${file}: not a page file — pages are index.tsx, [<param>].tsx or layout.tsx; see docs/naming.md#web-app`;
  }
  if (exported === want) return null;
  return `${file}: its default export is ${exported ?? "missing"}: name it ${want} after its file, export default function ${want}; see docs/naming.md#web-app`;
}

describe("page export names", () => {
  it("are Index, Show or Layout for every page and layout file", async () => {
    const problems: string[] = [];
    let checked = 0;
    for (const [key, load] of Object.entries(pages)) {
      const file = `pages/${key.replace(/^\.\//, "")}`;
      const module = (await load()) as { default?: unknown };
      const exported =
        typeof module.default === "function"
          ? (module.default as { name: string }).name
          : undefined;
      const problem = badPageExport(file, exported);
      if (problem) problems.push(problem);
      checked++;
    }
    expect(problems).toEqual([]);
    // A moved or emptied tree can't make the check pass quietly.
    expect(checked).toBeGreaterThanOrEqual(60);
  });

  it("refuses other names", () => {
    expect(badPageExport("index.tsx", "Index")).toBeNull();
    expect(badPageExport("[project].tsx", "Show")).toBeNull();
    expect(badPageExport("layout.tsx", "Layout")).toBeNull();
    expect(badPageExport("index.tsx", "Home")).toBe(
      "index.tsx: its default export is Home: name it Index after its file, export default function Index; see docs/naming.md#web-app",
    );
    expect(badPageExport("[project].tsx", "ProjectShow")).toBe(
      "[project].tsx: its default export is ProjectShow: name it Show after its file, export default function Show; see docs/naming.md#web-app",
    );
    expect(badPageExport("layout.tsx", "AppLayout")).toBe(
      "layout.tsx: its default export is AppLayout: name it Layout after its file, export default function Layout; see docs/naming.md#web-app",
    );
    expect(badPageExport("index.tsx", undefined)).toBe(
      "index.tsx: its default export is missing: name it Index after its file, export default function Index; see docs/naming.md#web-app",
    );
  });
});
