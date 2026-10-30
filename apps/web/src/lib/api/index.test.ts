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
describe("paths.projects", () => {
  const routes = paths.projects;
  it("builds index paths without defaults", () => {
    expect(routes.index()).toBe("/api/projects");
    expect(routes.index({ page: 1, sort: "-updated", q: "" })).toBe(
      "/api/projects",
    );
    expect(
      routes.index({
        page: 2,
        q: "50% & more",
        status: "In progress",
        sort: "name",
      }),
    ).toBe(
      "/api/projects?page=2&q=50%25+%26+more&status=In+progress&sort=name",
    );
    expect(routes.index({ perPage: 5 })).toBe("/api/projects?perPage=5");
  });
  it("builds record paths and recognises project keys", () => {
    expect(routes.show(42)).toBe("/api/projects/42");
    expect(routes.update(42)).toBe("/api/projects/42");
    expect(routes.create()).toBe("/api/projects");
    for (const key of [
      "/api/projects",
      "/api/projects?page=2",
      "/api/projects/4",
    ])
      expect(routes.matches(key)).toBe(true);
    for (const key of ["/api/projectsx", "/api/auth/me"])
      expect(routes.matches(key)).toBe(false);
  });
  it("types paths with their responses", () => {
    const page: ApiPath<Page<Project>> = routes.index();
    const project: ApiPath<Project> = routes.show(1);
    // @ts-expect-error a project path doesn't load a page
    const wrong: ApiPath<Page<Project>> = routes.show(1);
    expect([page, project, wrong]).toHaveLength(3);
  });
  it("accepts only the API's query parameters", () => {
    // @ts-expect-error not a status the API accepts
    routes.index({ status: "Archived" });
    // @ts-expect-error not a sort the API accepts
    routes.index({ sort: "user_id" });
    // @ts-expect-error the owner always comes from the session
    routes.index({ owner: 3 });
  });
});
describe("projects()", () => {
  it("creates with the session's CSRF token and only the editable fields", async () => {
    const fetch = stubFetch({ id: 9 }, 201);
    const project = { ...input, id: 9, createdAt: "", updatedAt: "" };
    await api("csrf").projects().create(project);
    const [url, options] = fetch.mock.calls[0];
    expect([url, options.method]).toEqual(["/api/projects", "POST"]);
    expect(options.headers.get("X-CSRF-Token")).toBe("csrf");
    expect(JSON.parse(options.body)).toEqual(input);
  });
  it("updates, shows and lists", async () => {
    const fetch = stubFetch();
    const projects = api("csrf").projects();
    await projects.update(4, input);
    await projects.show(4);
    await projects.index({ status: "Completed" });
    expect(
      fetch.mock.calls.map(([url, options]) => `${options.method} ${url}`),
    ).toEqual([
      "PATCH /api/projects/4",
      "GET /api/projects/4",
      "GET /api/projects?status=Completed",
    ]);
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
