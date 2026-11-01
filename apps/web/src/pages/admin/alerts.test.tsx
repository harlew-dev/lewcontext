import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createRoutesStub, Outlet } from "react-router";
import { SWRConfig } from "swr";
import { afterEach, describe, expect, it, vi } from "vitest";
import { adminLoader } from "@/lib/admin";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import type { AdminAlert, AdminSession } from "@/types/admin";
import AdminAlertIndex from "./[key]/alerts";
import AdminAlertShow from "./[key]/alerts/[alert]";
import AdminAdminIndex from "./[key]/admins";
import AdminMonitoringError from "./[key]/monitoring/errors/[error]";
import AdminLayoutRoute from "./layout";
// The Alerts pages and the housekeeping actions: the list by state, the
// sidebar's count, acknowledging, silencing (an action: a reason), the
// error group's Resolve and the admin's own alert email setting.
const key = "8a1f0c52-6d3e-4b7a-9c2d-5e8f1a3b7c90";
const admin = testSession({
  user: { id: "usr_00000000000009", name: "Olive Admin" },
});
function me(role: "viewer" | "operator", openAlerts = 2): AdminSession {
  return {
    user: admin.user,
    role,
    csrfToken: "admin-csrf",
    expiresAt: "",
    alertEmail: "critical",
    openAlerts,
  };
}
function alert(overrides: Partial<AdminAlert>): AdminAlert {
  return {
    id: "alt_00000000000001",
    signal: "admin.door_refusals",
    title: "Admin door refusals",
    unit: "count",
    page: "security",
    meaning: "Someone with the admin key was refused at the admin door.",
    level: "critical",
    openedAt: "2026-11-01T14:00:00Z",
    resolvedAt: null,
    lastValue: 3,
    peakValue: 4,
    critical: 1,
    forSeconds: 0,
    hourly: false,
    details: { drill: { name: "admin.login_attempted", window: "1d" } },
    notified: "mail",
    lastNotifiedAt: "2026-11-01T14:00:00Z",
    acknowledgedAt: null,
    acknowledgedBy: null,
    silencedUntil: null,
    silencedBy: null,
    ...overrides,
  };
}
function renderAdmin(path: string) {
  const Stub = createRoutesStub([
    {
      path: "/",
      Component: Outlet,
      children: [
        {
          Component: AdminLayoutRoute,
          loader: ({ params }) => adminLoader(params.key!),
          children: [
            { path: "admin/:key/alerts", Component: AdminAlertIndex },
            { path: "admin/:key/alerts/:alert", Component: AdminAlertShow },
            { path: "admin/:key/admins", Component: AdminAdminIndex },
            {
              path: "admin/:key/monitoring/errors/:error",
              Component: AdminMonitoringError,
            },
          ],
        },
      ],
    },
  ]);
  render(
    <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>
      <Stub initialEntries={[path]} />
    </SWRConfig>,
  );
}
afterEach(() => vi.restoreAllMocks());
describe("the Alerts list", () => {
  it("lists open alerts with their level in words, and counts them in the sidebar", async () => {
    stubApi({
      "GET /api/admin/me": { body: me("viewer") },
      "GET /api/admin/alerts": {
        body: {
          data: [
            alert({}),
            alert({
              id: "alt_00000000000002",
              signal: "http.latency_p95",
              title: "p95 latency",
              level: "warn",
              acknowledgedAt: "2026-11-01T14:10:00Z",
            }),
          ],
          next: null,
        },
      },
    });
    renderAdmin(`/admin/${key}/alerts`);
    const link = await screen.findByRole("link", {
      name: "Admin door refusals",
    });
    expect(link).toHaveAttribute(
      "href",
      `/admin/${key}/alerts/alt_00000000000001`,
    );
    expect(screen.getByText("Critical")).toBeVisible();
    expect(screen.getByText("Warning")).toBeVisible();
    expect(screen.getByText("Acknowledged")).toBeVisible();
    expect(
      screen.getByRole("link", { name: /Alerts\s*2\s*open/ }),
    ).toBeVisible();
  });
  it("says so when there are none", async () => {
    stubApi({
      "GET /api/admin/me": { body: me("viewer", 0) },
      "GET /api/admin/alerts": { body: { data: [], next: null } },
    });
    renderAdmin(`/admin/${key}/alerts`);
    expect(await screen.findByText("No open alerts")).toBeVisible();
  });
});
describe("one alert", () => {
  it("lets an operator acknowledge it, without a code or reason", async () => {
    const api = stubApi({
      "GET /api/admin/me": { body: me("operator") },
      "GET /api/admin/alerts/alt_00000000000001": [
        { body: alert({}) },
        { body: alert({ acknowledgedAt: "2026-11-01T14:20:00Z" }) },
      ],
      "PATCH /api/admin/alerts/alt_00000000000001": { body: alert({}) },
    });
    renderAdmin(`/admin/${key}/alerts/alt_00000000000001`);
    fireEvent.click(await screen.findByRole("button", { name: "Acknowledge" }));
    await waitFor(() =>
      expect(api.sent("PATCH")[0]).toMatchObject({
        url: "/api/admin/alerts/alt_00000000000001",
        body: { acknowledged: true },
        csrf: "admin-csrf",
      }),
    );
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Acknowledge" }),
      ).toBeDisabled(),
    );
  });
  it("hides every action from a viewer", async () => {
    stubApi({
      "GET /api/admin/me": { body: me("viewer") },
      "GET /api/admin/alerts/alt_00000000000001": { body: alert({}) },
    });
    renderAdmin(`/admin/${key}/alerts/alt_00000000000001`);
    await screen.findByText(/Someone with the admin key/);
    expect(screen.queryByRole("button", { name: "Acknowledge" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Silence" })).toBeNull();
  });
  it("silences a signal only with a reason", async () => {
    const api = stubApi({
      "GET /api/admin/me": { body: me("operator") },
      "GET /api/admin/alerts/alt_00000000000001": { body: alert({}) },
      "POST /api/admin/alerts/alt_00000000000001/silence": {
        status: 201,
        body: alert({}),
      },
    });
    renderAdmin(`/admin/${key}/alerts/alt_00000000000001`);
    fireEvent.click(await screen.findByRole("button", { name: "Silence" }));
    const reason = await screen.findByRole("textbox", { name: /Reason/ });
    const buttons = screen.getAllByRole("button", { name: "Silence" });
    const submit = buttons[buttons.length - 1]!;
    expect(submit).toBeDisabled();
    fireEvent.change(reason, {
      target: { value: "Planned maintenance on the admin host tonight" },
    });
    fireEvent.click(submit);
    await waitFor(() =>
      expect(api.sent("POST")[0]).toMatchObject({
        url: "/api/admin/alerts/alt_00000000000001/silence",
        body: {
          duration: "4h",
          reason: "Planned maintenance on the admin host tonight",
        },
      }),
    );
  });
});
describe("resolving an error group", () => {
  const group = {
    group: {
      id: "err_00000000000001",
      source: "request",
      route: "GET /api/x",
      message: "boom",
      count: 3,
      firstSeenAt: "2026-11-01T10:00:00Z",
      lastSeenAt: "2026-11-01T11:00:00Z",
      lastRequestId: "",
      lastVersion: "",
      resolvedAt: null,
      stack: "",
    },
    release: null,
    window: {
      name: "24h",
      from: "2026-10-31T14:30:00Z",
      to: "2026-11-01T14:47:00Z",
      bucketSeconds: 1800,
    },
    occurrences: { series: [], buckets: [] },
  };
  it("is an operator's button, sent without a code", async () => {
    const api = stubApi({
      "GET /api/admin/me": { body: me("operator") },
      "GET /api/admin/errors/err_00000000000001": [
        { body: group },
        {
          body: {
            ...group,
            group: { ...group.group, resolvedAt: "2026-11-01T14:50:00Z" },
          },
        },
      ],
      "PATCH /api/admin/errors/err_00000000000001": { status: 204 },
    });
    renderAdmin(`/admin/${key}/monitoring/errors/err_00000000000001`);
    fireEvent.click(await screen.findByRole("button", { name: "Resolve" }));
    await waitFor(() =>
      expect(api.sent("PATCH")[0]).toMatchObject({ body: { resolved: true } }),
    );
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Resolve" })).toBeNull(),
    );
  });
  it("isn't offered to a viewer", async () => {
    stubApi({
      "GET /api/admin/me": { body: me("viewer") },
      "GET /api/admin/errors/err_00000000000001": { body: group },
    });
    renderAdmin(`/admin/${key}/monitoring/errors/err_00000000000001`);
    await screen.findByText("boom");
    expect(screen.queryByRole("button", { name: "Resolve" })).toBeNull();
  });
});
describe("alert emails on the Admins page", () => {
  it("changes only the admin's own setting", async () => {
    const api = stubApi({
      "GET /api/admin/me": { body: me("viewer") },
      "GET /api/admin/admins": {
        body: {
          data: [
            {
              user: {
                id: admin.user.id,
                name: "Olive Admin",
                email: "o@example.com",
              },
              role: "viewer",
              mfaEnabled: true,
              mfaSetupUntil: null,
              alertEmail: "critical",
              grantedAt: "2026-01-01T00:00:00Z",
              lastSignedInAt: "2026-11-01T00:00:00Z",
              sessions: [],
            },
          ],
        },
      },
      "PATCH /api/admin/me": { status: 204 },
    });
    renderAdmin(`/admin/${key}/admins`);
    fireEvent.click(await screen.findByRole("combobox", { name: /Email me/ }));
    fireEvent.click(await screen.findByRole("option", { name: "None" }));
    await waitFor(() =>
      expect(api.sent("PATCH")[0]).toMatchObject({
        url: "/api/admin/me",
        body: { alertEmail: "none" },
      }),
    );
  });
});
