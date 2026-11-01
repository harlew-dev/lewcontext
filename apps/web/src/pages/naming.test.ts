import { describe, expect, it } from "vitest";
import type { RouteConfigEntry } from "@react-router/dev/routes";
import { routes } from "@/lib/routes";
import registry from "../routes";
// A resource page's default export is <Resource><Action>
// (docs/naming.md#web-app): routes.projects.show's page is ProjectShow.
// Each resource group's URLs are built with sample IDs, matched to their
// page in src/routes.ts, and the page imported.

// The app's page modules (the admin dashboard's are its own build).
const pages = import.meta.glob([
  "./**/*.tsx",
  "!./admin/**",
  "!./**/*.test.tsx",
]);
// Plurals that aren't the singular and an "s".
const irregular: Record<string, string> = {};
// A resource group's key as its page's resource: projects → Project.
export function resourceName(group: string): string {
  const singular = irregular[group] ?? group.replace(/s$/, "");
  return singular[0].toUpperCase() + singular.slice(1);
}
function capitalize(word: string): string {
  return word[0].toUpperCase() + word.slice(1);
}
// What's wrong with a page's export name, or null.
export function badPageExport(
  group: string,
  action: string,
  exported: string,
): string | null {
  const want = `${resourceName(group)}${capitalize(action)}`;
  return exported === want
    ? null
    : `routes.${group}.${action}'s page exports ${exported}: resource pages export <Resource><Action>, ${want}; see docs/naming.md#web-app`;
}
// Every page in the registry: its full path pattern and file. A route with
// an index child is that child's page.
function flatten(
  entries: RouteConfigEntry[],
  prefix = "",
): { path: string; file: string }[] {
  return entries.flatMap((entry) => {
    const path = [prefix, entry.path ?? ""].filter(Boolean).join("/");
    if (entry.children) return flatten(entry.children, path);
    return [{ path, file: entry.file }];
  });
}
function matches(pattern: string, url: string): boolean {
  const source = pattern
    .split("/")
    .map((part) => (part.startsWith(":") ? "[^/]+" : part))
    .join("/");
  return new RegExp(`^/${source}$`).test(url);
}
// Sample arguments: a workspace ID, then a record's.
const samples = ["wsp_2yEHYLwiwPVhLW", "prj_j292s2S2j7Qx4B"];
describe("page export names", () => {
  it("are <Resource><Action> for every resource page", async () => {
    const registered = flatten(registry);
    const problems: string[] = [];
    let checked = 0;
    for (const [group, value] of Object.entries(routes)) {
      if (typeof value !== "object") continue;
      for (const [action, build] of Object.entries(value)) {
        const fn = build as (...ids: string[]) => string;
        // Any sample fits a path parameter: only the URL's shape matters.
        const url = fn(...samples.slice(0, fn.length));
        const page = registered.find((r) => matches(r.path, url));
        const load = page && pages[`./${page.file.replace(/^pages\//, "")}`];
        if (!load) {
          problems.push(`routes.${group}.${action} (${url}) has no page`);
          continue;
        }
        const module = (await load()) as { default: { name: string } };
        const problem = badPageExport(group, action, module.default.name);
        if (problem) problems.push(problem);
        checked++;
      }
    }
    expect(problems).toEqual([]);
    expect(checked).toBeGreaterThanOrEqual(6);
  });
  it("refuses other names", () => {
    expect(badPageExport("projects", "index", "ProjectIndex")).toBeNull();
    expect(badPageExport("emailBlocks", "show", "EmailBlockShow")).toBeNull();
    expect(badPageExport("projects", "index", "Projects")).toBe(
      "routes.projects.index's page exports Projects: resource pages export <Resource><Action>, ProjectIndex; see docs/naming.md#web-app",
    );
    expect(badPageExport("workspaces", "edit", "WorkspaceSettings")).not.toBe(
      null,
    );
  });
});
