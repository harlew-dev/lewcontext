import { expect, test, type Browser } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { signUpThroughApi } from "./mail";
import { settingsPort, startServer, stopServer } from "./settings-server";
// Rate limits' challenge levels, against the real Turnstile script with
// Cloudflare's test keys (plans/features/implemented/captcha-challenges.md).
// The main server runs with RATE_LIMITS=off and no provider; this spec
// starts a second one with limits on and Turnstile, and signs in from the
// test's one address until auth:per-ip's challenge level (above 5) asks
// for a CAPTCHA. It needs Cloudflare: skipped offline.
const stamp = Date.now();
const email = `e2e-${stamp}-captcha@lewcontext.test`;
const password = "A thoughtful test passphrase!";
const passingSecret = "1x0000000000000000000000000000000AA";
function sql(statement: string) {
  execFileSync(
    "psql",
    [process.env.DATABASE_URL!, "-v", "ON_ERROR_STOP=1", "-Atc", statement],
    { encoding: "utf8" },
  );
}
let online = false;
test.beforeAll(async ({ request }) => {
  try {
    const script = await fetch(
      "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit",
      { signal: AbortSignal.timeout(8000) },
    );
    online = script.ok;
  } catch {
    online = false;
  }
  if (online)
    await signUpThroughApi(request, { name: "Cathy Captcha", email, password });
});
// Each test starts its own server; stop it even when the test fails.
test.afterEach(async () => {
  await stopServer();
});
test.afterAll(async () => {
  sql(
    `DELETE FROM users WHERE email = '${email}'; DELETE FROM rate_limits WHERE TRUE`,
  );
});
// A server with limits on and Turnstile with this site key.
async function turnstileServer(siteKey: string) {
  return startServer({
    RATE_LIMITS: "on",
    CAPTCHA_PROVIDER: "turnstile",
    CAPTCHA_SITE_KEY: siteKey,
    CAPTCHA_SECRET_KEY: passingSecret,
  });
}
async function loginPage(browser: Browser, origin: string) {
  const page = await (await browser.newContext()).newPage();
  const violations: string[] = [];
  page.on("console", (m) => {
    if (m.text().includes("Content Security Policy")) violations.push(m.text());
  });
  await page.goto(`${origin}/auth/login`);
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  return { page, violations };
}
test("the sixth sign-in is checked, invisibly, then goes through", async ({
  browser,
  request,
}) => {
  test.skip(!settingsPort, "needs E2E_SETTINGS_PORT");
  test.skip(!online, "needs Cloudflare (challenges.cloudflare.com)");
  const origin = await turnstileServer("1x00000000000000000000AA");
  // Five sign-ins from this address in the window need nothing.
  for (let i = 0; i < 5; i++) {
    const wrong = await request.post(`${origin}/api/auth/login`, {
      headers: { Origin: origin, "Content-Type": "application/json" },
      data: { email, password: "not the password at all" },
    });
    expect(wrong.status()).toBe(401);
  }
  // The page's CSP names Cloudflare only now that a provider is set.
  const html = await request.get(`${origin}/auth/login`);
  const csp = html.headers()["content-security-policy"];
  expect(csp).toContain("frame-src 'self' https://challenges.cloudflare.com");
  expect(csp.split(";").find((d) => d.includes("script-src"))).not.toContain(
    "unsafe-inline",
  );
  const { page, violations } = await loginPage(browser, origin);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(
    page.getByRole("dialog", { name: "Quick security check" }),
  ).toBeVisible();
  // It passes by itself, and the sign-in goes on.
  await expect(page).not.toHaveURL(/\/auth\/login/, { timeout: 30000 });
  expect(violations).toEqual([]);
  await page.context().close();
});
test("a check that wants an interaction shows the widget", async ({
  browser,
}) => {
  test.skip(!settingsPort || !online, "needs E2E_SETTINGS_PORT and Cloudflare");
  const origin = await turnstileServer("3x00000000000000000000FF");
  const { page } = await loginPage(browser, origin);
  await page.getByRole("button", { name: "Sign in" }).click();
  const dialog = page.getByRole("dialog", { name: "Quick security check" });
  await expect(dialog).toBeVisible();
  // Turnstile draws into a closed shadow root: the widget's slot grows.
  const slot = dialog.locator("[data-challenge-widget]");
  await expect
    .poll(async () => (await slot.boundingBox())?.height ?? 0, {
      timeout: 30000,
    })
    .toBeGreaterThan(40);
  // Cancelling leaves the form as it was, saying why nothing happened.
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(page.getByRole("alert")).toContainText("Check cancelled.");
  await expect(page.getByLabel("Email")).toHaveValue(email);
  await page.context().close();
});
test("a check that fails says so, and the form keeps its values", async ({
  browser,
}) => {
  test.skip(!settingsPort || !online, "needs E2E_SETTINGS_PORT and Cloudflare");
  const origin = await turnstileServer("2x00000000000000000000AB");
  const { page } = await loginPage(browser, origin);
  await page.getByRole("button", { name: "Sign in" }).click();
  const dialog = page.getByRole("dialog", { name: "Quick security check" });
  await expect(
    dialog.getByText("The check couldn't be completed."),
  ).toBeVisible({
    timeout: 30000,
  });
  await expect(dialog.getByRole("button", { name: "Try again" })).toBeVisible();
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(page.getByLabel("Email")).toHaveValue(email);
  await expect(page.getByLabel("Password", { exact: true })).toHaveValue(
    password,
  );
  await page.context().close();
});
