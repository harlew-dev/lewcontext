import { expect, test, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { enterMailedCode } from "./mail";
import { settingsPort, startServer, stopServer } from "./settings-server";
// The terms of service and privacy policy (plans/features/implemented/
// policies.md). The main server keeps the default, POLICIES=off: the pages
// are unknown URLs and sign-up asks nothing. A second server with
// POLICIES=on (settings-server.ts) publishes them from the same build.
const stamp = Date.now();
const email = `e2e-${stamp}-agrees@lewcontext.test`;
const password = "A thoughtful test passphrase!";
const dashboard = /\/app\/wsp_[0-9A-Za-z]{14}\/dashboard$/;
test.afterAll(async () => {
  await stopServer();
  execFileSync(
    "psql",
    [
      process.env.DATABASE_URL!,
      "-v",
      "ON_ERROR_STOP=1",
      "-Atc",
      `DELETE FROM workspaces WHERE id IN (SELECT m.workspace_id FROM workspace_user m JOIN users u ON u.id = m.user_id WHERE u.email = '${email}'); DELETE FROM users WHERE email = '${email}'`,
    ],
    { encoding: "utf8" },
  );
});
// Records CSP violations the page reports.
function cspViolations(page: Page): string[] {
  const found: string[] = [];
  page.on("console", (message) => {
    if (message.text().includes("Content Security Policy"))
      found.push(message.text());
  });
  return found;
}
test("policies off: the pages are unknown and sign-up asks nothing", async ({
  page,
  request,
}) => {
  expect(await (await request.get("/api/policies")).json()).toEqual({
    enabled: false,
  });
  for (const path of [
    "/policy/terms-of-service",
    "/policy/privacy-policy",
    "/policy/terms-of-service.data",
    "/.policies.json",
  ]) {
    const response = await request.get(path);
    expect(response.status(), path).toBe(404);
    expect(await response.text(), path).not.toContain("Last updated");
  }
  await page.goto("/policy/terms-of-service");
  await expect(
    page.getByRole("heading", { name: "This page has wandered off." }),
  ).toBeVisible();
  await page.goto("/auth/register");
  await expect(
    page.getByRole("heading", { name: "Create your account" }),
  ).toBeVisible();
  await expect(page.getByRole("checkbox")).toHaveCount(0);
  await expect(page.getByRole("link", { name: "Privacy Policy" })).toHaveCount(
    0,
  );
});
test("policies on: published pages, and sign-up agrees to them", async ({
  browser,
}) => {
  test.skip(
    !settingsPort,
    "needs the second port make test-e2e reserves (E2E_SETTINGS_PORT)",
  );
  const origin = await startServer({ POLICIES: "on" });
  const context = await browser.newContext();
  const page = await context.newPage();
  const violations = cspViolations(page);

  // Both pages are prerendered: their HTML holds the document, with a CSP
  // that hashes their inline scripts.
  for (const [path, title] of [
    ["/policy/terms-of-service", "Terms of Service"],
    ["/policy/privacy-policy", "Privacy Policy"],
  ]) {
    const response = await page.request.get(`${origin}${path}`);
    expect(response.status()).toBe(200);
    const html = await response.text();
    expect(html).toContain(`<title>${title} · `);
    expect(html).toMatch(new RegExp(`<h1[^>]*>${title}</h1>`));
    expect(html).toContain("Last updated");
    const csp = response.headers()["content-security-policy"];
    expect(csp.split(";").find((d) => d.includes("script-src"))).not.toContain(
      "unsafe-inline",
    );
  }
  await page.goto(`${origin}/policy/terms-of-service`);
  await expect(
    page.getByRole("heading", { level: 1, name: "Terms of Service" }),
  ).toBeVisible();
  // In-app navigation to the other one loads its .data file.
  const data = page.waitForResponse((r) =>
    r.url().includes("/policy/privacy-policy.data"),
  );
  await page
    .locator("article")
    .getByRole("link", { name: "Privacy Policy" })
    .last()
    .click();
  expect((await data).status()).toBe(200);
  await expect(
    page.getByRole("heading", { level: 1, name: "Privacy Policy" }),
  ).toBeVisible();

  // Sign-up can't go on unticked; ticked, it agrees to the versions shown.
  await page.goto(`${origin}/auth/register`);
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Full name").fill("Paula Policy");
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page).toHaveURL(`${origin}/auth/register`);
  const box = page.getByRole("checkbox", { name: /^I agree to the Terms/ });
  expect(await box.evaluate((el: HTMLInputElement) => el.validity.valid)).toBe(
    false,
  );
  await page.getByText("I agree to the").click();
  await expect(box).toBeChecked();
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page).toHaveURL(`${origin}/auth/register/verify`);
  await enterMailedCode(page, email);
  await expect(page).toHaveURL(`${origin}/app/onboarding`);
  await page.getByLabel("Name").fill("Paula’s workspace");
  await page.getByRole("button", { name: "Create workspace" }).click();
  await expect(page).toHaveURL(dashboard);
  await page.goto(`${origin}/app/settings`);
  await expect(
    page.getByText(/^You agreed to the Terms of Service \(/),
  ).toBeVisible();
  expect(violations).toEqual([]);
  await context.close();
});
