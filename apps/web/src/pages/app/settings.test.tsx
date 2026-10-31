import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Toaster } from "@/components/ui/toast";
import AccountSettings from "@/pages/app/settings";
import { SessionProvider } from "@/lib/session";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
const session = testSession({ csrfToken: "csrf-old" });
const rotated = { ...session, csrfToken: "csrf-new" };
function renderPage() {
  render(
    <SessionProvider session={session}>
      <AccountSettings />
      <Toaster />
    </SessionProvider>,
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
    type("Name", "  Augusta Ada King ");
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
    const email = screen.getByLabelText("Email address");
    expect(email).toHaveValue(session.user.email);
    expect(email).toHaveAttribute("readonly");
    expect(email).not.toBeDisabled();
    expect(screen.queryByText("Member since")).toBeNull();
    const reason = "Your sign-in email can’t be changed";
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
describe("change password", () => {
  it("checks the current password, then saves the new one", async () => {
    const api = stubApi({
      "POST /api/auth/password/verify": [{ status: 204 }],
      "PUT /api/auth/password": [{ status: 200, body: rotated }],
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
    expect(api.sent("PUT")).toHaveLength(0);
    type("Confirm new password", "a brand new passphrase");
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    expect(
      await screen.findByText(
        "Password updated. Other sessions were signed out.",
      ),
    ).toBeInTheDocument();
    expect(api.sent("PUT")[0]).toEqual({
      method: "PUT",
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
    expect(api.sent("PUT")).toHaveLength(0);
  });
  it("goes back to the first step when the API rejects the current password on save", async () => {
    stubApi({
      "POST /api/auth/password/verify": [{ status: 204 }],
      "PUT /api/auth/password": [wrongCurrent],
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
});
