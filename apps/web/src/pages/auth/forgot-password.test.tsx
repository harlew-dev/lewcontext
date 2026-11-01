import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { describe, expect, it } from "vitest";
import { appName } from "@/lib/app";
import { stubApi } from "@/test/api";
import ForgotPassword from "./forgot-password";
import ResetPassword from "./forgot-password/[token]";
import Login from "./login";
import Register from "./register";
// The three sign-in pages a password reset passes through, without their
// route loaders (which only send signed-in visitors on).
function renderAt(pathname: string, state?: unknown) {
  render(
    <MemoryRouter initialEntries={[{ pathname, state }]}>
      <Routes>
        <Route path="/auth/login" element={<Login />} />
        <Route path="/auth/register" element={<Register />} />
        <Route path="/auth/forgot-password" element={<ForgotPassword />} />
        <Route
          path="/auth/forgot-password/:token"
          element={<ResetPassword />}
        />
      </Routes>
    </MemoryRouter>,
  );
}
function type(label: string, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
}
const token = "aB3_-".repeat(8) + "xyz"; // 43 base64url characters
const deadLink = {
  status: 422,
  body: {
    error: "This link has expired or was already used. Request a new one.",
    field: "token",
  },
};
describe("sign-in pages", () => {
  it.each([
    "/auth/login",
    "/auth/register",
    "/auth/forgot-password",
    "/auth/forgot-password/not-a-token",
  ])("%s links the logo to the homepage", (path) => {
    renderAt(path);
    expect(screen.getByRole("link", { name: appName })).toHaveAttribute(
      "href",
      "/",
    );
  });
});
describe("asking for a reset link", () => {
  it("carries the email typed at sign-in, outside the URL", async () => {
    renderAt("/auth/login");
    type("Email", "ada@example.com");
    const link = screen.getByRole("link", { name: "Forgot password?" });
    expect(link).toHaveAttribute("href", "/auth/forgot-password");
    fireEvent.click(link);
    expect(
      await screen.findByRole("heading", { name: "Reset your password" }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Email")).toHaveValue("ada@example.com");
  });
  it("says the same thing whether or not an account exists", async () => {
    const api = stubApi({
      "POST /api/auth/password-reset": { status: 202 },
    });
    renderAt("/auth/forgot-password");
    type("Email", "ada@example.com");
    fireEvent.click(screen.getByRole("button", { name: "Send reset link" }));
    expect(
      await screen.findByRole("heading", { name: "Check your email" }),
    ).toBeInTheDocument();
    const status = screen.getByRole("status");
    expect(status).toHaveTextContent(
      "If an account uses this email, we’ve sent it a reset link:",
    );
    expect(status).toHaveTextContent("ada@example.com");
    expect(api.sent("POST")).toEqual([
      {
        method: "POST",
        url: "/api/auth/password-reset",
        body: { email: "ada@example.com" },
        csrf: null,
      },
    ]);
    // Back returns to the form to try again, keeping the email.
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(screen.getByLabelText("Email")).toHaveValue("ada@example.com");
  });
  it("shows a refusal and stays on the form", async () => {
    stubApi({
      "POST /api/auth/password-reset": {
        status: 429,
        body: { error: "Too many attempts. Try again in an hour." },
      },
    });
    renderAt("/auth/forgot-password");
    type("Email", "ada@example.com");
    fireEvent.click(screen.getByRole("button", { name: "Send reset link" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Too many attempts. Try again in an hour.",
    );
    expect(screen.queryByText("Check your email")).toBeNull();
  });
});
describe("the reset link page", () => {
  it("says a malformed link has expired without asking the API", async () => {
    const api = stubApi({});
    renderAt("/auth/forgot-password/not-a-token");
    expect(
      screen.getByRole("heading", { name: "This link has expired" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Request a new link" }),
    ).toHaveAttribute("href", "/auth/forgot-password");
    expect(api.fetch).not.toHaveBeenCalled();
  });
  it("says a dead link has expired before asking for a password", async () => {
    const api = stubApi({ "POST /api/auth/password-reset/check": deadLink });
    renderAt(`/auth/forgot-password/${token}`);
    expect(
      await screen.findByRole("heading", { name: "This link has expired" }),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("New password")).toBeNull();
    // The token goes in the body, not the URL.
    expect(api.sent("POST")[0]).toMatchObject({
      url: "/api/auth/password-reset/check",
      body: { token },
    });
  });
  it("offers a retry when the check can't reach the API", async () => {
    stubApi({
      "POST /api/auth/password-reset/check": [
        { network: true },
        { status: 204 },
      ],
    });
    renderAt(`/auth/forgot-password/${token}`);
    fireEvent.click(await screen.findByRole("button", { name: "Try again" }));
    expect(await screen.findByLabelText("New password")).toBeInTheDocument();
  });
  it("checks the new password, then sends the user to sign in with it", async () => {
    const api = stubApi({
      "POST /api/auth/password-reset/check": { status: 204 },
      "PATCH /api/auth/password-reset": { body: { email: "ada@example.com" } },
    });
    renderAt(`/auth/forgot-password/${token}`);
    await screen.findByLabelText("New password");
    const submit = screen.getByRole("button", { name: "Set new password" });
    type("New password", "too short");
    type("Confirm new password", "too short");
    fireEvent.click(submit);
    expect(
      await screen.findByText("Use at least 12 characters."),
    ).toBeVisible();
    type("New password", "a brand new passphrase");
    type("Confirm new password", "a different passphrase");
    fireEvent.click(submit);
    expect(await screen.findByText("The passwords don’t match.")).toBeVisible();
    expect(api.sent("PATCH")).toEqual([]);

    type("Confirm new password", "a brand new passphrase");
    fireEvent.click(submit);
    expect(
      await screen.findByText(
        "Password updated. Sign in with your new password.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Email")).toHaveValue("ada@example.com");
    expect(api.sent("PATCH")).toEqual([
      {
        method: "PATCH",
        url: "/api/auth/password-reset",
        body: { token, newPassword: "a brand new passphrase" },
        csrf: null,
      },
    ]);
  });
  it("shows the expired page when the link dies before submitting", async () => {
    stubApi({
      "POST /api/auth/password-reset/check": { status: 204 },
      "PATCH /api/auth/password-reset": deadLink,
    });
    renderAt(`/auth/forgot-password/${token}`);
    await screen.findByLabelText("New password");
    type("New password", "a brand new passphrase");
    type("Confirm new password", "a brand new passphrase");
    fireEvent.click(screen.getByRole("button", { name: "Set new password" }));
    expect(
      await screen.findByRole("heading", { name: "This link has expired" }),
    ).toBeInTheDocument();
  });
});
