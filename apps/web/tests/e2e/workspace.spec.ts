import { expect, test, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
const stamp = Date.now();
const email = `e2e-${stamp}@lewcontext.test`;
const returnEmail = `e2e-${stamp}-next@lewcontext.test`;
const scrollEmail = `e2e-${stamp}-scroll@lewcontext.test`;
const memberEmail = `e2e-${stamp}-member@lewcontext.test`;
const ownerEmail = `e2e-${stamp}-owner@lewcontext.test`;
const passwordEmail = `e2e-${stamp}-password@lewcontext.test`;
const sidebarEmail = `e2e-${stamp}-sidebar@lewcontext.test`;
const resetEmail = `e2e-${stamp}-reset@lewcontext.test`;
const crewEmail = `e2e-${stamp}-crew@lewcontext.test`;
const teammateEmail = `e2e-${stamp}-teammate@lewcontext.test`;
const deleteEmail = `e2e-${stamp}-delete@lewcontext.test`;
const switcherEmail = `e2e-${stamp}-switcher@lewcontext.test`;
const inviterEmail = `e2e-${stamp}-inviter@lewcontext.test`;
const inviteeEmail = `e2e-${stamp}-invitee@lewcontext.test`;
const joinerEmail = `e2e-${stamp}-joiner@lewcontext.test`;
const revokedEmail = `e2e-${stamp}-revoked@lewcontext.test`;
const emails = [
  email,
  returnEmail,
  scrollEmail,
  memberEmail,
  ownerEmail,
  passwordEmail,
  sidebarEmail,
  resetEmail,
  crewEmail,
  teammateEmail,
  deleteEmail,
  switcherEmail,
  inviterEmail,
  inviteeEmail,
  joinerEmail,
];
const password = "A thoughtful test passphrase!";
const workspaceUrl = /\/app\/(wsp_[0-9A-Za-z]{14})\/dashboard$/;
declare global {
  interface Window {
    // Set by the sidebar test if the docked sidebar's toggle ever rendered.
    sawDockedSidebar?: boolean;
  }
}
// Runs SQL against this run's database (DATABASE_URL, which
// playwright.config.ts derives from .env.testing) and returns its output; only
// for setup the UI can't do yet (invites) and cleanup.
function sql(statement: string): string {
  return execFileSync(
    "psql",
    [process.env.DATABASE_URL!, "-v", "ON_ERROR_STOP=1", "-Atc", statement],
    { encoding: "utf8" },
  ).trim();
}
const quoted = emails.map((e) => `'${e}'`).join(", ");
test.afterAll(() => {
  // Only remove the uniquely named accounts this run created, and the
  // workspaces they belong to. Cascades remove projects, memberships and
  // sessions. The database is rebuilt on the next server start anyway, but a
  // reused server (E2E_REUSE=1) isn't.
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
test("register, create, loading, filter, edit, paginate, not found, security, mobile, logout, and login", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  // The CSP's script hashes must cover every inline script the build wrote.
  page.on("console", (message) => {
    if (message.text().includes("Content Security Policy"))
      errors.push(message.text());
  });
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Context for AI agents." }),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: "Log in" })).toHaveAttribute(
    "href",
    "/auth/login",
  );
  await page.goto("/app");
  await expect(page).toHaveURL("/auth/login?next=%2Fapp");
  await page.getByRole("link", { name: "Sign up" }).click();
  await expect(page).toHaveURL("/auth/register?next=%2Fapp");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Full name").fill("Taylor Example");
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Create account" }).click();
  // A new account starts at onboarding, and can't skip it.
  await expect(page).toHaveURL("/app/onboarding");
  await page.getByRole("button", { name: "Account" }).click();
  await expect(page.getByText(`Signed in as ${email}`)).toBeVisible();
  await page.keyboard.press("Escape");
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
  await page
    .getByRole("button", { name: "Actions for Launch the good stuff" })
    .click();
  await page.getByRole("menuitem", { name: "Edit project" }).click();
  await dialog.getByRole("combobox").click();
  await page.getByRole("option", { name: "Completed", exact: true }).click();
  await dialog.getByRole("button", { name: "Save changes" }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Project updated" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Launch the good stuff" }),
  ).toBeVisible();
  // Records that don't exist render "not found" in the app; Go has already
  // sent the app shell, so the HTTP status is 200.
  for (const missing of [
    `/app/${ws}/projects/prj_00000000000000`,
    `/app/${ws}/projects/42`,
  ]) {
    await page.goto(missing);
    await expect(
      page.getByRole("heading", { name: "This project has wandered off." }),
    ).toBeVisible();
  }
  for (const missing of [
    "/app/wsp_00000000000000/dashboard",
    "/app/dashboard",
    "/app/projects",
  ]) {
    await page.goto(missing);
    await expect(
      page.getByRole("heading", { name: "This page has wandered off." }),
    ).toBeVisible();
  }
  await page.goto(dashboard);
  const response = await page.request.get(dashboard);
  const csp = response.headers()["content-security-policy"];
  // Inline scripts run only by hash; inline CSS is allowed for Styletron.
  expect(csp).toMatch(/script-src 'self'( 'sha256-[A-Za-z0-9+/=]+')+;/);
  expect(csp.split(";").find((d) => d.includes("script-src"))).not.toContain(
    "unsafe",
  );
  expect(response.headers()["cache-control"]).toContain("no-store");
  await page.getByRole("link", { name: "Projects", exact: true }).click();
  await expect(page).toHaveURL(`/app/${ws}/projects`);
  await expect(page.getByRole("heading", { name: "Projects" })).toBeVisible();
  await page.getByRole("combobox", { name: /Filter by status/ }).click();
  await page.getByRole("option", { name: "Planned", exact: true }).click();
  await expect(page.getByText("No matching projects")).toBeVisible();
  await page.getByRole("combobox", { name: /Filter by status/ }).click();
  await page.getByRole("option", { name: "All", exact: true }).click();
  await page
    .getByRole("searchbox", { name: "Search projects" })
    .fill("not-a-match");
  await expect(page).toHaveURL(`/app/${ws}/projects?q=not-a-match`);
  await expect(page.getByText("No matching projects")).toBeVisible();
  await page.getByRole("searchbox", { name: "Search projects" }).fill("");
  // Search applies after a 300 ms pause; wait for the full list to be current,
  // or the refresh below would reload the "not-a-match" search instead.
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
  // A background refresh: SWR revalidates on focus, but ignores focus for 5 s
  // after the list mounts, so keep focusing until one gets through.
  const interruption = page
    .getByRole("alert")
    .filter({ hasText: "Temporary service interruption" });
  await expect(async () => {
    await page.evaluate(() => window.dispatchEvent(new Event("focus")));
    await expect(interruption).toBeVisible({ timeout: 1_000 });
  }).toPass();
  await expect(
    page.getByRole("link", { name: "Launch the good stuff" }).first(),
  ).toBeVisible();
  await page.unroute(projectLists);
  await page.getByRole("button", { name: "Try again" }).click();
  await expect(interruption).toHaveCount(0);
  await paginate(page, ws);
  await page.getByRole("button", { name: "Account menu" }).click();
  await page.getByRole("menuitem", { name: "Account & security" }).click();
  await page.getByRole("button", { name: "Account menu" }).click();
  await expect(
    page.getByRole("menuitem", { name: "Account & security" }),
  ).toHaveAttribute("aria-current", "page");
  await page.keyboard.press("Escape");
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
  // The top bar has the workspace menu; the account menu is at the bottom
  // of the slide-over sidebar.
  await expect(
    page.getByRole("banner").getByRole("button", { name: "Workspace menu" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Open navigation" }).click();
  await page
    .getByRole("dialog", { name: "Navigation" })
    .getByRole("button", { name: "Account menu" })
    .click();
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
test("/ is the prerendered homepage, /app needs a session, and the pre-workspace URLs are gone", async ({
  request,
}) => {
  // Search engines get the homepage's content, title and description, and
  // BaseUI's styles, in the HTML itself.
  const root = await request.get("/", { maxRedirects: 0 });
  expect(root.status()).toBe(200);
  const html = await root.text();
  const appName = process.env.APP_NAME || "lewcontext";
  expect(html).toContain(`<title>${appName} — Context for AI agents</title>`);
  expect(html).toContain('<meta name="description"');
  expect(html).toContain("Context for AI agents.");
  expect(html).toContain('href="/auth/login"');
  expect(html).toContain("_styletron_hydrate_");
  // Signed-out visitors get no app HTML, only a redirect to login.
  const app = await request.get("/app", { maxRedirects: 0 });
  expect(app.status()).toBe(302);
  expect(app.headers()["location"]).toBe("/auth/login?next=%2Fapp");
  for (const gone of ["/login", "/projects", "/settings", "/auth"])
    expect((await request.get(gone, { maxRedirects: 0 })).status()).toBe(404);
});
test("the desktop sidebar hides and is remembered without flashing; the mobile one closes beside the workspace menu", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await register(page, sidebarEmail, "Jordan Example");
  await onboard(page, "Jordan’s desk");
  // The account menu is only in the sidebar; the workspace menu is in the
  // sidebar and, while it's hidden, in the top bar.
  const accountMenu = page.getByRole("button", { name: "Account menu" });
  const workspaceMenu = page.getByRole("button", { name: "Workspace menu" });
  await expect(accountMenu).toBeVisible();
  await page.getByRole("button", { name: "Hide sidebar" }).click();
  await expect(accountMenu).toBeHidden();
  await expect(
    page.getByRole("banner").getByRole("button", { name: "Workspace menu" }),
  ).toContainText("Jordan’s desk");
  // Reloading opens it hidden, without rendering it docked even for a moment.
  await page.addInitScript(() => {
    new MutationObserver(() => {
      if (document.querySelector('[aria-label="Hide sidebar"]'))
        window.sawDockedSidebar = true;
    }).observe(document, { childList: true, subtree: true, attributes: true });
  });
  await page.reload();
  await expect(
    page.getByRole("button", { name: "Show sidebar" }),
  ).toBeVisible();
  await expect(accountMenu).toBeHidden();
  expect(await page.evaluate(() => window.sawDockedSidebar)).toBeUndefined();
  await page.getByRole("button", { name: "Show sidebar" }).click();
  await expect(accountMenu).toBeVisible();
  // Docked, the top bar (and its workspace menu) is out of reach again.
  await expect(workspaceMenu).toHaveCount(1);
  // On a phone the drawer's close button sits beside the workspace menu.
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("button", { name: "Open navigation" }).click();
  const drawer = page.getByRole("dialog", { name: "Navigation" });
  await expect(
    drawer.getByRole("button", { name: "Workspace menu" }),
  ).toBeVisible();
  await expect(
    drawer.getByRole("button", { name: "Account menu" }),
  ).toBeVisible();
  await drawer.getByRole("button", { name: "Close navigation" }).click();
  await expect(drawer).toBeHidden();
  // Opening a page from a menu in it closes it too.
  await page.getByRole("button", { name: "Open navigation" }).click();
  await drawer.getByRole("button", { name: "Account menu" }).click();
  await page.getByRole("menuitem", { name: "Account & security" }).click();
  await expect(page).toHaveURL("/app/settings");
  await expect(drawer).toBeHidden();
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
test("workspaces: switch, create, rename and copy the ID", async ({
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
  await expect(
    page.getByRole("status").filter({ hasText: "Second studio created." }),
  ).toBeVisible();
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
  // The menu marks the page you're on.
  await page.getByRole("button", { name: "Workspace menu" }).click();
  await expect(
    page.getByRole("menuitem", { name: "Workspace settings" }),
  ).toHaveAttribute("aria-current", "page");
  await page.keyboard.press("Escape");
  // The only owner can't leave (canLeave in the workspace response), so
  // Leave isn't offered.
  await expect(
    page.getByRole("button", { name: "Delete workspace" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Leave workspace" }),
  ).toHaveCount(0);
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
});
test("the switcher loads more workspaces as it scrolls, and signing in opens the newest", async ({
  page,
}) => {
  // You can own 20 workspaces, so the ones past a page of the switcher that
  // aren't yours are added as memberships in the database (no invites yet).
  await register(page, switcherEmail, "Sam Switcher");
  await onboard(page, "First studio");
  const headers = await apiHeaders(page);
  for (let i = 0; i < 19; i++) {
    const response = await page.request.post("/api/workspaces", {
      headers,
      data: { name: `Bulk ${String(i).padStart(2, "0")}` },
    });
    expect(response.status()).toBe(201);
  }
  const refused = await page.request.post("/api/workspaces", {
    headers,
    data: { name: "One too many" },
  });
  expect(refused.status()).toBe(422);
  expect((await refused.json()).error).toBe("You can own up to 20 workspaces.");
  sql(
    `WITH created AS (INSERT INTO workspaces (name, created_at, updated_at) SELECT 'Bulk ' || n, now() + n * interval '1 millisecond', now() FROM generate_series(19, 22) AS n RETURNING id) INSERT INTO workspace_user (user_id, workspace_id, role) SELECT u.id, c.id, 'member' FROM created c JOIN users u ON u.email = '${switcherEmail}'`,
  );
  await page.goto("/app");
  await expect(page).toHaveURL(workspaceUrl);
  await expect(
    page.getByRole("button", { name: "Workspace menu" }),
  ).toContainText("Bulk 22");
  await page.getByRole("button", { name: "Workspace menu" }).click();
  const menu = page.getByRole("menu");
  const listed = menu.locator('a[href$="/dashboard"]');
  await expect(listed).toHaveCount(20);
  await listed.last().scrollIntoViewIfNeeded();
  await expect(listed).toHaveCount(24);
  await expect(listed.last()).toHaveText(/First studio$/);
  await expect(menu.getByRole("menuitem", { name: "Show more" })).toHaveCount(
    0,
  );
  // Signing in again opens the newest workspace.
  await page.keyboard.press("Escape");
  await page.context().clearCookies();
  await page.goto("/auth/login");
  await page.getByLabel("Email").fill(switcherEmail);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(
    page.getByRole("button", { name: "Workspace menu" }),
  ).toContainText("Bulk 22");
});
test("other people's workspaces are not found until you're a member; members can't rename and viewers can't change anything", async ({
  page,
  browser,
}) => {
  // Invites don't exist yet, so the membership is added in the database.
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
  for (const path of ["dashboard", "projects", "settings"]) {
    await page.goto(`/app/${theirs}/${path}`);
    await expect(
      page.getByRole("heading", { name: "This page has wandered off." }),
    ).toBeVisible();
  }
  expect(
    (await page.request.get(`/api/workspaces/${theirs}/projects`)).status(),
  ).toBe(404);
  sql(
    `INSERT INTO workspace_user (user_id, workspace_id, role) SELECT u.id, w.id, 'member' FROM users u, workspaces w WHERE u.email = '${memberEmail}' AND w.public_id = '${theirs}'`,
  );
  await page.goto(`/app/${theirs}/settings`);
  await expect(
    page.getByRole("img", { name: "Only owners and admins can rename this" }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Save" })).toHaveCount(0);
  await page.getByRole("link", { name: "Projects", exact: true }).click();
  await expect(page.getByText("Shared plans")).toBeVisible();
  await expect(page.getByRole("button", { name: "New project" })).toBeVisible();
  // Viewers read everything but can't change it, in the UI or the API.
  sql(
    `UPDATE workspace_user SET role = 'viewer' WHERE user_id = (SELECT id FROM users WHERE email = '${memberEmail}') AND workspace_id = (SELECT id FROM workspaces WHERE public_id = '${theirs}')`,
  );
  await page.reload();
  await expect(page.getByText("Shared plans").first()).toBeVisible();
  await expect(page.getByRole("button", { name: "New project" })).toHaveCount(
    0,
  );
  await page.getByRole("link", { name: "Shared plans" }).first().click();
  await expect(
    page.getByRole("heading", { name: "Shared plans" }),
  ).toBeVisible();
  // Viewers may neither edit nor delete it, so there's no ⋯ menu.
  await expect(
    page.getByRole("button", { name: "Actions for Shared plans" }),
  ).toHaveCount(0);
  const refused = await page.request.post(
    `/api/workspaces/${theirs}/projects`,
    {
      headers: await apiHeaders(page),
      data: { name: "Nope", description: "", status: "Planned", color: "blue" },
    },
  );
  expect(refused.status()).toBe(403);
  await page.goto(`/app/${theirs}/settings`);
  await expect(page.getByRole("button", { name: "Save" })).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Leave workspace" }),
  ).toBeEnabled();
});
test("members: an owner changes a member to viewer, then removes them", async ({
  page,
  browser,
}) => {
  // Invites don't exist yet, so the membership is added in the database.
  const teammate = await browser.newContext();
  const origin = { Origin: process.env.APP_ORIGIN || "http://localhost:3000" };
  const signedUp = await teammate.request.post("/api/auth/register", {
    headers: { ...origin, "Content-Type": "application/json" },
    data: { name: "Terry Teammate", email: teammateEmail, password },
  });
  expect(signedUp.status()).toBe(201);
  await register(page, crewEmail, "Casey Crew");
  const crew = await onboard(page, "Crew HQ");
  sql(
    `INSERT INTO workspace_user (user_id, workspace_id, role) SELECT u.id, w.id, 'member' FROM users u, workspaces w WHERE u.email = '${teammateEmail}' AND w.public_id = '${crew}'`,
  );
  expect((await teammate.request.get(`/api/workspaces/${crew}`)).status()).toBe(
    200,
  );

  await page.goto(`/app/${crew}/settings`);
  await page.getByRole("link", { name: "Members" }).click();
  await expect(page).toHaveURL(`/app/${crew}/settings/members`);
  const row = page.getByRole("row").filter({ hasText: "Terry Teammate" });
  await expect(row.getByText("Member", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("row").filter({ hasText: "Casey Crew" }).getByText("You"),
  ).toBeVisible();

  await row.getByRole("button", { name: "Actions for Terry Teammate" }).click();
  await page.getByRole("menuitem", { name: "Change role" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("combobox").click();
  await page.getByRole("option", { name: "Viewer" }).click();
  await dialog.getByRole("button", { name: "Save role" }).click();
  await expect(page.getByText("Terry Teammate is now a viewer.")).toBeVisible();
  await expect(row.getByText("Viewer", { exact: true })).toBeVisible();
  // BaseUI keeps a closed dialog for its 500 ms exit animation.
  await expect(dialog).toHaveCount(0);

  await row.getByRole("button", { name: "Actions for Terry Teammate" }).click();
  await page.getByRole("menuitem", { name: "Remove from workspace" }).click();
  await expect(dialog.getByRole("heading")).toHaveText(
    "Remove Terry Teammate from Crew HQ?",
  );
  await dialog.getByRole("button", { name: "Remove" }).click();
  await expect(
    page.getByText("Terry Teammate was removed from Crew HQ."),
  ).toBeVisible();
  await expect(row).toHaveCount(0);
  await expect(page.getByText("Showing 1 of 1")).toBeVisible();
  expect((await teammate.request.get(`/api/workspaces/${crew}`)).status()).toBe(
    404,
  );
  await teammate.close();
});
test("change password: checks the current one twice and signs out other sessions", async ({
  page,
  browser,
}) => {
  await register(page, passwordEmail, "Pat Example");
  await onboard(page, "Pat’s place");
  // Another browser signed in to the same account.
  const elsewhere = await browser.newContext({
    baseURL: new URL(page.url()).origin,
  });
  const signIn = await elsewhere.request.post("/api/auth/login", {
    headers: { Origin: new URL(page.url()).origin },
    data: { email: passwordEmail, password },
  });
  expect(signIn.status()).toBe(200);
  await page.getByRole("button", { name: "Account menu" }).click();
  await page.getByRole("menuitem", { name: "Account & security" }).click();
  await page.getByRole("button", { name: "Change password" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Current password").fill("not my password");
  await dialog.getByRole("button", { name: "Next" }).click();
  await expect(dialog.getByRole("alert")).toHaveText(
    "Current password is incorrect.",
  );
  await dialog.getByLabel("Current password").fill(password);
  await dialog.getByRole("button", { name: "Next" }).click();
  const changed = "A different test passphrase!";
  await dialog.getByLabel("New password", { exact: true }).fill(changed);
  await dialog.getByLabel("Confirm new password").fill(changed);
  await dialog.getByRole("button", { name: "Confirm" }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Password updated" }),
  ).toBeVisible();
  await expect(dialog).toHaveCount(0);
  // The other browser was signed out; this one carries on with a new session.
  expect((await elsewhere.request.get("/api/auth/me")).status()).toBe(401);
  await elsewhere.close();
  await page.getByRole("button", { name: "Sign out other sessions" }).click();
  await expect(
    page.getByText("All other sessions have been signed out."),
  ).toBeVisible();
  await page.getByRole("button", { name: "Account menu" }).click();
  await page.getByRole("menuitem", { name: "Sign out" }).click();
  await expect(page).toHaveURL("/auth/login");
  await page.getByLabel("Email").fill(passwordEmail);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(
    page.getByRole("alert").filter({ hasText: "Email or password" }),
  ).toBeVisible();
  await page.getByLabel("Password", { exact: true }).fill(changed);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(workspaceUrl);
});
// Mailpit, where the e2e server's email lands (MAILPIT_URL, from .env.testing).
const mailpit = process.env.MAILPIT_URL!;
// The newest email Mailpit holds for `to`: its HTML and plain-text parts, or
// nothing yet. Every run's recipients are unique, so other runs' mail never
// matches.
async function newestMailTo(
  to: string,
): Promise<{ HTML: string; Text: string } | undefined> {
  const query = new URLSearchParams({ query: `to:"${to}"` });
  const found = await (await fetch(`${mailpit}/api/v1/search?${query}`)).json();
  const id = found.messages?.[0]?.ID;
  if (!id) return undefined;
  return (await fetch(`${mailpit}/api/v1/message/${id}`)).json();
}
// The path of the newest link of a kind (a reset link, or an invitation)
// sent to `to`, once the server's worker has sent it (it polls every second).
// The plain-text part must carry the same link.
async function mailedLink(
  to: string,
  kind: "forgot-password" | "invitations",
): Promise<string> {
  let link = "";
  await expect
    .poll(
      async () => {
        const mail = await newestMailTo(to);
        const match = mail?.HTML.match(
          new RegExp(`href="https?://[^/"]+(/auth/${kind}/[\\w-]{43})"`),
        );
        link = match?.[1] ?? "";
        return link !== "" && mail!.Text.includes(link) ? link : "";
      },
      { message: `a ${kind} link for ${to} in Mailpit`, timeout: 15000 },
    )
    .not.toBe("");
  return link;
}
test("forgot password: a link from the mailbox sets a new password once", async ({
  page,
}) => {
  await register(page, resetEmail, "Robin Reset");
  await page.context().clearCookies();
  await page.goto("/auth/login");
  await page.getByLabel("Email").fill(resetEmail);
  await page.getByRole("link", { name: "Forgot password?" }).click();
  await expect(page).toHaveURL("/auth/forgot-password");
  // The email came along in navigation state, not the URL.
  await expect(page.getByLabel("Email")).toHaveValue(resetEmail);
  await page.getByRole("button", { name: "Send reset link" }).click();
  await expect(
    page.getByRole("heading", { name: "Check your email" }),
  ).toBeVisible();
  const link = await mailedLink(resetEmail, "forgot-password");
  const token = link.split("/").at(-1)!;
  // The token travels only in request bodies, never in an API URL.
  const apiURLs: string[] = [];
  page.on("request", (r) => {
    if (new URL(r.url()).pathname.startsWith("/api/")) apiURLs.push(r.url());
  });
  await page.goto(link);
  const reset = "A recovered test passphrase!";
  await page.getByLabel("New password", { exact: true }).fill(reset);
  await page.getByLabel("Confirm new password").fill(reset);
  await page.getByRole("button", { name: "Set new password" }).click();
  await expect(page).toHaveURL("/auth/login");
  await expect(
    page.getByText("Password updated. Sign in with your new password."),
  ).toBeVisible();
  await expect(page.getByLabel("Email")).toHaveValue(resetEmail);
  expect(apiURLs.filter((url) => url.includes(token))).toEqual([]);
  // The old password no longer works; the new one does.
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(
    page.getByRole("alert").filter({ hasText: "Email or password" }),
  ).toBeVisible();
  await page.getByLabel("Password", { exact: true }).fill(reset);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL("/app/onboarding");
  // The link worked once.
  await page.goto(link);
  await expect(
    page.getByRole("heading", { name: "This link has expired" }),
  ).toBeVisible();
});
test("deleting: a project from its row, the only workspace, then the account", async ({
  page,
}) => {
  await register(page, deleteEmail, "Dana Delete");
  const ws = await onboard(page, "Doomed HQ");
  const created = await page.request.post(`/api/workspaces/${ws}/projects`, {
    headers: await apiHeaders(page),
    data: {
      name: "Short-lived",
      description: "",
      status: "Planned",
      color: "rose",
    },
  });
  expect(created.status()).toBe(201);
  const dialog = page.getByRole("dialog");

  await page.goto(`/app/${ws}/projects`);
  const row = page.getByRole("row").filter({ hasText: "Short-lived" });
  await row.getByRole("button", { name: "Actions for Short-lived" }).click();
  await page.getByRole("menuitem", { name: "Delete project" }).click();
  await expect(dialog.getByRole("heading")).toHaveText("Delete Short-lived?");
  await dialog.getByRole("button", { name: "Delete project" }).click();
  await expect(page.getByText("Project deleted.")).toBeVisible();
  await expect(row).toHaveCount(0);
  await expect(dialog).toHaveCount(0);

  // The only owner of a workspace is sent there first.
  await page.goto("/app/settings");
  await expect(
    page.getByText(/You’re the only owner of a workspace/),
  ).toBeVisible();
  await page.getByRole("button", { name: "Delete account" }).click();
  await expect(dialog.getByRole("heading")).toHaveText(
    "Delete your workspaces first",
  );
  await expect(dialog.getByRole("button", { name: /Delete/ })).toHaveCount(0);
  await dialog.getByRole("link", { name: "Doomed HQ" }).click();
  await expect(page).toHaveURL(`/app/${ws}/settings`);
  await expect(dialog).toHaveCount(0);

  await page.getByRole("button", { name: "Delete workspace" }).click();
  const confirm = dialog.getByRole("button", { name: "Delete workspace" });
  await dialog.getByLabel(/to confirm/).fill("Doomed");
  await dialog.getByLabel("Your password").fill(password);
  await expect(confirm).toBeDisabled();
  await dialog.getByLabel(/to confirm/).fill("Doomed HQ");
  await confirm.click();
  // It was the only workspace, so onboarding opens.
  await expect(page).toHaveURL("/app/onboarding");
  await expect(page.getByText("Doomed HQ deleted.")).toBeVisible();
  expect((await page.request.get(`/api/workspaces/${ws}`)).status()).toBe(404);

  // Without a workspace, onboarding's menu deletes the account.
  await page.getByRole("button", { name: "Account" }).click();
  await page.getByRole("menuitem", { name: "Delete account" }).click();
  await expect(dialog.getByRole("heading")).toHaveText("Delete your account?");
  await dialog.getByLabel("Your password").fill(password);
  await dialog.getByRole("button", { name: "Delete account" }).click();
  await expect(page).toHaveURL("/auth/login");
  await expect(page.getByText("Your account has been deleted.")).toBeVisible();
  expect((await page.request.get("/api/auth/me")).status()).toBe(401);
  await page.getByLabel("Email").fill(deleteEmail);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByText("Email or password is incorrect.")).toBeVisible();
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
  await expect(page.getByText("Showing 1–20 of 23")).toBeVisible();
  const rows = page.locator("tbody tr");
  await expect(rows).toHaveCount(20);
  await expect(rows.first()).toContainText("Bulk 21");
  const pages = page.getByRole("navigation", { name: "Projects pages" });
  await pages.getByRole("link", { name: "Next page" }).click();
  await expect(page).toHaveURL(`${projects}?page=2`);
  await expect(rows).toHaveCount(3);
  await expect(rows.last()).toContainText("Launch the good stuff");
  await expect(page.getByText("Showing 21–23 of 23")).toBeVisible();
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
  await expect(page.getByText("Showing 1 of 1")).toBeVisible();
  await page.getByRole("searchbox", { name: "Search projects" }).fill("");
  await expect(page).toHaveURL(`${projects}?sort=name`);
  // Opening a URL directly renders the same page.
  await page.goto(`${projects}?page=2`);
  await expect(
    page.getByRole("link", { name: "Launch the good stuff" }).first(),
  ).toBeVisible();
  await expect(page.getByText("Bulk 21")).toHaveCount(0);
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
  await expect(page.getByText("Showing 1–20 of 24")).toBeVisible();
}
test("invitations: invite by email, join through the link as a new or existing account, revoke", async ({
  page,
  browser,
}) => {
  // Someone who already has an account, signed in in a browser of their own.
  const joiner = await browser.newContext();
  const joinerPage = await joiner.newPage();
  await register(joinerPage, joinerEmail, "Jo Joiner");
  await onboard(joinerPage, "Jo’s own");
  // The owner invites a new address, the existing account, and one to revoke.
  await register(page, inviterEmail, "Ivy Inviter");
  const ws = await onboard(page, "Invite studio");
  await page.goto(`/app/${ws}/settings/members`);
  for (const [email, role] of [
    [inviteeEmail, "Member"],
    [joinerEmail, "Viewer"],
    [revokedEmail, "Member"],
  ]) {
    await page.getByRole("button", { name: "Invite", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByLabel("Email").fill(email);
    if (role !== "Member") {
      await dialog.getByRole("combobox").click();
      await page.getByRole("option", { name: role }).click();
    }
    await dialog.getByRole("button", { name: "Send invitation" }).click();
    await expect(page.getByText(`Invitation sent to ${email}.`)).toBeVisible();
    await expect(dialog).toHaveCount(0);
  }
  const pending = page.getByRole("region", { name: "Pending invitations" });
  // "Sending…" until the worker has emailed each one; the list polls.
  await expect(pending.getByText("Sending…")).toHaveCount(0, {
    timeout: 15000,
  });
  await expect(pending.getByRole("row")).toHaveCount(4);
  const revokedLink = await mailedLink(revokedEmail, "invitations");
  await pending
    .getByRole("button", {
      name: `Actions for the invitation to ${revokedEmail}`,
    })
    .click();
  await page.getByRole("menuitem", { name: "Revoke invitation" }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Revoke invitation" })
    .click();
  await expect(
    page.getByText(`The invitation to ${revokedEmail} was revoked.`),
  ).toBeVisible();
  await expect(pending.getByRole("row")).toHaveCount(3);

  // A new account signs up through its link and lands in the workspace.
  const invitee = await browser.newContext();
  const inviteePage = await invitee.newPage();
  const link = await mailedLink(inviteeEmail, "invitations");
  const token = link.split("/").at(-1)!;
  const apiURLs: string[] = [];
  inviteePage.on("request", (r) => {
    if (new URL(r.url()).pathname.startsWith("/api/")) apiURLs.push(r.url());
  });
  await inviteePage.goto(link);
  await expect(
    inviteePage.getByRole("heading", { name: "Join Invite studio" }),
  ).toBeVisible();
  await expect(
    inviteePage.getByText(
      `Ivy Inviter (${inviterEmail}) invited you as a member.`,
      { exact: false },
    ),
  ).toBeVisible();
  await inviteePage.getByRole("link", { name: "Create an account" }).click();
  await expect(
    inviteePage.getByText("Invite studio has invited you to join.", {
      exact: false,
    }),
  ).toBeVisible();
  await expect(inviteePage.getByLabel("Email")).toHaveValue(inviteeEmail);
  await inviteePage.getByLabel("Full name").fill("Ines Invitee");
  await inviteePage.getByLabel("Password", { exact: true }).fill(password);
  await inviteePage.getByRole("button", { name: "Create account" }).click();
  await expect(inviteePage).toHaveURL(`/app/${ws}/dashboard`);
  await expect(
    inviteePage.getByText("You joined Invite studio."),
  ).toBeVisible();
  // The token never travelled in an API URL.
  expect(apiURLs.filter((url) => url.includes(token))).toEqual([]);
  // The link worked once.
  await inviteePage.goto(link);
  await expect(
    inviteePage.getByRole("heading", { name: "This invitation has expired" }),
  ).toBeVisible();

  // The existing account opens its link signed in, and joins on request.
  await joinerPage.goto(await mailedLink(joinerEmail, "invitations"));
  await joinerPage.getByRole("button", { name: "Join Invite studio" }).click();
  await expect(joinerPage).toHaveURL(`/app/${ws}/dashboard`);
  await expect(joinerPage.getByText("You joined Invite studio.")).toBeVisible();

  // The revoked link no longer works.
  await joinerPage.goto(revokedLink);
  await expect(
    joinerPage.getByRole("heading", { name: "This invitation has expired" }),
  ).toBeVisible();

  // The owner sees both as members, and nothing pending.
  await page.reload();
  await expect(page.getByRole("cell", { name: /^Ines Invitee/ })).toBeVisible();
  await expect(page.getByRole("cell", { name: /^Jo Joiner/ })).toBeVisible();
  await expect(
    page.getByRole("region", { name: "Pending invitations" }),
  ).toHaveCount(0);
  await joiner.close();
  await invitee.close();
});
