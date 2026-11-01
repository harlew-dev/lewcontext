import { expect, test, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { copyFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { signUpThroughApi } from "./mail";
import { codeFor } from "./totp";
// Alerts and the housekeeping actions (plans/features/implemented/admin/alerts.md):
// an operator sees an open alert in the sidebar's count and on the Alerts
// page, acknowledges it with no code, silences its signal with a reason and
// a fresh code, and resolves an error group from its page.
const stamp = Date.now();
const operatorEmail = `e2e-${stamp}-alerts-operator@lewcontext.test`;
const emails = [operatorEmail];
const adminKey = process.env.E2E_ADMIN_ROUTE_KEY!;
const password = "A thoughtful test passphrase!";
const fingerprint = "a".repeat(64);
// Runs SQL against this run's database (DATABASE_URL, from
// playwright.config.ts): only to grant the operator, check events and
// clean up.
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
    `DELETE FROM alerts WHERE signal = 'http.latency_p95'; DELETE FROM app_errors WHERE fingerprint = '${fingerprint}'; DELETE FROM users WHERE email IN (${quoted})`,
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
// Signs in at the admin door for the first time, setting MFA up with the
// setup key, and returns the key for later codes.
async function firstAdminSignIn(page: Page, email: string): Promise<string> {
  await page.goto(`/admin/${adminKey}`);
  await page.getByLabel("Email").fill(email);
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
  return setupKey;
}
test("an operator acknowledges and silences an alert and resolves an error group", async ({
  browser,
}) => {
  const errors: string[] = [];
  const operator = await browser.newContext();
  const admin = await operator.newPage();
  admin.on("pageerror", (error) => errors.push(error.message));
  admin.on("console", (message) => {
    if (message.text().includes("Content Security Policy"))
      errors.push(message.text());
  });
  await signUpThroughApi(operator.request, {
    name: "Opal Operator",
    email: operatorEmail,
    password,
  });
  await operator.clearCookies();
  sql(
    `INSERT INTO admins (user_id, role, mfa_setup_until) SELECT id, 'operator', now() + interval '24 hours' FROM users WHERE email = '${operatorEmail}'`,
  );
  // An open critical alert and an error group, as the server's loops would
  // leave them.
  sql(
    `INSERT INTO alerts (signal, level, last_value, peak_value, details) VALUES ('http.latency_p95', 'critical', 3200, 4100, '{}')`,
  );
  sql(
    `INSERT INTO app_errors (fingerprint, source, route, message, first_seen_at, last_seen_at) VALUES ('${fingerprint}', 'request', 'GET /api/example', 'ERROR: example failure (SQLSTATE <n>)', now() - interval '1 hour', now())`,
  );
  const setupKey = await firstAdminSignIn(admin, operatorEmail);

  // The sidebar counts it; the Alerts page lists it.
  const alertsLink = admin.getByRole("link", { name: /^Alerts/ });
  await expect(alertsLink).toContainText(/\d/);
  await alertsLink.click();
  await expect(admin.getByRole("heading", { name: "Alerts" })).toBeVisible();
  await expect(admin.getByText("Critical", { exact: true })).toBeVisible();
  await shoot(admin, "alerts-list");
  await admin.getByRole("link", { name: "Latency, 95th percentile" }).click();
  await expect(
    admin.getByRole("heading", { name: "Latency, 95th percentile" }),
  ).toBeVisible();
  await shoot(admin, "alert-page");

  // Acknowledging needs no dialog and no code.
  await admin.getByRole("button", { name: "Acknowledge" }).click();
  // The state badge; the "Acknowledged" row label on the page is a term.
  await expect(
    admin.locator("span").filter({ hasText: /^Acknowledged$/ }),
  ).toBeVisible();
  expect(
    sql(`SELECT count(*) FROM events WHERE name = 'admin.alert.acknowledged'`),
  ).not.toBe("0");

  // Silencing asks why, then for a code.
  await admin.getByRole("button", { name: "Silence" }).click();
  const dialog = admin.getByRole("dialog");
  await dialog
    .getByLabel("Reason")
    .fill("Planned maintenance of the admin host tonight.");
  await shoot(admin, "silence-dialog");
  await dialog.getByRole("button", { name: "Silence" }).click();
  const code = dialog.getByLabel("Authentication code");
  await expect(code).toBeVisible();
  await code.fill(codeFor(setupKey, 1));
  await dialog.getByRole("button", { name: "Silence" }).click();
  await expect(admin.getByText("Silenced", { exact: true })).toBeVisible();
  expect(
    sql(
      `SELECT payload->>'reason' FROM events WHERE name = 'admin.alert.silenced'`,
    ),
  ).toBe("Planned maintenance of the admin host tonight.");

  // Resolving an error group: no dialog either.
  const group = sql(
    `SELECT public_id FROM app_errors WHERE fingerprint = '${fingerprint}'`,
  );
  await admin.goto(`/admin/${adminKey}/monitoring/errors/${group}`);
  await expect(admin.getByRole("button", { name: "Resolve" })).toBeVisible();
  await shoot(admin, "error-resolve");
  await admin.getByRole("button", { name: "Resolve" }).click();
  await expect(admin.getByRole("button", { name: "Resolve" })).toHaveCount(0);
  expect(
    sql(
      `SELECT resolved_at IS NOT NULL FROM app_errors WHERE fingerprint = '${fingerprint}'`,
    ),
  ).toBe("t");
  expect(errors).toEqual([]);
});
