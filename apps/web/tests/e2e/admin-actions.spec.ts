import { expect, test, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { copyFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { signUpThroughApi } from "./mail";
import { codeFor } from "./totp";
// The admin actions (plans/features/admin/actions.md): an operator suspends
// an account, whose open session ends and whose sign-in then says why; the
// operator unsuspends it; and blocks an address on the IP blocks page.
const stamp = Date.now();
const operatorEmail = `e2e-${stamp}-actions-operator@lewcontext.test`;
const targetEmail = `e2e-${stamp}-actions-target@lewcontext.test`;
const emails = [operatorEmail, targetEmail];
const adminKey = process.env.E2E_ADMIN_ROUTE_KEY!;
const password = "A thoughtful test passphrase!";
const blockedNetwork = "203.0.113.0/24";
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
    `DELETE FROM ip_blocks WHERE network = '${blockedNetwork}'; DELETE FROM users WHERE email IN (${quoted})`,
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
async function signIn(page: Page, email: string) {
  await page.goto("/auth/login");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
}
test("an operator suspends an account, which is signed out and told why at sign-in, then unsuspends it", async ({
  browser,
}) => {
  const errors: string[] = [];
  // The account, signed in on its own browser.
  const target = await browser.newContext();
  const person = await target.newPage();
  await signUpThroughApi(target.request, {
    name: "Tess Target",
    email: targetEmail,
    password,
  });
  // No workspace yet: onboarding, a signed-in page.
  await person.goto("/app");
  await expect(
    person.getByRole("heading", { name: "New workspace" }),
  ).toBeVisible();

  // An operator, granted from the command line.
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
  const setupKey = await firstAdminSignIn(admin, operatorEmail);

  // The account's page, with its Actions.
  await admin
    .getByRole("searchbox", { name: "Search users" })
    .fill(targetEmail);
  await admin.getByRole("row").filter({ hasText: "Tess Target" }).click();
  await expect(
    admin.getByRole("heading", { name: "Tess Target" }),
  ).toBeVisible();
  const suspend = admin.getByRole("button", { name: "Suspend account" });
  await suspend.scrollIntoViewIfNeeded();
  await shoot(admin, "user-page-actions");

  // Suspend: a reason, "Also revoke invitations" ticked, then a code.
  await suspend.click();
  const dialog = admin.getByRole("dialog");
  await expect(dialog.getByRole("heading")).toHaveText("Suspend Tess Target?");
  await expect(
    dialog.getByRole("checkbox", { name: /Also revoke invitations/ }),
  ).toBeChecked();
  const reason = "Sent invitation spam to strangers, ticket 4521.";
  await dialog.getByLabel("Reason").fill(reason);
  await shoot(admin, "suspend-dialog");
  await dialog.getByRole("button", { name: "Suspend account" }).click();
  const code = dialog.getByLabel("Authentication code");
  await expect(code).toBeVisible();
  await code.fill(codeFor(setupKey, 1));
  await dialog.getByRole("button", { name: "Suspend account" }).click();
  await expect(admin.getByText("Tess Target is suspended.")).toBeVisible();
  await expect(admin.getByText(reason)).toBeVisible();
  await expect(
    admin.getByRole("button", { name: "Unsuspend account" }),
  ).toBeVisible();

  // Their open session ended: the next page sends them to sign in.
  await person.reload();
  await expect(person).toHaveURL(/\/auth\/login/);
  // A wrong password reads as for anyone; the right one says why.
  await person.getByLabel("Email").fill(targetEmail);
  await person.getByLabel("Password", { exact: true }).fill("not the password");
  await person.getByRole("button", { name: "Sign in" }).click();
  await expect(
    person.getByText("Email or password is incorrect."),
  ).toBeVisible();
  await signIn(person, targetEmail);
  await expect(
    person.getByText("This account has been suspended. Contact support."),
  ).toBeVisible();
  await shoot(person, "suspended-sign-in");

  // Recorded, with the reason, by the operator.
  const actor = `(SELECT id FROM users WHERE email = '${operatorEmail}')`;
  const targetId = `(SELECT public_id FROM users WHERE email = '${targetEmail}')`;
  expect(
    sql(
      `SELECT payload->>'reason' FROM events WHERE name = 'admin.user.suspended' AND payload->>'user' = ${targetId} AND actor_id = ${actor}`,
    ),
  ).toBe(reason);

  // Unsuspend: the window is still open, so no code.
  await admin.getByRole("button", { name: "Unsuspend account" }).click();
  await admin
    .getByRole("dialog")
    .getByLabel("Reason")
    .fill("Reviewed: the invitations were a misunderstanding.");
  await admin
    .getByRole("dialog")
    .getByRole("button", { name: "Unsuspend account" })
    .click();
  await expect(admin.getByText("Tess Target can sign in again.")).toBeVisible();
  await signIn(person, targetEmail);
  await expect(person).toHaveURL(/\/app/);

  // IP blocks: block a documentation range, listed with who and why.
  await admin.getByRole("link", { name: "IP blocks" }).first().click();
  await expect(
    admin.getByRole("heading", { name: "IP blocks", exact: true }),
  ).toBeVisible();
  await admin.getByRole("button", { name: "Block an address" }).click();
  const block = admin.getByRole("dialog");
  await block.getByLabel("Address or range").fill(blockedNetwork);
  await block
    .getByLabel("Reason")
    .fill("Credential stuffing from this range, ticket 4522.");
  await block.getByRole("button", { name: "Block" }).click();
  await expect(admin.getByText(`${blockedNetwork} is blocked.`)).toBeVisible();
  await expect(admin.getByRole("dialog")).toHaveCount(0);
  await expect(
    admin.getByRole("row").filter({ hasText: blockedNetwork }),
  ).toContainText("Opal Operator");
  await shoot(admin, "ip-blocks-page");

  expect(errors).toEqual([]);
  await operator.close();
  await target.close();
});
