import { describe, expect, it } from "vitest";
import { stubApi } from "@/test/api";
import { adminClient, adminHttp, adminPaths } from "./admin";
// The admin dashboard's client, apart from lib/api/index.ts (only the admin
// build imports it).
const stubFetch = (body: unknown = {}, status = 200) =>
  stubApi({ "*": { status, body } });
describe("the admin client", () => {
  const key = "8a1f0c52-6d3e-4b7a-9c2d-5e8f1a3b7c90";
  it("builds list paths without defaults, and never puts the key in one", () => {
    expect(adminPaths.users.index()).toBe("/api/admin/users");
    expect(adminPaths.users.index({ page: 1, sort: "-created", q: "" })).toBe(
      "/api/admin/users",
    );
    expect(adminPaths.users.index({ page: 2, q: "ada@", sort: "email" })).toBe(
      "/api/admin/users?page=2&q=ada%40&sort=email",
    );
    expect(adminPaths.workspaces.index({ sort: "-name" })).toBe(
      "/api/admin/workspaces?sort=-name",
    );
    expect(adminPaths.users.show("usr_2yEHYLwiwPVhLW")).toBe(
      "/api/admin/users/usr_2yEHYLwiwPVhLW",
    );
    // @ts-expect-error workspaces don't sort by email
    adminPaths.workspaces.index({ sort: "email" });
    // @ts-expect-error not a sort the API accepts
    adminPaths.users.index({ sort: "password" });
  });
  it("builds the Events, Mail and projects paths, leaving out defaults", () => {
    expect(adminPaths.events.index()).toBe("/api/admin/events");
    expect(
      adminPaths.events.index({
        name: "workspace.member.",
        email: "ada@example.com",
        ip: "10.0.0.0/16",
        adminReads: "include",
        before: "evt_2yEHYLwiwPVhLW",
      }),
    ).toBe(
      "/api/admin/events?name=workspace.member.&email=ada%40example.com&ip=10.0.0.0%2F16&adminReads=include&before=evt_2yEHYLwiwPVhLW",
    );
    expect(adminPaths.events.show("evt_2yEHYLwiwPVhLW")).toBe(
      "/api/admin/events/evt_2yEHYLwiwPVhLW",
    );
    expect(adminPaths.catalogue()).toBe("/api/admin/catalogue");
    expect(adminPaths.mail()).toBe("/api/admin/mail");
    expect(adminPaths.mail("24h")).toBe("/api/admin/mail");
    expect(adminPaths.mail("7d")).toBe("/api/admin/mail?window=7d");
    expect(
      adminPaths.deliveries.index({
        status: "failed",
        before: "evt_2yEHYLwiwPVhLW:mail.password_reset",
      }),
    ).toBe(
      "/api/admin/deliveries?status=failed&before=evt_2yEHYLwiwPVhLW%3Amail.password_reset",
    );
    expect(
      adminPaths
        .projects("wsp_2yEHYLwiwPVhLW")
        .index({ page: 2, sort: "-updated", status: "Planned" }),
    ).toBe(
      "/api/admin/workspaces/wsp_2yEHYLwiwPVhLW/projects?page=2&status=Planned",
    );
    // @ts-expect-error the summary has no 1-hour window
    adminPaths.mail("1h");
    // @ts-expect-error not a delivery status
    adminPaths.deliveries.index({ status: "bounced" });
    // @ts-expect-error admin reads are shown with "include" only
    adminPaths.events.index({ adminReads: "yes" });
  });
  it("sends the key in X-Admin-Key, the codes in bodies, and the admin CSRF token", async () => {
    const stub = stubFetch({ mfaRequired: true });
    const admin = adminClient(adminHttp(key, "admin-csrf"));
    await admin.login({ email: "olive@example.com", password: "pw" });
    await admin.loginMfa("123456");
    await admin.users().index({ q: "x" });
    await admin.logout();
    expect(stub.sent().map(({ method, url }) => `${method} ${url}`)).toEqual([
      "POST /api/admin/login",
      "POST /api/admin/login/mfa",
      "GET /api/admin/users?q=x",
      "POST /api/admin/logout",
    ]);
    for (const request of stub.sent()) {
      expect(request.adminKey).toBe(key);
      expect(request.csrf).toBe("admin-csrf");
    }
    expect(stub.sent()[1].body).toEqual({ code: "123456" });
  });
});
