import { expect, test, type Page } from "@playwright/test";
import { createHmac } from "node:crypto";
import { enterMailedCode } from "./mail";
import { execFileSync } from "node:child_process";
const email = `e2e-${Date.now()}-mfa@lewcontext.test`;
const password = "A thoughtful test passphrase!";
// Runs SQL against this run's database, only for cleanup.
function sql(statement: string): string {
  return execFileSync(
    "psql",
    [process.env.DATABASE_URL!, "-v", "ON_ERROR_STOP=1", "-Atc", statement],
    { encoding: "utf8" },
  ).trim();
}
test.afterAll(() => {
  sql(
    `DELETE FROM workspaces WHERE id IN (SELECT m.workspace_id FROM workspace_user m JOIN users u ON u.id = m.user_id WHERE u.email = '${email}'); DELETE FROM users WHERE email = '${email}'`,
  );
});
// An authenticator app's code (RFC 6238: HMAC-SHA1, 30-second steps, 6
// digits) for a base32 setup key, `ahead` steps from now. A step works once,
// and one step of drift is allowed: setup uses this step's code, and signing
// in the next step's, whichever step the clock has reached by then.
function codeFor(key: string, ahead: number): string {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  const bytes: number[] = [];
  let bits = 0;
  let value = 0;
  for (const c of key.replace(/\s/g, "")) {
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
async function signIn(page: Page) {
  await page.context().clearCookies();
  await page.goto("/auth/login");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(
    page.getByRole("heading", { name: "Two-factor authentication" }),
  ).toBeVisible();
}
test("two-factor authentication: enable with a hidden QR code, sign in with a code and a recovery code, disable", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  // Signed out, the login page's session check is a 401, which the console
  // logs too; only CSP violations count.
  page.on("console", (m) => {
    if (m.type() === "error" && m.text().includes("Content Security Policy"))
      errors.push(m.text());
  });

  await page.goto("/auth/register");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Full name").fill("Two Factor");
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Create account" }).click();
  await enterMailedCode(page, email);
  await expect(page).toHaveURL("/app/onboarding");
  await page.getByLabel("Name").fill("Second Factor Co");
  await page.getByRole("button", { name: "Create workspace" }).click();
  await expect(page).toHaveURL(/\/dashboard$/);

  // Enable: the QR code and the key stay hidden until asked for.
  await page.goto("/app/settings");
  await page.getByRole("button", { name: "Enable" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Your password").fill(password);
  await dialog.getByRole("button", { name: "Next" }).click();
  await expect(
    dialog.getByRole("button", { name: "Reveal QR code" }),
  ).toBeVisible();
  await expect(dialog.getByRole("img", { name: /QR code to add/ })).toHaveCount(
    0,
  );
  await dialog.getByRole("button", { name: "Reveal QR code" }).click();
  await expect(
    dialog.getByRole("img", { name: /QR code to add/ }),
  ).toBeVisible();
  await dialog.getByRole("button", { name: "Use setup key instead" }).click();
  // The key starts masked; the eye shows it.
  await dialog.getByRole("button", { name: "Show the key" }).click();
  const key = await dialog
    .getByLabel("Setup key", { exact: true })
    .inputValue();
  expect(key).toMatch(/^[A-Z2-7 ]{39}$/);
  await dialog.getByLabel("Code from your app").fill(codeFor(key, 0));
  // The sixth digit submits.

  // The recovery codes: saved before the dialog lets go.
  const done = dialog.getByRole("button", { name: "I’ve saved these codes" });
  await expect(done).toBeDisabled();
  await dialog.getByRole("button", { name: "Reveal recovery codes" }).click();
  const codes = await dialog
    .getByRole("list", { name: "Recovery codes" })
    .getByRole("listitem")
    .allInnerTexts();
  expect(codes).toHaveLength(10);
  await done.click();
  await expect(
    page.getByText(
      "Two-factor authentication is on. Other sessions were signed out.",
    ),
  ).toBeVisible();
  await expect(page.getByText(/^On since/)).toBeVisible();

  // Signing in needs a code now: the password alone starts no session.
  await signIn(page);
  expect(
    (await page.context().cookies()).some(
      (c) => c.name === "lewcontext_session",
    ),
  ).toBe(false);
  await page.getByLabel("Authentication code").fill(codeFor(key, 1));
  // The sixth digit submits.
  await expect(page).toHaveURL(/\/dashboard$/);

  // A recovery code instead, once.
  await signIn(page);
  await page
    .getByRole("button", { name: "Lost your device? Use a recovery code" })
    .click();
  await page.getByLabel("Recovery code").fill(codes[0]);
  await page.getByRole("button", { name: "Verify" }).click();
  await expect(page).toHaveURL(/\/dashboard$/);

  // Disable, with the password and another recovery code.
  await page.goto("/app/settings");
  await page.getByRole("button", { name: "Disable" }).click();
  await dialog.getByLabel("Your password").fill(password);
  await dialog
    .getByRole("button", { name: "Lost your device? Use a recovery code" })
    .click();
  await dialog.getByLabel("Recovery code").fill(codes[1]);
  await dialog
    .getByRole("button", { name: "Disable two-factor authentication" })
    .click();
  await expect(
    page.getByText("Two-factor authentication is off."),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Enable" })).toBeVisible();

  await page.context().clearCookies();
  await page.goto("/auth/login");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/dashboard$/);
  expect(errors).toEqual([]);
});
