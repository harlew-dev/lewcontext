import { describe, expect, it } from "vitest";
import type { Project } from "@/types/projects";
import type { Page } from "@/lib/pagination";
import { stubApi } from "@/test/api";
import { client, paths, type ApiPath } from ".";
import { browserHttp } from "./http";
const stubFetch = (body: unknown = {}, status = 200) =>
  stubApi({ "*": { status, body } });
// "METHOD url" for each request sent.
const requests = (api: ReturnType<typeof stubFetch>) =>
  api.sent().map(({ method, url }) => `${method} ${url}`);
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
    expect(routes.store()).toBe(base);
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
    const stub = stubFetch({ id: prj }, 201);
    const project = { ...input, id: prj, createdAt: "", updatedAt: "" };
    await api("csrf").projects(ws).store(project);
    const [sent] = stub.sent();
    expect([sent.url, sent.method]).toEqual([base, "POST"]);
    expect(sent.csrf).toBe("csrf");
    expect(sent.body).toEqual(input);
  });
  it("updates, shows and lists", async () => {
    const stub = stubFetch();
    const projects = api("csrf").projects(ws);
    await projects.update(prj, input);
    await projects.show(prj);
    await projects.index({ status: "Completed" });
    expect(requests(stub)).toEqual([
      `PATCH ${base}/${prj}`,
      `GET ${base}/${prj}`,
      `GET ${base}?status=Completed`,
    ]);
  });
});
describe("workspaces()", () => {
  it("creates, renames, shows and lists, sending only the name", async () => {
    const stub = stubFetch({ id: ws });
    const workspaces = api("csrf").workspaces();
    await workspaces.store({ name: "Acme" });
    await workspaces.update(ws, { name: "Acme Inc" });
    await workspaces.show(ws);
    await workspaces.index({ page: 2 });
    expect(requests(stub)).toEqual([
      "POST /api/workspaces",
      `PATCH /api/workspaces/${ws}`,
      `GET /api/workspaces/${ws}`,
      "GET /api/workspaces?page=2",
    ]);
    expect(stub.sent()[1].body).toEqual({ name: "Acme Inc" });
    expect(stub.sent()[0].csrf).toBe("csrf");
  });
});
describe("auth() and sessions()", () => {
  it("signs in, signs out and revokes other sessions", async () => {
    const stub = stubFetch();
    await api().auth().login({ email: "a@b.test", password: "long password" });
    await api("csrf").auth().logout();
    await api("csrf").sessions().destroyOthers();
    expect(requests(stub)).toEqual([
      "POST /api/auth/login",
      "POST /api/auth/logout",
      "DELETE /api/auth/sessions",
    ]);
    expect(stub.sent()[2].csrf).toBe("csrf");
  });
});
