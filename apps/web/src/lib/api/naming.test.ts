import { describe, expect, it } from "vitest";
import type { Http } from "./http";
import { client, paths } from "./index";
// The API client uses the API's actions (docs/naming.md#web-app): index,
// show, store, update and destroy, plus a resource's listed named verbs.
// A new verb is added below with its reason, after the user agrees
// (docs/checks.md).

const actions = ["index", "show", "store", "update", "destroy"];
// Named verbs a resource's client may have, with their reason.
const clientVerbs: Record<string, Record<string, string>> = {
  workspaces: { leave: "DELETE …/membership: a member leaving" },
  invitations: {
    check: "POST /api/invitations/check: an emailed link's check",
    accept: "POST /api/invitations/accept: joining by the link",
  },
  emailBlocks: { check: "POST /api/email-blocks/check: a block link's check" },
};
// What typed paths may hold besides the actions: `matches` (an SWR key
// filter for a resource's lists) and listed singletons.
const pathExtras: Record<string, Record<string, string>> = {
  workspaces: { membership: "the singleton …/membership (leaving)" },
  invitations: {},
  emailBlocks: { check: "POST /api/email-blocks/check: a block link's check" },
};
// Resources that are named actions on the session and password, like
// /api/auth/* itself (docs/naming.md#controllers-internalhttpapi).
const namedActionResources: Record<string, string> = {
  get: "loads any typed path, for SWR",
  auth: "/api/auth/*: named actions on the account and its session",
  sessions: "/api/auth/sessions: signing out other sessions",
};
// The names in `methods` that aren't actions or listed for `resource`.
export function badClientNames(
  resource: string,
  methods: string[],
  extras: Record<string, Record<string, string>>,
  allowMatches = false,
): string[] {
  return methods
    .filter(
      (name) =>
        !actions.includes(name) &&
        !(extras[resource] && name in extras[resource]) &&
        !(allowMatches && name === "matches"),
    )
    .map(
      (name) =>
        `${resource}.${name}: the API client uses the API's actions (${actions.join(", ")}) or a listed verb; see docs/naming.md#web-app`,
    );
}
const ws = "wsp_2yEHYLwiwPVhLW";
const stubHttp: Http = {
  get: () => Promise.reject(new Error("not called")),
  send: () => Promise.reject(new Error("not called")),
  text: () => Promise.reject(new Error("not called")),
};
// A resource's methods: called with a workspace ID when it takes one.
function methodsOf(value: unknown): string[][] {
  if (typeof value === "function") {
    const forms = [value(ws)];
    if (value.length === 0 || value.name === "forWorkspace")
      forms.push(value());
    return forms.map((form) => Object.keys(form as object));
  }
  return [Object.keys(value as object)];
}
describe("API client names", () => {
  it("are the API's actions or listed verbs", () => {
    const api = client(stubHttp);
    const problems: string[] = [];
    for (const [resource, value] of Object.entries(api)) {
      if (resource in namedActionResources) continue;
      for (const methods of methodsOf(value))
        problems.push(...badClientNames(resource, methods, clientVerbs));
    }
    for (const [resource, value] of Object.entries(paths)) {
      if (resource in namedActionResources) continue;
      for (const methods of methodsOf(value))
        problems.push(...badClientNames(resource, methods, pathExtras, true));
    }
    expect(problems).toEqual([]);
    expect(Object.keys(api).length).toBeGreaterThan(5);
  });
  it("refuses other names", () => {
    expect(badClientNames("projects", ["index", "create"], {})).toEqual([
      "projects.create: the API client uses the API's actions (index, show, store, update, destroy) or a listed verb; see docs/naming.md#web-app",
    ]);
    expect(badClientNames("projects", ["list", "remove"], {})).toHaveLength(2);
    expect(badClientNames("projects", ["matches"], {})).toHaveLength(1);
    expect(badClientNames("projects", ["matches"], {}, true)).toEqual([]);
    expect(badClientNames("workspaces", ["leave"], clientVerbs)).toEqual([]);
    expect(badClientNames("projects", ["leave"], clientVerbs)).toHaveLength(1);
  });
});
