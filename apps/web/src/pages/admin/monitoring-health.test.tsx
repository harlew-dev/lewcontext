import type { ReactNode } from "react";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { createRoutesStub, Outlet, useLocation } from "react-router";
import { SWRConfig } from "swr";
import { describe, expect, it } from "vitest";
import { adminLoader } from "@/lib/admin";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import type {
  ConfigurationSections,
  ErrorGroup,
  ErrorsSections,
  ErrorView,
  IntegritySections,
  MonitoringView,
  PerformanceSections,
  Signal,
} from "@/types/admin";
import AdminMonitoringConfiguration from "./[key]/monitoring/configuration";
import AdminMonitoringErrors from "./[key]/monitoring/errors";
import AdminMonitoringError from "./[key]/monitoring/errors/[error]";
import AdminMonitoringIntegrity from "./[key]/monitoring/integrity";
import AdminMonitoringPerformance from "./[key]/monitoring/performance";
import AdminLayoutRoute from "./layout";
// The Monitoring pages of part 3: Integrity, Performance, Errors (and one
// group), Configuration.
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
const emptySeries = { series: [], buckets: [] };
function signal(overrides: Partial<Signal>): Signal {
  return {
    name: "integrity.critical_checks",
    label: "Critical checks failing",
    unit: "count",
    value: 0,
    sample: 0,
    level: "ok",
    windowSeconds: 60,
    ...overrides,
  };
}
function view<S>(page: string, sections: S, signals: Signal[] = []) {
  return {
    page,
    window,
    signals,
    sections,
    unavailable: [],
  } as MonitoringView<S>;
}
function Location() {
  const { pathname, search } = useLocation();
  return <output data-testid="location">{pathname + search}</output>;
}
function renderAdmin(path: string) {
  function page(Component: () => ReactNode) {
    return function Page() {
      return (
        <>
          <Component />
          <Location />
        </>
      );
    };
  }
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
              path: "admin/:key/monitoring/integrity",
              Component: page(AdminMonitoringIntegrity),
            },
            {
              path: "admin/:key/monitoring/performance",
              Component: page(AdminMonitoringPerformance),
            },
            {
              path: "admin/:key/monitoring/errors",
              Component: page(AdminMonitoringErrors),
            },
            {
              path: "admin/:key/monitoring/errors/:error",
              Component: page(AdminMonitoringError),
            },
            {
              path: "admin/:key/monitoring/configuration",
              Component: page(AdminMonitoringConfiguration),
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
const none = { count: 0, samples: [] };
const integrity = view<IntegritySections>(
  "integrity",
  {
    checks: [
      {
        name: "integrity.admins_without_mfa",
        samples: ["usr_00000000000002"],
      },
      {
        name: "integrity.servers_out_of_step",
        samples: ["srv_00000000000003"],
      },
      { name: "integrity.overdue_deliveries", samples: [] },
    ],
    duplicates: {
      count: 1,
      samples: ["evt_00000000000004"],
      byName: [{ name: "project.created", count: 1 }],
    },
    duplicateSends: none,
    retriedDeliveries: none,
    timedOutWrites: [],
  },
  [
    signal({ value: 1, level: "critical", critical: 1 }),
    signal({
      name: "integrity.warning_checks",
      label: "Warning checks failing",
      value: 1,
      level: "warn",
      warn: 1,
    }),
    signal({
      name: "integrity.admins_without_mfa",
      label: "Admins without two-factor",
      value: 1,
      level: "critical",
      critical: 1,
    }),
    signal({
      name: "integrity.servers_out_of_step",
      label: "Servers out of step",
      value: 1,
      level: "warn",
      warn: 1,
    }),
    signal({
      name: "integrity.overdue_deliveries",
      label: "Deliveries overdue",
      critical: 1,
      unavailable: true,
    }),
  ],
);
describe("the Integrity page", () => {
  it("lists each check with its level in words and the rows behind it", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/monitoring/integrity": { body: integrity },
    });
    renderAdmin(`/admin/${key}/monitoring/integrity`);
    const row = (await screen.findByText("Admins without two-factor")).closest(
      "tr",
    )!;
    expect(within(row).getByText("Critical")).toBeVisible();
    expect(
      within(row).getByRole("link", { name: "usr_00000000000002" }),
    ).toHaveAttribute("href", `/admin/${key}/users/usr_00000000000002`);
    // A server isn't a page: named, not linked.
    const servers = screen.getByText("Servers out of step").closest("tr")!;
    expect(within(servers).getByText("srv_00000000000003")).toBeVisible();
    expect(within(servers).queryByRole("link")).toBeNull();
    // A check not read in time says so, never "OK".
    const overdue = screen.getByText("Deliveries overdue").closest("tr")!;
    expect(within(overdue).getAllByText("Not loaded")).not.toHaveLength(0);
    expect(within(overdue).queryByText("OK")).toBeNull();
    // Duplicates: the count and the second create, linked to its event.
    expect(screen.getByText("1 project.")).toBeVisible();
    expect(
      screen.getByRole("link", { name: "evt_00000000000004" }),
    ).toHaveAttribute("href", `/admin/${key}/events/evt_00000000000004`);
    expect(
      screen.getByText("No email was sent twice for one request."),
    ).toBeVisible();
  });
});
const route = (name: string, requests: number, p95: number, status5xx = 0) => ({
  route: name,
  requests,
  status4xx: 0,
  status429: 0,
  status5xx,
  timeouts: 0,
  p50: p95 / 2,
  p95,
  p99: p95 * 1.5,
});
const performance = view<PerformanceSections>("performance", {
  metricsStore: "off",
  requests: [],
  routes: [
    route("GET /api/auth/me", 900, 12),
    route("POST /api/workspaces/{workspace}/projects", 30, 4200, 2),
    route("GET /api/rare", 3, 9000),
  ],
  database: {
    operations: [],
    slowQueries: [],
    inUse: [],
    waits: { from: window.from, waits: 0, waitedMs: 0, minutes: 0 },
    statements: { available: false, statements: [], hidden: 0 },
  },
  worker: { deliveries: emptySeries, lag: [], pending: [] },
  slotsBusy: 0,
  servers: [],
});
// The first cell of each body row of the routes table.
function firstColumn(): string[] {
  const table = screen
    .getByRole("columnheader", { name: "Route" })
    .closest("table")!;
  return within(table)
    .getAllByRole("row")
    .slice(1)
    .map((r) => within(r).getAllByRole("cell")[0].textContent ?? "");
}
describe("the Performance page", () => {
  it("says metric storage is off, marks the slowest routes and sorts them in the URL", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/monitoring/performance": { body: performance },
    });
    renderAdmin(`/admin/${key}/monitoring/performance`);
    expect(
      await screen.findByText(/Metric storage is off \(METRICS_STORE=off\)/),
    ).toBeVisible();
    // By requests; the slowest five of the routes with 20 requests or
    // more are marked (here both), never the rare one.
    expect(firstColumn()).toEqual([
      "GET /api/auth/meSlowest",
      "POST /api/workspaces/{workspace}/projectsSlowest",
      "GET /api/rare",
    ]);
    fireEvent.click(screen.getByRole("link", { name: "p95" }));
    await waitFor(() =>
      expect(screen.getByTestId("location")).toHaveTextContent("sort=p95"),
    );
    expect(firstColumn()[0]).toContain("GET /api/rare");
    expect(
      screen.getByText(/This database doesn.t have pg_stat_statements/),
    ).toBeVisible();
  });
  it("says when pg_stat_statements couldn't be read", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/monitoring/performance": {
        body: {
          ...performance,
          sections: {
            ...performance.sections,
            database: {
              ...performance.sections.database,
              statements: {
                available: false,
                unavailable: "unreadable",
                statements: [],
                hidden: 0,
              },
            },
          },
        },
      },
    });
    renderAdmin(`/admin/${key}/monitoring/performance`);
    expect(
      await screen.findByText(/pg_stat_statements couldn.t be read/),
    ).toBeVisible();
  });
});
const group = (over: Partial<ErrorGroup>): ErrorGroup => ({
  id: "err_00000000000001",
  source: "request",
  route: "POST /api/workspaces/{workspace}/projects",
  message: "ERROR: duplicate key value (SQLSTATE <n>)",
  count: 7,
  firstSeenAt: "2026-11-01T14:00:00Z",
  lastSeenAt: "2026-11-01T14:40:00Z",
  lastRequestId: "req_00000000000042",
  lastVersion: "abc1234def56",
  resolvedAt: null,
  new: false,
  ...over,
});
const userText = "Claim your prize at evil.example/login";
const errors = view<ErrorsSections>(
  "errors",
  {
    release: { version: "abc1234def56", startedAt: "2026-11-01T13:00:00Z" },
    groups: [
      // New by the server's flag: as text, 12:30-02:00 sorts before the
      // release's 13:00Z, though it's 14:30 UTC, after it.
      group({ firstSeenAt: "2026-11-01T12:30:00-02:00", new: true }),
      group({
        id: "err_00000000000002",
        source: "client",
        route: "/app/:workspace/projects",
        message: userText,
        firstSeenAt: "2026-10-30T10:00:00Z",
        lastRequestId: "",
        lastVersion: "",
      }),
    ],
    occurrences: emptySeries,
  },
  [
    signal({
      name: "errors.new",
      label: "New error groups",
      value: 1,
      level: "warn",
      warn: 1,
      windowSeconds: 86400,
    }),
  ],
);
describe("the Errors page", () => {
  it("labels a browser's group as user-reported, keeps its text as text, and marks what's new in the release", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/monitoring/errors": { body: errors },
    });
    renderAdmin(`/admin/${key}/monitoring/errors`);
    const text = await screen.findByText(userText);
    // Never a link, nor inside one.
    expect(text.closest("a")).toBeNull();
    expect(screen.queryByRole("link", { name: userText })).toBeNull();
    const row = text.closest("tr")!;
    expect(within(row).getByText("User-reported")).toBeVisible();
    expect(within(row).queryByText("New")).toBeNull();
    const first = screen
      .getByText("ERROR: duplicate key value (SQLSTATE <n>)")
      .closest("tr")!;
    expect(within(first).getByText("New")).toBeVisible();
    expect(
      within(first).getByRole("link", { name: "Details" }),
    ).toHaveAttribute(
      "href",
      `/admin/${key}/monitoring/errors/err_00000000000001`,
    );
    expect(screen.getByText(/The current release, abc1234def56/)).toBeVisible();
  });
});
const one: ErrorView = {
  window,
  group: {
    ...group({ source: "client", message: userText, lastVersion: "" }),
    stack: "at render (index-<token>.js:<n>:<n>)",
  },
  release: null,
  occurrences: emptySeries,
};
describe("one error group's page", () => {
  it("shows the stack and last request as text, the Errors item current", async () => {
    const api = stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/errors/err_00000000000001": { body: one },
    });
    renderAdmin(`/admin/${key}/monitoring/errors/err_00000000000001`);
    expect(
      await screen.findByText("at render (index-<token>.js:<n>:<n>)"),
    ).toBeVisible();
    expect(screen.getByText(userText).closest("a")).toBeNull();
    expect(screen.getByText(/Reported by a browser/)).toBeVisible();
    expect(screen.getByText("req_00000000000042")).toBeVisible();
    expect(screen.getByText("Not known")).toBeVisible();
    expect(screen.getByRole("link", { name: "Errors" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(api.sent("GET").map((s) => s.url)).toContain(
      "/api/admin/errors/err_00000000000001",
    );
  });
});
const configuration = view<ConfigurationSections>(
  "configuration",
  {
    servers: [
      {
        id: "srv_00000000000001",
        hostname: "web-1",
        version: "abc1234def56",
        startedAt: "2026-11-01T13:00:00Z",
        lastSeenAt: "2026-11-01T14:46:00Z",
        live: true,
      },
      {
        id: "srv_00000000000002",
        hostname: "web-2",
        version: "def5678abc12",
        startedAt: "2026-11-01T12:00:00Z",
        lastSeenAt: "2026-11-01T14:46:00Z",
        live: true,
      },
    ],
    compared: ["srv_00000000000001", "srv_00000000000002"],
    settings: [
      { name: "MAIL_DRIVER", values: ["smtp", "off"], differs: true },
      { name: "ADMIN_ROUTE_KEY", values: ["set", "set"], differs: false },
    ],
    warnings: [
      { id: "previous_key", count: 3 },
      { id: "allowlist_without_client_ip", count: 2 },
    ],
  },
  [
    signal({
      name: "servers.versions",
      label: "Versions live",
      value: 2,
      level: "warn",
      warn: 2,
      windowSeconds: 120,
    }),
  ],
);
describe("the Configuration page", () => {
  it("warns with what to do and marks a setting that differs", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/monitoring/configuration": { body: configuration },
    });
    renderAdmin(`/admin/${key}/monitoring/configuration`);
    expect(
      await screen.findByText(
        "ADMIN_ROUTE_KEY_PREVIOUS is still set; 3 admin sessions use it.",
      ),
    ).toBeVisible();
    expect(
      screen.getByText(
        "ADMIN_ALLOWED_IPS without CLIENT_IP_HEADER on 2 servers.",
      ),
    ).toBeVisible();
    const mail = screen.getByText("MAIL_DRIVER").closest("tr")!;
    expect(within(mail).getByText("Differs")).toBeVisible();
    const key2 = screen.getByText("ADMIN_ROUTE_KEY").closest("tr")!;
    expect(within(key2).queryByText("Differs")).toBeNull();
    expect(screen.getByRole("columnheader", { name: "web-2" })).toBeVisible();
  });
});
