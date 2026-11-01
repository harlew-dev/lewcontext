import { render, screen, within } from "@testing-library/react";
import { createRoutesStub, Outlet } from "react-router";
import { SWRConfig } from "swr";
import { describe, expect, it } from "vitest";
import { adminLoader } from "@/lib/admin";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import type { DataSections, MonitoringView, Signal } from "@/types/admin";
import AdminMonitoringData from "./[key]/monitoring/data";
import AdminLayoutRoute from "./layout";
// The Monitoring Data page.
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
function signal(overrides: Partial<Signal>): Signal {
  return {
    name: "data.database_size",
    label: "Database size",
    unit: "bytes",
    value: 0,
    sample: 1,
    level: "ok",
    windowSeconds: 60,
    ...overrides,
  };
}
const target = (
  over: Partial<DataSections["retention"][number]>,
): DataSections["retention"][number] => ({
  target: "events",
  description: "the audit trail apart from its audit events",
  keepDays: 90,
  minimises: false,
  oldest: "2026-08-05T14:47:00Z",
  overdue: false,
  lastRun: {
    target: "events",
    at: "2026-11-01T09:00:00Z",
    event: "evt_00000000000007",
    deleted: 1204,
    kept: 2,
    cutoff: "2026-08-03T09:00:00Z",
  },
  notRunning: false,
  ...over,
});
const data: MonitoringView<DataSections> = {
  page: "data",
  window,
  unavailable: [],
  signals: [
    signal({ value: 1.5 * 1024 ** 3 }),
    signal({
      name: "data.prune_not_running",
      label: "Prune targets not running",
      unit: "count",
      value: 1,
      level: "warn",
      warn: 1,
    }),
  ],
  sections: {
    database: {
      sizeBytes: 1.5 * 1024 ** 3,
      maxConnections: 100,
      connections: [
        { state: "idle", count: 8 },
        { state: "hidden", count: 2 },
      ],
      longestQuerySeconds: 0.2,
      longestTransactionSeconds: 400,
      idleInTransaction: 1,
      lockWaits: 0,
      cacheHitShare: 99.2,
    },
    tables: [
      {
        name: "events",
        totalBytes: 900 * 1024 ** 2,
        tableBytes: 600 * 1024 ** 2,
        indexBytes: 300 * 1024 ** 2,
        liveRows: 1_200_000,
        deadRows: 400_000,
        lastVacuum: null,
        lastAnalyze: "2026-11-01T12:00:00Z",
        seqScans: 50,
        indexScans: 10,
        deadShare: 25,
        scanned: true,
        bloated: true,
      },
    ],
    growth: {
      days: [
        { day: "2026-10-30", liveRows: 1_000_000, totalBytes: 1024 ** 3 },
        {
          day: "2026-10-31",
          liveRows: 1_100_000,
          totalBytes: 1.25 * 1024 ** 3,
        },
      ],
      tables: [
        {
          name: "events",
          liveRows: 1_100_000,
          totalBytes: 800 * 1024 ** 2,
          rowsPerDay: 100_000,
          bytesPerDay: 256 * 1024 ** 2,
        },
      ],
      bytesPerDay: 256 * 1024 ** 2,
      projectionDays: 90,
      projectedBytes: 23.75 * 1024 ** 3,
    },
    log: {
      chart: {
        series: ["auth.login_attempted", "other"],
        buckets: [{ start: "2026-11-01T14:00:00Z", counts: [30, 5] }],
      },
      names: [{ name: "auth.login_attempted", count: 30 }],
      total: 35,
    },
    retention: [
      target({}),
      target({
        target: "request_hours",
        description: "hourly request sums older than the cutoff",
        oldest: "2026-07-01T00:00:00Z",
        overdue: true,
        notRunning: true,
        lastRun: null,
      }),
    ],
  },
};
function renderData() {
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
              path: "admin/:key/monitoring/data",
              Component: AdminMonitoringData,
            },
          ],
        },
      ],
    },
  ]);
  render(
    <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>
      <Stub initialEntries={[`/admin/${key}/monitoring/data`]} />
    </SWRConfig>,
  );
}
describe("the Data page", () => {
  it("shows retention in words, with each run and name linking to its events", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/monitoring/data": { body: data },
    });
    renderData();
    // Sizes in bytes' units.
    expect(await screen.findAllByText("1.5 GB")).not.toHaveLength(0);
    // Retention: a target's state is words, never colour alone.
    const late = screen.getByText("request_hours").closest("tr")!;
    expect(within(late).getByText("Overdue, Not running")).toBeVisible();
    expect(within(late).getByText("Warning")).toBeVisible();
    expect(within(late).getByText("Never")).toBeVisible();
    expect(within(late).getByText("123 days")).toBeVisible();
    const kept = screen.getAllByText("events")[0].closest("tr")!;
    expect(within(kept).getByText("OK")).toBeVisible();
    expect(within(kept).getByText("1,204 deleted, 2 kept")).toBeVisible();
    expect(
      within(kept).getByRole("link", { name: /Nov 1, 09:00/ }),
    ).toHaveAttribute("href", `/admin/${key}/events/evt_00000000000007`);
    expect(
      screen.getByRole("link", { name: "every run on Events" }),
    ).toHaveAttribute("href", expect.stringContaining("name=prune.completed"));
    // The database: counts and durations, another role's connections
    // named as hidden.
    expect(screen.getByText("Another role's (state hidden)")).toBeVisible();
    expect(screen.getByText("7 min")).toBeVisible();
    // Tables marked in words.
    expect(screen.getByText("Mostly sequential")).toBeVisible();
    expect(screen.getByText("Needs a vacuum")).toBeVisible();
    // Growth and its projection.
    expect(
      screen.getByText("+256 MB a day. At this rate, 23.8 GB in 90 days."),
    ).toBeVisible();
    // What fills the log: each name opens its events.
    const log = screen.getByRole("list", { name: "Recorded most" });
    expect(
      within(log).getByRole("link", { name: /auth\.login_attempted/ }),
    ).toHaveAttribute("href", expect.stringContaining("auth.login_attempted"));
  });
  it("says growth needs two days of sizes", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/monitoring/data": {
        body: {
          ...data,
          sections: {
            ...data.sections,
            growth: {
              ...data.sections.growth,
              days: data.sections.growth.days.slice(1),
              projectedBytes: null,
            },
          },
        },
      },
    });
    renderData();
    expect(
      await screen.findByText(
        "Growth needs two days of sizes: the server writes one a day.",
      ),
    ).toBeVisible();
  });
});
