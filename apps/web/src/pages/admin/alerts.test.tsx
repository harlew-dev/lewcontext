import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { createRoutesStub, Outlet } from "react-router";
import { SWRConfig } from "swr";
import { afterEach, describe, expect, it, vi } from "vitest";
import { adminLoader } from "@/lib/admin";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import type { AdminAlert, AdminSession } from "@/types/admin";
import AdminAlertIndex from "./[key]/alerts/index";
import AdminAlertShow from "./[key]/alerts/[alert]";
import AdminAdminIndex from "./[key]/admins";
import AdminMonitoringError from "./[key]/monitoring/errors/[error]";
import AdminLayoutRoute from "./layout";
import { pick } from "@/test/pick";
// The Alerts pages and the housekeeping actions: the list by state, the
// sidebar's count, acknowledging, silencing (an action: a reason), the
// error group's Resolve, and the Subscriptions tab and Subscribe dialog.
const key = "8a1f0c52-6d3e-4b7a-9c2d-5e8f1a3b7c90";
const admin = testSession({
  user: {
    id: "usr_00000000000009",
    name: "Olive Admin",
    email: "olive@example.com",
  },
});
function me(role: "viewer" | "operator", openAlerts = 2): AdminSession {
  return {
    user: admin.user,
    role,
    csrfToken: "admin-csrf",
    expiresAt: "",
    openAlerts,
  };
}
function alert(overrides: Partial<AdminAlert>): AdminAlert {
  return {
    state: "active",
    currentLevel: "critical",
    rawLevel: "critical",
    healthySince: null,
    healthyChecks: 0,
    recoveredAt: null,
    lastCheckedAt: null,
    lastSuccessfulAt: null,
    healthInterrupted: false,
    revision: 1,
    sequence: 1,
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
  const cycle = {
    id: "inc_00000000000001",
    number: 1,
    openedAt: "2026-11-01T10:00:00Z",
    resolvedAt: null,
    resolutionSource: null,
    resolver: null,
    resolutionMessage: null,
  };
  const thread = { currentCycle: cycle, cycles: [cycle], nextCycle: null };
  const comment = {
    id: "msg_00000000000001",
    body: "Reproduced locally; fix is in review.",
    createdAt: "2026-11-01T12:00:00Z",
    author: { id: "usr_00000000000002", name: "Ada Viewer" },
    redactedAt: null,
    redactor: null,
  };
  const messages = {
    "GET /api/admin/errors/err_00000000000001/messages": {
      body: { data: [comment], next: null },
    },
  };
  it("is an operator's choice of a comment, sent without a code", async () => {
    const api = stubApi({
      "GET /api/admin/me": { body: me("operator") },
      "GET /api/admin/errors/err_00000000000001": [
        { body: { ...group, ...thread } },
        {
          body: {
            ...group,
            group: { ...group.group, resolvedAt: "2026-11-01T14:50:00Z" },
            ...thread,
          },
        },
      ],
      ...messages,
      "PATCH /api/admin/errors/err_00000000000001": { status: 204 },
    });
    renderAdmin(`/admin/${key}/monitoring/errors/err_00000000000001`);
    await screen.findByText("Reproduced locally; fix is in review.");
    fireEvent.click(
      await screen.findByRole("button", { name: "Mark as resolved" }),
    );
    // The dialog asks to confirm the chosen comment as the decision.
    fireEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", {
        name: "Mark as resolved",
      }),
    );
    await waitFor(() =>
      expect(api.sent("PATCH")[0]).toMatchObject({
        body: {
          resolved: true,
          cycle: cycle.id,
          message: comment.id,
          lastSeenAt: group.group.lastSeenAt,
        },
      }),
    );
  });
  it("lets a viewer discuss but not resolve", async () => {
    stubApi({
      "GET /api/admin/me": { body: me("viewer") },
      "GET /api/admin/errors/err_00000000000001": {
        body: { ...group, ...thread },
      },
      ...messages,
    });
    renderAdmin(`/admin/${key}/monitoring/errors/err_00000000000001`);
    await screen.findByText("Reproduced locally; fix is in review.");
    expect(screen.getByRole("button", { name: "Post comment" })).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Mark as resolved" }),
    ).toBeNull();
  });
});
const rules = [
  {
    signal: "http.latency_p95",
    title: "Latency, 95th percentile",
    meaning: "Requests are slow.",
    page: "performance",
    unit: "ms",
    warn: 1000,
    critical: 3000,
    minSample: 20,
    windowSeconds: 3600,
    from: "warn",
    cap: "critical",
    levels: ["warn", "critical"],
    effectiveLevels: [],
    forSeconds: 600,
    cadenceSeconds: 60,
    recoverySeconds: 600,
    recoveryChecks: 11,
    preference: "off",
    ineffective: false,
  },
  {
    signal: "auth.possible_takeovers",
    title: "Possible account takeovers",
    meaning: "An account changed after a new sign-in.",
    page: "security",
    unit: "count",
    warn: 1,
    critical: 0,
    minSample: 0,
    windowSeconds: 86400,
    from: "warn",
    cap: "critical",
    levels: ["warn"],
    effectiveLevels: [],
    forSeconds: 0,
    cadenceSeconds: 60,
    recoverySeconds: 600,
    recoveryChecks: 11,
    preference: "off",
    ineffective: false,
  },
];
const catalogReply = { body: { data: rules, retired: [] } };
const webhook = {
  id: "whk_00000000000001",
  kind: "webhook",
  name: "Pager",
  format: "json",
  signals: ["http.latency_p95"],
  events: ["opened", "recovered"],
  enabled: true,
  revision: 1,
  keyId: "key_Hq4Zr8Tn",
  overlapUntil: null,
  createdAt: "2026-11-01T00:00:00Z",
  updatedAt: "2026-11-01T00:00:00Z",
  lastDelivery: { status: "delivered", at: "2026-11-02T12:00:00Z" },
};
async function openSubscribe() {
  fireEvent.click(await screen.findByRole("button", { name: "Subscribe" }));
  return await screen.findByRole("dialog");
}
async function choose(dialog: HTMLElement, label: string, option: RegExp) {
  fireEvent.click(await within(dialog).findByRole("combobox", { name: label }));
  pick(await screen.findByRole("option", { name: option }));
  fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
}
describe("the Subscriptions tab and the Subscribe dialog", () => {
  it("lists your email and the webhooks, and nothing is subscribed by default", async () => {
    stubApi({
      "GET /api/admin/me": { body: me("viewer") },
      "GET /api/admin/alert-rules": catalogReply,
      "GET /api/admin/alert-webhooks": { body: { data: [webhook] } },
    });
    renderAdmin(`/admin/${key}/alerts?view=subscriptions`);
    expect(
      await screen.findByText(/You aren.t subscribed to any alert email/),
    ).toBeInTheDocument();
    expect(await screen.findByText("Pager")).toBeInTheDocument();
    expect(
      screen.getByText("1 signal, opened, recovery confirmed"),
    ).toBeInTheDocument();
    // A viewer reads it but has no row actions.
    expect(
      screen.queryByRole("button", { name: "Actions for Pager" }),
    ).toBeNull();
  });
  it("lets any admin subscribe by email in one dialog, with no code", async () => {
    const api = stubApi({
      "GET /api/admin/me": { body: me("viewer") },
      "GET /api/admin/alert-rules": catalogReply,
      "GET /api/admin/alert-webhooks": { body: { data: [] } },
      "PATCH /api/admin/alert-subscriptions": { status: 204 },
    });
    renderAdmin(`/admin/${key}/alerts?view=subscriptions`);
    const dialog = await openSubscribe();
    // A viewer's Type offers only Email, and the address is locked.
    fireEvent.click(
      within(dialog).getByRole("combobox", { name: /Subscription/ }),
    );
    expect(screen.getAllByRole("option")).toHaveLength(1);
    pick(screen.getByRole("option", { name: "Email" }));
    expect(
      within(dialog).getByDisplayValue("olive@example.com"),
    ).toHaveAttribute("readonly");
    await choose(dialog, "Events", /^Latency/);
    fireEvent.click(
      within(dialog).getByRole("radio", { name: /Critical alerts only/ }),
    );
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(api.sent("PATCH")[0]).toMatchObject({
        url: "/api/admin/alert-subscriptions",
        body: {
          signals: [
            {
              signal: "http.latency_p95",
              minLevel: "critical",
              events: [
                "opened",
                "escalated",
                "recovered",
                "reopened",
                "resolved",
                "reminded",
              ],
            },
          ],
        },
      }),
    );
  });
  it("lets an operator add a webhook in the same dialog, with a reason, then copy its key", async () => {
    const api = stubApi({
      "GET /api/admin/me": { body: me("operator") },
      "GET /api/admin/alert-rules": catalogReply,
      "GET /api/admin/alert-webhooks": { body: { data: [] } },
      "POST /api/admin/alert-webhooks": { status: 201, body: webhook },
      "GET /api/admin/alert-webhooks/whk_00000000000001/signing-key": {
        body: { key: "k".repeat(43), keyId: "key_Hq4Zr8Tn" },
      },
    });
    renderAdmin(`/admin/${key}/alerts?view=subscriptions`);
    const dialog = await openSubscribe();
    fireEvent.click(
      within(dialog).getByRole("combobox", { name: /Subscription/ }),
    );
    pick(await screen.findByRole("option", { name: "Webhook" }));
    // Nothing yet to save: it needs a name and a URL.
    expect(
      within(dialog).getByRole("button", { name: "Add webhook" }),
    ).toBeDisabled();
    fireEvent.change(within(dialog).getByLabelText("Name"), {
      target: { value: "Pager" },
    });
    fireEvent.change(within(dialog).getByLabelText("URL"), {
      target: { value: "https://hooks.example.net/pager" },
    });
    await choose(dialog, "Events", /^Latency/);
    await choose(dialog, "Events", /^Opened/);
    // Example payloads are formatted code, for the event chosen.
    fireEvent.click(within(dialog).getByText(/Example payloads/));
    fireEvent.click(within(dialog).getByRole("button", { name: "Opened" }));
    expect(within(dialog).getByText(/"event": "alert.opened"/)).toBeVisible();
    // Headers are always shown, not collapsible. The verifying steps name
    // the key ID header too, so match them all and check the first (the
    // headers block, which comes before them).
    expect(within(dialog).getAllByText(/X-Lewcontext-Key-Id/)[0]).toBeVisible();
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Add webhook" }),
    );
    const confirm = (await screen.findAllByRole("dialog")).at(-1)!;
    fireEvent.change(within(confirm).getByLabelText("Reason"), {
      target: { value: "Paging the on-call channel for latency." },
    });
    fireEvent.click(within(confirm).getByRole("button", { name: "Add" }));
    await waitFor(() =>
      expect(api.sent("POST")[0]).toMatchObject({
        url: "/api/admin/alert-webhooks",
        body: {
          kind: "webhook",
          name: "Pager",
          url: "https://hooks.example.net/pager",
          format: "json",
          signals: ["http.latency_p95"],
          events: ["opened"],
          enabled: true,
          reason: "Paging the on-call channel for latency.",
        },
      }),
    );
    // The dialog stays to show its own key, read-only with a Copy button.
    fireEvent.click(
      await screen.findByRole("button", { name: "Show signing key" }),
    );
    expect(await screen.findByDisplayValue("k".repeat(43))).toHaveAttribute(
      "readonly",
    );
    expect(
      screen.getByRole("button", { name: "Copy signing key" }),
    ).toBeInTheDocument();
  });
});

describe("the Alerts tab and its filters", () => {
  const list = { body: { data: [alert({})], next: null } };
  const urls = (api: ReturnType<typeof stubApi>) =>
    api.sent("GET").map((r) => r.url);
  it("has the table only on the Alerts tab, and each tab reads only its own data", async () => {
    const api = stubApi({
      "GET /api/admin/me": { body: me("viewer") },
      "GET /api/admin/alerts": list,
      "GET /api/admin/alert-rules": catalogReply,
    });
    renderAdmin(`/admin/${key}/alerts?view=catalog`);
    await screen.findByText("Latency, 95th percentile");
    expect(screen.queryByRole("table")).toBeNull();
    expect(urls(api).some((u) => u.startsWith("/api/admin/alerts"))).toBe(
      false,
    );
    cleanup();
    const second = stubApi({
      "GET /api/admin/me": { body: me("viewer") },
      "GET /api/admin/alerts": list,
      "GET /api/admin/alert-rules": catalogReply,
    });
    renderAdmin(`/admin/${key}/alerts`);
    expect(await screen.findByRole("table")).toBeInTheDocument();
    expect(second.sent("GET").some((r) => r.url.includes("alert-rules"))).toBe(
      false,
    );
    // One tab for the list, not a tab per state.
    expect(screen.queryByRole("link", { name: "Resolved" })).toBeNull();
    expect(screen.getByRole("link", { name: "Alerts" })).toBeInTheDocument();
  });
  it("searches, and filters through its dialog, in the URL", async () => {
    const api = stubApi({
      "GET /api/admin/me": { body: me("viewer") },
      "GET /api/admin/alerts": list,
      "GET /api/admin/alert-rules": catalogReply,
    });
    renderAdmin(`/admin/${key}/alerts`);
    fireEvent.change(
      await screen.findByRole("searchbox", { name: /Search alerts/ }),
      {
        target: { value: "latency" },
      },
    );
    await waitFor(() =>
      expect(urls(api)).toContain("/api/admin/alerts?q=latency"),
    );
    fireEvent.click(screen.getByRole("button", { name: /Filters/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("combobox", { name: "Level" }));
    pick(await screen.findByRole("option", { name: "Critical" }));
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Apply filters" }),
    );
    await waitFor(() =>
      expect(urls(api)).toContain("/api/admin/alerts?q=latency&level=critical"),
    );
    // Each applied filter is a badge that removes itself.
    const applied = await screen.findByRole("list", {
      name: "Applied filters",
    });
    fireEvent.click(
      within(applied).getByRole("button", { name: "Remove filter: Critical" }),
    );
    await waitFor(() =>
      expect(urls(api).at(-1)).toBe("/api/admin/alerts?q=latency"),
    );
  });
  it("asks for a window as a start time", async () => {
    const api = stubApi({
      "GET /api/admin/me": { body: me("viewer") },
      "GET /api/admin/alerts": list,
    });
    renderAdmin(`/admin/${key}/alerts`);
    fireEvent.click(await screen.findByRole("combobox", { name: "Opened" }));
    pick(await screen.findByRole("option", { name: "Last 7 days" }));
    await waitFor(() =>
      expect(urls(api).at(-1)).toMatch(
        /^\/api\/admin\/alerts\?from=\d{4}-\d\d-\d\dT\d\d%3A\d\d%3A00Z$/,
      ),
    );
  });
});

describe("the Catalog tab", () => {
  it("is a list of collapsible rules with a search and filters", async () => {
    stubApi({
      "GET /api/admin/me": { body: me("viewer") },
      "GET /api/admin/alert-rules": catalogReply,
    });
    renderAdmin(`/admin/${key}/alerts?view=catalog`);
    const first = await screen.findByRole("button", {
      name: /Latency, 95th percentile/,
    });
    expect(first).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(first);
    expect(first).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("Requests are slow.")).toBeVisible();
    fireEvent.change(
      screen.getByRole("searchbox", { name: /Search the catalog/ }),
      {
        target: { value: "takeover" },
      },
    );
    await waitFor(() =>
      expect(
        screen.queryByRole("button", { name: /Latency, 95th percentile/ }),
      ).toBeNull(),
    );
    expect(
      screen.getByRole("button", { name: /Possible account takeovers/ }),
    ).toBeInTheDocument();
    // Filters: a page with nothing in it says so.
    fireEvent.click(screen.getByRole("button", { name: /Filters/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("combobox", { name: "Page" }));
    pick(await screen.findByRole("option", { name: "Performance" }));
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Apply filters" }),
    );
    expect(await screen.findByText("No rules match")).toBeInTheDocument();
  });
});

describe("email subscription events", () => {
  it("shows the events you chose and saves only what changed", async () => {
    const subscribed = [
      { ...rules[0], preference: "warn", events: ["opened", "recovered"] },
      rules[1],
    ];
    const api = stubApi({
      "GET /api/admin/me": { body: me("viewer") },
      "GET /api/admin/alert-rules": { body: { data: subscribed, retired: [] } },
      "GET /api/admin/alert-webhooks": { body: { data: [] } },
      "PATCH /api/admin/alert-subscriptions": { status: 204 },
    });
    renderAdmin(`/admin/${key}/alerts?view=subscriptions`);
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    const dialog = await screen.findByRole("dialog");
    // Signals and events are one box, with the events as its last group.
    fireEvent.click(within(dialog).getByRole("combobox", { name: "Events" }));
    expect(
      await screen.findByRole("option", { name: /^Alert events/ }),
    ).toBeInTheDocument();
    pick(await screen.findByRole("option", { name: /^Resolved/ }));
    fireEvent.keyDown(document.activeElement ?? document.body, {
      key: "Escape",
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(api.sent("PATCH")[0]).toMatchObject({
        url: "/api/admin/alert-subscriptions",
        body: {
          signals: [
            {
              signal: "http.latency_p95",
              minLevel: "warn",
              events: ["opened", "recovered", "resolved"],
            },
          ],
        },
      }),
    );
  });
});
