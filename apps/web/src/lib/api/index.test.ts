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
  it("filters to the workspaces you can't leave", () => {
    expect(paths.workspaces.index({ canLeave: false })).toBe(
      "/api/workspaces?canLeave=false",
    );
    // @ts-expect-error false is the only value the API accepts
    paths.workspaces.index({ canLeave: true });
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
  it("updates, shows, lists and deletes", async () => {
    const stub = stubFetch();
    const projects = api("csrf").projects(ws);
    await projects.update(prj, input);
    await projects.show(prj);
    await projects.index({ status: "Completed" });
    await projects.destroy(prj);
    expect(requests(stub)).toEqual([
      `PATCH ${base}/${prj}`,
      `GET ${base}/${prj}`,
      `GET ${base}?status=Completed`,
      `DELETE ${base}/${prj}`,
    ]);
    expect(stub.sent()[3].body).toBeUndefined();
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
  it("deletes with the typed name and the password", async () => {
    const stub = stubApi({ "*": { status: 204 } });
    await api("csrf")
      .workspaces()
      .destroy(ws, { name: "Acme", password: "secret" });
    expect(stub.sent()).toEqual([
      {
        method: "DELETE",
        url: `/api/workspaces/${ws}`,
        body: { name: "Acme", password: "secret" },
        csrf: "csrf",
      },
    ]);
  });
});
describe("auth() and sessions()", () => {
  it("deletes the account with the password", async () => {
    const stub = stubApi({ "*": { status: 204 } });
    await api("csrf").auth().destroy({ password: "secret" });
    expect(requests(stub)).toEqual(["DELETE /api/auth/me"]);
    expect(stub.sent()[0].body).toEqual({ password: "secret" });
  });
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
  it("sets up, turns off and renews two-factor authentication, codes only in bodies", async () => {
    const stub = stubFetch();
    const confirmation = { password: "secret", code: "123456" };
    await api().auth().loginMfa("123456");
    await api("csrf").auth().mfa().setup("secret");
    await api("csrf").auth().mfa().store("123456");
    await api("csrf").auth().mfa().recoveryCodes().store(confirmation);
    await api("csrf").auth().mfa().destroy(confirmation);
    expect(requests(stub)).toEqual([
      "POST /api/auth/login/mfa",
      "POST /api/auth/mfa/setup",
      "POST /api/auth/mfa",
      "POST /api/auth/mfa/recovery-codes",
      "DELETE /api/auth/mfa",
    ]);
    expect(stub.sent().map((r) => r.body)).toEqual([
      { code: "123456" },
      { password: "secret" },
      { code: "123456" },
      confirmation,
      confirmation,
    ]);
    expect(stub.sent()[0].csrf).toBeNull();
    expect(stub.sent()[4].csrf).toBe("csrf");
  });
  it("verifies an email with a code, the code only in bodies", async () => {
    const stub = stubFetch();
    await api().auth().registerCheck();
    await api().auth().registerResend();
    await api().auth().registerVerify("482913");
    await api("csrf").auth().emailVerification().store();
    await api("csrf").auth().emailVerification().update("482913");
    expect(requests(stub)).toEqual([
      "POST /api/auth/register/check",
      "POST /api/auth/register/resend",
      "POST /api/auth/register/verify",
      "POST /api/auth/email-verification",
      "PATCH /api/auth/email-verification",
    ]);
    expect(stub.sent().map((r) => r.body)).toEqual([
      {},
      {},
      { code: "482913" },
      {},
      { code: "482913" },
    ]);
    // A sign-up has no session yet; an account's verification has one.
    expect(stub.sent()[2].csrf).toBeNull();
    expect(stub.sent()[4].csrf).toBe("csrf");
  });
});
describe("invitations", () => {
  const base = `/api/workspaces/${ws}/invitations`;
  it("builds a workspace's paths", () => {
    const routes = paths.invitations(ws);
    expect(routes.index()).toBe(base);
    expect(routes.index({ perPage: 100 })).toBe(`${base}?perPage=100`);
    expect(routes.matches(`${base}?perPage=100`)).toBe(true);
    expect(routes.matches(`/api/workspaces/${ws}/members`)).toBe(false);
  });
  it("sends only the email and role, and the link's token only in bodies", async () => {
    const stub = stubFetch();
    const token = "t".repeat(43);
    await api("csrf")
      .invitations(ws)
      .store({ email: "bob@example.com", role: "member" });
    await api("csrf").invitations(ws).destroy("inv_00000000000001");
    await api().invitations().check(token);
    await api("csrf").invitations().accept(token);
    expect(requests(stub)).toEqual([
      `POST ${base}`,
      `DELETE ${base}/inv_00000000000001`,
      "POST /api/invitations/check",
      "POST /api/invitations/accept",
    ]);
    expect(stub.sent().map((s) => s.body)).toEqual([
      { email: "bob@example.com", role: "member" },
      undefined,
      { token },
      { token },
    ]);
    expect(stub.sent().every((s) => !s.url.includes(token))).toBe(true);
  });
});
describe("the email blocks client", () => {
  it("checks, blocks and unblocks with the link's token only in bodies", async () => {
    const stub = stubFetch();
    const token = "t".repeat(43);
    await api().emailBlocks().check(token);
    await api().emailBlocks().store(token);
    await api().emailBlocks().destroy(token);
    expect(requests(stub)).toEqual([
      "POST /api/email-blocks/check",
      "POST /api/email-blocks",
      "DELETE /api/email-blocks",
    ]);
    expect(stub.sent().map((s) => s.body)).toEqual([
      { token },
      { token },
      { token },
    ]);
    expect(paths.emailBlocks().check()).toBe("/api/email-blocks/check");
  });
});
describe("the client errors client", () => {
  it("sends only the message, stack, route and release, with the CSRF token", async () => {
    const stub = stubApi({ "*": { status: 202 } });
    const report = {
      message: "TypeError: x",
      stack: "at f (a.js:1:2)",
      route: "/app/:workspace/projects",
      release: "",
    };
    await api("csrf-7")
      .clientErrors()
      .store({ ...report, ...{ url: "https://example.com/?q=ada" } });
    expect(requests(stub)).toEqual(["POST /api/client-errors"]);
    expect(stub.sent()[0]).toMatchObject({ body: report, csrf: "csrf-7" });
    expect(paths.clientErrors().store()).toBe("/api/client-errors");
  });
});
