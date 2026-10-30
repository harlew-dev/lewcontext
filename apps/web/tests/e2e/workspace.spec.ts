import { expect, test, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
const email = `e2e-${Date.now()}@forma.test`;
const password = "A thoughtful test passphrase!";
test.afterAll(() => {
  // Only remove the uniquely named account created by this test. Cascades remove its projects/sessions.
  if (process.env.TEST_DATABASE_URL)
    execFileSync(
      "psql",
      [
        process.env.TEST_DATABASE_URL,
        "-v",
        "ON_ERROR_STOP=1",
        "-c",
        `DELETE FROM users WHERE email='${email}'`,
      ],
      { stdio: "ignore" },
    );
});
test("register, create, loading, filter, edit, paginate, SSR, security, mobile, logout, and login", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/");
  await expect(page).toHaveURL("/auth/login");
  await page.getByRole("link", { name: "Sign up" }).click();
  await expect(page).toHaveURL("/auth/register");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Full name").fill("Taylor Example");
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page).toHaveURL("/app/dashboard");
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
  await page.route("**/api/projects", async (route) => {
    if (route.request().method() === "POST") await gate;
    await route.continue();
  });
  await dialog.getByRole("button", { name: "Create project" }).click();
  await expect(dialog.locator('button[type="submit"]')).toBeDisabled();
  release();
  await expect(
    page.getByRole("status").filter({ hasText: "Project created" }),
  ).toBeVisible();
  await page.unroute("**/api/projects");
  await page
    .getByRole("link", { name: "Launch the good stuff" })
    .first()
    .click();
  await expect(page).toHaveURL(/\/app\/projects\/\d+$/);
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
  expect((await page.request.get("/app/projects/999999999")).status()).toBe(
    404,
  );
  await page.goto("/app/dashboard");
  const html = await (await page.request.get("/app/dashboard")).text();
  expect(html).toContain("Launch the good stuff");
  expect(html).toContain("_styletron_hydrate_");
  const response = await page.request.get("/app/dashboard");
  expect(response.headers()["content-security-policy"]).toContain("nonce-");
  expect(response.headers()["cache-control"]).toContain("no-store");
  await page.getByRole("link", { name: "Projects", exact: true }).click();
  await expect(page).toHaveURL("/app/projects");
  await expect(page.getByRole("heading", { name: "Projects" })).toBeVisible();
  await page.getByRole("combobox", { name: /Filter by status/ }).click();
  await page.getByRole("option", { name: "Planned", exact: true }).click();
  await expect(page.getByText("No matching projects")).toBeVisible();
  await page.getByRole("combobox", { name: /Filter by status/ }).click();
  await page.getByRole("option", { name: "All statuses" }).click();
  await page
    .getByRole("searchbox", { name: "Search projects" })
    .fill("not-a-match");
  await expect(page.getByText("No matching projects")).toBeVisible();
  await page.getByRole("searchbox", { name: "Search projects" }).fill("");
  await page.route("**/api/projects", (route) =>
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
  await page.unroute("**/api/projects");
  await page.getByRole("button", { name: "Try again" }).click();
  await expect(
    page
      .getByRole("alert")
      .filter({ hasText: "Temporary service interruption" }),
  ).toHaveCount(0);
  await paginate(page);
  await page.getByRole("link", { name: "Account & security" }).click();
  await page.getByRole("button", { name: "Sign out other sessions" }).click();
  await expect(
    page.getByText("All other sessions have been signed out."),
  ).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/app/dashboard");
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
  await expect(
    page.getByRole("heading", { name: "Welcome back, Taylor." }),
  ).toBeVisible();
  expect(errors).toEqual([]);
});
test("root and pre-nesting URLs redirect to the nested pages", async ({
  request,
}) => {
  const redirects = {
    "/": "/app/dashboard",
    "/login": "/auth/login",
    "/projects": "/app/projects",
    "/settings": "/app/settings",
  };
  for (const [from, to] of Object.entries(redirects)) {
    const response = await request.get(from, { maxRedirects: 0 });
    expect(response.status()).toBe(307);
    expect(response.headers()["location"]).toBe(to);
  }
  const dashboard = await request.get("/app/dashboard", { maxRedirects: 0 });
  expect(dashboard.headers()["location"]).toBe("/auth/login");
});
test("unauthenticated API requests are rejected", async ({ request }) => {
  const response = await request.get("/api/projects");
  expect(response.status()).toBe(401);
});
// Paging, sorting and searching run in the API: create a second page of
// projects through it, then check the URL drives what the table shows.
async function paginate(page: Page) {
  const me = await (await page.request.get("/api/auth/me")).json();
  const headers = {
    Origin: new URL(page.url()).origin,
    "Content-Type": "application/json",
    "X-CSRF-Token": me.csrfToken,
  };
  for (let i = 0; i < 22; i++) {
    const response = await page.request.post("/api/projects", {
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
  await page.goto("/app/projects");
  await expect(page.getByText("23 projects in your workspace")).toBeVisible();
  const rows = page.locator("tbody tr");
  await expect(rows).toHaveCount(20);
  await expect(rows.first()).toContainText("Bulk 21");
  const pages = page.getByRole("navigation", { name: "Projects pages" });
  await pages.getByRole("link", { name: "Next page" }).click();
  await expect(page).toHaveURL("/app/projects?page=2");
  await expect(rows).toHaveCount(3);
  await expect(rows.last()).toContainText("Launch the good stuff");
  await expect(pages.getByRole("link", { name: "Page 2" })).toHaveAttribute(
    "aria-current",
    "page",
  );
  // Sorting starts again from page 1; a second click reverses it.
  await page.getByRole("link", { name: "Project", exact: true }).click();
  await expect(page).toHaveURL("/app/projects?sort=name");
  await expect(rows.first()).toContainText("Bulk 00");
  await expect(
    page.getByRole("columnheader", { name: "Project" }),
  ).toHaveAttribute("aria-sort", "ascending");
  await page.getByRole("link", { name: "Project", exact: true }).click();
  await expect(page).toHaveURL("/app/projects?sort=-name");
  await expect(rows.first()).toContainText("Launch the good stuff");
  await page.goBack();
  await expect(page).toHaveURL("/app/projects?sort=name");
  await expect(rows.first()).toContainText("Bulk 00");
  // The server searches every page, not only the loaded one.
  await page.getByRole("searchbox", { name: "Search projects" }).fill("launch");
  await expect(page).toHaveURL("/app/projects?q=launch&sort=name");
  await expect(rows).toHaveCount(1);
  await expect(page.getByText("1 matching project")).toBeVisible();
  await page.getByRole("searchbox", { name: "Search projects" }).fill("");
  await expect(page).toHaveURL("/app/projects?sort=name");
  // A URL renders the same page on the server.
  const html = await (await page.request.get("/app/projects?page=2")).text();
  expect(html).toContain("Launch the good stuff");
  expect(html).not.toContain("Bulk 21");
  await page.goto("/app/projects?page=9");
  await expect(page.getByText("There’s nothing on page 9")).toBeVisible();
  await page.getByRole("link", { name: "Go to last page" }).click();
  await expect(page).toHaveURL("/app/projects?page=2");
  await expect(rows).toHaveCount(3);
  // A project created from page 2 shows first on page 1.
  await page.getByRole("button", { name: "New project" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Project name", { exact: true }).fill("Fresh idea");
  await dialog.getByRole("button", { name: "Create project" }).click();
  await expect(page).toHaveURL("/app/projects");
  await expect(rows.first()).toContainText("Fresh idea");
  await expect(page.getByText("24 projects in your workspace")).toBeVisible();
}
