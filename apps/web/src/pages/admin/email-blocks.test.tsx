import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { createRoutesStub, Outlet } from "react-router";
import { SWRConfig } from "swr";
import { describe, expect, it } from "vitest";
import { adminLoader } from "@/lib/admin";
import { gate, stubApi, type Reply } from "@/test/api";
import { testSession } from "@/test/session";
import {
  adminEmailBlockSortable,
  type AdminEmailBlock,
  type AdminRole,
  type AdminSession,
} from "@/types/admin";
import AdminEmailBlockIndex from "./[key]/email-blocks";
import AdminLayoutRoute from "./layout";
// The Email blocks page (plans/features/implemented/admin/email-blocks.md):
// the list's states, and Unblock behind a reason and, when the API asks, a
// fresh code (AdminActionDialog).
const key = "8a1f0c52-6d3e-4b7a-9c2d-5e8f1a3b7c90";
const admin = testSession({
  user: { id: "usr_00000000000009", name: "Olive Admin" },
});
function me(role: AdminRole): AdminSession {
  return { user: admin.user, role, csrfToken: "admin-csrf", expiresAt: "" };
}
const list = "/api/admin/email-blocks";
const blocks: AdminEmailBlock[] = [
  {
    email: "ada@example.com",
    createdAt: "2026-10-30T12:00:00Z",
    linkExpiresAt: "2027-10-31T09:00:00Z",
  },
  {
    email: "bob@example.com",
    createdAt: "2025-01-02T12:00:00Z",
    linkExpiresAt: null,
  },
];
function pageOf(data: AdminEmailBlock[]) {
  return {
    data,
    pages: { current: 1, total: 1, perPage: 20, items: data.length },
  };
}
const reason = "The owner asked support to get invitations again.";
const stepUp = {
  status: 403,
  body: { error: "Enter a code to continue.", stepUp: true },
};
function renderPage(
  path: string,
  replies: Record<string, Reply | Reply[]>,
  role: AdminRole = "operator",
) {
  const api = stubApi({ "GET /api/admin/me": { body: me(role) }, ...replies });
  const Stub = createRoutesStub([
    {
      path: "/",
      Component: Outlet,
      children: [
        // The sign-in page, where an ended admin session lands.
        { path: "admin/:key", Component: () => <p>Admin sign-in page</p> },
        {
          Component: AdminLayoutRoute,
          loader: ({ params }) => adminLoader(params.key!),
          children: [
            {
              path: "admin/:key/email-blocks",
              Component: AdminEmailBlockIndex,
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
  return api;
}
async function openUnblock(email: string) {
  const row = (await screen.findByText(email)).closest("tr") as HTMLElement;
  fireEvent.click(
    within(row).getByRole("button", { name: `Actions for ${email}` }),
  );
  fireEvent.click(await screen.findByRole("menuitem", { name: "Unblock" }));
  return screen.findByRole("dialog");
}
function typeReason(dialog: HTMLElement, value: string) {
  fireEvent.change(within(dialog).getByLabelText("Reason"), {
    target: { value },
  });
}
describe("the Email blocks list", () => {
  it("shows a skeleton, then each address with when it was blocked and whether its link works", async () => {
    const held = gate();
    renderPage(`/admin/${key}/email-blocks`, {
      [`GET ${list}`]: { gate: held.promise, body: pageOf(blocks) },
    });
    expect(
      await screen.findByLabelText("Loading email blocks"),
    ).toBeInTheDocument();
    held.release();
    const [ada, bob] = (await screen.findAllByRole("row")).slice(1);
    expect(ada).toHaveTextContent("ada@example.com");
    expect(ada).toHaveTextContent("October 30, 2026");
    expect(ada).toHaveTextContent("Works until October 31, 2027");
    expect(within(bob).getByText("Expired")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Email blocks" }),
    ).toBeInTheDocument();
  });
  it("says so when nothing is blocked", async () => {
    renderPage(`/admin/${key}/email-blocks`, {
      [`GET ${list}`]: { body: pageOf([]) },
    });
    expect(await screen.findByText("No blocked addresses")).toBeInTheDocument();
  });
  it("names the search that matched nothing", async () => {
    const api = renderPage(`/admin/${key}/email-blocks?q=zed`, {
      [`GET ${list}?q=zed`]: { body: pageOf([]) },
    });
    expect(
      await screen.findByText("No addresses match “zed”"),
    ).toBeInTheDocument();
    expect(api.sent("GET").map((r) => r.url)).toContain(`${list}?q=zed`);
  });
  it("keeps the rows when the reload after unblocking fails, with Try again", async () => {
    renderPage(`/admin/${key}/email-blocks`, {
      [`GET ${list}`]: [
        { body: pageOf(blocks) },
        { status: 500, body: { error: "Something went wrong." } },
        { body: pageOf(blocks.slice(1)) },
      ],
      // The step-up window is open already: no code asked for.
      [`DELETE ${list}`]: { status: 204 },
    });
    const dialog = await openUnblock("ada@example.com");
    typeReason(dialog, reason);
    fireEvent.click(within(dialog).getByRole("button", { name: "Unblock" }));
    expect(
      await screen.findByText("ada@example.com unblocked."),
    ).toBeInTheDocument();
    expect(
      await screen.findByText("Something went wrong.", {}, { timeout: 15000 }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("row", { name: /ada@example.com/ }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() =>
      expect(
        screen.queryByRole("row", { name: /ada@example.com/ }),
      ).not.toBeInTheDocument(),
    );
    expect(screen.getByText("bob@example.com")).toBeInTheDocument();
  }, 20000);
  it("links each address to its activity, and offers a viewer no Unblock", async () => {
    renderPage(
      `/admin/${key}/email-blocks`,
      { [`GET ${list}`]: { body: pageOf(blocks) } },
      "viewer",
    );
    const row = (await screen.findByText("ada@example.com")).closest(
      "tr",
    ) as HTMLElement;
    fireEvent.click(
      within(row).getByRole("button", { name: "Actions for ada@example.com" }),
    );
    const activity = await screen.findByRole("menuitem", {
      name: "Show activity",
    });
    expect(activity).toHaveAttribute(
      "href",
      `/admin/${key}/events?email=ada%40example.com`,
    );
    expect(
      screen.queryByRole("menuitem", { name: "Unblock" }),
    ).not.toBeInTheDocument();
  });
  it("mirrors the API's sort allowlist", () => {
    // Vitest runs from apps/web.
    const go = readFileSync(
      resolve(process.cwd(), "../api/internal/store/admin_email_blocks.go"),
      "utf8",
    );
    const map = go.match(
      /var adminEmailBlockSorts = map\[string\]string\{([^}]*)\}/,
    );
    const keys = [...(map?.[1] ?? "").matchAll(/"(-?\w+)":/g)]
      .map((m) => m[1])
      .sort();
    const both = adminEmailBlockSortable.flatMap((f) => [f, `-${f}`]).sort();
    expect(keys).toEqual(both);
  });
});
describe("unblocking", () => {
  it("needs a reason, asks for a code when the API does, steps up and tries again", async () => {
    const api = renderPage(`/admin/${key}/email-blocks`, {
      [`GET ${list}`]: [
        { body: pageOf(blocks) },
        { body: pageOf([blocks[1]]) },
      ],
      [`DELETE ${list}`]: [stepUp, { status: 204 }],
      "POST /api/admin/step-up": { status: 204 },
    });
    const dialog = await openUnblock("ada@example.com");
    expect(within(dialog).getByRole("heading")).toHaveTextContent(
      "Unblock ada@example.com?",
    );
    const confirm = within(dialog).getByRole("button", { name: "Unblock" });
    // The reason is required: 10 characters at least, once trimmed.
    expect(confirm).toBeDisabled();
    typeReason(dialog, "   too short   ");
    expect(confirm).toBeDisabled();
    typeReason(dialog, reason);
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    // The API wants a fresh code: the dialog asks for one, and waits for it.
    const code = await within(dialog).findByLabelText("Authentication code");
    // The code says why it's asked for (its description), and has focus.
    expect(code).toHaveAccessibleDescription(
      "Enter a code to continue. It lets you take actions for the next 5 minutes.",
    );
    expect(code).toHaveFocus();
    expect(confirm).toBeDisabled();
    fireEvent.change(code, { target: { value: "123456" } });
    fireEvent.click(confirm);
    expect(
      await screen.findByText("ada@example.com unblocked."),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    // The step-up, then the action again, each with the key and the admin
    // CSRF token; the address only ever in a body, never a URL.
    const writes = api.sent().filter((r) => r.method !== "GET");
    expect(writes).toEqual([
      expect.objectContaining({
        method: "DELETE",
        url: list,
        body: { email: "ada@example.com", reason },
        csrf: "admin-csrf",
        adminKey: key,
      }),
      expect.objectContaining({
        method: "POST",
        url: "/api/admin/step-up",
        body: { code: "123456" },
        csrf: "admin-csrf",
      }),
      expect.objectContaining({
        method: "DELETE",
        url: list,
        body: { email: "ada@example.com", reason },
      }),
    ]);
    expect(api.sent().some((r) => r.url.includes("ada"))).toBe(false);
    // The list reloads (one more recorded read).
    await waitFor(() =>
      expect(screen.queryByText("ada@example.com")).not.toBeInTheDocument(),
    );
    expect(api.sent("GET").filter((r) => r.url === list)).toHaveLength(2);
  });
  it("shows a wrong code on its field, empties it, and keeps the dialog", async () => {
    renderPage(`/admin/${key}/email-blocks`, {
      [`GET ${list}`]: { body: pageOf(blocks) },
      [`DELETE ${list}`]: stepUp,
      "POST /api/admin/step-up": {
        status: 422,
        body: { error: "That code didn’t work.", field: "code" },
      },
    });
    const dialog = await openUnblock("ada@example.com");
    typeReason(dialog, reason);
    const confirm = within(dialog).getByRole("button", { name: "Unblock" });
    fireEvent.click(confirm);
    fireEvent.change(
      await within(dialog).findByLabelText("Authentication code"),
      { target: { value: "000000" } },
    );
    fireEvent.click(confirm);
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "That code didn’t work.",
    );
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(within(dialog).getByLabelText("Authentication code")).toHaveValue(
      "",
    );
  });
  it("keeps a step-up's 429 in the dialog, with the reason and code", async () => {
    renderPage(`/admin/${key}/email-blocks`, {
      [`GET ${list}`]: { body: pageOf(blocks) },
      [`DELETE ${list}`]: stepUp,
      "POST /api/admin/step-up": {
        status: 429,
        body: { error: "Too many attempts. Try again in 12 minutes." },
      },
    });
    const dialog = await openUnblock("ada@example.com");
    typeReason(dialog, reason);
    const confirm = within(dialog).getByRole("button", { name: "Unblock" });
    fireEvent.click(confirm);
    const code = await within(dialog).findByLabelText("Authentication code");
    fireEvent.change(code, { target: { value: "123456" } });
    fireEvent.click(confirm);
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "Too many attempts. Try again in 12 minutes.",
    );
    expect(code).toHaveValue("123456");
    expect(within(dialog).getByLabelText("Reason")).toHaveValue(reason);
  });
  it("goes back to the sign-in when the step-up ends the session (401)", async () => {
    renderPage(`/admin/${key}/email-blocks`, {
      [`GET ${list}`]: { body: pageOf(blocks) },
      [`DELETE ${list}`]: stepUp,
      "POST /api/admin/step-up": {
        status: 401,
        body: { error: "Too many wrong codes. Sign in again." },
      },
    });
    const dialog = await openUnblock("ada@example.com");
    typeReason(dialog, reason);
    const confirm = within(dialog).getByRole("button", { name: "Unblock" });
    fireEvent.click(confirm);
    fireEvent.change(
      await within(dialog).findByLabelText("Authentication code"),
      { target: { value: "000000" } },
    );
    fireEvent.click(confirm);
    expect(await screen.findByText("Admin sign-in page")).toBeInTheDocument();
  });
  it("reloads the list when the address was unblocked meanwhile (404), keeping the error", async () => {
    const api = renderPage(`/admin/${key}/email-blocks`, {
      [`GET ${list}`]: [
        { body: pageOf(blocks) },
        { body: pageOf([blocks[1]]) },
      ],
      [`DELETE ${list}`]: {
        status: 404,
        body: { error: "This address isn't blocked." },
      },
    });
    const dialog = await openUnblock("ada@example.com");
    typeReason(dialog, reason);
    fireEvent.click(within(dialog).getByRole("button", { name: "Unblock" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "This address isn't blocked.",
    );
    await waitFor(() =>
      expect(api.sent("GET").filter((r) => r.url === list)).toHaveLength(2),
    );
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });
  it("shows the action as pending, then keeps the API's refusal in the dialog", async () => {
    const held = gate();
    const api = renderPage(`/admin/${key}/email-blocks`, {
      [`GET ${list}`]: { body: pageOf(blocks) },
      [`DELETE ${list}`]: {
        status: 404,
        body: { error: "This address isn't blocked." },
        gate: held.promise,
      },
    });
    const dialog = await openUnblock("ada@example.com");
    typeReason(dialog, reason);
    const confirm = within(dialog).getByRole("button", { name: "Unblock" });
    fireEvent.click(confirm);
    fireEvent.click(confirm);
    await waitFor(() => expect(confirm).toHaveAttribute("data-loading"));
    expect(within(dialog).getByLabelText("Reason")).toBeDisabled();
    held.release();
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "This address isn't blocked.",
    );
    // One request, however many clicks; the reason is kept.
    expect(api.sent("DELETE")).toHaveLength(1);
    expect(within(dialog).getByLabelText("Reason")).toHaveValue(reason);
  });
});
