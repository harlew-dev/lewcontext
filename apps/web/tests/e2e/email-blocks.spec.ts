import { expect, test, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import {
  mailCountTo,
  mailHeaders,
  mailTo,
  signUpThroughApi,
  type Mail,
} from "./mail";
// Blocking an email address from an invitation's block link
// (plans/features/implemented/email-blocks.md): the block, the silent refusals of
// invitations and sign-up, unblocking, and an address with an account.
const stamp = Date.now();
const inviterEmail = `e2e-${stamp}-blocker-inviter@lewcontext.test`;
const blockedEmail = `e2e-${stamp}-blocked@lewcontext.test`;
const registeredEmail = `e2e-${stamp}-blocker-registered@lewcontext.test`;
const emails = [inviterEmail, blockedEmail, registeredEmail];
const password = "A thoughtful test passphrase!";
const invitationSubject = "You’ve been invited to join a workspace";
// Runs SQL against this run's database (DATABASE_URL, from
// playwright.config.ts) and returns its output: only to wait for the worker
// and to clean up.
function sql(statement: string): string {
  return execFileSync(
    "psql",
    [process.env.DATABASE_URL!, "-v", "ON_ERROR_STOP=1", "-Atc", statement],
    { encoding: "utf8" },
  ).trim();
}
const quoted = emails.map((e) => `'${e}'`).join(", ");
test.afterAll(() => {
  sql(
    `DELETE FROM workspaces WHERE id IN (SELECT m.workspace_id FROM workspace_user m JOIN users u ON u.id = m.user_id WHERE u.email IN (${quoted})); DELETE FROM users WHERE email IN (${quoted}); DELETE FROM email_blocks WHERE email IN (${quoted}); DELETE FROM email_block_links WHERE email IN (${quoted})`,
  );
});
// Headers for API mutations from the page's signed-in browser context.
async function apiHeaders(page: Page) {
  const me = await (await page.request.get("/api/auth/me")).json();
  return {
    Origin: process.env.APP_ORIGIN!,
    "Content-Type": "application/json",
    "X-CSRF-Token": me.csrfToken,
  };
}
// Invites `to` to a new workspace of the signed-in page's account.
async function invite(page: Page, to: string) {
  const headers = await apiHeaders(page);
  const created = await page.request.post("/api/workspaces", {
    headers,
    data: { name: `Blocks ${Math.random().toString(36).slice(2, 8)}` },
  });
  expect(created.status()).toBe(201);
  const { id } = await created.json();
  const invited = await page.request.post(`/api/workspaces/${id}/invitations`, {
    headers,
    data: { email: to, role: "member" },
  });
  expect(invited.status()).toBe(201);
}
// Waits until the worker has run every listener on the events named `event`
// for `to` (sent or skipped).
async function deliveredAll(event: string, to: string, count: number) {
  await expect
    .poll(
      () =>
        Number(
          sql(
            `SELECT count(*) FROM event_deliveries d JOIN events e ON e.id = d.event_id WHERE e.name = '${event}' AND e.payload->>'email' = '${to}' AND d.delivered_at IS NOT NULL`,
          ),
        ),
      { message: `${count} ${event} deliveries for ${to}`, timeout: 15000 },
    )
    .toBe(count);
}
// The block link's path in an invitation: in both parts and as
// List-Unsubscribe, the URL form only.
async function blockLinkIn(mail: Mail): Promise<string> {
  const match = mail.HTML.match(
    /href="https?:\/\/[^/"]+(\/auth\/email-blocks\/[\w-]{43})"/,
  );
  expect(match, `a block link in:\n${mail.HTML}`).not.toBeNull();
  const path = match![1];
  expect(mail.Text).toContain(path);
  const headers = await mailHeaders(mail);
  expect(headers["List-Unsubscribe"]?.[0]).toMatch(
    new RegExp(`^<https?://[^>]+${path}>$`),
  );
  expect(headers["List-Unsubscribe-Post"]).toBeUndefined();
  return path;
}
test("blocking an address from an invitation, silently refused, then unblocked", async ({
  page,
  browser,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await signUpThroughApi(page.request, {
    name: "Ivy Inviter",
    email: inviterEmail,
    password,
  });
  await invite(page, blockedEmail);
  const first = await mailTo(blockedEmail, invitationSubject);
  const link = await blockLinkIn(first);

  // The invitee, signed out in a browser of their own, blocks the address.
  const invitee = await browser.newContext();
  const inviteePage = await invitee.newPage();
  inviteePage.on("pageerror", (error) => errors.push(error.message));
  inviteePage.on("console", (message) => {
    if (message.text().includes("Content Security Policy"))
      errors.push(message.text());
  });
  await inviteePage.goto(link);
  await expect(
    inviteePage.getByRole("heading", { name: "Block this address?" }),
  ).toBeVisible();
  await expect(inviteePage.getByText(blockedEmail)).toBeVisible();
  await inviteePage.getByRole("button", { name: "Block this address" }).click();
  await expect(
    inviteePage.getByRole("heading", { name: "This address is blocked" }),
  ).toBeVisible();
  await expect(inviteePage.getByRole("status")).toHaveText("Address blocked.");

  // Inviting it again works as for anyone, but nothing reaches the inbox.
  await invite(page, blockedEmail);
  await deliveredAll("workspace.invitation.created", blockedEmail, 2);
  expect(await mailCountTo(blockedEmail)).toBe(1);

  // Signing up with it reaches the code page, which says why no code may
  // come; none does.
  const signUp = await browser.newContext();
  const signUpPage = await signUp.newPage();
  await signUpPage.goto("/auth/register");
  await signUpPage.getByLabel("Email").fill(blockedEmail);
  await signUpPage.getByLabel("Full name").fill("Bo Blocked");
  await signUpPage.getByLabel("Password", { exact: true }).fill(password);
  await signUpPage.getByRole("button", { name: "Create account" }).click();
  await expect(signUpPage).toHaveURL("/auth/register/verify");
  await expect(
    signUpPage.getByText("If you blocked this address, no code can reach it"),
  ).toBeVisible();
  await deliveredAll("auth.registration.requested", blockedEmail, 1);
  expect(await mailCountTo(blockedEmail)).toBe(1);
  await signUp.close();

  // Unblocking from the same link lets the next invitation through.
  await inviteePage.getByRole("button", { name: "Unblock" }).click();
  await expect(
    inviteePage.getByRole("heading", { name: "Block this address?" }),
  ).toBeVisible();
  await expect(inviteePage.getByRole("status")).toHaveText(
    "Address unblocked.",
  );
  await invite(page, blockedEmail);
  const again = await mailTo(blockedEmail, invitationSubject, first);
  expect(await blockLinkIn(again)).not.toBe(link);
  await invitee.close();
  expect(errors).toEqual([]);
});
test("an address with an account is told to delete it first", async ({
  page,
  browser,
}) => {
  const registered = await browser.newContext();
  await signUpThroughApi(registered.request, {
    name: "Rae Registered",
    email: registeredEmail,
    password,
  });
  await registered.close();
  await page.goto("/auth/login");
  await page.getByLabel("Email").fill(inviterEmail);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/app\//);
  await invite(page, registeredEmail);
  const link = await blockLinkIn(
    await mailTo(registeredEmail, invitationSubject),
  );
  await page.goto(link);
  await expect(
    page.getByRole("heading", { name: "This address has an account" }),
  ).toBeVisible();
  await expect(page.getByText("delete your account first")).toBeVisible();
  await expect(page.getByRole("link", { name: "Sign in" })).toHaveAttribute(
    "href",
    "/auth/login",
  );
  await expect(
    page.getByRole("button", { name: "Block this address" }),
  ).toHaveCount(0);
});
