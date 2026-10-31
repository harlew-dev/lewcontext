import { expect, type APIRequestContext, type Page } from "@playwright/test";
// Mailpit, where the e2e server's email lands (MAILPIT_URL, from .env.testing).
const mailpit = process.env.MAILPIT_URL!;
export type Mail = { Subject: string; HTML: string; Text: string };
// The newest email Mailpit holds for `to`: its subject, HTML and plain-text
// parts, or nothing yet. Every run's recipients are unique, so other runs'
// mail never matches.
export async function newestMailTo(to: string): Promise<Mail | undefined> {
  const query = new URLSearchParams({ query: `to:"${to}"` });
  const found = await (await fetch(`${mailpit}/api/v1/search?${query}`)).json();
  const id = found.messages?.[0]?.ID;
  if (!id) return undefined;
  return (await fetch(`${mailpit}/api/v1/message/${id}`)).json();
}
// The newest email to `to` with `subject`, once the server's worker has sent
// it (it polls every second). `other` skips one already read (a code sent
// again).
export async function mailTo(
  to: string,
  subject: string,
  other?: Mail,
): Promise<Mail> {
  let found: Mail | undefined;
  await expect
    .poll(
      async () => {
        const mail = await newestMailTo(to);
        found =
          mail?.Subject === subject && mail.Text !== other?.Text
            ? mail
            : undefined;
        return found !== undefined;
      },
      { message: `“${subject}” for ${to} in Mailpit`, timeout: 15000 },
    )
    .toBe(true);
  return found!;
}
// The code in the newest "Confirm your email address" to `to`, shown spaced
// ("482 913") in both parts, with no link in either: a code is typed, never
// clicked.
export async function mailedCode(to: string, other?: Mail) {
  const mail = await mailTo(to, "Confirm your email address", other);
  const code = mail.Text.match(/\b(\d{3}) (\d{3})\b/);
  expect(code, `a code in:\n${mail.Text}`).not.toBeNull();
  expect(mail.HTML).toContain(code![0]);
  expect(mail.HTML).not.toContain("<a ");
  return { code: code![1] + code![2], mail };
}
// Types the code sent to `to` into the page's code field, which submits at
// the sixth digit.
export async function enterMailedCode(page: Page, to: string) {
  const { code } = await mailedCode(to);
  await page.getByLabel("Verification code").fill(code);
}
// Signs up through the API as the web app does: the sign-up, then the code
// from Mailpit. The request context keeps the sign-up's cookie, and then
// the session. Returns the session (with its CSRF token).
export async function signUpThroughApi(
  request: APIRequestContext,
  account: { name: string; email: string; password: string },
) {
  const headers = {
    Origin: process.env.APP_ORIGIN!,
    "Content-Type": "application/json",
  };
  const started = await request.post("/api/auth/register", {
    headers,
    data: account,
  });
  expect(started.status()).toBe(202);
  const { code } = await mailedCode(account.email);
  const verified = await request.post("/api/auth/register/verify", {
    headers,
    data: { code },
  });
  expect(verified.status()).toBe(201);
  return verified.json();
}
