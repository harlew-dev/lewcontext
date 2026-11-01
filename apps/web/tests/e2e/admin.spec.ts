import { expect, test, type Page } from "@playwright/test";
import { createHmac } from "node:crypto";
import { execFileSync } from "node:child_process";
import { signUpThroughApi } from "./mail";
// The run's admin key, made by playwright.config.ts and passed to the server.
const key = process.env.E2E_ADMIN_ROUTE_KEY!;
const stamp = Date.now();
const adminEmail = `e2e-${stamp}-admin@lewcontext.test`;
const otherEmail = `e2e-${stamp}-not-admin@lewcontext.test`;
// A reset request whose email failed, seeded for the Mail page.
const brokenEmail = `e2e-${stamp}-broken@lewcontext.test`;
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
  sql(`DELETE FROM events WHERE payload->>'email' = '${brokenEmail}'`);
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
// What the app build must never hold (scripts/check-app-build.sh checks the
// files too): the admin API's paths and header, admin routes, and the admin
// build's file path.
const adminMarkers = [
  "/api/admin",
  "X-Admin-Key",
  "pages/admin",
  "admin-assets",
];
test("the public pages ship no admin code", async ({ page, request }) => {
  const scripts: string[] = [];
  // Scripts and module preloads alike: everything JavaScript it fetched.
  page.on("response", (response) => {
    if (new URL(response.url()).pathname.endsWith(".js"))
      scripts.push(response.url());
  });
  await page.goto("/");
  await expect(page.getByRole("link", { name: "Log in" })).toBeVisible();
  const routes = await page.evaluate(() =>
    Object.keys(
      (window as unknown as { __reactRouterManifest: { routes: object } })
        .__reactRouterManifest.routes,
    ),
  );
  expect(routes.length).toBeGreaterThan(5);
  expect(routes.filter((id) => id.includes("admin"))).toEqual([]);
  // The manifest and every script the homepage loaded, read in full.
  expect(scripts.some((url) => /\/assets\/manifest-/.test(url))).toBe(true);
  for (const url of scripts) {
    const body = await (await request.get(url)).text();
    for (const marker of adminMarkers) expect(body, url).not.toContain(marker);
  }
});
// The admin build's files open only with the pass the admin page sets: the
// same file from a browser that never opened the page is the 404 page.
test("admin files are the 404 page without the assets pass", async ({
  page,
  browser,
}) => {
  const door = await page.goto(`/admin/${key}`);
  expect(door?.status()).toBe(200);
  const html = await door!.text();
  const file = /\/admin-assets\/assets\/[\w.-]+\.js/.exec(html)?.[0];
  expect(file).toBeTruthy();
  const pass = (await page.context().cookies()).find(
    (c) => c.name === "lewcontext_admin_assets",
  );
  expect(pass?.path).toBe("/admin-assets/");
  expect(pass?.httpOnly).toBe(true);
  expect(pass?.sameSite).toBe("Strict");
  const withPass = await page.request.get(file!);
  expect(withPass.status()).toBe(200);
  expect(withPass.headers()["cache-control"]).toBe(
    "private, max-age=28800, immutable",
  );

  const fresh = await browser.newContext();
  const stranger = await fresh.newPage();
  const nowhere = await stranger.request.get("/nowhere");
  const response = await stranger.goto(file!);
  expect(response?.status()).toBe(404);
  await expect(stranger.getByText("This page has wandered off.")).toBeVisible();
  expect(await response!.text()).toBe(await nowhere.text());
  expect(response!.headers()["cache-control"]).toBe("no-store");
  await fresh.close();
});
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

  // Events: filter by the other account's email, open its refused sign-in
  // at the door, and follow the account it names.
  await page.getByRole("link", { name: "Events" }).first().click();
  await expect(page.getByRole("heading", { name: "Events" })).toBeVisible();
  await page.getByLabel("Email").fill(otherEmail.toUpperCase());
  await page.getByRole("button", { name: "Apply filters" }).click();
  await expect(page).toHaveURL(
    new RegExp(
      `/admin/${key}/events\\?email=${encodeURIComponent(otherEmail)}$`,
    ),
  );
  const attempt = page
    .getByRole("row")
    .filter({ hasText: "admin.login_attempted" });
  await expect(attempt).toContainText("not_admin");
  await attempt.getByRole("link").first().click();
  await expect(
    page.getByRole("heading", { name: "admin.login_attempted" }),
  ).toBeVisible();
  await expect(page.locator("pre")).toContainText(otherEmail);
  await page.getByRole("link", { name: "Nora Notadmin" }).click();
  await expect(
    page.getByRole("heading", { name: "Nora Notadmin" }),
  ).toBeVisible();
  // The event's IP is the client's, kept in its column.
  expect(
    sql(
      `SELECT host(ip) FROM events WHERE name = 'admin.login_attempted' AND payload->>'email' = '${otherEmail}'`,
    ),
  ).toMatch(/^(127\.0\.0\.1|::1)$/);

  // The pass ends mid-visit (deleted here): the next page's code fails to
  // load, React Router reloads the page, and the page gate renews the pass.
  // Mail's code hasn't been loaded yet.
  await page.context().clearCookies({ name: "lewcontext_admin_assets" });
  await page.getByRole("link", { name: "Mail" }).first().click();
  await expect
    .poll(async () =>
      (await page.context().cookies()).some(
        (c) => c.name === "lewcontext_admin_assets",
      ),
    )
    .toBe(true);
  await expect(
    page.getByRole("heading", { name: "Nora Notadmin" }),
  ).toBeVisible();

  // Mail: a delivery that failed (seeded, as a broken template leaves one)
  // is listed under Failed with its error.
  sql(
    `WITH e AS (INSERT INTO events (name, payload) VALUES ('auth.password_reset.requested', '{"email": "${brokenEmail}"}') RETURNING id)
     INSERT INTO event_deliveries (event_id, listener, attempts, failed_at, last_error)
     SELECT id, 'mail.password_reset', 6, now(), 'render forgot-password: template: no such template' FROM e`,
  );
  await page.getByRole("link", { name: "Mail" }).first().click();
  // Exact: "Emails sent per hour" and "How mail is sent" are headings too.
  await expect(
    page.getByRole("heading", { name: "Mail", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("table", { name: "Emails sent per hour" }),
  ).toBeAttached();
  await page
    .getByRole("navigation", { name: "Delivery status" })
    .getByRole("link", { name: /^Failed/ })
    .click();
  const failed = page.getByRole("row").filter({ hasText: brokenEmail });
  await expect(failed).toContainText("Failed");
  await expect(failed).toContainText("no such template");
  expect(errors).toEqual([]);

  // Admins: the admin's own row (a viewer, as granted), this session with
  // the address it signed in from, and no other session to sign out (the
  // first one ended with Sign out). The read is recorded.
  await page.getByRole("link", { name: "Admins" }).first().click();
  await expect(
    page.getByRole("heading", { name: "Admins", exact: true }),
  ).toBeVisible();
  const own = page.getByRole("row").filter({ hasText: adminEmail });
  await expect(own).toContainText("Viewer");
  await expect(own).toContainText("MFA");
  const thisSession = page.getByRole("row").filter({ hasText: "This session" });
  await expect(thisSession).toContainText(/127\.0\.0\.1|::1/);
  await expect(
    page.getByRole("button", { name: "Sign out my other sessions" }),
  ).toBeDisabled();
  expect(
    sql(
      `SELECT host(ip) FROM admin_sessions WHERE user_id = (SELECT id FROM users WHERE email = '${adminEmail}')`,
    ),
  ).toMatch(/^(127\.0\.0\.1|::1)$/);
  expect(
    Number(
      sql(
        `SELECT count(*) FROM events WHERE name = 'admin.admin.listed' AND payload->>'admin' = (SELECT public_id FROM users WHERE email = '${adminEmail}')`,
      ),
    ),
  ).toBe(1);
  expect(errors).toEqual([]);

  // Back to app is another build: it loads as a new page, which Go answers
  // (this browser has no app session, so login, returning to /app).
  await page.getByRole("button", { name: "Admin account menu" }).click();
  await page.getByRole("menuitem", { name: "Back to app" }).click();
  await expect(page).toHaveURL(/\/auth\/login\?next=%2Fapp$/);
  await expect(
    page.getByRole("heading", { name: "Sign in to your account" }),
  ).toBeVisible();
  expect(errors).toEqual([]);
});
