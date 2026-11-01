import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { createRoutesStub, Outlet, useLocation } from "react-router";
import { SWRConfig } from "swr";
import { describe, expect, it, vi } from "vitest";
import { adminLoader } from "@/lib/admin";
import { stubApi, type Reply, type Sent } from "@/test/api";
import { testSession } from "@/test/session";
import {
  failureKinds,
  type AdminEvent,
  type AdminEventPage,
  type AdminMail,
  type EventSummary,
} from "@/types/admin";
import AdminEventIndex from "./[key]/events";
import AdminEventShow from "./[key]/events/[event]";
import AdminDeliveryIndex from "./[key]/mail";
import AdminLayoutRoute from "./layout";
const key = "8a1f0c52-6d3e-4b7a-9c2d-5e8f1a3b7c90";
const admin = testSession({
  user: { id: "usr_00000000000009", name: "Olive Admin" },
});
const me = { user: admin.user, csrfToken: "admin-csrf", expiresAt: "" };
const catalogue = {
  events: [
    "auth.login_attempted",
    "auth.logged_in",
    "auth.mfa.enabled",
    "auth.mfa.checked",
    "workspace.created",
    "workspace.member.removed",
    "admin.user.viewed",
  ],
  eventGroups: [
    {
      id: "auth.*",
      label: "Accounts and sign-in",
      names: ["auth.login_attempted", "auth.logged_in"],
      groups: [
        {
          id: "auth.mfa.*",
          label: "Two-factor authentication",
          names: ["auth.mfa.enabled", "auth.mfa.checked"],
          groups: [],
        },
      ],
    },
    {
      id: "workspace.*",
      label: "Workspaces",
      names: ["workspace.created", "workspace.member.removed"],
      groups: [],
    },
    {
      id: "admin-reads",
      label: "Admin reads",
      names: ["admin.user.viewed"],
      groups: [],
    },
  ],
  limits: ["auth:per-ip"],
  listeners: ["mail.password_reset"],
};
function event(n: number, overrides: Partial<AdminEvent> = {}): AdminEvent {
  return {
    id: `evt_0000000000000${n}`,
    name: "auth.login_attempted",
    createdAt: "2026-10-30T12:00:00Z",
    failed: false,
    actor: null,
    user: { id: "usr_00000000000001", name: "Ada", email: "ada@example.com" },
    workspace: null,
    ip: "203.0.113.5",
    payload: { email: "ada@example.com", result: "wrong_password" },
    ...overrides,
  };
}
// Shows where the page went, for asserting on the URL.
function Where() {
  const { pathname, search } = useLocation();
  return <output aria-label="Location">{pathname + search}</output>;
}
// The admin layout with the Events, event and Mail routes.
function renderAdmin(path: string) {
  const Stub = createRoutesStub([
    {
      path: "/",
      Component: () => (
        <>
          <Outlet />
          <Where />
        </>
      ),
      children: [
        {
          Component: AdminLayoutRoute,
          loader: ({ params }) => adminLoader(params.key!),
          children: [
            { path: "admin/:key/events", Component: AdminEventIndex },
            { path: "admin/:key/events/:event", Component: AdminEventShow },
            { path: "admin/:key/mail", Component: AdminDeliveryIndex },
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
// The Events list requests a test sent, in order.
function eventRequests(sent: Sent[]): string[] {
  return sent
    .map((r) => r.url)
    .filter((url) => url.startsWith("/api/admin/events"));
}
const start = "2026-11-01T12:00:00Z";
const next = "2026-11-01T12:30:00Z";
function okSummary(): EventSummary {
  return {
    status: "ok",
    bucketSeconds: 1800,
    total: 12,
    stackedBy: "group",
    series: ["auth.*", "workspace.*", "other"],
    buckets: [
      { start, counts: [5, 1, 0] },
      { start: next, counts: [4, 1, 1] },
    ],
    topNames: [
      { name: "auth.login_attempted", count: 9 },
      { name: "workspace.created", count: 2 },
    ],
    failures: {
      total: 4,
      series: [...failureKinds],
      buckets: [
        { start, counts: [3, 0, 0, 0, 0] },
        { start: next, counts: [0, 1, 0, 0, 0] },
      ],
    },
    limitsReached: 1,
    signInAttempts: 9,
  };
}
function eventPage(
  data: AdminEvent[],
  extra: Partial<AdminEventPage> = {},
): AdminEventPage {
  return {
    data,
    next: null,
    window: { from: "2026-10-31T12:00:00Z", to: "2026-11-01T12:47:00Z" },
    refs: { account: null, workspace: null },
    ...extra,
  };
}
function stubEvents(replies: Record<string, Reply | Reply[]>) {
  return stubApi({
    "GET /api/admin/me": { body: me },
    "GET /api/admin/catalogue": { body: catalogue },
    ...replies,
  });
}
function location() {
  return screen.getByLabelText("Location").textContent;
}
describe("the Events page", () => {
  it("shows the tiles, charts and top events from the newest page, with their numbers for screen readers", async () => {
    const api = stubEvents({
      "GET /api/admin/events": {
        body: eventPage([event(1, { failed: true })], {
          summary: okSummary(),
        }),
      },
    });
    renderAdmin(`/admin/${key}/events`);
    expect(
      await screen.findByText(
        "12 events in the last 24 hours, most at 12:00 UTC (6).",
      ),
    ).toBeInTheDocument();
    expect(eventRequests(api.sent())).toEqual([
      "/api/admin/events?summary=include",
    ]);
    expect(screen.getByText("Oct 31, 12:00 – Nov 1, 12:47 UTC")).toBeVisible();
    const events = screen.getByRole("table", { name: "Events over time" });
    const rows = within(events).getAllByRole("row");
    expect(
      within(rows[0])
        .getAllByRole("columnheader")
        .map((c) => c.textContent),
    ).toEqual(["Time", "Accounts and sign-in", "Workspaces", "Other", "Total"]);
    expect(
      within(rows[2])
        .getAllByRole("cell")
        .map((c) => c.textContent),
    ).toEqual(["4", "1", "1", "6"]);
    expect(
      screen.getByText("4 failed checks, most of them sign-in (3)."),
    ).toBeInTheDocument();
    expect(screen.getByText("33% of sign-ins")).toBeInTheDocument();
    const top = screen.getByRole("list", { name: "Top events" });
    expect(
      within(top).getByRole("link", { name: /auth.login_attempted/ }),
    ).toHaveAttribute("href", `/admin/${key}/events?name=auth.login_attempted`);
    // The row's Failed badge comes from the API's `failed`.
    const row = screen.getByRole("row", { name: /wrong_password/ });
    expect(within(row).getByText("Failed")).toBeInTheDocument();
  });
  it("reads names canonically, waiting for the catalogue, and leaves out what the API would refuse", async () => {
    const api = stubEvents({
      "GET /api/admin/events": { body: eventPage([event(1)]) },
    });
    renderAdmin(
      `/admin/${key}/events?window=7d&name=auth.logged_in,auth.login_attempted,auth.mfa.*,nope.thing,BAD&account=usr_x`,
    );
    expect(
      await screen.findByText("wrong_password · ada@example.com"),
    ).toBeInTheDocument();
    const urls = api.sent().map((r) => r.url);
    expect(urls.indexOf("/api/admin/catalogue")).toBeLessThan(
      urls.findIndex((u) => u.startsWith("/api/admin/events")),
    );
    expect(eventRequests(api.sent())).toEqual([
      "/api/admin/events?window=7d&name=auth.*&summary=include",
    ]);
    expect(screen.getByText("Events: auth.*")).toBeInTheDocument();
  });
  it("applies the dialog's filters in one navigation, and Cancel and Clear all apply nothing", async () => {
    const api = stubEvents({
      "GET /api/admin/events": { body: eventPage([event(1)]) },
    });
    renderAdmin(`/admin/${key}/events?ip=203.0.113.5`);
    await screen.findByText("wrong_password · ada@example.com");
    fireEvent.click(screen.getByRole("button", { name: /^Filters/ }));
    let dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByLabelText("IP address or range")).toHaveValue(
      "203.0.113.5",
    );
    fireEvent.click(within(dialog).getByRole("button", { name: "Clear all" }));
    expect(within(dialog).getByLabelText("IP address or range")).toHaveValue(
      "",
    );
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(location()).toBe(`/admin/${key}/events?ip=203.0.113.5`);
    expect(eventRequests(api.sent())).toHaveLength(1);

    // Opened again, it starts from what's applied.
    fireEvent.click(screen.getByRole("button", { name: /^Filters/ }));
    dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByLabelText("IP address or range")).toHaveValue(
      "203.0.113.5",
    );
    fireEvent.change(within(dialog).getByLabelText("Email"), {
      target: { value: " ADA@example.com " },
    });
    fireEvent.click(within(dialog).getAllByRole("combobox")[0]);
    fireEvent.click(
      await screen.findByRole("option", { name: /^auth\.mfa\.\* / }),
    );
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Apply filters" }),
    );
    const applied = `/admin/${key}/events?name=auth.mfa.*&email=ada%40example.com&ip=203.0.113.5`;
    await waitFor(() => expect(location()).toBe(applied));
    await waitFor(() => expect(eventRequests(api.sent())).toHaveLength(2));
    expect(eventRequests(api.sent())[1]).toBe(
      `/api/admin/events${applied.slice(applied.indexOf("?"))}&summary=include`,
    );
  });
  it("refuses a malformed filter on its field, without loading", async () => {
    const api = stubEvents({
      "GET /api/admin/events": { body: eventPage([event(1)]) },
    });
    renderAdmin(`/admin/${key}/events`);
    await screen.findByText("wrong_password · ada@example.com");
    fireEvent.click(screen.getByRole("button", { name: /^Filters/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Workspace"), {
      target: { value: "acme" },
    });
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Apply filters" }),
    );
    expect(
      await within(dialog).findByText("A workspace ID starts with wsp_."),
    ).toBeInTheDocument();
    expect(eventRequests(api.sent())).toHaveLength(1);
  });
  it("names the account and workspace in badges, and removes one filter each", async () => {
    stubEvents({
      "GET /api/admin/events": {
        body: eventPage([event(1)], {
          refs: {
            account: {
              id: "usr_00000000000001",
              name: "Ada Lovelace",
              email: "ada@example.com",
            },
            workspace: { id: "wsp_00000000000002" },
          },
        }),
      },
    });
    renderAdmin(
      `/admin/${key}/events?account=usr_00000000000001&workspace=wsp_00000000000002&ip=203.0.113.5`,
    );
    expect(
      await screen.findByText("Account: Ada Lovelace"),
    ).toBeInTheDocument();
    // A deleted workspace: its ID alone.
    expect(screen.getByText("Workspace: wsp_00000000000002")).toBeVisible();
    expect(screen.getByRole("button", { name: /^Filters 3/ })).toBeVisible();
    fireEvent.click(
      screen.getByRole("button", { name: "Remove filter: IP: 203.0.113.5" }),
    );
    await waitFor(() =>
      expect(location()).toBe(
        `/admin/${key}/events?account=usr_00000000000001&workspace=wsp_00000000000002`,
      ),
    );
    // Focus moves to the badge before it (the last one was removed).
    await waitFor(() =>
      expect(document.activeElement).toBe(
        screen.getByRole("button", {
          name: "Remove filter: Workspace: wsp_00000000000002",
        }),
      ),
    );
    fireEvent.click(await screen.findByRole("button", { name: "Clear all" }));
    await waitFor(() => expect(location()).toBe(`/admin/${key}/events`));
  });
  it("writes the time range, and reloads when the same one is picked", async () => {
    const api = stubEvents({
      "GET /api/admin/events": { body: eventPage([event(1)]) },
    });
    renderAdmin(`/admin/${key}/events?email=ada%40example.com`);
    await screen.findByText("wrong_password · ada@example.com");
    fireEvent.click(screen.getByRole("combobox", { name: /Time range/ }));
    fireEvent.click(await screen.findByRole("option", { name: "Last hour" }));
    await waitFor(() =>
      expect(location()).toBe(
        `/admin/${key}/events?window=1h&email=ada%40example.com`,
      ),
    );
    await waitFor(() => expect(eventRequests(api.sent())).toHaveLength(2));
    fireEvent.click(screen.getByRole("combobox", { name: /Time range/ }));
    fireEvent.click(await screen.findByRole("option", { name: "Last hour" }));
    await waitFor(() => expect(eventRequests(api.sent())).toHaveLength(3));
  });
  // The calendar hands back local dates at local midnight; the range is
  // those days in UTC, east and west of it alike.
  for (const zone of ["Pacific/Kiritimati", "America/Los_Angeles"])
    it(`builds a custom range from the calendar's days in UTC (${zone})`, async () => {
      const tz = process.env.TZ;
      process.env.TZ = zone;
      vi.useFakeTimers({
        toFake: ["Date"],
        now: new Date("2026-11-01T12:00:00Z"),
      });
      try {
        stubEvents({
          "GET /api/admin/events": { body: eventPage([event(1)]) },
        });
        renderAdmin(`/admin/${key}/events`);
        await screen.findByText("wrong_password · ada@example.com");
        fireEvent.click(screen.getByRole("combobox", { name: /Time range/ }));
        fireEvent.click(
          await screen.findByRole("option", { name: "Custom range…" }),
        );
        const dialog = await screen.findByRole("dialog");
        fireEvent.click(
          within(dialog).getByRole("gridcell", { name: /October 20th 2026/ }),
        );
        fireEvent.click(
          within(dialog).getByRole("gridcell", { name: /October 23rd 2026/ }),
        );
        expect(within(dialog).getByText("Oct 20 – Oct 23 UTC")).toBeVisible();
        fireEvent.click(
          within(dialog).getByRole("button", { name: "Apply range" }),
        );
        await waitFor(() =>
          expect(location()).toBe(
            `/admin/${key}/events?from=2026-10-20T00%3A00%3A00Z&to=2026-10-24T00%3A00%3A00Z`,
          ),
        );
        expect(
          await screen.findByRole("combobox", { name: /Oct 20 – Oct 23 UTC/ }),
        ).toBeInTheDocument();
      } finally {
        vi.useRealTimers();
        process.env.TZ = tz;
      }
    });
  it("reaches any bucket from the keyboard: a day and UTC start and end times", async () => {
    vi.useFakeTimers({
      toFake: ["Date"],
      now: new Date("2026-11-01T12:00:00Z"),
    });
    try {
      stubEvents({
        "GET /api/admin/events": { body: eventPage([event(1)]) },
      });
      renderAdmin(`/admin/${key}/events?ip=203.0.113.5`);
      await screen.findByText("wrong_password · ada@example.com");
      fireEvent.click(screen.getByRole("combobox", { name: /Time range/ }));
      fireEvent.click(
        await screen.findByRole("option", { name: "Custom range…" }),
      );
      const dialog = await screen.findByRole("dialog");
      const apply = within(dialog).getByRole("button", { name: "Apply range" });
      expect(apply).toBeDisabled();
      fireEvent.click(
        within(dialog).getByRole("gridcell", { name: /October 20th 2026/ }),
      );
      fireEvent.change(within(dialog).getByLabelText("Start time (UTC)"), {
        target: { value: "14:00" },
      });
      fireEvent.change(within(dialog).getByLabelText("End time (UTC)"), {
        target: { value: "13:30" },
      });
      // An end before the start isn't a range.
      expect(apply).toBeDisabled();
      fireEvent.change(within(dialog).getByLabelText("End time (UTC)"), {
        target: { value: "14:30" },
      });
      expect(
        within(dialog).getByText("Oct 20, 14:00 – 14:30 UTC"),
      ).toBeVisible();
      fireEvent.click(apply);
      await waitFor(() =>
        expect(location()).toBe(
          `/admin/${key}/events?ip=203.0.113.5&from=2026-10-20T14%3A00%3A00Z&to=2026-10-20T14%3A30%3A00Z`,
        ),
      );
      // Opened again, it starts from the applied range's days and times.
      fireEvent.click(screen.getByRole("combobox", { name: /Time range/ }));
      fireEvent.click(
        await screen.findByRole("option", {
          name: /Oct 20, 14:00 – 14:30 UTC/,
        }),
      );
      const again = await screen.findByRole("dialog");
      expect(within(again).getByLabelText("Start time (UTC)")).toHaveValue(
        "14:00",
      );
      expect(within(again).getByLabelText("End time (UTC)")).toHaveValue(
        "14:30",
      );
    } finally {
      vi.useRealTimers();
    }
  });
  it("waits for the catalogue before applying, so the names aren't dropped", async () => {
    let release = () => {};
    const gate = new Promise<void>((resolve) => (release = resolve));
    const api = stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/catalogue": { body: catalogue, gate },
      "GET /api/admin/events": { body: eventPage([event(1)]) },
    });
    renderAdmin(`/admin/${key}/events?ip=203.0.113.5`);
    await screen.findByText("wrong_password · ada@example.com");
    fireEvent.click(screen.getByRole("button", { name: /^Filters/ }));
    const dialog = await screen.findByRole("dialog");
    const apply = within(dialog).getByRole("button", { name: "Apply filters" });
    expect(apply).toBeDisabled();
    expect(eventRequests(api.sent())).toHaveLength(1);
    release();
    await waitFor(() => expect(apply).toBeEnabled());
  });
  it("retries charts kept on an older page by going to the newest", async () => {
    const api = stubEvents({
      "GET /api/admin/events?summary=include": {
        body: eventPage([event(1)], {
          next: "evt_00000000000001",
          summary: { status: "too_slow" },
        }),
      },
      "GET /api/admin/events?before=evt_00000000000001": {
        body: eventPage([event(2, { name: "rate_limit.reached" })]),
      },
    });
    renderAdmin(`/admin/${key}/events`);
    await screen.findByText("wrong_password · ada@example.com");
    fireEvent.click(screen.getByRole("link", { name: "Older" }));
    await waitFor(() =>
      expect(location()).toBe(`/admin/${key}/events?before=evt_00000000000001`),
    );
    await screen.findByText("rate_limit.reached");
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(location()).toBe(`/admin/${key}/events`));
    // Not the older rows again.
    expect(
      eventRequests(api.sent()).filter((u) => u.includes("before=")),
    ).toHaveLength(1);
  });
  it("asks for the summary on the newest page only, and keeps it while paging", async () => {
    const api = stubEvents({
      "GET /api/admin/events?summary=include": {
        body: eventPage([event(1)], {
          next: "evt_00000000000001",
          summary: okSummary(),
        }),
      },
      "GET /api/admin/events?before=evt_00000000000001": {
        body: eventPage([
          event(2, {
            payload: { limit: "auth:per-ip:block" },
            name: "rate_limit.reached",
          }),
        ]),
      },
    });
    renderAdmin(`/admin/${key}/events`);
    await screen.findByText("wrong_password · ada@example.com");
    expect(screen.getByRole("button", { name: "Newer" })).toBeDisabled();
    fireEvent.click(screen.getByRole("link", { name: "Older" }));
    expect(await screen.findByText("auth:per-ip:block")).toBeInTheDocument();
    expect(
      screen.getByRole("table", { name: "Events over time" }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("link", { name: "Newer" }));
    expect(
      await screen.findByText("wrong_password · ada@example.com"),
    ).toBeInTheDocument();
    // The newest page comes back from the cache: no second recorded read.
    expect(eventRequests(api.sent())).toEqual([
      "/api/admin/events?summary=include",
      "/api/admin/events?before=evt_00000000000001",
    ]);
  });
  it("arriving at an older page, says the charts come with the newest", async () => {
    stubEvents({
      "GET /api/admin/events": { body: eventPage([event(2)]) },
    });
    renderAdmin(`/admin/${key}/events?before=evt_00000000000001`);
    expect(
      await screen.findByText("Charts show with the newest events.", {
        exact: false,
      }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Show newest" }));
    await waitFor(() => expect(location()).toBe(`/admin/${key}/events`));
  });
  it("says why there are no charts, with Retry or a shorter range", async () => {
    const api = stubEvents({
      "GET /api/admin/events?window=all&summary=include": {
        body: eventPage([event(1)], {
          window: null,
          summary: { status: "too_wide" },
        }),
      },
      "GET /api/admin/events?summary=include": [
        { body: eventPage([event(1)], { summary: { status: "too_slow" } }) },
        { body: eventPage([event(1)], { summary: { status: "busy" } }) },
      ],
    });
    renderAdmin(`/admin/${key}/events?window=all`);
    expect(
      await screen.findByText("Charts cover 7 days or less.", {
        exact: false,
      }),
    ).toBeInTheDocument();
    expect(screen.getByText("All time, newest first")).toBeVisible();
    fireEvent.click(
      screen.getByRole("button", { name: "Show the last 7 days" }),
    );
    await waitFor(() =>
      expect(location()).toBe(`/admin/${key}/events?window=7d`),
    );
    cleanup();
    renderAdmin(`/admin/${key}/events`);
    expect(
      await screen.findByText(
        "Charts took too long. Add filters or pick a shorter range.",
      ),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(
      await screen.findByText("Charts are busy. Try again in a moment."),
    ).toBeInTheDocument();
    expect(
      eventRequests(api.sent()).filter(
        (u) => u === "/api/admin/events?summary=include",
      ),
    ).toHaveLength(2);
  });
  it("opens a row in place, with its payload as text, without a request", async () => {
    const api = stubEvents({
      "GET /api/admin/events": {
        body: eventPage([
          event(1, { payload: { name: "<script>alert(1)</script>" } }),
        ]),
      },
    });
    renderAdmin(`/admin/${key}/events`);
    const details = await screen.findByRole("button", {
      name: "Details of auth.login_attempted at Oct 30, 12:00:00",
    });
    expect(details).toHaveAttribute("aria-expanded", "false");
    const before = api.sent().length;
    fireEvent.click(details);
    expect(details).toHaveAttribute("aria-expanded", "true");
    const payload = screen.getByText(/"name": "<script>alert\(1\)<\/script>"/);
    expect(payload.tagName).toBe("PRE");
    expect(document.querySelector("pre script")).toBeNull();
    expect(screen.getByRole("link", { name: "Open event" })).toHaveAttribute(
      "href",
      `/admin/${key}/events/evt_00000000000001`,
    );
    expect(api.sent()).toHaveLength(before);
    // An IP keeps the window and filters.
    expect(screen.getByRole("link", { name: "203.0.113.5" })).toHaveAttribute(
      "href",
      `/admin/${key}/events?ip=203.0.113.5`,
    );
  });
  it("shows the loading state, then why nothing is listed", async () => {
    let release = () => {};
    const gate = new Promise<void>((resolve) => (release = resolve));
    stubEvents({
      "GET /api/admin/events": {
        body: eventPage([], {
          summary: {
            ...(okSummary() as Extract<EventSummary, { status: "ok" }>),
            total: 0,
          },
        }),
        gate,
      },
    });
    renderAdmin(`/admin/${key}/events`);
    expect(await screen.findByLabelText("Loading events")).toBeInTheDocument();
    expect(screen.getAllByRole("img", { name: "Loading chart" })).toHaveLength(
      3,
    );
    release();
    expect(
      await screen.findByText("No events in the last 24 hours"),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("table", { name: "Events over time" }),
    ).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: "Show the last 7 days" }),
    );
    await waitFor(() =>
      expect(location()).toBe(`/admin/${key}/events?window=7d`),
    );
  });
  it("says when nothing matches, and keeps an error with Retry", async () => {
    stubEvents({
      "GET /api/admin/events": [
        { status: 400, body: { error: "Check the filters." } },
        { body: eventPage([]) },
      ],
    });
    renderAdmin(`/admin/${key}/events?email=nobody%40example.com`);
    expect(await screen.findByText("Check the filters.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("No events match")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Clear filters" }));
    await waitFor(() => expect(location()).toBe(`/admin/${key}/events`));
  });
});
describe("downloading the Events as CSV", () => {
  it("sends the page's filters, saves the API's file and says how many rows", async () => {
    const saved: Blob[] = [];
    const createObjectURL = vi.fn((blob: Blob) => {
      saved.push(blob);
      return "blob:events";
    });
    const { createObjectURL: realCreate, revokeObjectURL: realRevoke } = URL;
    URL.createObjectURL = createObjectURL;
    URL.revokeObjectURL = vi.fn();
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => {});
    const csv =
      "time,event,name\r\n2026-11-01T12:00:00Z,evt_00000000000001,auth.logged_in\r\n2026-11-01T11:00:00Z,evt_00000000000002,auth.logged_in\r\n";
    const api = stubEvents({
      "GET /api/admin/events": { body: eventPage([event(1)]) },
      "GET /api/admin/events/export": [
        {
          status: 503,
          body: { error: "Exports are busy. Try again in a moment." },
        },
        { text: csv },
      ],
    });
    renderAdmin(`/admin/${key}/events?window=7d&ip=198.51.100.7`);
    const button = await screen.findByRole("button", { name: "Download CSV" });
    fireEvent.click(button);
    expect(
      await screen.findByText("Exports are busy. Try again in a moment."),
    ).toBeInTheDocument();
    expect(saved).toHaveLength(0);
    fireEvent.click(button);
    expect(await screen.findByText("Downloaded 2 events.")).toBeInTheDocument();
    const exports = api
      .sent("GET")
      .filter((r) => r.url.startsWith("/api/admin/events/export"));
    expect(exports.map((r) => r.url)).toEqual([
      "/api/admin/events/export?window=7d&ip=198.51.100.7",
      "/api/admin/events/export?window=7d&ip=198.51.100.7",
    ]);
    expect(exports[1].adminKey).toBe(key);
    expect(saved).toHaveLength(1);
    expect(click).toHaveBeenCalledTimes(1);
    click.mockRestore();
    URL.createObjectURL = realCreate;
    URL.revokeObjectURL = realRevoke;
  });
  it("says rows were left out only when the API says so, not by counting them", async () => {
    const { createObjectURL: realCreate, revokeObjectURL: realRevoke } = URL;
    URL.createObjectURL = vi.fn(() => "blob:events");
    URL.revokeObjectURL = vi.fn();
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => {});
    const header = "time,event,name\r\n";
    const row = "2026-11-01T12:00:00Z,evt_00000000000001,auth.logged_in\r\n";
    stubEvents({
      "GET /api/admin/events": { body: eventPage([event(1)]) },
      "GET /api/admin/events/export": [
        // Exactly 10,000 rows, all there were.
        { text: header + row.repeat(10_000) },
        // Two rows of more: a narrow page of a truncated answer.
        {
          text: header + row.repeat(2),
          headers: { "X-Export-Truncated": "true" },
        },
      ],
    });
    renderAdmin(`/admin/${key}/events?window=7d`);
    const button = await screen.findByRole("button", { name: "Download CSV" });
    fireEvent.click(button);
    expect(
      await screen.findByText("Downloaded 10,000 events."),
    ).toBeInTheDocument();
    fireEvent.click(button);
    expect(
      await screen.findByText(
        "Downloaded the newest 10,000 events. Narrow the filters for the rest.",
      ),
    ).toBeInTheDocument();
    click.mockRestore();
    URL.createObjectURL = realCreate;
    URL.revokeObjectURL = realRevoke;
  });
});
describe("an event's page", () => {
  it("shows the payload as text, markup included, with its deliveries and related events", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/events/evt_00000000000001": {
        body: {
          ...event(1, {
            name: "project.created",
            payload: {
              name: "<script>alert(1)</script>",
              description: "<h1>Hack</h1>",
            },
            user: { id: "usr_00000000000002" },
          }),
          userAgent: "Mozilla/5.0",
          deliveries: [
            {
              listener: "mail.password_reset",
              status: "failed",
              attempts: 6,
              nextAttemptAt: null,
              deliveredAt: null,
              failedAt: "2026-10-30T13:00:00Z",
              lastError: "550 mailbox unavailable",
            },
          ],
          related: [event(2, { name: "auth.password_reset.sent" })],
        },
      },
    });
    renderAdmin(`/admin/${key}/events/evt_00000000000001`);
    const payload = await screen.findByText(
      /"name": "<script>alert\(1\)<\/script>"/,
    );
    expect(payload.tagName).toBe("PRE");
    expect(document.querySelector("pre script, pre h1")).toBeNull();
    expect(screen.getByText("Failed")).toBeInTheDocument();
    expect(screen.getByText("550 mailbox unavailable")).toBeInTheDocument();
    expect(screen.getByText("auth.password_reset.sent")).toBeInTheDocument();
    // A deleted account is its ID alone, without a link.
    expect(screen.getByText("usr_00000000000002")).not.toHaveAttribute("href");
  });
  it("is not found for a malformed ID, without a request", async () => {
    const api = stubApi({ "GET /api/admin/me": { body: me } });
    renderAdmin(`/admin/${key}/events/evt_nope`);
    expect(
      await screen.findByText("This event has wandered off."),
    ).toBeInTheDocument();
    expect(api.sent().map((r) => r.url)).toEqual(["/api/admin/me"]);
  });
});
const mail: AdminMail = {
  window: "24h",
  driver: {
    driver: "smtp",
    host: "smtp.example.com",
    port: 587,
    tls: "starttls",
    fromAddress: "no-reply@example.com",
    fromName: "lewcontext",
  },
  queue: [
    {
      listener: "mail.password_reset",
      due: 1,
      retrying: 2,
      oldestPendingAt: null,
      failed: 3,
      delivered: 40,
    },
    {
      listener: "mail.workspace_invitation",
      due: 0,
      retrying: 0,
      oldestPendingAt: null,
      failed: 0,
      delivered: 5,
    },
  ],
  volume: [],
  requests: [
    { kind: "password_reset", requested: 9, sent: 4 },
    { kind: "registration", requested: 2, sent: 2 },
    { kind: "email_verification", requested: 0, sent: 0 },
  ],
  topRecipients: [
    {
      email: "flood@example.com",
      total: 5,
      resets: 5,
      invitations: 0,
      registrations: 0,
    },
  ],
  lastDeliveredAt: null,
  lastFailure: {
    listener: "mail.password_reset",
    failedAt: "2026-10-30T13:00:00Z",
    error: "dial tcp: refused",
  },
};
describe("the Mail page", () => {
  it("shows the queue, the volume as a table, frequent addresses and the failed tab first", async () => {
    const hour = new Date(
      Math.floor(Date.now() / 3_600_000) * 3_600_000,
    ).toISOString();
    const api = stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/mail": {
        body: {
          ...mail,
          volume: [
            { hour, kind: "password_reset", count: 4 },
            { hour, kind: "invitation", count: 2 },
          ],
        },
      },
      "GET /api/admin/deliveries": { body: { data: [], next: null } },
    });
    renderAdmin(`/admin/${key}/mail`);
    expect(
      await screen.findByText(
        "6 emails sent in the last 24 hours, the most at",
        { exact: false },
      ),
    ).toBeInTheDocument();
    // The chart's numbers, for screen readers: the newest hour's row.
    const table = screen.getByRole("table", { name: "Emails sent per hour" });
    const rows = within(table).getAllByRole("row");
    expect(rows).toHaveLength(25);
    expect(
      within(rows[24])
        .getAllByRole("cell")
        .map((c) => c.textContent),
    ).toEqual(["4", "2", "0", "0", "0", "6"]);
    expect(
      screen.getByRole("link", { name: "flood@example.com" }),
    ).toHaveAttribute("href", `/admin/${key}/events?email=flood%40example.com`);
    const tabs = within(
      screen.getByRole("navigation", { name: "Delivery status" }),
    ).getAllByRole("link");
    expect(tabs.map((t) => t.textContent)).toEqual([
      "Failed (3)",
      "All",
      "Retrying",
      "Pending",
      "Delivered",
    ]);
    expect(screen.getByText("dial tcp: refused")).toBeInTheDocument();
    expect(screen.queryByText(/password/i, { selector: "dd" })).toBeNull();
    expect(
      api
        .sent()
        .map((r) => r.url)
        .sort(),
    ).toEqual(["/api/admin/deliveries", "/api/admin/mail", "/api/admin/me"]);
  });
  it("loads the window and status from the URL", async () => {
    const api = stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/mail": { body: { ...mail, window: "7d" } },
      "GET /api/admin/deliveries": { body: { data: [], next: null } },
    });
    renderAdmin(`/admin/${key}/mail?window=7d&status=retrying`);
    expect(
      await screen.findByText("No retrying deliveries"),
    ).toBeInTheDocument();
    expect(api.sent().map((r) => r.url)).toEqual(
      expect.arrayContaining([
        "/api/admin/mail?window=7d",
        "/api/admin/deliveries?status=retrying",
      ]),
    );
  });
});
