import { expect, test } from "@playwright/test";
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
test("register, create, loading, filter, edit, SSR, security, mobile, logout, and login", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/");
  await expect(page).toHaveURL("/login");
  await page.getByRole("button", { name: "Create an account" }).click();
  await page.getByLabel("Your name").fill("Taylor Example");
  await page.getByLabel("Email address").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Create your workspace" }).click();
  await expect(
    page.getByRole("heading", { name: "Welcome back, Taylor." }),
  ).toBeVisible();
  await expect(page.getByText("Room for your next big thing")).toBeVisible();
  await page.getByRole("button", { name: "New project" }).click();
  await page
    .getByLabel("Project name", { exact: true })
    .fill("Launch the good stuff");
  await page
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
  await page
    .getByRole("button", { name: "Create project", exact: true })
    .click();
  await expect(
    page.getByRole("dialog").locator('button[type="submit"]'),
  ).toBeDisabled();
  release();
  await expect(
    page.getByRole("status").filter({ hasText: "Project created" }),
  ).toBeVisible();
  await page.unroute("**/api/projects");
  await page
    .getByRole("button", { name: "Edit Launch the good stuff" })
    .click();
  await page.getByRole("combobox").click();
  await page.getByRole("option", { name: "Completed", exact: true }).click();
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Project updated" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Planned", exact: true }).click();
  await expect(page.getByText("No matching projects")).toBeVisible();
  await page.getByRole("button", { name: /All projects/ }).click();
  await page
    .getByRole("textbox", { name: "Search projects" })
    .fill("not-a-match");
  await expect(page.getByText("No matching projects")).toBeVisible();
  await page.getByRole("textbox", { name: "Search projects" }).fill("");
  const html = await (await page.request.get("/")).text();
  expect(html).toContain("Launch the good stuff");
  expect(html).toContain("_styletron_hydrate_");
  const response = await page.request.get("/");
  expect(response.headers()["content-security-policy"]).toContain("nonce-");
  expect(response.headers()["cache-control"]).toContain("no-store");
  await page.getByRole("link", { name: "Projects", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Your projects." }),
  ).toBeVisible();
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
    page.getByRole("button", { name: "Edit Launch the good stuff" }),
  ).toBeVisible();
  await page.unroute("**/api/projects");
  await page.getByRole("button", { name: "Try again" }).click();
  await expect(
    page
      .getByRole("alert")
      .filter({ hasText: "Temporary service interruption" }),
  ).toHaveCount(0);
  await page.getByRole("link", { name: "Account & security" }).click();
  await page.getByRole("button", { name: "Sign out other sessions" }).click();
  await expect(
    page.getByText("All other sessions have been signed out."),
  ).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Welcome back, Taylor." }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(page).toHaveURL("/login");
  await page.getByLabel("Email address").fill(email);
  await page
    .getByLabel("Password", { exact: true })
    .fill("A wrong password long enough");
  await page.getByRole("button", { name: "Sign in to your workspace" }).click();
  await expect(
    page.getByRole("alert").filter({ hasText: "Email or password" }),
  ).toHaveText("Email or password is incorrect.");
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in to your workspace" }).click();
  await expect(
    page.getByRole("heading", { name: "Welcome back, Taylor." }),
  ).toBeVisible();
  expect(errors).toEqual([]);
});
test("unauthenticated API requests are rejected", async ({ request }) => {
  const response = await request.get("/api/projects");
  expect(response.status()).toBe(401);
});
