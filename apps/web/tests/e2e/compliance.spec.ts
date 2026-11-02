import { expect, test, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { copyFileSync, mkdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { signUpThroughApi } from "./mail";
import { codeFor } from "./totp";
// Compliance (docs/compliance.md): someone downloads their data from
// Account & security, and an admin downloads the Events list as CSV; both
// recorded.
const stamp = Date.now();
const personEmail = `e2e-${stamp}-data-person@lewcontext.test`;
const adminEmail = `e2e-${stamp}-data-admin@lewcontext.test`;
const emails = [personEmail, adminEmail];
const adminKey = process.env.E2E_ADMIN_ROUTE_KEY!;
const password = "A thoughtful test passphrase!";
// Runs SQL against this run's database (DATABASE_URL): only to grant the
// admin, check events and clean up.
function sql(statement: string): string {
  return execFileSync(
    "psql",
    [process.env.DATABASE_URL!, "-v", "ON_ERROR_STOP=1", "-Atc", statement],
    { encoding: "utf8" },
  ).trim();
}
test.afterAll(() => {
  const quoted = emails.map((e) => `'${e}'`).join(", ");
  sql(
    `DELETE FROM workspaces WHERE id IN (SELECT m.workspace_id FROM workspace_user m JOIN users u ON u.id = m.user_id WHERE u.email IN (${quoted})); DELETE FROM users WHERE email IN (${quoted})`,
  );
});
// Attaches a screenshot to the results and, with E2E_SCREENSHOTS set, copies
// it there too, for review.
async function shoot(page: Page, name: string) {
  const path = test.info().outputPath(`${name}.png`);
  await page.screenshot({ path });
  await test.info().attach(name, { path, contentType: "image/png" });
  const dir = process.env.E2E_SCREENSHOTS;
  if (dir) {
    mkdirSync(dir, { recursive: true });
    copyFileSync(path, join(dir, `${name}.png`));
  }
}
test("someone downloads their data, after their password", async ({
  browser,
}) => {
  const context = await browser.newContext({ acceptDownloads: true });
  const page = await context.newPage();
  const signedIn = await signUpThroughApi(context.request, {
    name: "Dara Data",
    email: personEmail,
    password,
  });
  const created = await context.request.post("/api/workspaces", {
    headers: {
      Origin: process.env.APP_ORIGIN!,
      "Content-Type": "application/json",
      "X-CSRF-Token": signedIn.csrfToken,
    },
    data: { name: "Dara's workspace" },
  });
  expect(created.status()).toBe(201);
  await page.goto("/app/settings");
  const download = page.getByRole("button", { name: "Download your data" });
  await download.scrollIntoViewIfNeeded();
  await download.click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("heading")).toHaveText("Download your data");
  await dialog.getByLabel("Your password").fill("not the password");
  await dialog.getByRole("button", { name: "Download" }).click();
  await expect(
    dialog.getByText("Current password is incorrect."),
  ).toBeVisible();
  await dialog.getByLabel("Your password").fill(password);
  await shoot(page, "download-my-data-dialog");
  const [file] = await Promise.all([
    page.waitForEvent("download"),
    dialog.getByRole("button", { name: "Download" }).click(),
  ]);
  expect(file.suggestedFilename()).toMatch(
    /^lewcontext-export-\d{4}-\d{2}-\d{2}\.json$/,
  );
  const text = readFileSync((await file.path())!, "utf8");
  const data = JSON.parse(text);
  expect(data.account.email).toBe(personEmail);
  expect(data.ownedWorkspaces[0].name).toBe("Dara's workspace");
  expect(text).not.toMatch(/password_?hash|csrf|token|secret/i);
  await expect(page.getByText("Your data has been downloaded.")).toBeVisible();
  await shoot(page, "download-my-data-done");
  expect(
    sql(
      `SELECT count(*) FROM events e JOIN users u ON u.id = e.user_id WHERE e.name = 'auth.data_exported' AND u.email = '${personEmail}'`,
    ),
  ).toBe("1");
  await context.close();
});
test("an admin downloads the Events list as CSV, recorded with its filters", async ({
  browser,
}) => {
  const context = await browser.newContext({ acceptDownloads: true });
  const page = await context.newPage();
  await signUpThroughApi(context.request, {
    name: "Ada Admin",
    email: adminEmail,
    password,
  });
  await context.clearCookies();
  sql(
    `INSERT INTO admins (user_id, role, mfa_setup_until) SELECT id, 'viewer', now() + interval '24 hours' FROM users WHERE email = '${adminEmail}'`,
  );
  await page.goto(`/admin/${adminKey}`);
  await page.getByLabel("Email").fill(adminEmail);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.getByRole("button", { name: "Use setup key instead" }).click();
  await page.getByRole("button", { name: "Show the key" }).click();
  const setupKey = await page
    .getByLabel("Setup key", { exact: true })
    .inputValue();
  await page.getByLabel("Code from your app").fill(codeFor(setupKey, 0));
  await page.getByRole("button", { name: "Reveal recovery codes" }).click();
  await page.getByRole("button", { name: "I’ve saved these codes" }).click();
  await expect(page.getByRole("heading", { name: "Users" })).toBeVisible();

  // Its own sign-up's events: the admin's address, which it typed.
  await page.goto(`/admin/${adminKey}/events?email=${adminEmail}&window=7d`);
  const button = page.getByRole("button", { name: "Download CSV" });
  await expect(button).toBeEnabled();
  await shoot(page, "events-download-csv");
  const [file] = await Promise.all([
    page.waitForEvent("download"),
    button.click(),
  ]);
  expect(file.suggestedFilename()).toMatch(/^events-\d{4}-\d{2}-\d{2}\.csv$/);
  const csv = readFileSync((await file.path())!, "utf8");
  expect(csv.split("\r\n")[0]).toBe(
    "time,event,name,failed,actor,actor_email,user,user_email,workspace,workspace_name,ip,payload",
  );
  expect(csv).toContain("auth.registration.requested");
  await expect(page.getByText(/^Downloaded \d+ events?\.$/)).toBeVisible();
  expect(
    sql(
      `SELECT payload->'filters'->>'email' FROM events WHERE name = 'admin.event.exported' ORDER BY id DESC LIMIT 1`,
    ),
  ).toBe(adminEmail);
  await context.close();
});
