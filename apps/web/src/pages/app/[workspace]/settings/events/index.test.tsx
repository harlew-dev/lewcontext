import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { SWRConfig } from "swr";
import { describe, expect, it } from "vitest";
import WorkspaceSettingsLayout from "@/pages/app/[workspace]/settings/layout";
import EventIndex from "@/pages/app/[workspace]/settings/events/index";
import { SessionProvider } from "@/lib/session";
import { WorkspaceProvider } from "@/lib/workspace";
import { stubApi } from "@/test/api";
import { pick } from "@/test/pick";
import { testSession } from "@/test/session";
import { testWorkspace } from "@/test/workspace";
import type { EventPage, WorkspaceEvent } from "@/types/events";
import type { Role } from "@/types/workspaces";
const session = testSession();
const events = "/api/workspaces/wsp_2yEHYLwiwPVhLW/events";
const members = "/api/workspaces/wsp_2yEHYLwiwPVhLW/members";
const projects = "/api/workspaces/wsp_2yEHYLwiwPVhLW/projects";
const ada = { id: "usr_00000000000001", name: "Ada Lovelace" };
const noRefs = {
  actor: null,
  subject: null,
  account: null,
  project: null,
  key: null,
};
const renamed: WorkspaceEvent = {
  id: "evt_00000000000001",
  name: "project.updated",
  createdAt: "2026-10-31T14:05:09Z",
  actor: ada,
  subject: null,
  requestId: "req_00000000000001",
  details: {
    project: "prj_00000000000001",
    changes: { name: { from: "Website", to: "<b>Marketing</b>" } },
  },
};
const removed: WorkspaceEvent = {
  id: "evt_00000000000002",
  name: "workspace.member.removed",
  createdAt: "2026-10-31T13:00:00Z",
  actor: null,
  apiKey: "apk_00000000000001",
  subject: { id: "usr_00000000000002", name: "Bob Member" },
  requestId: "",
  details: { user: "usr_00000000000002", role: "member" },
};
function page(data: WorkspaceEvent[], next: string | null = null): EventPage {
  return { data, next, refs: noRefs };
}
const memberPage = {
  data: [{ ...ada, email: "ada@example.test", role: "owner", addedAt: "" }],
  pages: { current: 1, total: 1, perPage: 100, items: 1 },
};
function Where() {
  const location = useLocation();
  return (
    <output data-testid="where">{location.pathname + location.search}</output>
  );
}
function renderAs(role: Role, at = "/app/wsp_2yEHYLwiwPVhLW/settings/events") {
  const workspace = testWorkspace(role);
  return render(
    <SWRConfig value={{ provider: () => new Map() }}>
      <SessionProvider session={session}>
        <WorkspaceProvider
          workspace={workspace}
          first={{ data: [workspace], pages: memberPage.pages }}
        >
          <MemoryRouter initialEntries={[at]}>
            <Where />
            <Routes>
              <Route
                path="/app/:workspace/settings"
                element={<WorkspaceSettingsLayout />}
              >
                <Route path="events" element={<EventIndex />} />
              </Route>
            </Routes>
          </MemoryRouter>
        </WorkspaceProvider>
      </SessionProvider>
    </SWRConfig>,
  );
}
const emptyProjects = {
  data: [{ id: "prj_00000000000001", name: "Website" }],
  pages: { current: 1, total: 1, perPage: 100, items: 1 },
};
function stubLogs(list: unknown) {
  return stubApi({
    [`GET ${events}`]: { body: list },
    [`GET ${members}`]: { body: memberPage },
    [`GET ${projects}`]: { body: emptyProjects },
  });
}
const apiUrls = (api: ReturnType<typeof stubApi>) =>
  api.sent("GET").map((s) => s.url);
describe("workspace logs", () => {
  it.each(["member", "viewer"] as const)(
    "tells a %s they can't see the logs, without asking the API",
    (role) => {
      const api = stubApi({});
      renderAs(role);
      expect(
        screen.getByText(
          "Only owners and admins can see this workspace’s logs.",
        ),
      ).toBeInTheDocument();
      expect(screen.queryByRole("link", { name: "Logs" })).toBeNull();
      expect(api.sent()).toHaveLength(0);
    },
  );
  it.each(["owner", "admin"] as const)(
    "shows an %s the changes, who made them linking to the member",
    async (role) => {
      stubLogs(page([renamed, removed]));
      renderAs(role);
      expect(await screen.findByText("updated a project")).toBeInTheDocument();
      expect(screen.getByRole("link", { name: "Logs" })).toBeInTheDocument();
      // A name typed by a member is text, not markup.
      expect(screen.getByText(/<b>Marketing<\/b>/)).toBeInTheDocument();
      expect(document.querySelector("b")).toBeNull();
      // Who did it links to their page; a key's write names the key.
      expect(
        screen.getByRole("link", { name: "Ada Lovelace" }),
      ).toHaveAttribute(
        "href",
        "/app/wsp_2yEHYLwiwPVhLW/settings/members/usr_00000000000001",
      );
      expect(
        screen.getByText("API key apk_00000000000001"),
      ).toBeInTheDocument();
      expect(
        screen.getByText(/removed Bob Member \(member\) from the workspace/),
      ).toBeInTheDocument();
      expect(screen.getByText("Oct 31, 2026, 2:05 PM")).toBeInTheDocument();
    },
  );
  it("opens a change to its details with its request ID, loading nothing", async () => {
    const api = stubLogs(page([renamed]));
    renderAs("owner");
    await screen.findByText("updated a project");
    const before = api.fetch.mock.calls.length;
    fireEvent.click(
      screen.getByRole("button", {
        name: /Details: Ada Lovelace updated a project/,
      }),
    );
    const list = screen.getByText("Name changed").closest("dl") as HTMLElement;
    expect(
      within(list).getByText("Website → <b>Marketing</b>"),
    ).toBeInTheDocument();
    expect(within(list).getByText("prj_00000000000001")).toBeInTheDocument();
    expect(within(list).getByText("Request ID")).toBeInTheDocument();
    expect(within(list).getByText("req_00000000000001")).toBeInTheDocument();
    expect(api.fetch.mock.calls.length).toBe(before);
  });
  it("opens a change's page from its time", async () => {
    stubLogs(page([renamed]));
    renderAs("owner");
    expect(
      await screen.findByRole("link", { name: "Oct 31, 2026, 2:05 PM" }),
    ).toHaveAttribute(
      "href",
      "/app/wsp_2yEHYLwiwPVhLW/settings/events/evt_00000000000001",
    );
  });
  it("reads its filters from the URL and asks the API for them", async () => {
    const api = stubLogs(page([renamed]));
    renderAs(
      "owner",
      "/app/wsp_2yEHYLwiwPVhLW/settings/events?type=projects,workspace&q=api%20key&window=30d&actor=usr_00000000000001&project=prj_00000000000001&before=evt_00000000000009&junk=1",
    );
    await screen.findByText("updated a project");
    expect(apiUrls(api)).toContain(
      `${events}?name=projects%2Cworkspace&actor=usr_00000000000001&project=prj_00000000000001&q=api+key&window=30d&before=evt_00000000000009`,
    );
    // Every filter, including the date, is a badge.
    const applied = screen.getByRole("list", { name: "Applied filters" });
    expect(
      within(applied).getByText("Type: Projects, Workspace"),
    ).toBeVisible();
    expect(within(applied).getByText("Person: A person")).toBeVisible();
    expect(within(applied).getByText("Date: Last 30 days")).toBeVisible();
    expect(within(applied).getByText("Search: api key")).toBeVisible();
    expect(screen.getByRole("button", { name: /Filters/ })).toHaveTextContent(
      "5",
    );
  });
  it("takes a custom range from the URL", async () => {
    const api = stubLogs(page([renamed]));
    renderAs(
      "owner",
      "/app/wsp_2yEHYLwiwPVhLW/settings/events?from=2026-10-01T00:00:00Z&to=2026-10-03T00:00:00Z",
    );
    await screen.findByText("updated a project");
    expect(apiUrls(api)).toContain(
      `${events}?from=2026-10-01T00%3A00%3A00Z&to=2026-10-03T00%3A00%3A00Z`,
    );
    expect(screen.getByText("Date: Oct 1 – Oct 2 UTC")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: /Filters/ }));
    const dialog = await screen.findByRole("dialog", {
      name: "Filter changes",
    });
    expect(
      within(dialog).getByRole("combobox", { name: "Date" }),
    ).toHaveTextContent("Oct 1 – Oct 2 UTC");
  });
  it("keeps the date badge when nothing else is applied", async () => {
    stubLogs(page([renamed]));
    renderAs("owner");
    await screen.findByText("updated a project");
    expect(screen.getByText("Date: Last 7 days")).toBeVisible();
  });
  it("opens a custom date from inside the Filters dialog", async () => {
    stubLogs(page([renamed]));
    renderAs("owner");
    await screen.findByText("updated a project");
    fireEvent.click(screen.getByRole("button", { name: /Filters/ }));
    const dialog = await screen.findByRole("dialog", {
      name: "Filter changes",
    });
    fireEvent.click(within(dialog).getByRole("combobox", { name: "Date" }));
    pick(await screen.findByRole("option", { name: "Custom range…" }));
    expect(
      await screen.findByRole("dialog", { name: "Custom range" }),
    ).toBeInTheDocument();
  });
  it("removes a filter from its badge, or all of them, and goes back to the newest", async () => {
    stubLogs(page([renamed]));
    renderAs(
      "owner",
      "/app/wsp_2yEHYLwiwPVhLW/settings/events?type=projects&q=site&before=evt_00000000000009",
    );
    await screen.findByText("updated a project");
    fireEvent.click(
      screen.getByRole("button", { name: "Remove filter: Search: site" }),
    );
    expect(screen.getByTestId("where").textContent).toBe(
      "/app/wsp_2yEHYLwiwPVhLW/settings/events?type=projects",
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Remove filter: Type: Projects" }),
    );
    expect(screen.getByTestId("where").textContent).toBe(
      "/app/wsp_2yEHYLwiwPVhLW/settings/events",
    );
  });
  it("clears every filter at once, including the date", async () => {
    stubLogs(page([renamed]));
    renderAs(
      "owner",
      "/app/wsp_2yEHYLwiwPVhLW/settings/events?type=projects&q=site&window=30d",
    );
    await screen.findByText("updated a project");
    fireEvent.click(screen.getByRole("button", { name: "Clear all" }));
    expect(screen.getByTestId("where").textContent).toBe(
      "/app/wsp_2yEHYLwiwPVhLW/settings/events",
    );
  });
  it("applies the Filters dialog's person and drops the cursor", async () => {
    stubLogs(page([renamed]));
    renderAs(
      "owner",
      "/app/wsp_2yEHYLwiwPVhLW/settings/events?before=evt_00000000000009",
    );
    await screen.findByText("updated a project");
    fireEvent.click(screen.getByRole("button", { name: /Filters/ }));
    const dialog = await screen.findByRole("dialog", {
      name: "Filter changes",
    });
    fireEvent.click(within(dialog).getAllByRole("combobox")[1]);
    pick(await screen.findByRole("option", { name: "Ada Lovelace" }));
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Apply filters" }),
    );
    expect(screen.getByTestId("where").textContent).toBe(
      "/app/wsp_2yEHYLwiwPVhLW/settings/events?actor=usr_00000000000001",
    );
  });
  it("ignores values the API would refuse", async () => {
    const api = stubLogs(page([]));
    renderAs(
      "owner",
      "/app/wsp_2yEHYLwiwPVhLW/settings/events?type=admin&actor=ada&window=1h&project=x&from=yesterday",
    );
    await screen.findByText("No changes in this period");
    expect(apiUrls(api)).toContain(events);
  });
  it("says why it is empty, and clears the filters", async () => {
    stubLogs(page([]));
    renderAs("owner", "/app/wsp_2yEHYLwiwPVhLW/settings/events?q=nothing");
    expect(
      await screen.findByText("No changes match these filters"),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Clear filters" }));
    expect(screen.getByTestId("where").textContent).toBe(
      "/app/wsp_2yEHYLwiwPVhLW/settings/events",
    );
  });
  it("offers Older when there is a next page", async () => {
    stubLogs(page([renamed], "evt_00000000000001"));
    renderAs("owner");
    const older = await screen.findByRole("link", { name: /Older/ });
    expect(older.getAttribute("href")).toMatch(/\?before=evt_00000000000001$/);
  });
  it("shows a failure with Try again", async () => {
    stubApi({
      [`GET ${events}`]: [
        { status: 500, body: { error: "Something went wrong." } },
        { body: page([renamed]) },
      ],
      [`GET ${members}`]: { body: memberPage },
      [`GET ${projects}`]: { body: emptyProjects },
    });
    renderAs("owner");
    fireEvent.click(await screen.findByRole("button", { name: "Try again" }));
    expect(await screen.findByText("updated a project")).toBeInTheDocument();
  });
});
