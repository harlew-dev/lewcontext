import { expect, test, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
const stamp = Date.now();
const email = `e2e-${stamp}@forma.test`;
const returnEmail = `e2e-${stamp}-next@forma.test`;
const scrollEmail = `e2e-${stamp}-scroll@forma.test`;
const memberEmail = `e2e-${stamp}-member@forma.test`;
const ownerEmail = `e2e-${stamp}-owner@forma.test`;
const emails = [email, returnEmail, scrollEmail, memberEmail, ownerEmail];
const password = "A thoughtful test passphrase!";
const workspaceUrl = /\/app\/(wsp_[0-9A-Za-z]{14})\/dashboard$/;
// Runs SQL against the test database and returns its output; only for setup
// the UI can't do yet (invites) and cleanup.
function sql(statement: string): string {
  return execFileSync(
    "psql",
    [
      process.env.TEST_DATABASE_URL!,
      "-v",
      "ON_ERROR_STOP=1",
      "-Atc",
      statement,
    ],
    { encoding: "utf8" },
  ).trim();
}
const quoted = emails.map((e) => `'${e}'`).join(", ");
test.afterAll(() => {
  // Only remove the uniquely named accounts this run created, and the
  // workspaces they belong to. Cascades remove projects, memberships and sessions.
  if (process.env.TEST_DATABASE_URL)
    sql(
      `DELETE FROM workspaces WHERE id IN (SELECT m.workspace_id FROM workspace_user m JOIN users u ON u.id = m.user_id WHERE u.email IN (${quoted})); DELETE FROM users WHERE email IN (${quoted})`,
    );
});
// The workspace ID in the current URL.
function workspaceOf(page: Page): string {
  return new URL(page.url()).pathname.split("/")[2];
}
// Headers for API mutations from the page's signed-in browser context.
async function apiHeaders(page: Page) {
  const me = await (await page.request.get("/api/auth/me")).json();
  return {
    Origin: new URL(page.url()).origin,
    "Content-Type": "application/json",
    "X-CSRF-Token": me.csrfToken,
  };
}
async function register(page: Page, address: string, name: string) {
  await page.goto("/auth/register");
  await page.getByLabel("Email").fill(address);
  await page.getByLabel("Full name").fill(name);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page).toHaveURL("/app/onboarding");
}
async function onboard(page: Page, workspace: string) {
  await expect(page).toHaveURL("/app/onboarding");
  await page.getByLabel("Name").fill(workspace);
  await page.getByRole("button", { name: "Create workspace" }).click();
  await expect(page).toHaveURL(workspaceUrl);
  return workspaceOf(page);
}
test("register, create, loading, filter, edit, paginate, SSR, security, mobile, logout, and login", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/");
  await expect(page).toHaveURL("/auth/login?next=%2Fapp");
  await page.getByRole("link", { name: "Sign up" }).click();
  await expect(page).toHaveURL("/auth/register?next=%2Fapp");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Full name").fill("Taylor Example");
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Create account" }).click();
  // A new account starts at onboarding, and can't skip it.
  await expect(page).toHaveURL("/app/onboarding");
  await expect(page.getByText(`Signed in as ${email}`)).toBeVisible();
  await page.goto("/app");
  await expect(page).toHaveURL("/app/onboarding");
  await expect(
    page.getByRole("button", { name: "Create workspace" }),
  ).toBeDisabled();
  const ws = await onboard(page, "Taylor’s studio");
  const dashboard = `/app/${ws}/dashboard`;
  await expect(
    page.getByRole("button", { name: "Workspace menu" }),
  ).toContainText("Taylor’s studio");
  // With a workspace, onboarding moves on.
  await page.goto("/app/onboarding");
  await expect(page).toHaveURL(dashboard);
  await expect(
    page.getByRole("heading", { name: "Welcome back, Taylor." }),
  ).toBeVisible();
  await expect(page.getByText("Room for your next big thing")).toBeVisible();
  await page.getByRole("button", { name: "New project" }).click();
  const dialog = page.getByRole("dialog");
  await dialog
    .getByLabel("Project name", { exact: true })
    .fill("Launch the good stuff");
  await dialog
    .getByLabel("Description")
    .fill("A real project stored in PostgreSQL.");
  let release: () => void = () => {};
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route(`**/api/workspaces/${ws}/projects`, async (route) => {
    if (route.request().method() === "POST") await gate;
    await route.continue();
  });
  await dialog.getByRole("button", { name: "Create project" }).click();
  await expect(dialog.locator('button[type="submit"]')).toBeDisabled();
  release();
  await expect(
    page.getByRole("status").filter({ hasText: "Project created" }),
  ).toBeVisible();
  await page.unroute(`**/api/workspaces/${ws}/projects`);
  await page
    .getByRole("link", { name: "Launch the good stuff" })
    .first()
    .click();
  await expect(page).toHaveURL(
    new RegExp(`/app/${ws}/projects/prj_[0-9A-Za-z]{14}$`),
  );
  await page.getByRole("button", { name: "Edit project" }).click();
  await dialog.getByRole("combobox").click();
  await page.getByRole("option", { name: "Completed", exact: true }).click();
  await dialog.getByRole("button", { name: "Save changes" }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Project updated" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Launch the good stuff" }),
  ).toBeVisible();
  for (const missing of [
    `/app/${ws}/projects/prj_00000000000000`,
    `/app/${ws}/projects/42`,
    "/app/wsp_00000000000000/dashboard",
    "/app/dashboard",
    "/app/projects",
  ])
    expect((await page.request.get(missing)).status()).toBe(404);
  await page.goto(dashboard);
  const html = await (await page.request.get(dashboard)).text();
  expect(html).toContain("Launch the good stuff");
  expect(html).toContain("_styletron_hydrate_");
  const response = await page.request.get(dashboard);
  expect(response.headers()["content-security-policy"]).toContain("nonce-");
  expect(response.headers()["cache-control"]).toContain("no-store");
  await page.getByRole("link", { name: "Projects", exact: true }).click();
  await expect(page).toHaveURL(`/app/${ws}/projects`);
  await expect(page.getByRole("heading", { name: "Projects" })).toBeVisible();
  await page.getByRole("combobox", { name: /Filter by status/ }).click();
  await page.getByRole("option", { name: "Planned", exact: true }).click();
  await expect(page.getByText("No matching projects")).toBeVisible();
  await page.getByRole("combobox", { name: /Filter by status/ }).click();
  await page.getByRole("option", { name: "All statuses" }).click();
  await page
    .getByRole("searchbox", { name: "Search projects" })
    .fill("not-a-match");
  await expect(page).toHaveURL(`/app/${ws}/projects?q=not-a-match`);
  await expect(page.getByText("No matching projects")).toBeVisible();
  await page.getByRole("searchbox", { name: "Search projects" }).fill("");
  // Search applies after a 300 ms pause; wait for the full list to be current,
  // or Refresh would reload the "not-a-match" search instead.
  await expect(page).toHaveURL(`/app/${ws}/projects`);
  await expect(
    page.getByRole("link", { name: "Launch the good stuff" }).first(),
  ).toBeVisible();
  // Any project list request, with or without a query string.
  const projectLists = new RegExp(`/api/workspaces/${ws}/projects(\\?.*)?$`);
  await page.route(projectLists, (route) =>
    route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({ error: "Temporary service interruption" }),
    }),
  );
  await page.getByRole("button", { name: "Refresh projects" }).click();
  await expect(
    page
      .getByRole("alert")
      .filter({ hasText: "Temporary service interruption" }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Launch the good stuff" }).first(),
  ).toBeVisible();
  await page.unroute(projectLists);
  await page.getByRole("button", { name: "Try again" }).click();
  await expect(
    page
      .getByRole("alert")
      .filter({ hasText: "Temporary service interruption" }),
  ).toHaveCount(0);
  await paginate(page, ws);
  await page.getByRole("button", { name: "Account menu" }).click();
  await page.getByRole("menuitem", { name: "Account & security" }).click();
  await page.getByRole("button", { name: "Sign out other sessions" }).click();
  await expect(
    page.getByText("All other sessions have been signed out."),
  ).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(dashboard);
  await expect(
    page.getByRole("heading", { name: "Welcome back, Taylor." }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
  await page.getByRole("button", { name: "Open navigation" }).click();
  await expect(
    page.getByRole("dialog", { name: "Navigation" }).getByRole("link", {
      name: "Projects",
    }),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Account menu" }).click();
  await page.getByRole("menuitem", { name: "Sign out" }).click();
  await expect(page).toHaveURL("/auth/login");
  await page.getByLabel("Email").fill(email);
  await page
    .getByLabel("Password", { exact: true })
    .fill("A wrong password long enough");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(
    page.getByRole("alert").filter({ hasText: "Email or password" }),
  ).toHaveText("Email or password is incorrect.");
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(dashboard);
  await expect(
    page.getByRole("heading", { name: "Welcome back, Taylor." }),
  ).toBeVisible();
  expect(errors).toEqual([]);
});
test("/ opens /app, and the pre-workspace URLs are gone", async ({
  request,
}) => {
  const root = await request.get("/", { maxRedirects: 0 });
  expect(root.status()).toBe(307);
  expect(root.headers()["location"]).toBe("/app");
  const app = await request.get("/app", { maxRedirects: 0 });
  expect(app.headers()["location"]).toBe("/auth/login?next=%2Fapp");
  for (const gone of ["/login", "/projects", "/settings", "/auth"])
    expect((await request.get(gone, { maxRedirects: 0 })).status()).toBe(404);
});
test("signing in returns to the page that needed it, and only to our pages", async ({
  page,
}) => {
  await register(page, returnEmail, "Robin Example");
  const ws = await onboard(page, "Robin’s place");
  await page.context().clearCookies();
  const next = `next=%2Fapp%2F${ws}%2Fprojects%3Fstatus%3DPlanned`;
  await page.goto(`/app/${ws}/projects?status=Planned`);
  await expect(page).toHaveURL(`/auth/login?${next}`);
  await page.getByLabel("Email").fill(returnEmail);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(`/app/${ws}/projects?status=Planned`);
  // Signed in, the auth pages move straight on.
  await page.goto("/auth/login");
  await expect(page).toHaveURL(`/app/${ws}/dashboard`);
  await page.goto("/auth/register?next=%2Fapp%2Fsettings");
  await expect(page).toHaveURL("/app/settings");
  await page.goto("/auth/login?next=https%3A%2F%2Fexample.com");
  await expect(page).toHaveURL(`/app/${ws}/dashboard`);
  // Another site in ?next= falls back to the newest workspace.
  await page.context().clearCookies();
  await page.goto("/auth/login?next=%2F%2Fexample.com");
  await page.getByLabel("Email").fill(returnEmail);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(`/app/${ws}/dashboard`);
});
test("workspaces: switch, create, rename, copy the ID, and open the newest", async ({
  page,
  context,
}) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await register(page, scrollEmail, "Sam Example");
  const first = await onboard(page, "First studio");
  // A second workspace from the switcher opens straight away.
  await page.getByRole("button", { name: "Workspace menu" }).click();
  await page.getByRole("menuitem", { name: "New workspace…" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Workspace name").fill("Second studio");
  await dialog.getByRole("button", { name: "Create workspace" }).click();
  await expect(page).not.toHaveURL(`/app/${first}/dashboard`);
  await expect(page).toHaveURL(workspaceUrl);
  await expect(dialog).toHaveCount(0);
  const second = workspaceOf(page);
  await expect(page.getByText("Room for your next big thing")).toBeVisible();
  // Both are listed, newest first, and switching is a link.
  await page.getByRole("button", { name: "Workspace menu" }).click();
  const menu = page.getByRole("menu");
  const listed = menu.locator('a[href$="/dashboard"]');
  // Each item is the workspace's initial, then its name.
  await expect(listed).toHaveText([/Second studio$/, /First studio$/]);
  await expect(listed.first()).toHaveAttribute("aria-current", "page");
  await menu.getByRole("menuitem", { name: "First studio" }).click();
  await expect(page).toHaveURL(`/app/${first}/dashboard`);
  // Rename in settings; the switcher follows.
  await page.getByRole("button", { name: "Workspace menu" }).click();
  await page.getByRole("menuitem", { name: "Workspace settings" }).click();
  await expect(page).toHaveURL(`/app/${first}/settings`);
  await expect(page.getByText("Owner", { exact: true })).toBeVisible();
  await page.getByLabel("Name", { exact: true }).fill("Renamed studio");
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByText("Workspace renamed.")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Workspace menu" }),
  ).toContainText("Renamed studio");
  await expect(page.getByLabel("Workspace ID", { exact: true })).toHaveValue(
    first,
  );
  await page.getByRole("button", { name: "Copy Workspace ID" }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Copied" }),
  ).toBeVisible();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(first);
  // More than a page of workspaces: the switcher loads the rest as it scrolls.
  const headers = await apiHeaders(page);
  for (let i = 0; i < 23; i++) {
    const response = await page.request.post("/api/workspaces", {
      headers,
      data: { name: `Bulk ${String(i).padStart(2, "0")}` },
    });
    expect(response.status()).toBe(201);
  }
  await page.goto("/app");
  await expect(page).toHaveURL(workspaceUrl);
  await expect(
    page.getByRole("button", { name: "Workspace menu" }),
  ).toContainText("Bulk 22");
  await page.getByRole("button", { name: "Workspace menu" }).click();
  await expect(listed).toHaveCount(20);
  await listed.last().scrollIntoViewIfNeeded();
  await expect(listed).toHaveCount(25);
  await expect(listed.last()).toHaveText(/Renamed studio$/);
  await expect(menu.getByRole("menuitem", { name: "Show more" })).toHaveCount(
    0,
  );
  // Signing in again opens the newest workspace.
  await page.keyboard.press("Escape");
  await page.context().clearCookies();
  await page.goto("/auth/login");
  await page.getByLabel("Email").fill(scrollEmail);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(
    page.getByRole("button", { name: "Workspace menu" }),
  ).toContainText("Bulk 22");
  expect(workspaceOf(page)).not.toBe(second);
});
test("other people's workspaces are 404s until you're a member, then read-only for members", async ({
  page,
  browser,
}) => {
  // Invites don't exist yet, so the membership is added in the database.
  test.skip(!process.env.TEST_DATABASE_URL, "needs TEST_DATABASE_URL");
  // The owner, set up through the API in a browser context of their own.
  const owner = await browser.newContext();
  const origin = { Origin: process.env.APP_ORIGIN || "http://localhost:3000" };
  const signedUp = await owner.request.post("/api/auth/register", {
    headers: { ...origin, "Content-Type": "application/json" },
    data: { name: "Owen Owner", email: ownerEmail, password },
  });
  expect(signedUp.status()).toBe(201);
  const headers = {
    ...origin,
    "Content-Type": "application/json",
    "X-CSRF-Token": (await signedUp.json()).csrfToken,
  };
  const created = await owner.request.post("/api/workspaces", {
    headers,
    data: { name: "Owen’s office" },
  });
  const theirs = (await created.json()).id;
  await owner.request.post(`/api/workspaces/${theirs}/projects`, {
    headers,
    data: {
      name: "Shared plans",
      description: "",
      status: "Planned",
      color: "blue",
    },
  });
  await owner.close();
  await register(page, memberEmail, "Morgan Example");
  await onboard(page, "Morgan’s own");
  for (const path of ["dashboard", "projects", "settings"])
    expect((await page.request.get(`/app/${theirs}/${path}`)).status()).toBe(
      404,
    );
  expect(
    (await page.request.get(`/api/workspaces/${theirs}/projects`)).status(),
  ).toBe(404);
  sql(
    `INSERT INTO workspace_user (user_id, workspace_id, role) SELECT u.id, w.id, 'member' FROM users u, workspaces w WHERE u.email = '${memberEmail}' AND w.public_id = '${theirs}'`,
  );
  await page.goto(`/app/${theirs}/settings`);
  await expect(
    page.getByText("Only owners and admins can rename this workspace."),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Save" })).toHaveCount(0);
  await expect(page.getByText("Member", { exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Projects", exact: true }).click();
  await expect(page.getByText("Shared plans")).toBeVisible();
});
test("unauthenticated API requests are rejected", async ({ request }) => {
  for (const path of [
    "/api/workspaces",
    "/api/workspaces/wsp_00000000000000/projects",
  ])
    expect((await request.get(path)).status()).toBe(401);
});
// Paging, sorting and searching run in the API: create a second page of
// projects through it, then check the URL drives what the table shows.
async function paginate(page: Page, ws: string) {
  const headers = await apiHeaders(page);
  const projects = `/app/${ws}/projects`;
  for (let i = 0; i < 22; i++) {
    const response = await page.request.post(`/api/workspaces/${ws}/projects`, {
      headers,
      data: {
        name: `Bulk ${String(i).padStart(2, "0")}`,
        description: "",
        status: "Planned",
        color: "blue",
      },
    });
    expect(response.status()).toBe(201);
  }
  await page.goto(projects);
  await expect(page.getByText("23 projects in this workspace")).toBeVisible();
  const rows = page.locator("tbody tr");
  await expect(rows).toHaveCount(20);
  await expect(rows.first()).toContainText("Bulk 21");
  const pages = page.getByRole("navigation", { name: "Projects pages" });
  await pages.getByRole("link", { name: "Next page" }).click();
  await expect(page).toHaveURL(`${projects}?page=2`);
  await expect(rows).toHaveCount(3);
  await expect(rows.last()).toContainText("Launch the good stuff");
  await expect(pages.getByRole("link", { name: "Page 2" })).toHaveAttribute(
    "aria-current",
    "page",
  );
  // Sorting starts again from page 1; a second click reverses it.
  await page.getByRole("link", { name: "Project", exact: true }).click();
  await expect(page).toHaveURL(`${projects}?sort=name`);
  await expect(rows.first()).toContainText("Bulk 00");
  await expect(
    page.getByRole("columnheader", { name: "Project" }),
  ).toHaveAttribute("aria-sort", "ascending");
  await page.getByRole("link", { name: "Project", exact: true }).click();
  await expect(page).toHaveURL(`${projects}?sort=-name`);
  await expect(rows.first()).toContainText("Launch the good stuff");
  await page.goBack();
  await expect(page).toHaveURL(`${projects}?sort=name`);
  await expect(rows.first()).toContainText("Bulk 00");
  // The server searches every page, not only the loaded one.
  await page.getByRole("searchbox", { name: "Search projects" }).fill("launch");
  await expect(page).toHaveURL(`${projects}?q=launch&sort=name`);
  await expect(rows).toHaveCount(1);
  await expect(page.getByText("1 matching project")).toBeVisible();
  await page.getByRole("searchbox", { name: "Search projects" }).fill("");
  await expect(page).toHaveURL(`${projects}?sort=name`);
  // A URL renders the same page on the server.
  const html = await (await page.request.get(`${projects}?page=2`)).text();
  expect(html).toContain("Launch the good stuff");
  expect(html).not.toContain("Bulk 21");
  await page.goto(`${projects}?page=9`);
  await expect(page.getByText("There’s nothing on page 9")).toBeVisible();
  await page.getByRole("link", { name: "Go to last page" }).click();
  await expect(page).toHaveURL(`${projects}?page=2`);
  await expect(rows).toHaveCount(3);
  // A project created from page 2 shows first on page 1.
  await page.getByRole("button", { name: "New project" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Project name", { exact: true }).fill("Fresh idea");
  await dialog.getByRole("button", { name: "Create project" }).click();
  await expect(page).toHaveURL(projects);
  await expect(rows.first()).toContainText("Fresh idea");
  await expect(page.getByText("24 projects in this workspace")).toBeVisible();
}
