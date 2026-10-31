import { expect, test, type Page } from "@playwright/test";
import { createHmac } from "node:crypto";
import { execFileSync } from "node:child_process";
import { signUpThroughApi } from "./mail";
// The run's admin key, made by playwright.config.ts and passed to the server.
const key = process.env.E2E_ADMIN_ROUTE_KEY!;
const stamp = Date.now();
const adminEmail = `e2e-${stamp}-admin@lewcontext.test`;
const otherEmail = `e2e-${stamp}-not-admin@lewcontext.test`;
const password = "A thoughtful test passphrase!";
// Runs SQL against this run's database: granting admin (only the operator's
// CLI does that, never the API), and cleanup.
function sql(statement: string): string {
  return execFileSync(
    "psql",
    [process.env.DATABASE_URL!, "-v", "ON_ERROR_STOP=1", "-Atc", statement],
    { encoding: "utf8" },
  ).trim();
}
test.afterAll(() => {
  sql(`DELETE FROM users WHERE email IN ('${adminEmail}', '${otherEmail}')`);
});
// An authenticator app's code for a base32 setup key, `ahead` 30-second
// steps from now (RFC 6238), as mfa.spec.ts computes it.
function codeFor(setupKey: string, ahead: number): string {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  const bytes: number[] = [];
  let bits = 0;
  let value = 0;
  for (const c of setupKey.replace(/\s/g, "")) {
    value = (value << 5) | alphabet.indexOf(c);
    bits += 5;
    if (bits >= 8) {
      bytes.push((value >>> (bits - 8)) & 255);
      bits -= 8;
    }
  }
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(Date.now() / 30000) + ahead));
  const mac = createHmac("sha1", Buffer.from(bytes)).update(counter).digest();
  const offset = mac[mac.length - 1] & 15;
  return String((mac.readUInt32BE(offset) & 0x7fffffff) % 1_000_000).padStart(
    6,
    "0",
  );
}
// Creates an account through the API, then forgets its session.
async function register(page: Page, email: string, name: string) {
  await signUpThroughApi(page.request, { name, email, password });
  await page.context().clearCookies();
}
async function passwordStep(page: Page, email: string) {
  await page.goto(`/admin/${key}`);
  await expect(
    page.getByRole("heading", { name: "Admin sign-in" }),
  ).toBeVisible();
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
}
test("a wrong key is the ordinary 404 page", async ({ page }) => {
  for (const path of [`/admin/${key}x`, "/admin/x/users", "/admin"]) {
    const response = await page.goto(path);
    expect(response?.status()).toBe(404);
    await expect(page.getByText("This page has wandered off.")).toBeVisible();
    await expect(page.getByText("Admin sign-in")).toHaveCount(0);
  }
});
test("the admin door: first sign-in sets up MFA, then Users, Workspaces, sign out and back in with a code", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => {
    if (m.type() === "error" && m.text().includes("Content Security Policy"))
      errors.push(m.text());
  });
  await register(page, adminEmail, "Olive Admin");
  await register(page, otherEmail, "Nora Notadmin");
  // admin grant, as the operator's CLI does it: MFA off, so a 24-hour
  // window to set it up at the first admin sign-in.
  sql(
    `INSERT INTO admins (user_id, mfa_setup_until) SELECT id, now() + interval '24 hours' FROM users WHERE email = '${adminEmail}'`,
  );

  // The door answers with stricter headers.
  const door = await page.goto(`/admin/${key}`);
  expect(door?.status()).toBe(200);
  expect(door?.headers()["referrer-policy"]).toBe("no-referrer");
  expect(door?.headers()["x-robots-tag"]).toBe("noindex, nofollow");

  // A non-admin's right password gets the same refusal as a wrong one.
  await passwordStep(page, otherEmail);
  await expect(page.getByText("Email or password is incorrect.")).toBeVisible();

  // The first sign-in sets MFA up, with the QR code hidden until revealed.
  await passwordStep(page, adminEmail);
  await expect(
    page.getByRole("heading", { name: "Set up two-factor authentication" }),
  ).toBeVisible();
  await expect(page.getByRole("img", { name: /QR code to add/ })).toHaveCount(
    0,
  );
  await page.getByRole("button", { name: "Reveal QR code" }).click();
  await expect(page.getByRole("img", { name: /QR code to add/ })).toBeVisible();
  await page.getByRole("button", { name: "Use setup key instead" }).click();
  await page.getByRole("button", { name: "Show the key" }).click();
  const setupKey = await page
    .getByLabel("Setup key", { exact: true })
    .inputValue();
  await page.getByLabel("Code from your app").fill(codeFor(setupKey, 0));
  // The sixth digit submits.
  const done = page.getByRole("button", { name: "I’ve saved these codes" });
  await expect(done).toBeDisabled();
  await page.getByRole("button", { name: "Reveal recovery codes" }).click();
  await done.click();

  // Users: search, open the admin's own row.
  await expect(page).toHaveURL(`/admin/${key}/users`);
  await expect(page.getByRole("heading", { name: "Users" })).toBeVisible();
  await page.getByRole("searchbox", { name: "Search users" }).fill(adminEmail);
  await expect(page).toHaveURL(new RegExp(`/admin/${key}/users\\?q=`));
  await page.getByRole("link", { name: "Open Olive Admin" }).first().click();
  await expect(
    page.getByRole("heading", { name: "Olive Admin" }),
  ).toBeVisible();
  await expect(page.getByText("Not in any workspace.")).toBeVisible();

  // Workspaces.
  await page.getByRole("link", { name: "Workspaces" }).first().click();
  // Exact: the empty list's "No workspaces yet" is a heading too.
  await expect(
    page.getByRole("heading", { name: "Workspaces", exact: true }),
  ).toBeVisible();
  // The key is in the URL only, never in the page's text.
  expect(await page.locator("body").innerText()).not.toContain(key);

  // Signing out of the dashboard, then in again: a code this time.
  await page.getByRole("button", { name: "Admin account menu" }).click();
  await page.getByRole("menuitem", { name: "Sign out" }).click();
  await expect(
    page.getByRole("heading", { name: "Admin sign-in" }),
  ).toBeVisible();
  const inner = await page.goto(`/admin/${key}/users`);
  expect(inner?.url()).toMatch(new RegExp(`/admin/${key}$`));
  await passwordStep(page, adminEmail);
  await expect(
    page.getByRole("heading", { name: "Two-factor authentication" }),
  ).toBeVisible();
  await page.getByLabel("Authentication code").fill(codeFor(setupKey, 1));
  // The sixth digit submits.
  await expect(page.getByRole("heading", { name: "Users" })).toBeVisible();
  expect(errors).toEqual([]);

  // Every read was recorded in the audit trail.
  expect(
    Number(
      sql(
        `SELECT count(*) FROM events WHERE name = 'admin.user.viewed' AND payload->>'admin' = (SELECT public_id FROM users WHERE email = '${adminEmail}')`,
      ),
    ),
  ).toBe(1);
});
