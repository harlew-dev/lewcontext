import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { SWRConfig } from "swr";
import { describe, expect, it, vi } from "vitest";
import { Toaster } from "@/components/ui/toast";
import AccountSettings from "@/pages/app/settings";
import Login from "@/pages/auth/login";
import { SessionProvider } from "@/lib/session";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
const session = testSession({ csrfToken: "csrf-old" });
const rotated = { ...session, csrfToken: "csrf-new" };
// Account settings at /app/settings, with login (where deleting the account
// leads) and a workspace's settings (where the last-owner warning links).
function renderPage(current = session) {
  render(
    <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>
      <SessionProvider session={current}>
        <MemoryRouter initialEntries={["/app/settings"]}>
          <Routes>
            <Route path="/app/settings" element={<AccountSettings />} />
            <Route path="/auth/login" element={<Login />} />
            <Route
              path="/app/:workspace/settings"
              element={<p>Workspace settings</p>}
            />
          </Routes>
        </MemoryRouter>
        <Toaster />
      </SessionProvider>
    </SWRConfig>,
  );
}
function renderSettings() {
  renderPage();
  fireEvent.click(screen.getByRole("button", { name: "Change password" }));
}
function type(label: string, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
}
const wrongCurrent = {
  status: 422,
  body: { error: "Current password is incorrect.", field: "currentPassword" },
};
describe("profile", () => {
  it("saves the trimmed name and shows it as the account's", async () => {
    const api = stubApi({
      "PATCH /api/auth/me": [
        { body: { ...session.user, name: "Augusta Ada King" } },
      ],
    });
    renderPage();
    const save = screen.getByRole("button", { name: "Save" });
    expect(save).toBeDisabled();
    // Disabled, it says why: on hover, and as its description.
    expect(save).toHaveAccessibleDescription("No changes made");
    fireEvent.mouseEnter(save.parentElement!);
    expect(await screen.findByRole("tooltip")).toHaveTextContent(
      "No changes made",
    );
    type("Name", " ");
    expect(save).toHaveAccessibleDescription("Fill in required fields");
    type("Name", "  Augusta Ada King ");
    expect(save).not.toHaveAccessibleDescription();
    fireEvent.click(save);
    expect(await screen.findByText("Profile updated.")).toBeInTheDocument();
    expect(api.sent("PATCH")[0]).toEqual({
      method: "PATCH",
      url: "/api/auth/me",
      body: { name: "Augusta Ada King" },
      csrf: "csrf-old",
    });
    expect(screen.getByLabelText("Name")).toHaveValue("Augusta Ada King");
    // The renewed session holds the new name, so there's nothing to save.
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });
  it("shows the email address read-only, with a tooltip saying why", async () => {
    renderPage();
    // The label carries the verification badge, so it's read with the field.
    const email = screen.getByLabelText("Email address Verified");
    // Sensitive, not secret: in the page, but blurred until the eye shows it.
    expect(email).toHaveValue(session.user.email);
    expect(email).toHaveAttribute("data-blurred");
    fireEvent.click(
      screen.getByRole("button", { name: "Reveal email address" }),
    );
    expect(email).not.toHaveAttribute("data-blurred");
    const hide = screen.getByRole("button", { name: "Hide email address" });
    expect(hide).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(hide);
    expect(email).toHaveAttribute("data-blurred");
    // A click anywhere on the blurred text reveals it too.
    fireEvent.click(screen.getByText(session.user.email));
    expect(email).not.toHaveAttribute("data-blurred");
    // And so does a click on its "Click to reveal" tooltip.
    fireEvent.click(screen.getByRole("button", { name: "Hide email address" }));
    fireEvent.mouseEnter(screen.getByText(session.user.email).parentElement!);
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip).toHaveTextContent("Click to reveal");
    fireEvent.click(within(tooltip).getByRole("button"));
    expect(email).not.toHaveAttribute("data-blurred");
    expect(email).toHaveAttribute("readonly");
    expect(email).not.toBeDisabled();
    expect(screen.queryByText("Member since")).toBeNull();
    const reason = "Your email can’t be changed";
    fireEvent.mouseEnter(screen.getByRole("img", { name: reason }));
    expect(await screen.findByRole("tooltip")).toHaveTextContent(reason);
  });
  it("keeps the typed name when saving fails", async () => {
    stubApi({
      "PATCH /api/auth/me": [
        { status: 422, body: { error: "Use a name of 1–80 characters." } },
      ],
    });
    renderPage();
    type("Name", "Ada");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(
      await screen.findByText("Use a name of 1–80 characters."),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toHaveValue("Ada");
  });
});
describe("email verification", () => {
  const unverified = testSession({
    csrfToken: "csrf-old",
    user: { emailVerifiedAt: null },
  });
  it("says nothing about verification when it's off", () => {
    renderPage(testSession({ emailVerification: "off" }));
    expect(screen.getByLabelText("Email address")).toBeInTheDocument();
    expect(screen.queryByText("Verified")).toBeNull();
    expect(screen.queryByRole("button", { name: "Verify email" })).toBeNull();
  });
  it("verifies an unverified email with a code sent to it", async () => {
    const verifiedAt = "2026-10-31T12:00:00Z";
    const api = stubApi({
      "POST /api/auth/email-verification": { status: 202 },
      "PATCH /api/auth/email-verification": [
        {
          status: 422,
          body: { error: "That code isn’t right.", field: "code" },
        },
        { body: { user: { ...unverified.user, emailVerifiedAt: verifiedAt } } },
      ],
    });
    renderPage(unverified);
    expect(
      screen.getByLabelText("Email address Not verified"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("We haven’t confirmed this address."),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Verify email" }));
    const dialog = await screen.findByRole("dialog");
    // The address stays out of the dialog: the field keeps it blurred.
    expect(dialog).not.toHaveTextContent(unverified.user.email);
    fireEvent.click(within(dialog).getByRole("button", { name: "Send code" }));
    const code = await within(dialog).findByLabelText("Verification code");
    expect(code).toHaveAttribute("autocomplete", "one-time-code");
    fireEvent.change(code, { target: { value: "111111" } });
    expect(
      await within(dialog).findByText("That code isn’t right."),
    ).toBeInTheDocument();
    fireEvent.change(code, { target: { value: "482913" } });
    expect(await screen.findByText("Email verified.")).toBeInTheDocument();
    expect(api.sent("POST")[0]).toMatchObject({
      url: "/api/auth/email-verification",
      csrf: "csrf-old",
    });
    expect(api.sent("PATCH").map((r) => r.body)).toEqual([
      { code: "111111" },
      { code: "482913" },
    ]);
    // The renewed session says verified.
    expect(
      await screen.findByLabelText("Email address Verified"),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Verify email" })).toBeNull();
  });
  it("asks for a new code when the last one ended", async () => {
    stubApi({
      "POST /api/auth/email-verification": { status: 202 },
      "PATCH /api/auth/email-verification": {
        status: 410,
        body: { error: "This code has expired. Send a new one." },
      },
    });
    renderPage(unverified);
    fireEvent.click(screen.getByRole("button", { name: "Verify email" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Send code" }));
    fireEvent.change(
      await within(dialog).findByLabelText("Verification code"),
      {
        target: { value: "482913" },
      },
    );
    expect(
      await within(dialog).findByText("This code has expired. Send a new one."),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("button", { name: "Send code" }),
    ).toBeInTheDocument();
  });
});
describe("change password", () => {
  it("checks the current password, then saves the new one", async () => {
    const api = stubApi({
      "POST /api/auth/password/verify": [{ status: 204 }],
      "PATCH /api/auth/password": [{ status: 200, body: rotated }],
      "DELETE /api/auth/sessions": [{ status: 204 }],
    });
    renderSettings();
    type("Current password", "my old passphrase");
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await screen.findByLabelText("New password");
    expect(api.sent("POST")[0].body).toEqual({
      password: "my old passphrase",
    });
    type("New password", "a brand new passphrase");
    type("Confirm new password", "a brand new passphrase!");
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    expect(
      await screen.findByText("The passwords don’t match."),
    ).toBeInTheDocument();
    expect(api.sent("PATCH")).toHaveLength(0);
    type("Confirm new password", "a brand new passphrase");
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    expect(
      await screen.findByText(
        "Password updated. Other sessions were signed out.",
      ),
    ).toBeInTheDocument();
    expect(api.sent("PATCH")[0]).toEqual({
      method: "PATCH",
      url: "/api/auth/password",
      body: {
        currentPassword: "my old passphrase",
        newPassword: "a brand new passphrase",
      },
      csrf: "csrf-old",
    });
    // Later requests use the rotated session's CSRF token.
    fireEvent.click(
      screen.getByRole("button", { name: "Sign out other sessions" }),
    );
    await waitFor(() => expect(api.sent("DELETE")).toHaveLength(1));
    expect(api.sent("DELETE")[0].csrf).toBe("csrf-new");
  });
  it("stays on the first step when the current password is wrong", async () => {
    stubApi({
      "POST /api/auth/password/verify": [
        { status: 422, body: { ...wrongCurrent.body, field: "password" } },
      ],
    });
    renderSettings();
    type("Current password", "not my password");
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Current password is incorrect.",
    );
    expect(screen.queryByLabelText("New password")).toBeNull();
  });
  it("refuses a short new password without asking the API", async () => {
    const api = stubApi({
      "POST /api/auth/password/verify": [{ status: 204 }],
    });
    renderSettings();
    type("Current password", "my old passphrase");
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await screen.findByLabelText("New password");
    type("New password", "too short");
    type("Confirm new password", "too short");
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Use at least 12 characters.",
    );
    expect(api.sent("PATCH")).toHaveLength(0);
  });
  it("goes back to the first step when the API rejects the current password on save", async () => {
    stubApi({
      "POST /api/auth/password/verify": [{ status: 204 }],
      "PATCH /api/auth/password": [wrongCurrent],
    });
    renderSettings();
    type("Current password", "my old passphrase");
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await screen.findByLabelText("New password");
    type("New password", "a brand new passphrase");
    type("Confirm new password", "a brand new passphrase");
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Current password is incorrect.",
    );
    expect(screen.getByLabelText("Current password")).toBeInTheDocument();
  });
  it("shows other failures above the form and keeps the step", async () => {
    stubApi({
      "POST /api/auth/password/verify": [
        {
          status: 429,
          body: { error: "Too many attempts. Try again in 15 minutes." },
        },
      ],
    });
    renderSettings();
    type("Current password", "my old passphrase");
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(
      await screen.findByText("Too many attempts. Try again in 15 minutes."),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Current password")).toBeInTheDocument();
  });
  describe("with two-factor authentication on", () => {
    const withMfa = testSession({
      csrfToken: "csrf-old",
      user: { mfaEnabledAt: "2026-10-30T12:00:00Z" },
    });
    function renderWithMfa() {
      renderPage(withMfa);
      fireEvent.click(screen.getByRole("button", { name: "Change password" }));
    }
    function chooseNewPassword() {
      type("New password", "a brand new passphrase");
      type("Confirm new password", "a brand new passphrase");
      fireEvent.click(screen.getByRole("button", { name: "Next" }));
    }
    it("asks for the new password, then a code instead of the current one", async () => {
      const api = stubApi({
        "PATCH /api/auth/password": [
          {
            status: 422,
            body: { error: "That code didn’t work.", field: "code" },
          },
          { status: 200, body: { ...withMfa, csrfToken: "csrf-new" } },
        ],
      });
      renderWithMfa();
      expect(screen.queryByLabelText("Current password")).toBeNull();
      chooseNewPassword();
      const confirm = await screen.findByRole("button", { name: "Confirm" });
      expect(confirm).toBeDisabled();
      type("Authentication code", "000000");
      fireEvent.click(confirm);
      expect(
        await screen.findByText("That code didn’t work."),
      ).toBeInTheDocument();
      expect(screen.getByLabelText("Authentication code")).toBeInTheDocument();
      type("Authentication code", "123456");
      fireEvent.click(confirm);
      expect(
        await screen.findByText(
          "Password updated. Other sessions were signed out.",
        ),
      ).toBeInTheDocument();
      expect(api.sent("POST")).toHaveLength(0);
      expect(api.sent("PATCH").map((r) => r.body)).toEqual([
        { newPassword: "a brand new passphrase", code: "000000" },
        { newPassword: "a brand new passphrase", code: "123456" },
      ]);
    });
    it("goes back to the new password when the API refuses it", async () => {
      stubApi({
        "PATCH /api/auth/password": [
          {
            status: 422,
            body: {
              error: "Choose a password different from your current one.",
              field: "newPassword",
            },
          },
        ],
      });
      renderWithMfa();
      chooseNewPassword();
      type("Authentication code", "123456");
      fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
      expect(
        await screen.findByText(
          "Choose a password different from your current one.",
        ),
      ).toBeInTheDocument();
      expect(screen.getByLabelText("New password")).toHaveValue(
        "a brand new passphrase",
      );
    });
  });
});
describe("deleting the account", () => {
  const acme = { id: "wsp_2yEHYLwiwPVhLW", name: "Acme" };
  // The workspaces you can't leave, and how many you're in.
  function workspaceStubs(blocking: (typeof acme)[], total: number) {
    const page = (data: unknown[], items: number) => ({
      body: { data, pages: { current: 1, total: 1, perPage: 20, items } },
    });
    return {
      "GET /api/workspaces?perPage=100&canLeave=false": page(
        blocking,
        blocking.length,
      ),
      "GET /api/workspaces": page([], total),
    };
  }
  async function openDeletion() {
    const button = screen.getByRole("button", { name: "Delete account" });
    await waitFor(() => expect(button).not.toHaveAttribute("data-loading"));
    fireEvent.click(button);
    return screen.findByRole("dialog");
  }
  it("sends the only owner of a workspace there first, with no way to delete", async () => {
    stubApi(workspaceStubs([acme], 3));
    renderPage();
    expect(
      await screen.findByText(/You’re the only owner of a workspace/),
    ).toBeInTheDocument();
    const dialog = await openDeletion();
    expect(within(dialog).getByRole("heading")).toHaveTextContent(
      "Delete your workspaces first",
    );
    expect(within(dialog).queryByRole("button", { name: /Delete/ })).toBeNull();
    const link = within(dialog).getByRole("link", { name: "Acme" });
    expect(link).toHaveAttribute("href", "/app/wsp_2yEHYLwiwPVhLW/settings");
    fireEvent.click(link);
    expect(await screen.findByText("Workspace settings")).toBeInTheDocument();
  });
  it("asks for the password, then signs out to login", async () => {
    const api = stubApi({
      ...workspaceStubs([], 2),
      "DELETE /api/auth/me": [
        {
          status: 422,
          body: { error: "Current password is incorrect.", field: "password" },
        },
        { status: 204 },
      ],
    });
    renderPage();
    const dialog = await openDeletion();
    expect(within(dialog).getByRole("heading")).toHaveTextContent(
      "Delete your account?",
    );
    await waitFor(() =>
      expect(dialog).toHaveTextContent("removes you from 2 workspaces"),
    );
    const confirm = within(dialog).getByRole("button", {
      name: "Delete account",
    });
    expect(confirm).toBeDisabled();
    fireEvent.change(within(dialog).getByLabelText("Your password"), {
      target: { value: "wrong" },
    });
    fireEvent.click(confirm);
    expect(
      await within(dialog).findByText("Current password is incorrect."),
    ).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText("Your password"), {
      target: { value: "correct horse battery staple" },
    });
    fireEvent.click(confirm);
    expect(
      await screen.findByText("Your account has been deleted."),
    ).toBeInTheDocument();
    expect(api.sent("DELETE")[1]).toEqual({
      method: "DELETE",
      url: "/api/auth/me",
      body: { password: "correct horse battery staple" },
      csrf: "csrf-old",
    });
  });
  it("switches to the warning when the API says you're still an only owner", async () => {
    stubApi({
      ...workspaceStubs([], 1),
      "DELETE /api/auth/me": {
        status: 409,
        body: {
          error: "Delete the workspaces you own first.",
          workspaces: [acme],
        },
      },
    });
    renderPage();
    const dialog = await openDeletion();
    fireEvent.change(within(dialog).getByLabelText("Your password"), {
      target: { value: "correct horse battery staple" },
    });
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Delete account" }),
    );
    expect(
      await screen.findByRole("heading", {
        name: "Delete your workspaces first",
      }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Acme" })).toBeInTheDocument();
  });
});
describe("two-factor authentication", () => {
  const setup = {
    secret: "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP",
    uri: "otpauth://totp/lewcontext:ada?secret=JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP",
  };
  const recoveryCodes = ["k7p2m-x9qfr", "a1b2c-d3e4f"];
  const wrongCode = {
    status: 422,
    body: { error: "That code didn’t work.", field: "code" },
  };
  const wrongPassword = {
    status: 422,
    body: { error: "Current password is incorrect.", field: "password" },
  };
  function section() {
    return within(
      screen.getByRole("heading", { name: "Two-factor authentication" })
        .parentElement!.parentElement!,
    );
  }
  it("enables it: password, the hidden QR code, a code, then the recovery codes", async () => {
    const api = stubApi({
      "POST /api/auth/mfa/setup": [wrongPassword, { body: setup }],
      "POST /api/auth/mfa": [
        wrongCode,
        { status: 201, body: { recoveryCodes } },
      ],
    });
    renderPage();
    // Off is just the Enable button.
    expect(section().queryByText(/On since/)).toBeNull();
    fireEvent.click(section().getByRole("button", { name: "Enable" }));
    const dialog = within(await screen.findByRole("dialog"));

    type("Your password", "wrong");
    fireEvent.click(dialog.getByRole("button", { name: "Next" }));
    expect(
      await dialog.findByText("Current password is incorrect."),
    ).toBeInTheDocument();
    type("Your password", "correct horse");
    fireEvent.click(dialog.getByRole("button", { name: "Next" }));
    expect(
      await dialog.findByRole("button", { name: "Reveal QR code" }),
    ).toBeInTheDocument();
    expect(document.body.innerHTML).not.toContain(setup.secret);

    type("Code from your app", "000000");
    expect(
      await dialog.findByText("That code didn’t work."),
    ).toBeInTheDocument();
    type("Code from your app", "123456");
    const done = await dialog.findByRole("button", {
      name: "I’ve saved these codes",
    });
    expect(done).toBeDisabled();
    // The step can't be dismissed before the codes are saved.
    fireEvent.keyDown(document.activeElement ?? document.body, {
      key: "Escape",
    });
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    fireEvent.click(
      dialog.getByRole("button", { name: "Reveal recovery codes" }),
    );
    expect(dialog.getByText("k7p2m-x9qfr")).toBeInTheDocument();
    fireEvent.click(done);
    expect(
      await screen.findByText(
        "Two-factor authentication is on. Other sessions were signed out.",
      ),
    ).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(section().getByText(/^On since/)).toBeInTheDocument();
    expect(api.sent("POST").map((r) => [r.url, r.body])).toEqual([
      ["/api/auth/mfa/setup", { password: "wrong" }],
      ["/api/auth/mfa/setup", { password: "correct horse" }],
      ["/api/auth/mfa", { code: "000000" }],
      ["/api/auth/mfa", { code: "123456" }],
    ]);
  });
  it("disables it with the password and a code", async () => {
    const api = stubApi({
      "DELETE /api/auth/mfa": [wrongCode, { status: 204 }],
    });
    renderPage(testSession({ user: { mfaEnabledAt: "2026-10-30T12:00:00Z" } }));
    fireEvent.click(section().getByRole("button", { name: "Disable" }));
    const dialog = within(await screen.findByRole("dialog"));
    const confirm = dialog.getByRole("button", {
      name: "Disable two-factor authentication",
    });
    expect(confirm).toBeDisabled();
    type("Your password", "correct horse");
    fireEvent.click(
      dialog.getByRole("button", {
        name: "Lost your device? Use a recovery code",
      }),
    );
    type("Recovery code", "k7p2m-x9qfr");
    fireEvent.click(confirm);
    expect(
      await dialog.findByText("That code didn’t work."),
    ).toBeInTheDocument();
    fireEvent.click(confirm);
    expect(
      await screen.findByText("Two-factor authentication is off."),
    ).toBeInTheDocument();
    expect(
      section().getByRole("button", { name: "Enable" }),
    ).toBeInTheDocument();
    expect(section().queryByText(/On since/)).toBeNull();
    expect(api.sent("DELETE")[1].body).toEqual({
      password: "correct horse",
      code: "k7p2m-x9qfr",
    });
  });
  it("replaces the recovery codes", async () => {
    stubApi({
      "POST /api/auth/mfa/recovery-codes": {
        status: 201,
        body: { recoveryCodes },
      },
    });
    renderPage(testSession({ user: { mfaEnabledAt: "2026-10-30T12:00:00Z" } }));
    fireEvent.click(
      section().getByRole("button", { name: "New recovery codes" }),
    );
    const dialog = within(await screen.findByRole("dialog"));
    type("Your password", "correct horse");
    type("Authentication code", "123456");
    fireEvent.click(dialog.getByRole("button", { name: "Create new codes" }));
    const done = await dialog.findByRole("button", {
      name: "I’ve saved these codes",
    });
    expect(done).toBeDisabled();
    fireEvent.click(
      dialog.getByRole("button", { name: "Reveal recovery codes" }),
    );
    fireEvent.click(done);
    expect(
      await screen.findByText(
        "New recovery codes saved. The old ones no longer work.",
      ),
    ).toBeInTheDocument();
  });
});
describe("terms and privacy", () => {
  const published = {
    body: {
      enabled: true,
      termsOfService: "2026-11-01",
      privacyPolicy: "2026-10-15",
    },
  };
  const agreed = testSession({
    user: {
      policies: {
        termsOfService: "2026-11-01",
        privacyPolicy: "2026-10-15",
        acceptedAt: "2026-11-03T09:00:00Z",
      },
    },
  });
  it("isn't there while the policies aren't published", async () => {
    stubApi({ "GET /api/policies": { body: { enabled: false } } });
    renderPage(agreed);
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(
      screen.queryByRole("heading", { name: "Terms and privacy" }),
    ).toBeNull();
  });
  it("says what the account agreed to, and when", async () => {
    stubApi({ "GET /api/policies": published });
    renderPage(agreed);
    expect(await screen.findByText(/^You agreed to the/)).toHaveTextContent(
      "You agreed to the Terms of Service (November 1, 2026) and read the Privacy Policy (October 15, 2026) on November 3, 2026.",
    );
    expect(screen.queryByText(/were updated|was updated/)).toBeNull();
  });
  it("says when a document was updated since", async () => {
    stubApi({
      "GET /api/policies": {
        body: {
          enabled: true,
          termsOfService: "2026-12-01",
          privacyPolicy: "2026-10-15",
        },
      },
    });
    renderPage(agreed);
    expect(
      await screen.findByText(
        "The Terms of Service were updated on December 1, 2026.",
      ),
    ).toBeInTheDocument();
  });
  it("links both for an account without a record", async () => {
    stubApi({ "GET /api/policies": published });
    renderPage();
    expect(await screen.findByText(/^Read our/)).toHaveTextContent(
      "Read our Terms of Service and Privacy Policy.",
    );
    expect(
      screen.getByRole("link", { name: "Privacy Policy" }),
    ).toHaveAttribute("href", "/policy/privacy-policy");
  });
});
// A saved file's text (jsdom's Blob has no text()).
function blobText(blob: Blob): Promise<string> {
  return new Promise((resolve) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.readAsText(blob);
  });
}
describe("downloading your data", () => {
  const workspaces = {
    body: { data: [], pages: { current: 1, total: 1, perPage: 20, items: 0 } },
  };
  it("asks for the password, then saves the API's answer as a file", async () => {
    const saved: Blob[] = [];
    const createObjectURL = vi.fn((blob: Blob) => {
      saved.push(blob);
      return "blob:export";
    });
    const { createObjectURL: realCreate, revokeObjectURL: realRevoke } = URL;
    URL.createObjectURL = createObjectURL;
    URL.revokeObjectURL = vi.fn();
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => {});
    const exported = { account: { id: "usr_2yEHYLwiwPVhLW" }, events: [] };
    const api = stubApi({
      "GET /api/workspaces": workspaces,
      "POST /api/auth/export": [
        {
          status: 422,
          body: { error: "Current password is incorrect.", field: "password" },
        },
        { body: exported },
      ],
    });
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "Download your data" }));
    const dialog = await screen.findByRole("dialog");
    const confirm = within(dialog).getByRole("button", { name: "Download" });
    expect(confirm).toBeDisabled();
    fireEvent.change(within(dialog).getByLabelText("Your password"), {
      target: { value: "wrong" },
    });
    fireEvent.click(confirm);
    expect(
      await within(dialog).findByText("Current password is incorrect."),
    ).toBeInTheDocument();
    expect(saved).toHaveLength(0);
    fireEvent.change(within(dialog).getByLabelText("Your password"), {
      target: { value: "correct horse battery staple" },
    });
    fireEvent.click(confirm);
    expect(
      await screen.findByText("Your data has been downloaded."),
    ).toBeInTheDocument();
    expect(api.sent("POST")[1]).toEqual({
      method: "POST",
      url: "/api/auth/export",
      body: { password: "correct horse battery staple" },
      csrf: "csrf-old",
    });
    expect(saved).toHaveLength(1);
    expect(JSON.parse(await blobText(saved[0]))).toEqual(exported);
    expect(click).toHaveBeenCalledTimes(1);
    click.mockRestore();
    URL.createObjectURL = realCreate;
    URL.revokeObjectURL = realRevoke;
  });
  it("keeps the dialog open with the limit's refusal", async () => {
    stubApi({
      "GET /api/workspaces": workspaces,
      "POST /api/auth/export": {
        status: 429,
        body: { error: "Too many requests. Try again in 23 hours." },
      },
    });
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "Download your data" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Your password"), {
      target: { value: "correct horse battery staple" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Download" }));
    expect(
      await within(dialog).findByText(
        "Too many requests. Try again in 23 hours.",
      ),
    ).toBeInTheDocument();
  });
});
