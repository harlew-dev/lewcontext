import { expect, test, type Browser, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { enterMailedCode } from "./mail";
import { settingsPort, startServer, stopServer } from "./settings-server";
// Turning email verification on for an install that has accounts, as
// docs/deployment.md describes: accounts made while it was off, then
// verification on (unverified accounts may verify from Account settings),
// then required (they must, at their next sign-in).
//
// The main server runs with the defaults (on, optional) for the whole run.
// This spec also starts and stops a second server with other settings
// (settings-server.ts). Both share the run's APP_KEY, since either one's
// worker may send a code the other checks.
const stamp = Date.now();
const ada = `e2e-${stamp}-ada-rollout@lewcontext.test`;
const ben = `e2e-${stamp}-ben-rollout@lewcontext.test`;
const password = "A thoughtful test passphrase!";
const dashboard = /\/app\/wsp_[0-9A-Za-z]{14}\/dashboard$/;
test.skip(
  !settingsPort,
  "needs the second port make test-e2e reserves (E2E_SETTINGS_PORT)",
);
function sql(statement: string): string {
  return execFileSync(
    "psql",
    [process.env.DATABASE_URL!, "-v", "ON_ERROR_STOP=1", "-Atc", statement],
    { encoding: "utf8" },
  ).trim();
}
test.afterAll(async () => {
  await stopServer();
  const quoted = [ada, ben].map((e) => `'${e}'`).join(", ");
  sql(
    `DELETE FROM workspaces WHERE id IN (SELECT m.workspace_id FROM workspace_user m JOIN users u ON u.id = m.user_id WHERE u.email IN (${quoted})); DELETE FROM users WHERE email IN (${quoted})`,
  );
});
// A page in a browser context of its own: both servers are on localhost,
// and browsers share cookies across ports.
async function freshPage(browser: Browser) {
  return (await browser.newContext()).newPage();
}
async function signIn(page: Page, origin: string, email: string) {
  await page.goto(`${origin}/auth/login`);
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
}
test("email verification rollout: off, then on, then required", async ({
  browser,
}) => {
  // 1. Off: sign-up as before, no code, and a taken email says so.
  let origin = await startServer({ EMAIL_VERIFICATION: "off" });
  for (const [email, name] of [
    [ada, "Ada Rollout"],
    [ben, "Ben Rollout"],
  ]) {
    const page = await freshPage(browser);
    await page.goto(`${origin}/auth/register`);
    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Full name").fill(name);
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByRole("button", { name: "Create account" }).click();
    await expect(page).toHaveURL(`${origin}/app/onboarding`);
    await page.getByLabel("Name").fill(`${name}’s workspace`);
    await page.getByRole("button", { name: "Create workspace" }).click();
    await expect(page).toHaveURL(dashboard);
    // Nothing about verification shows.
    await page.goto(`${origin}/app/settings`);
    await expect(
      page.getByLabel("Email address", { exact: true }),
    ).toBeVisible();
    await expect(page.getByText("Not verified")).toHaveCount(0);
    await page.context().close();
  }
  const again = await freshPage(browser);
  await again.goto(`${origin}/auth/register`);
  await again.getByLabel("Email").fill(ada);
  await again.getByLabel("Full name").fill("Ada Again");
  await again.getByLabel("Password", { exact: true }).fill(password);
  await again.getByRole("button", { name: "Create account" }).click();
  await expect(
    again
      .getByRole("alert")
      .filter({ hasText: "Unable to create this account" }),
  ).toBeVisible();
  await again.context().close();
  await stopServer();

  // 2. The main server (on, optional): Ada verifies from Account settings;
  // Ben, still unverified, isn't stopped.
  const main = process.env.APP_ORIGIN!;
  const adaPage = await freshPage(browser);
  await signIn(adaPage, main, ada);
  await expect(adaPage).toHaveURL(dashboard);
  await adaPage.goto(`${main}/app/settings`);
  await expect(adaPage.getByLabel("Email address Not verified")).toBeVisible();
  await adaPage.getByRole("button", { name: "Verify email" }).click();
  const dialog = adaPage.getByRole("dialog");
  await dialog.getByRole("button", { name: "Send code" }).click();
  await enterMailedCode(adaPage, ada);
  await expect(adaPage.getByText("Email verified.")).toBeVisible();
  await expect(adaPage.getByLabel("Email address Verified")).toBeVisible();
  await adaPage.context().close();
  const benPage = await freshPage(browser);
  await signIn(benPage, main, ben);
  await expect(benPage).toHaveURL(dashboard);
  await benPage.context().close();

  // 3. Required: Ben must verify at his next sign-in before anything else;
  // Ada, verified, goes straight in.
  origin = await startServer({
    EMAIL_VERIFICATION: "on",
    EMAIL_VERIFICATION_EXISTING: "required",
  });
  const gated = await freshPage(browser);
  await signIn(gated, origin, ben);
  await expect(gated).toHaveURL(new RegExp(`^${origin}/app/verify-email`));
  await expect(
    gated.getByText("You must verify your email to continue."),
  ).toBeVisible();
  const refused = await gated.request.get(`${origin}/api/workspaces`);
  expect(refused.status()).toBe(403);
  expect(await refused.json()).toMatchObject({ verificationRequired: true });
  // A signed-in page sends him back here.
  await gated.goto(`${origin}/app/settings`);
  await expect(gated).toHaveURL(
    `${origin}/app/verify-email?next=%2Fapp%2Fsettings`,
  );
  await gated.getByRole("button", { name: "Send code" }).click();
  await enterMailedCode(gated, ben);
  await expect(gated).toHaveURL(`${origin}/app/settings`);
  await expect(gated.getByLabel("Email address Verified")).toBeVisible();
  await gated.context().close();
  const verified = await freshPage(browser);
  await signIn(verified, origin, ada);
  await expect(verified).toHaveURL(dashboard);
  await verified.context().close();
  await stopServer();
});
