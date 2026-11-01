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
  AccountsSections,
  AddressView,
  IpsSections,
  MonitoringView,
  Signal,
  UsageSections,
  WorkspacesSections,
} from "@/types/admin";
import AdminMonitoringAccounts from "./[key]/monitoring/accounts";
import AdminMonitoringIPs from "./[key]/monitoring/ips";
import AdminMonitoringAddress from "./[key]/monitoring/ips/[ip]";
import AdminMonitoringUsage from "./[key]/monitoring/usage";
import AdminMonitoringWorkspaces from "./[key]/monitoring/workspaces";
import AdminLayoutRoute from "./layout";
// The Monitoring pages of part 2: IP addresses and one address, Accounts,
// Workspaces, Usage.
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
    name: "ips.sign_up_cluster",
    label: "Most accounts from one address",
    unit: "count",
    value: 0,
    sample: 0,
    level: "ok",
    windowSeconds: 86400,
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
const row = (
  ip: string,
  network: boolean,
  n: Partial<Record<string, number>>,
) => ({
  ip,
  network,
  events: 0,
  failedSignIns: 0,
  signUps: 0,
  limitsReached: 0,
  accounts: 0,
  ...n,
});
const ips = view<IpsSections>(
  "ips",
  {
    addresses: [
      row("2001:db8:5:5::/64", true, { events: 20, signUps: 20 }),
      row("10.0.0.1", false, { events: 100 }),
      row("203.0.113.5", false, { events: 4, failedSignIns: 3 }),
      row("2001:db8:5:5::1", false, { events: 1, signUps: 1 }),
    ],
    clusters: [
      { period: "2026-10-31T14:30:00Z", ip: "2001:db8:5:5::/64", accounts: 20 },
    ],
    blocks: { live: 2 },
  },
  [
    signal({ value: 20, level: "critical", warn: 5, critical: 20 }),
    signal({
      name: "ips.busiest_share",
      label: "Events from the busiest address",
      unit: "percent",
      value: 80,
      sample: 125,
      level: "warn",
      warn: 80,
    }),
  ],
);
function Location() {
  const { pathname, search } = useLocation();
  return <output data-testid="location">{pathname + search}</output>;
}
function WithLocation({ children }: { children: React.ReactNode }) {
  return (
    <>
      {children}
      <Location />
    </>
  );
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
              path: "admin/:key/monitoring/ips",
              Component: () => (
                <WithLocation>
                  <AdminMonitoringIPs />
                </WithLocation>
              ),
            },
            {
              path: "admin/:key/monitoring/ips/:ip",
              Component: () => (
                <WithLocation>
                  <AdminMonitoringAddress />
                </WithLocation>
              ),
            },
            {
              path: "admin/:key/monitoring/accounts",
              Component: AdminMonitoringAccounts,
            },
            {
              path: "admin/:key/monitoring/workspaces",
              Component: AdminMonitoringWorkspaces,
            },
            {
              path: "admin/:key/monitoring/usage",
              Component: AdminMonitoringUsage,
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
// The rows of the first table under a heading, as text.
function addressColumn(): string[] {
  const table = screen.getAllByRole("table")[0];
  return within(table)
    .getAllByRole("row")
    .slice(1)
    .map((r) => within(r).getAllByRole("cell")[0].textContent ?? "");
}
describe("the IP addresses page", () => {
  it("warns when one address holds the traffic, sorts by a column, and opens an address", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/monitoring/ips": { body: ips },
    });
    renderAdmin(`/admin/${key}/monitoring/ips`);
    expect(
      await screen.findByText(/One address sent 80% of the last day/),
    ).toBeVisible();
    expect(screen.getByText("Critical")).toBeVisible();
    // The live IP blocks, counted, linking to their page.
    const blocks = screen.getByText(
      /2 addresses or ranges are blocked right now/,
    );
    expect(blocks).toBeVisible();
    expect(
      within(blocks).getByRole("link", { name: "IP blocks" }),
    ).toHaveAttribute("href", `/admin/${key}/ip-blocks`);
    // By events, IPv6 by its /64.
    expect(addressColumn()).toEqual([
      "10.0.0.1",
      "2001:db8:5:5::/64",
      "203.0.113.5",
    ]);
    const slash = screen.getAllByRole("link", { name: "2001:db8:5:5::/64" })[0];
    expect(slash).toHaveAttribute(
      "href",
      `/admin/${key}/monitoring/ips/2001:db8:5:5::%2F64`,
    );
    // Sorted by failed sign-ins: a link, kept in the URL.
    fireEvent.click(screen.getByRole("link", { name: /Failed sign-ins/ }));
    await waitFor(() =>
      expect(screen.getByTestId("location")).toHaveTextContent(
        "sort=failedSignIns",
      ),
    );
    expect(addressColumn()[0]).toBe("203.0.113.5");
    expect(
      screen.getByRole("columnheader", { name: /Failed sign-ins/ }),
    ).toHaveAttribute("aria-sort", "descending");
  });
});
const address: AddressView = {
  ip: "2001:db8:7:7::1",
  network: "2001:db8:7:7::/64",
  window,
  events: 7,
  firstSeen: "2026-11-01T11:00:00Z",
  lastSeen: "2026-11-01T14:00:00Z",
  byName: [{ name: "auth.login_attempted", count: 4 }],
  accounts: [
    {
      account: { id: "usr_00000000000002", name: "Taylor Reed" },
      signedIn: 1,
      registered: 1,
      failed: 1,
    },
  ],
  unknownEmails: 2,
  limits: [],
  addresses: [],
  blocks: [],
};
const reason = "Credential stuffing from this network, ticket 4521.";
describe("one address's page", () => {
  it("shows what it did, the accounts from it, and its /64, with the window in the URL", async () => {
    const api = stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/ips/2001:db8:7:7::1": { body: address },
    });
    renderAdmin(`/admin/${key}/monitoring/ips/2001:db8:7:7::1`);
    expect(
      await screen.findByRole("link", { name: "Taylor Reed" }),
    ).toHaveAttribute("href", `/admin/${key}/users/usr_00000000000002`);
    expect(
      screen.getByRole("heading", { name: "2001:db8:7:7::1" }),
    ).toBeVisible();
    expect(
      screen.getByText(/tried 2 different emails with no account/),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: "2001:db8:7:7::/64" }),
    ).toHaveAttribute(
      "href",
      `/admin/${key}/monitoring/ips/2001:db8:7:7::%2F64`,
    );
    expect(
      screen.getByRole("link", { name: "Its events" }).getAttribute("href"),
    ).toContain("ip=2001%3Adb8%3A7%3A7%3A%3A1");
    // The sidebar's IP addresses item is current on an address's page.
    expect(screen.getByRole("link", { name: "IP addresses" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(api.sent("GET").map((s) => s.url)).toContain(
      "/api/admin/ips/2001:db8:7:7::1",
    );
    // A viewer can't block it.
    expect(
      screen.queryByRole("button", { name: "Block this address" }),
    ).not.toBeInTheDocument();
  });
  it("lets an operator block its /64, then shows the block", async () => {
    const blocked = {
      ...address,
      blocks: [
        { network: "2001:db8:7:7::/64", expiresAt: "2026-11-02T14:00:00Z" },
      ],
    };
    const api = stubApi({
      "GET /api/admin/me": { body: { ...me, role: "operator" } },
      "GET /api/admin/ips/2001:db8:7:7::1": [
        { body: address },
        { body: blocked },
      ],
      "POST /api/admin/ip-blocks": {
        status: 201,
        body: {
          id: "ipb_00000000000001",
          network: "2001:db8:7:7::/64",
          reason,
          createdBy: null,
          createdAt: "2026-11-01T14:00:00Z",
          expiresAt: "2026-11-02T14:00:00Z",
        },
      },
    });
    renderAdmin(`/admin/${key}/monitoring/ips/2001:db8:7:7::1`);
    await screen.findByRole("link", { name: "Taylor Reed" });
    expect(screen.queryByText(/is blocked until/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Block this address" }));
    const dialog = await screen.findByRole("dialog");
    // Its /64 offered: one person on IPv6 usually has a whole /64.
    expect(within(dialog).getByLabelText("Address or range")).toHaveValue(
      "2001:db8:7:7::/64",
    );
    fireEvent.change(within(dialog).getByLabelText("Reason"), {
      target: { value: reason },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Block" }));
    expect(
      await screen.findByText(/2001:db8:7:7::\/64 is blocked\./),
    ).toBeInTheDocument();
    expect(api.sent("POST")[0]).toEqual(
      expect.objectContaining({
        url: "/api/admin/ip-blocks",
        body: { network: "2001:db8:7:7::/64", expiresIn: "24h", reason },
        adminKey: key,
      }),
    );
    // Reloaded: the block shown, linking to the IP blocks page.
    const notice = (await screen.findByText(/is blocked until/))
      .parentElement as HTMLElement;
    expect(notice).toHaveTextContent(
      "2001:db8:7:7::/64 is blocked until Nov 2, 2026, 2:00 PM UTC. Requests from it are refused.",
    );
    expect(
      within(notice).getByRole("link", { name: "IP blocks" }),
    ).toHaveAttribute("href", `/admin/${key}/ip-blocks`);
  });
});
const accounts = view<AccountsSections>("accounts", {
  review: [
    {
      account: { id: "usr_00000000000003", name: "Visit http://cheap.example" },
      createdAt: "2026-11-01T12:00:00Z",
      new: true,
      signals: [
        { signal: "invitations", count: 30, of: 25 },
        { signal: "links", count: 1 },
        {
          signal: "links",
          count: 1,
          workspace: { id: "wsp_00000000000004", name: "Free money" },
        },
      ],
    },
  ],
  topWriters: [],
  topOwners: [],
});
describe("the Accounts page", () => {
  it("lists each account with its signals in words, new ones marked", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/monitoring/accounts": { body: accounts },
    });
    renderAdmin(`/admin/${key}/monitoring/accounts`);
    expect(
      await screen.findByText("30 invitations to 25 addresses in a day"),
    ).toBeVisible();
    expect(screen.getByText("New")).toBeVisible();
    expect(screen.getByText("A link or markup in its name")).toBeVisible();
    expect(screen.getByRole("link", { name: "Free money" })).toHaveAttribute(
      "href",
      `/admin/${key}/workspaces/wsp_00000000000004`,
    );
    expect(
      screen.getByText("No account wrote anything in this window."),
    ).toBeVisible();
  });
});
const workspaces = view<WorkspacesSections>("workspaces", {
  quotas: {
    caps: { projects: 1000, members: 100, invitations: 50, owned: 20 },
    workspaces: [
      {
        workspace: { id: "wsp_00000000000005", name: "Northwind Archive" },
        projects: 850,
        members: 1,
        invitations: 50,
        used: 100,
      },
    ],
    owners: [],
  },
  rows: {
    tables: [
      { table: "projects", label: "Projects" },
      { table: "events", label: "Events" },
    ],
    workspaces: [
      {
        workspace: { id: "wsp_00000000000005", name: "Northwind Archive" },
        counts: [850, 12],
        total: 862,
      },
    ],
  },
  growth: [],
  invitations: [
    {
      workspace: { id: "wsp_00000000000006", name: "Spam Co" },
      sent: 30,
      accepted: 0,
      revoked: 0,
      expired: 0,
    },
  ],
  throwaways: [],
  totals: { estimate: 41, series: emptySeries },
});
describe("the Workspaces page", () => {
  it("shows each quota as a meter with its level in words, rows per table, and low acceptance", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/monitoring/workspaces": { body: workspaces },
    });
    renderAdmin(`/admin/${key}/monitoring/workspaces`);
    const projects = await screen.findByRole("meter", { name: "Projects" });
    expect(projects).toHaveAttribute("aria-valuetext", "850 of 1,000 used");
    expect(
      screen.getByRole("meter", { name: "Live invitations" }),
    ).toHaveAttribute("aria-valuetext", "50 of 50 used");
    expect(screen.getByText("Critical")).toBeVisible();
    // 850 of 1,000 and 51 of 100: a warning for the projects only.
    expect(screen.getAllByText("Warning")).toHaveLength(2);
    expect(screen.getByRole("columnheader", { name: "Events" })).toBeVisible();
    expect(screen.getByText("862")).toBeVisible();
    expect(screen.getByText(/About 41 workspaces/)).toBeVisible();
  });
});
const usage = view<UsageSections>("usage", {
  active: { day: 3, week: 4, month: 5, withMfa: 1 },
  signUps: emptySeries,
  activation: {
    workspace: {
      accounts: 4,
      activated: 2,
      from: "2026-10-24T14:47:00Z",
      to: "2026-10-31T14:47:00Z",
    },
    invited: {
      accounts: 0,
      activated: 0,
      from: "2026-10-18T14:47:00Z",
      to: "2026-10-25T14:47:00Z",
    },
  },
  features: emptySeries,
  sizes: {
    members: [{ size: "1", workspaces: 2 }],
    projects: [{ size: "0", workspaces: 2 }],
  },
  inactive: {
    before: "2024-11-01T14:47:00Z",
    count: 21,
    accounts: [
      {
        account: { id: "usr_00000000000007", name: "Riley Neverback" },
        lastSeenAt: null,
        createdAt: "2023-11-01T00:00:00Z",
        admin: true,
      },
    ],
  },
});
describe("the Usage page", () => {
  it("shows active accounts, activation, and the accounts not seen for two years", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/monitoring/usage": { body: usage },
    });
    renderAdmin(`/admin/${key}/monitoring/usage`);
    expect(
      await screen.findByText(/2 of the 4 accounts made Oct 24/),
    ).toBeVisible();
    expect(screen.getByText("25%")).toBeVisible();
    expect(screen.getByText("Never")).toBeVisible();
    expect(
      screen.getByText(/21 accounts; the 1 unseen longest below/),
    ).toBeVisible();
    expect(screen.getByRole("link", { name: "Riley Neverback" })).toBeVisible();
    // Every Monitoring page built is in the sidebar.
    for (const name of ["IP addresses", "Accounts", "Workspaces", "Usage"])
      expect(screen.getAllByRole("link", { name })[0]).toBeVisible();
  });
});
