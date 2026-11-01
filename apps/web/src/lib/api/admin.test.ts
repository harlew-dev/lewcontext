import { describe, expect, it } from "vitest";
import { stubApi } from "@/test/api";
import { adminClient, adminHttp, adminPaths, needsStepUp } from "./admin";
import { ApiError } from "./http";
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
  it("builds the Email blocks paths, and sends an address only in a body", async () => {
    expect(adminPaths.emailBlocks.index()).toBe("/api/admin/email-blocks");
    expect(
      adminPaths.emailBlocks.index({ q: "ada@", sort: "-created", page: 1 }),
    ).toBe("/api/admin/email-blocks?q=ada%40");
    expect(adminPaths.emailBlocks.index({ sort: "email", page: 3 })).toBe(
      "/api/admin/email-blocks?page=3&sort=email",
    );
    // @ts-expect-error blocks don't sort by name
    adminPaths.emailBlocks.index({ sort: "name" });
    const api = stubApi({ "*": { status: 204 } });
    const client = adminClient(adminHttp(key, "admin-csrf"));
    await client
      .emailBlocks()
      .destroy({ email: "ada@example.com", reason: "A reason here." });
    await client.stepUp("123 456");
    expect(api.sent()).toEqual([
      expect.objectContaining({
        method: "DELETE",
        url: "/api/admin/email-blocks",
        body: { email: "ada@example.com", reason: "A reason here." },
        csrf: "admin-csrf",
        adminKey: key,
      }),
      expect.objectContaining({
        method: "POST",
        url: "/api/admin/step-up",
        body: { code: "123 456" },
        csrf: "admin-csrf",
        adminKey: key,
      }),
    ]);
  });
  it("tells a refusal for want of a fresh code from any other 403", () => {
    expect(
      needsStepUp(
        new ApiError("Enter a code to continue.", 403, undefined, {
          stepUp: true,
        }),
      ),
    ).toBe(true);
    expect(
      needsStepUp(new ApiError("Your admin role doesn't allow this.", 403)),
    ).toBe(false);
    expect(
      needsStepUp(new ApiError("Nope", 422, undefined, { stepUp: true })),
    ).toBe(false);
    expect(needsStepUp(new Error("Enter a code to continue."))).toBe(false);
  });
  it("builds the Events, Mail and projects paths, leaving out defaults", () => {
    expect(adminPaths.events.index()).toBe("/api/admin/events");
    // The default window is left out; names are one comma-separated list.
    expect(
      adminPaths.events.index({
        window: "24h",
        names: ["auth.*", "workspace.invitation.created"],
        outcome: "failed",
        email: "ada@example.com",
        ip: "10.0.0.0/16",
        summary: true,
      }),
    ).toBe(
      "/api/admin/events?name=auth.*,workspace.invitation.created&outcome=failed&email=ada%40example.com&ip=10.0.0.0%2F16&summary=include",
    );
    expect(
      adminPaths.events.index({
        window: "7d",
        names: [],
        before: "evt_2yEHYLwiwPVhLW",
        summary: false,
      }),
    ).toBe("/api/admin/events?window=7d&before=evt_2yEHYLwiwPVhLW");
    expect(
      adminPaths.events.index({
        from: "2026-10-20T00:00:00Z",
        to: "2026-10-24T00:00:00Z",
      }),
    ).toBe(
      "/api/admin/events?from=2026-10-20T00%3A00%3A00Z&to=2026-10-24T00%3A00%3A00Z",
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
    // @ts-expect-error admin reads are a group of names, not a toggle
    adminPaths.events.index({ adminReads: "include" });
    // @ts-expect-error not a window the API resolves
    adminPaths.events.index({ window: "30d" });
  });
  it("sends the key in X-Admin-Key, the codes in bodies, and the admin CSRF token", async () => {
    const stub = stubFetch({ mfaRequired: true });
    const admin = adminClient(adminHttp(key, "admin-csrf"));
    await admin.login({ email: "olive@example.com", password: "pw" });
    await admin.loginMfa("123456");
    await admin.users().index({ q: "x" });
    await admin.admins().index();
    await admin.sessions().destroyOthers();
    await admin.logout();
    expect(stub.sent().map(({ method, url }) => `${method} ${url}`)).toEqual([
      "POST /api/admin/login",
      "POST /api/admin/login/mfa",
      "GET /api/admin/users?q=x",
      "GET /api/admin/admins",
      "DELETE /api/admin/sessions",
      "POST /api/admin/logout",
    ]);
    for (const request of stub.sent()) {
      expect(request.adminKey).toBe(key);
      expect(request.csrf).toBe("admin-csrf");
    }
    expect(stub.sent()[1].body).toEqual({ code: "123456" });
  });
  it("calls back when an answer says the key is being retired, answered or refused", async () => {
    stubApi({
      "GET /api/admin/me": {
        body: {},
        headers: { "X-Admin-Key-Status": "previous" },
      },
      "GET /api/admin/admins": {
        status: 401,
        body: { error: "Please sign in to continue." },
        headers: { "X-Admin-Key-Status": "previous" },
      },
      "*": { body: {} },
    });
    let retiring = 0;
    const admin = adminClient(adminHttp(key, undefined, () => retiring++));
    await admin.users().index();
    expect(retiring).toBe(0);
    await admin.me();
    await admin
      .admins()
      .index()
      .catch(() => {});
    expect(retiring).toBe(2);
  });
});
