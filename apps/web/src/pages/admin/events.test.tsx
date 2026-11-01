import { fireEvent, render, screen, within } from "@testing-library/react";
import { createRoutesStub, Outlet, useLocation } from "react-router";
import { SWRConfig } from "swr";
import { describe, expect, it } from "vitest";
import { adminLoader } from "@/lib/admin";
import { stubApi, type Sent } from "@/test/api";
import { testSession } from "@/test/session";
import type { AdminEvent, AdminMail } from "@/types/admin";
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
    "workspace.created",
    "workspace.member.removed",
  ],
  limits: ["auth:per-ip"],
  listeners: ["mail.password_reset"],
};
function event(n: number, overrides: Partial<AdminEvent> = {}): AdminEvent {
  return {
    id: `evt_0000000000000${n}`,
    name: "auth.login_attempted",
    createdAt: "2026-10-30T12:00:00Z",
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
describe("the Events page", () => {
  it("reads its filters from the URL and writes them back when applied", async () => {
    const api = stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/catalogue": { body: catalogue },
      "GET /api/admin/events": { body: { data: [event(1)], next: null } },
    });
    renderAdmin(`/admin/${key}/events?account=usr_00000000000001&ip=nope`);
    expect(
      await screen.findByText("wrong_password · ada@example.com"),
    ).toBeInTheDocument();
    // A value the API would refuse is left out, not sent.
    expect(eventRequests(api.sent())).toEqual([
      "/api/admin/events?account=usr_00000000000001",
    ]);
    expect(screen.getByLabelText("Account")).toHaveValue("usr_00000000000001");
    expect(screen.getByRole("link", { name: "203.0.113.5" })).toHaveAttribute(
      "href",
      `/admin/${key}/events?ip=203.0.113.5`,
    );

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: " ADA@example.com " },
    });
    fireEvent.change(screen.getByLabelText("From (UTC)"), {
      target: { value: "2026-10-30" },
    });
    fireEvent.change(screen.getByLabelText("To (UTC)"), {
      target: { value: "2026-10-30" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply filters" }));
    const applied = `/admin/${key}/events?account=usr_00000000000001&email=ada%40example.com&from=2026-10-30T00%3A00%3A00Z&to=2026-10-31T00%3A00%3A00Z`;
    expect(await screen.findByText(applied)).toBeInTheDocument();
    expect(eventRequests(api.sent()).at(-1)).toBe(
      `/api/admin/events${applied.slice(applied.indexOf("?"))}`,
    );
  });
  it("refuses a malformed filter on its field, without loading", async () => {
    const api = stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/catalogue": { body: catalogue },
      "GET /api/admin/events": { body: { data: [event(1)], next: null } },
    });
    renderAdmin(`/admin/${key}/events`);
    await screen.findByText("wrong_password · ada@example.com");
    fireEvent.change(screen.getByLabelText("Workspace"), {
      target: { value: "acme" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply filters" }));
    expect(
      await screen.findByText("A workspace ID starts with wsp_."),
    ).toBeInTheDocument();
    expect(eventRequests(api.sent())).toEqual(["/api/admin/events"]);
  });
  it("pages older and back newer by the cursor", async () => {
    const api = stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/catalogue": { body: catalogue },
      "GET /api/admin/events": {
        body: { data: [event(1)], next: "evt_00000000000001" },
      },
      "GET /api/admin/events?before=evt_00000000000001": {
        body: {
          data: [
            event(2, {
              payload: { limit: "auth:per-ip:block" },
              name: "rate_limit.reached",
            }),
          ],
          next: null,
        },
      },
    });
    renderAdmin(`/admin/${key}/events`);
    await screen.findByText("wrong_password · ada@example.com");
    expect(screen.getByRole("button", { name: "Newer" })).toBeDisabled();
    fireEvent.click(screen.getByRole("link", { name: "Older" }));
    expect(await screen.findByText("auth:per-ip:block")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Older" })).toBeDisabled();
    fireEvent.click(screen.getByRole("link", { name: "Newer" }));
    expect(
      await screen.findByText("wrong_password · ada@example.com"),
    ).toBeInTheDocument();
    // The newest page comes back from the cache: no second recorded read.
    expect(eventRequests(api.sent())).toEqual([
      "/api/admin/events",
      "/api/admin/events?before=evt_00000000000001",
    ]);
  });
  it("says when nothing matches, and keeps an error with Retry", async () => {
    stubApi({
      "GET /api/admin/me": { body: me },
      "GET /api/admin/catalogue": { body: catalogue },
      "GET /api/admin/events": [
        { status: 400, body: { error: "Check the filters." } },
        { body: { data: [], next: null } },
      ],
    });
    renderAdmin(`/admin/${key}/events?email=nobody%40example.com`);
    expect(await screen.findByText("Check the filters.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("No events match")).toBeInTheDocument();
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
