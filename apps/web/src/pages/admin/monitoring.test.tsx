import { fireEvent, render, screen, within } from "@testing-library/react";
import { createRoutesStub, Outlet, useLocation } from "react-router";
import { SWRConfig } from "swr";
import { describe, expect, it } from "vitest";
import { adminLoader } from "@/lib/admin";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import type {
  LimitsSections,
  MonitoringView,
  OverviewSections,
  SecuritySections,
  Signal,
} from "@/types/admin";
import AdminMonitoringOverview from "./[key]/monitoring";
import AdminMonitoringLimits from "./[key]/monitoring/limits";
import AdminMonitoringSecurity from "./[key]/monitoring/security";
import AdminLayoutRoute from "./layout";
const key = "8a1f0c52-6d3e-4b7a-9c2d-5e8f1a3b7c90";
const admin = testSession({
  user: { id: "usr_00000000000009", name: "Olive Admin" },
});
const me = {
  user: admin.user,
  role: "viewer",
  csrfToken: "admin-csrf",
  expiresAt: "",
};
const window = {
  name: "24h" as const,
  from: "2026-10-31T14:30:00Z",
  to: "2026-11-01T14:47:00Z",
  bucketSeconds: 1800,
};
const buckets = (n: number[][]) =>
  n.map((counts, i) => ({
    start: new Date(Date.UTC(2026, 10, 1, 13, i * 30)).toISOString(),
    counts,
  }));
function signal(overrides: Partial<Signal>): Signal {
  return {
    name: "auth.failed_sign_ins",
    label: "Failed sign-ins",
    unit: "count",
    value: 0,
    sample: 0,
    level: "ok",
    windowSeconds: 3600,
    ...overrides,
  };
}
const overview: MonitoringView<OverviewSections> = {
  page: "overview",
  window,
  unavailable: [],
  signals: [
    signal({
      value: 1204,
      level: "critical",
      warn: 100,
      critical: 1000,
      drill: { name: "auth.login_attempted", outcome: "failed", window: "1h" },
    }),
    signal({
      name: "http.latency_p95",
      label: "Latency, 95th percentile",
      unit: "ms",
      value: 1750,
      level: "warn",
      warn: 1000,
      critical: 3000,
    }),
    signal({ name: "servers.live", label: "Live servers", value: 2 }),
    signal({
      name: "servers.versions",
      label: "Versions live",
      value: 2,
      level: "warn",
      warn: 2,
    }),
  ],
  sections: {
    tiles: [
      {
        id: "failed_sign_ins",
        label: "Failed sign-ins",
        page: "security",
        signals: ["auth.failed_sign_ins"],
        series: {
          label: "Failed sign-ins",
          unit: "count",
          points: [
            { start: "2026-11-01T13:00:00Z", value: 4 },
            { start: "2026-11-01T13:30:00Z", value: 1200 },
          ],
        },
      },
      {
        id: "latency",
        label: "Latency",
        page: "performance",
        signals: ["http.latency_p95"],
        series: null,
      },
      {
        id: "servers",
        label: "Servers",
        page: "configuration",
        signals: ["servers.live", "servers.versions"],
        series: null,
      },
    ],
    metricsStore: "off",
  },
};
const emptySeries = { series: [], buckets: [] };
const flow = { series: emptySeries, topAddresses: [], limit: null };
const security: MonitoringView<SecuritySections> = {
  page: "security",
  window,
  unavailable: [],
  signals: [
    signal({
      name: "auth.stuffing_successes",
      label: "…of them signing someone in",
      value: 1,
      level: "critical",
      critical: 1,
      drill: { name: "auth.login_attempted", window: "1h" },
    }),
  ],
  sections: {
    signIns: {
      series: [
        "succeeded",
        "mfa_required",
        "wrong_password",
        "unknown_account",
      ],
      buckets: buckets([
        [5, 1, 2, 0],
        [3, 0, 40, 30],
      ]),
    },
    stuffing: [
      {
        period: "2026-11-01T14:00:00Z",
        ip: "203.0.113.66",
        emails: 40,
        attempts: 41,
        successes: 1,
      },
    ],
    guessing: [],
    unknown: { attempts: 81, unknown: 30 },
    successAfterFailures: [],
    takeovers: [
      {
        changedAt: "2026-11-01T14:20:00Z",
        change: "auth.password_changed",
        event: "evt_00000000000001",
        account: { id: "usr_00000000000002", name: "Taylor Reed" },
        signedInAt: "2026-11-01T14:05:00Z",
        network: "203.0.113.0/24",
      },
    ],
    secondFactor: {
      failures: [],
      backupCodes: { used: 2, lowAccounts: 1 },
    },
    reauthentication: [],
    resets: flow,
    signUps: {
      ...flow,
      limit: {
        name: "register:global",
        max: 1000,
        windowSeconds: 3600,
        hits: 1000,
        resetsAt: "2026-11-01T15:10:00Z",
        reached: 1,
        lastReached: "2026-11-01T14:17:00Z",
      },
    },
    adminDoor: {
      counts: [{ outcome: "not_admin", count: 1 }],
      series: {
        series: ["failed", "refused", "moved"],
        buckets: buckets([
          [0, 0, 0],
          [2, 1, 0],
        ]),
      },
      recent: [],
    },
    sessions: { series: emptySeries, live: 12, accountsAtCap: 0 },
  },
};
const limits: MonitoringView<LimitsSections> = {
  page: "limits",
  window,
  unavailable: [],
  signals: [
    signal({
      name: "limits.shared_reached",
      label: "Shared limits reached",
      value: 0,
    }),
  ],
  sections: {
    reached: { series: ["auth:per-ip"], buckets: buckets([[2], [1]]) },
    byLimit: [
      { limit: "auth:per-ip", windows: 3, lastReached: "2026-11-01T14:00:00Z" },
    ],
    refused: [{ label: "auth:per-ip:block", count: 412 }],
    shared: [],
    blocked: [
      { name: "auth:per-ip", max: 60, subjects: 2 },
      { name: "api-read:per-user", max: 1000, subjects: null },
      { name: "project:per-user", max: 100, subjects: 0 },
    ],
    topAccounts: [],
    topAddresses: [],
    budgets: [],
    quotas: [],
    routes: [],
  },
};
function Location() {
  const { pathname, search } = useLocation();
  return <output data-testid="location">{pathname + search}</output>;
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
            {
              path: "admin/:key/monitoring",
              Component: () => (
                <>
                  <AdminMonitoringOverview />
                  <Location />
                </>
              ),
            },
            {
              path: "admin/:key/monitoring/security",
              Component: () => (
                <>
                  <AdminMonitoringSecurity />
                  <Location />
                </>
              ),
            },
            {
              path: "admin/:key/monitoring/limits",
              Component: AdminMonitoringLimits,
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
describe("the Monitoring Overview", () => {
  it("shows each tile with its level in words, its signals and links", async () => {
    const api = stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/monitoring/overview": { body: overview },
    });
    renderAdmin(`/admin/${key}/monitoring`);
    const failed = await screen.findByRole("link", { name: "Failed sign-ins" });
    expect(failed).toHaveAttribute("href", `/admin/${key}/monitoring/security`);
    expect(screen.getByText("1,204")).toBeInTheDocument();
    expect(screen.getByText("Critical")).toBeInTheDocument();
    // Never colour alone: every level not OK is written out.
    expect(screen.getAllByText("Warning")).toHaveLength(3);
    expect(screen.getByText("1.8 s")).toBeInTheDocument();
    // Every tile's page is built: each links to it.
    expect(screen.getByRole("link", { name: "Latency" })).toHaveAttribute(
      "href",
      `/admin/${key}/monitoring/performance`,
    );
    expect(screen.getByRole("link", { name: "Servers" })).toHaveAttribute(
      "href",
      `/admin/${key}/monitoring/configuration`,
    );
    expect(screen.queryByText("Its page comes later.")).toBeNull();
    // Metric storage off: the latency tile says so, not the others.
    expect(screen.getAllByText("Metric storage is off.")).toHaveLength(1);
    // The sparkline's numbers for screen readers.
    const table = screen.getByRole("table", {
      name: "Failed sign-ins over the window",
    });
    expect(within(table).getByText("1,200")).toBeInTheDocument();
    // The sidebar's Monitoring heading, Overview current.
    const nav = screen.getByRole("heading", { name: "Monitoring" });
    expect(nav).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Overview" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(api.sent("GET").map((s) => s.url)).toContain(
      "/api/admin/monitoring/overview",
    );
  });
  it("says when the Integrity tile read only some checks in time", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/monitoring/overview": {
        body: {
          ...overview,
          signals: [
            signal({
              name: "integrity.critical_checks",
              label: "Critical checks failing",
              value: 0,
              sample: 2,
              critical: 1,
              partial: true,
            }),
            signal({
              name: "integrity.warning_checks",
              label: "Warning checks failing",
              value: 1,
              sample: 5,
              level: "warn",
              warn: 1,
            }),
          ],
          sections: {
            ...overview.sections,
            tiles: [
              {
                id: "integrity",
                label: "Integrity",
                page: "integrity",
                signals: [
                  "integrity.critical_checks",
                  "integrity.warning_checks",
                ],
                series: null,
              },
            ],
          },
        },
      },
    });
    renderAdmin(`/admin/${key}/monitoring`);
    expect(
      await screen.findByText(
        "Critical checks failing: partial, only 2 checks were read in time.",
      ),
    ).toBeInTheDocument();
    // Only the partial one says so.
    expect(screen.queryByText(/Warning checks failing: partial/)).toBeNull();
  });
  it("keeps what it showed when a reload fails, with Retry", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/monitoring/overview": { body: overview },
      "GET /api/admin/monitoring/overview?window=7d": {
        status: 503,
        body: { error: "Monitoring is busy. Try again in a moment." },
      },
    });
    renderAdmin(`/admin/${key}/monitoring`);
    await screen.findByText("1,204");
    fireEvent.click(screen.getByRole("combobox", { name: /Time range/ }));
    fireEvent.click(await screen.findByRole("option", { name: "Last 7 days" }));
    expect(
      await screen.findByText("Monitoring is busy. Try again in a moment."),
    ).toBeInTheDocument();
    expect(screen.getByText("1,204")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Try again" })).toBeVisible();
    expect(screen.getByTestId("location")).toHaveTextContent(
      `/admin/${key}/monitoring?window=7d`,
    );
  });
});
describe("the Security page", () => {
  it("lists the stuffing address with its Events, and links each signal to its rows", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/monitoring/security": { body: security },
    });
    renderAdmin(`/admin/${key}/monitoring/security`);
    const ip = await screen.findByRole("link", { name: "203.0.113.66" });
    expect(ip.getAttribute("href")).toContain("ip=203.0.113.66");
    expect(ip.getAttribute("href")).toContain("name=auth.login_attempted");
    expect(
      screen.getByRole("link", { name: "…of them signing someone in" }),
    ).toHaveAttribute(
      "href",
      `/admin/${key}/events?window=1h&name=auth.login_attempted`,
    );
    expect(screen.getByRole("link", { name: "Taylor Reed" })).toHaveAttribute(
      "href",
      `/admin/${key}/users/usr_00000000000002`,
    );
    expect(screen.getByText(/30 of 81 \(37%\) named an email/)).toBeVisible();
    // The shared limit, reached: a meter with its word.
    const meter = screen.getByRole("meter", { name: "register:global used" });
    expect(meter).toHaveAttribute(
      "aria-valuetext",
      expect.stringContaining("1,000 of 1,000"),
    );
    expect(screen.getAllByText("Critical").length).toBeGreaterThanOrEqual(2);
    // The admin door's chart: three series, named.
    const door = screen.getByRole("table", { name: "Admin door" });
    expect(within(door).getByText("Refusals")).toBeInTheDocument();
  });
  it("shows a chart's numbers as a table on request", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/monitoring/security": { body: security },
    });
    renderAdmin(`/admin/${key}/monitoring/security`);
    await screen.findByText("Credential stuffing");
    const toggle = screen.getAllByRole("button", { name: "Show as table" })[0];
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    expect(toggle).toHaveTextContent("Hide table");
    const table = screen.getByRole("table", { name: "Sign-in attempts" });
    expect(table).not.toHaveClass("sr-only");
    expect(within(table).getByText("40")).toBeInTheDocument();
  });
});
describe("a section that took too long", () => {
  it("shows Try again in its place, and the rest of the page", async () => {
    const slow = {
      ...security,
      signals: [
        ...security.signals,
        signal({
          name: "auth.possible_takeovers",
          label: "Possible takeovers",
          unavailable: true,
        }),
      ],
      // Missing, as the server sends it: the gate mustn't render it.
      sections: { ...security.sections, takeovers: null },
      unavailable: ["takeovers"],
    } as unknown as MonitoringView<SecuritySections>;
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/monitoring/security": { body: slow },
    });
    renderAdmin(`/admin/${key}/monitoring/security`);
    expect(
      await screen.findByText("This section took too long to load."),
    ).toBeVisible();
    expect(screen.getByText("Not loaded")).toBeVisible();
    expect(screen.getByRole("link", { name: "203.0.113.66" })).toBeVisible();
    expect(screen.getAllByRole("button", { name: "Try again" })).toHaveLength(
      1,
    );
  });
});
describe("the Rate limits page", () => {
  it("says how many are at a limit, never who, and which limits it can't see", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/monitoring/limits": { body: limits },
    });
    renderAdmin(`/admin/${key}/monitoring/limits`);
    expect(await screen.findByText("Blocked right now")).toBeVisible();
    expect(screen.getByText("Counted in each server’s memory")).toBeVisible();
    expect(screen.queryByText("project:per-user")).toBeNull();
    expect(screen.getByText("412")).toBeVisible();
    expect(
      screen.getByText("No account reached a limit in this window."),
    ).toBeVisible();
  });
});
