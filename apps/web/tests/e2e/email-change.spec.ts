import {
  expect,
  test,
  type APIRequestContext,
  type Page,
} from "@playwright/test";
import type { Session } from "../../src/lib/types";
import { mailedCode, mailCountTo, mailTo, signUpThroughApi } from "./mail";
import { settingsPort, startServer, stopServer } from "./settings-server";
import { codeFor } from "./totp";

const password = "An email-change test passphrase!";

function account(suffix: string) {
  return {
    name: "Email Change",
    email: `e2e-${Date.now()}-${suffix}@lewcontext.test`,
    password,
  };
}
function headers(origin: string, session?: Session) {
  return {
    Origin: origin,
    ...(session ? { "X-CSRF-Token": session.csrfToken } : {}),
  };
}
function watchErrors(page: Page) {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    if (
      message.type() === "error" &&
      message.text().includes("Content Security Policy")
    )
      errors.push(message.text());
  });
  return errors;
}
async function screenshot(page: Page, name: string) {
  const path = test.info().outputPath(`${name}.png`);
  await page.screenshot({ path });
  await test.info().attach(name, { path, contentType: "image/png" });
}
async function createWorkspace(
  request: APIRequestContext,
  origin: string,
  session: Session,
) {
  const response = await request.post(`${origin}/api/workspaces`, {
    headers: headers(origin, session),
    data: { name: "Email-change workspace" },
  });
  expect(response.status()).toBe(201);
}
async function removeAccount(request: APIRequestContext, origin: string) {
  const current = await request.get(`${origin}/api/auth/me`);
  if (current.status() === 401) return;
  expect(current.status()).toBe(200);
  const session: Session = await current.json();
  const listed = await request.get(`${origin}/api/workspaces`);
  if (listed.status() === 200) {
    const workspaces: { data: { id: string; name: string }[] } =
      await listed.json();
    for (const workspace of workspaces.data) {
      const deleted = await request.delete(
        `${origin}/api/workspaces/${workspace.id}`,
        {
          headers: headers(origin, session),
          data: { name: workspace.name, password },
        },
      );
      expect(deleted.status()).toBe(204);
    }
  }
  const deleted = await request.delete(`${origin}/api/auth/me`, {
    headers: headers(origin, session),
    data: { password },
  });
  expect(deleted.status()).toBe(204);
}
async function destination(page: Page, email: string) {
  const dialog = page.getByRole("dialog", { name: "Change email address" });
  await dialog.getByLabel("New email address").fill(email);
  await dialog.getByRole("button", { name: "Continue", exact: true }).click();
  return dialog;
}
// Completing keeps the dialog open on a success screen until it's dismissed.
async function dismissSuccess(
  page: Page,
  email: string,
  by: "Done" | "Escape" = "Done",
) {
  const dialog = page.getByRole("dialog", { name: "Email address changed" });
  await expect(dialog).toContainText(email);
  if (by === "Escape") await page.keyboard.press("Escape");
  else await dialog.getByRole("button", { name: "Done", exact: true }).click();
  await expect(dialog).toBeHidden();
}
// Account settings: the edited email field's Save opens the dialog already
// at confirming account access.
async function saveEditedEmail(page: Page, email: string) {
  await page.getByLabel(/^Email address/).fill(email);
  await page.getByRole("button", { name: "Save", exact: true }).click();
  return page.getByRole("dialog", { name: "Change email address" });
}
async function enterChangeCode(page: Page, email: string) {
  const { code, mail } = await mailedCode(
    email,
    undefined,
    "Confirm your email change",
  );
  expect(mail.Text).toContain("existing account");
  await page.getByLabel("Email change code").fill(code);
}

test("email change resumes in this browser, verifies the new inbox and revokes another browser", async ({
  page,
  browser,
}) => {
  const origin = process.env.APP_ORIGIN!;
  const original = account("email-original");
  const replacement = account("email-replacement").email;
  const mistyped = account("email-mistyped").email;
  const other = await browser.newContext();
  const errors = watchErrors(page);
  try {
    const session: Session = await signUpThroughApi(page.request, original);
    await createWorkspace(page.request, origin, session);
    const signedIn = await other.request.post(`${origin}/api/auth/login`, {
      headers: headers(origin),
      data: { email: original.email, password },
    });
    expect(signedIn.status()).toBe(200);
    const otherPage = await other.newPage();
    await otherPage.goto(`${origin}/app/settings`);
    const oldCookie = (await page.context().cookies()).find(
      (cookie) => cookie.name === "lewcontext_session",
    )?.value;
    await page.goto("/app/settings");
    const requested = page.getByText("You have an email change request.");
    await expect(requested).toHaveCount(0);
    // A first request, for an address typed wrongly. Closing keeps it, and
    // the field's description is then the way back into it.
    let dialog = await saveEditedEmail(page, mistyped);
    await expect(dialog.getByLabel("Current password")).toBeFocused();
    await dialog.getByLabel("Current password").fill(password);
    await dialog
      .getByRole("button", { name: "Send code", exact: true })
      .click();
    await expect(dialog.getByLabel("Email change code")).toBeVisible();
    await dialog.getByRole("button", { name: "Close", exact: true }).click();
    await expect(dialog).toBeHidden();
    await expect(requested).toBeVisible();
    await page.reload();
    await expect(requested).toBeVisible();
    // Saving another address always starts the same way, says nothing about
    // the earlier request, and replaces it once account access is confirmed.
    dialog = await saveEditedEmail(page, replacement);
    await expect(dialog.getByLabel("Current password")).toBeFocused();
    await expect(dialog).not.toContainText(/request|already|active/i);
    await dialog.getByLabel("Current password").fill(password);
    await dialog
      .getByRole("button", { name: "Send code", exact: true })
      .click();
    await expect(dialog.getByLabel("Email change code")).toBeVisible();
    const first = await mailedCode(
      replacement,
      undefined,
      "Confirm your email change",
    );
    await page.reload();
    await expect(requested).toBeVisible();
    await page.getByRole("button", { name: "Continue", exact: true }).click();
    dialog = page.getByRole("dialog");
    await expect(dialog.getByLabel("Email change code")).toBeVisible();
    await expect(dialog).toContainText(replacement);
    await dialog.getByRole("button", { name: "Send a new code" }).click();
    await expect(dialog.getByLabel("Current password")).toHaveValue("");
    await dialog.getByLabel("Current password").fill(password);
    await dialog
      .getByRole("button", { name: "Send code", exact: true })
      .click();
    const second = await mailedCode(
      replacement,
      first.mail,
      "Confirm your email change",
    );
    const wrong = second.code === "000000" ? "111111" : "000000";
    await dialog.getByLabel("Email change code").fill(wrong);
    await expect(dialog.getByRole("alert")).toContainText(/code/i);
    await screenshot(page, "email-change-desktop");
    await page.setViewportSize({ width: 390, height: 844 });
    await screenshot(page, "email-change-mobile");
    await expect(
      dialog.getByRole("button", { name: "Change email address", exact: true }),
    ).toBeInViewport();
    await dialog.getByLabel("Email change code").fill(second.code);
    await screenshot(page, "email-change-success");
    await dismissSuccess(page, replacement);
    await expect(page.getByLabel("Email address Verified")).toHaveValue(
      replacement,
    );
    await expect(page.getByRole("button", { name: "Save" })).toBeDisabled();
    const changed: Session = await (
      await page.request.get("/api/auth/me")
    ).json();
    expect(changed.user.id).toBe(session.user.id);
    expect(changed.csrfToken).not.toBe(session.csrfToken);
    expect(
      (await page.context().cookies()).find(
        (cookie) => cookie.name === "lewcontext_session",
      )?.value,
    ).not.toBe(oldCookie);
    expect((await other.request.get(`${origin}/api/auth/me`)).status()).toBe(
      401,
    );
    await otherPage.reload();
    await expect(otherPage).toHaveURL(/\/auth\/login/);
    const previousNotice = await mailTo(
      original.email,
      "Your account email address changed",
    );
    expect(previousNotice.Text).not.toContain(replacement);
    await mailTo(replacement, "Your new account email address");
    const oldLogin = await page.request.post("/api/auth/login", {
      headers: headers(origin),
      data: { email: original.email, password },
    });
    expect(oldLogin.status()).toBe(401);
    const newLogin = await page.request.post("/api/auth/login", {
      headers: headers(origin),
      data: { email: replacement, password },
    });
    expect(newLogin.status()).toBe(200);
    expect((await newLogin.json()).user.id).toBe(session.user.id);
    expect(errors).toEqual([]);
  } finally {
    await removeAccount(page.request, origin);
    await other.close();
  }
});

test("onboarding supports MFA recovery proof and a registered destination gets an informational notice", async ({
  page,
  browser,
}) => {
  const origin = process.env.APP_ORIGIN!;
  const original = account("email-mfa");
  const recipient = account("email-existing");
  const replacement = account("email-mfa-replacement").email;
  const recipientContext = await browser.newContext();
  const errors = watchErrors(page);
  try {
    const session: Session = await signUpThroughApi(page.request, original);
    const recipientSession: Session = await signUpThroughApi(
      recipientContext.request,
      recipient,
    );
    const setup = await page.request.post("/api/auth/mfa/setup", {
      headers: headers(origin, session),
      data: { password },
    });
    expect(setup.status()).toBe(200);
    const { secret }: { secret: string } = await setup.json();
    const enabled = await page.request.post("/api/auth/mfa", {
      headers: headers(origin, session),
      data: { code: codeFor(secret, 0) },
    });
    expect(enabled.status()).toBe(201);
    const { recoveryCodes }: { recoveryCodes: string[] } = await enabled.json();
    await page.goto("/app/onboarding");
    await page.getByRole("button", { name: "Account", exact: true }).click();
    await page.getByRole("menuitem", { name: "Change email address" }).click();
    let dialog = await destination(page, recipient.email);
    await expect(dialog.getByLabel("Current password")).toHaveCount(0);
    await dialog.getByRole("button", { name: /Use a recovery code/ }).click();
    await dialog.getByLabel("Recovery code").fill(recoveryCodes[0]);
    await dialog
      .getByRole("button", { name: "Send code", exact: true })
      .click();
    const notice = await mailTo(recipient.email, "Email change requested");
    expect(notice.Text).toContain("permanently removes its data");
    expect(notice.Text).not.toContain(original.email);
    expect(notice.Text).not.toMatch(/\b\d{3} \d{3}\b/);
    await dialog.getByRole("button", { name: "Cancel change" }).click();
    // Cancelling ends on its own screen, with only a Close button.
    const cancelled = page.getByRole("dialog", {
      name: "Your change has been cancelled",
    });
    await expect(cancelled.getByRole("button")).toHaveText("Close");
    await cancelled.getByRole("button", { name: "Close", exact: true }).click();
    await expect(cancelled).toBeHidden();
    await page.getByRole("button", { name: "Account", exact: true }).click();
    await page.getByRole("menuitem", { name: "Change email address" }).click();
    dialog = await destination(page, replacement);
    await dialog.getByRole("button", { name: /Use a recovery code/ }).click();
    await screenshot(page, "email-change-mfa-proof");
    await dialog.getByLabel("Recovery code").fill(recoveryCodes[1]);
    await dialog
      .getByRole("button", { name: "Send code", exact: true })
      .click();
    await enterChangeCode(page, replacement);
    await dismissSuccess(page, replacement, "Escape");
    const after: Session = await (
      await page.request.get("/api/auth/me")
    ).json();
    expect(after.user.mfaEnabledAt).not.toBeNull();
    expect(
      (
        await (
          await recipientContext.request.get(`${origin}/api/auth/me`)
        ).json()
      ).user.id,
    ).toBe(recipientSession.user.id);
    await page.context().clearCookies();
    await page.goto("/auth/login");
    await page.getByLabel("Email", { exact: true }).fill(replacement);
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await page.getByRole("button", { name: /Use a recovery code/ }).click();
    await page.getByLabel("Recovery code").fill(recoveryCodes[2]);
    await page.getByRole("button", { name: "Verify", exact: true }).click();
    await expect(page).toHaveURL(/\/app\/onboarding$/);
    expect(errors).toEqual([]);
  } finally {
    await removeAccount(page.request, origin);
    await removeAccount(recipientContext.request, origin);
    await recipientContext.close();
  }
});

test("verification-off works without SMTP, then the required-verification gate can replace an unreachable inbox", async ({
  browser,
}) => {
  test.skip(!settingsPort, "needs the reserved settings-server port");
  const original = account("email-no-smtp");
  const unverifiedAddress = account("email-unverified").email;
  const verifiedAddress = account("email-gate-corrected").email;
  let origin = await startServer({
    EMAIL_VERIFICATION: "off",
    EMAIL_VERIFICATION_EXISTING: "optional",
    MAIL_DRIVER: "off",
  });
  const context = await browser.newContext({ baseURL: origin });
  const page = await context.newPage();
  const errors = watchErrors(page);
  try {
    const registered = await context.request.post("/api/auth/register", {
      headers: headers(origin),
      data: original,
    });
    expect(registered.status()).toBe(201);
    await page.goto("/app/onboarding");
    await page.getByRole("button", { name: "Account", exact: true }).click();
    await page.getByRole("menuitem", { name: "Change email address" }).click();
    const dialog = await destination(page, unverifiedAddress);
    await dialog.getByLabel("Current password").fill(password);
    await dialog
      .getByRole("button", { name: "Change email address", exact: true })
      .click();
    await dismissSuccess(page, unverifiedAddress);
    const current: Session = await (
      await context.request.get("/api/auth/me")
    ).json();
    expect(current.user.emailVerifiedAt).toBeNull();
    expect(current.user.email).toBe(unverifiedAddress);
    expect(await mailCountTo(unverifiedAddress)).toBe(0);
    await createWorkspace(context.request, origin, current);
    await stopServer();
    origin = await startServer({
      EMAIL_VERIFICATION: "on",
      EMAIL_VERIFICATION_EXISTING: "required",
      MAIL_DRIVER: "smtp",
    });
    await page.goto("/app/settings");
    await expect(page).toHaveURL(/\/app\/verify-email\?next=/);
    await page.getByRole("button", { name: "Send code", exact: true }).click();
    await expect(page.getByLabel("Verification code")).toBeVisible();
    await page
      .getByRole("button", { name: "Change email address", exact: true })
      .click();
    const correction = await destination(page, verifiedAddress);
    await correction.getByLabel("Current password").fill(password);
    await correction
      .getByRole("button", { name: "Send code", exact: true })
      .click();
    await screenshot(page, "email-change-verification-gate");
    await enterChangeCode(page, verifiedAddress);
    // The gate waits for the success screen to be dismissed.
    await expect(page).toHaveURL(/\/app\/verify-email/);
    await dismissSuccess(page, verifiedAddress);
    await expect(page).toHaveURL(`${origin}/app/settings`);
    await expect(page.getByLabel("Email address Verified")).toHaveValue(
      verifiedAddress,
    );
    expect(errors).toEqual([]);
  } finally {
    await removeAccount(context.request, origin);
    await context.close();
    await stopServer();
  }
});
