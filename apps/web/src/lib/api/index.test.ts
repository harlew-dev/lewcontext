import { afterEach, describe, expect, it, vi } from "vitest";
import type { Project } from "@/types/projects";
import type { Page } from "@/lib/pagination";
import { client, paths, type ApiPath } from ".";
import { browserHttp } from "./http";
afterEach(() => vi.unstubAllGlobals());
function stubFetch(body: unknown = {}, status = 200) {
  // A fresh Response per call: a body can only be read once.
  const fetch = vi
    .fn()
    .mockImplementation(
      async () => new Response(JSON.stringify(body), { status }),
    );
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
const input = {
  name: "Launch",
  description: "",
  status: "Planned",
  color: "violet",
} as const;
// The browser client useApi() builds, with or without a session's token.
function api(csrfToken?: string) {
  return client(browserHttp(csrfToken));
}
const ws = "wsp_2yEHYLwiwPVhLW";
const prj = "prj_j292s2S2j7Qx4B";
const base = `/api/workspaces/${ws}/projects`;
describe("paths.projects(workspace)", () => {
  const routes = paths.projects(ws);
  it("builds index paths without defaults", () => {
    expect(routes.index()).toBe(base);
    expect(routes.index({ page: 1, sort: "-updated", q: "" })).toBe(base);
    expect(
      routes.index({
        page: 2,
        q: "50% & more",
        status: "In progress",
        sort: "name",
      }),
    ).toBe(`${base}?page=2&q=50%25+%26+more&status=In+progress&sort=name`);
    expect(routes.index({ perPage: 5 })).toBe(`${base}?perPage=5`);
  });
  it("builds record paths and recognises only this workspace's keys", () => {
    expect(routes.show(prj)).toBe(`${base}/${prj}`);
    expect(routes.update(prj)).toBe(`${base}/${prj}`);
    expect(routes.create()).toBe(base);
    for (const key of [base, `${base}?page=2`, `${base}/${prj}`])
      expect(routes.matches(key)).toBe(true);
    const other = paths.projects("wsp_00000000000000").index();
    for (const key of [`${base}x`, "/api/auth/me", other])
      expect(routes.matches(key)).toBe(false);
  });
  it("types paths with their responses", () => {
    const page: ApiPath<Page<Project>> = routes.index();
    const project: ApiPath<Project> = routes.show(prj);
    // @ts-expect-error a project path doesn't load a page
    const wrong: ApiPath<Page<Project>> = routes.show(prj);
    expect([page, project, wrong]).toHaveLength(3);
  });
  it("accepts only the API's query parameters", () => {
    // @ts-expect-error not a status the API accepts
    routes.index({ status: "Archived" });
    // @ts-expect-error not a sort the API accepts
    routes.index({ sort: "user_id" });
    // @ts-expect-error the workspace always comes from the path
    routes.index({ workspace: 3 });
  });
});
describe("paths.workspaces", () => {
  it("pages the list, leaving out page 1", () => {
    expect(paths.workspaces.index()).toBe("/api/workspaces");
    expect(paths.workspaces.index({ page: 1 })).toBe("/api/workspaces");
    expect(paths.workspaces.index({ page: 3, perPage: 1 })).toBe(
      "/api/workspaces?page=3&perPage=1",
    );
    expect(paths.workspaces.show(ws)).toBe(`/api/workspaces/${ws}`);
    // @ts-expect-error the list has no search
    paths.workspaces.index({ q: "Acme" });
  });
});
describe("projects(workspace)", () => {
  it("creates with the session's CSRF token and only the editable fields", async () => {
    const fetch = stubFetch({ id: prj }, 201);
    const project = { ...input, id: prj, createdAt: "", updatedAt: "" };
    await api("csrf").projects(ws).create(project);
    const [url, options] = fetch.mock.calls[0];
    expect([url, options.method]).toEqual([base, "POST"]);
    expect(options.headers.get("X-CSRF-Token")).toBe("csrf");
    expect(JSON.parse(options.body)).toEqual(input);
  });
  it("updates, shows and lists", async () => {
    const fetch = stubFetch();
    const projects = api("csrf").projects(ws);
    await projects.update(prj, input);
    await projects.show(prj);
    await projects.index({ status: "Completed" });
    expect(
      fetch.mock.calls.map(([url, options]) => `${options.method} ${url}`),
    ).toEqual([
      `PATCH ${base}/${prj}`,
      `GET ${base}/${prj}`,
      `GET ${base}?status=Completed`,
    ]);
  });
});
describe("workspaces()", () => {
  it("creates, renames, shows and lists, sending only the name", async () => {
    const fetch = stubFetch({ id: ws });
    const workspaces = api("csrf").workspaces();
    await workspaces.create({ name: "Acme" });
    await workspaces.update(ws, { name: "Acme Inc" });
    await workspaces.show(ws);
    await workspaces.index({ page: 2 });
    expect(
      fetch.mock.calls.map(([url, options]) => `${options.method} ${url}`),
    ).toEqual([
      "POST /api/workspaces",
      `PATCH /api/workspaces/${ws}`,
      `GET /api/workspaces/${ws}`,
      "GET /api/workspaces?page=2",
    ]);
    expect(JSON.parse(fetch.mock.calls[1][1].body)).toEqual({
      name: "Acme Inc",
    });
    expect(fetch.mock.calls[0][1].headers.get("X-CSRF-Token")).toBe("csrf");
  });
});
describe("auth() and sessions()", () => {
  it("signs in, signs out and revokes other sessions", async () => {
    const fetch = stubFetch();
    await api().auth().login({ email: "a@b.test", password: "long password" });
    await api("csrf").auth().logout();
    await api("csrf").sessions().destroyOthers();
    expect(
      fetch.mock.calls.map(([url, options]) => `${options.method} ${url}`),
    ).toEqual([
      "POST /api/auth/login",
      "POST /api/auth/logout",
      "DELETE /api/auth/sessions",
    ]);
    expect(fetch.mock.calls[2][1].headers.get("X-CSRF-Token")).toBe("csrf");
  });
});
