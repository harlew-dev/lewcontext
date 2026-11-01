import { fireEvent, render, screen } from "@testing-library/react";
import { createRoutesStub, useLocation } from "react-router";
import { SWRConfig } from "swr";
import { describe, expect, it } from "vitest";
import { registerVerifyLoader } from "@/lib/loaders";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import Login from "./login";
import Register from "./register";
import RegisterVerify from "./register/verify";
const token = "aB3_-".repeat(8) + "xyz"; // 43 base64url characters
const link = `/auth/invitations/${token}`;
const signedOut = { status: 401, body: { error: "Please sign in." } };
const waiting = { verificationRequired: true, email: "ada@example.com" };
const pending = { email: "ada@example.com", expiresAt: "2026-10-31T12:15:00Z" };
function Landed({ name }: { name: string }) {
  const location = useLocation();
  return (
    <p>
      {name} {location.search}
    </p>
  );
}
// Register and its code page, with the code page's real loader (the API is
// stubbed), login, and where a finished sign-up lands.
function renderAt(entry: string) {
  const Stub = createRoutesStub([
    { path: "/auth/register", Component: Register },
    {
      path: "/auth/register/verify",
      Component: RegisterVerify,
      loader: ({ request }) => registerVerifyLoader(request),
    },
    { path: "/auth/login", Component: Login },
    { path: "/app/onboarding", Component: () => <Landed name="onboarding" /> },
    {
      path: "/auth/invitations/:token",
      Component: () => <Landed name="invitation" />,
    },
  ]);
  const [pathname, search = ""] = entry.split("?");
  render(
    <SWRConfig value={{ provider: () => new Map() }}>
      <Stub initialEntries={[{ pathname, search: search && `?${search}` }]} />
    </SWRConfig>,
  );
}
function type(label: string, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
}
function signUp() {
  type("Email", "ada@example.com");
  type("Full name", "Ada Lovelace");
  type("Password", "correct horse battery staple");
  fireEvent.click(screen.getByRole("button", { name: "Create account" }));
}
describe("signing up with an emailed code", () => {
  it("asks for the code sent to the email, then starts at onboarding", async () => {
    const api = stubApi({
      "GET /api/auth/me": signedOut,
      "POST /api/auth/register": { status: 202, body: waiting },
      "POST /api/auth/register/check": { body: pending },
      "POST /api/auth/register/verify": [
        {
          status: 422,
          body: { error: "That code isn’t right.", field: "code" },
        },
        { status: 201, body: testSession() },
      ],
    });
    renderAt("/auth/register");
    signUp();
    expect(
      await screen.findByRole("heading", { name: "Check your email" }),
    ).toBeInTheDocument();
    expect(screen.getByText("ada@example.com")).toBeInTheDocument();
    const code = screen.getByLabelText("Verification code");
    expect(code).toHaveFocus();
    expect(code).toHaveAttribute("autocomplete", "one-time-code");
    type("Verification code", "000000");
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "That code isn’t right.",
    );
    type("Verification code", "482 913");
    expect(await screen.findByText(/onboarding/)).toBeInTheDocument();
    expect(api.sent("POST").map((r) => [r.url, r.body])).toEqual([
      [
        "/api/auth/register",
        {
          name: "Ada Lovelace",
          email: "ada@example.com",
          password: "correct horse battery staple",
        },
      ],
      ["/api/auth/register/check", {}],
      ["/api/auth/register/verify", { code: "000000" }],
      ["/api/auth/register/verify", { code: "482913" }],
    ]);
  });
  it("returns to the invitation link it came from", async () => {
    stubApi({
      "GET /api/auth/me": signedOut,
      "POST /api/invitations/check": { status: 404 },
      "POST /api/auth/register": { status: 202, body: waiting },
      "POST /api/auth/register/check": { body: pending },
      "POST /api/auth/register/verify": { status: 201, body: testSession() },
    });
    renderAt(`/auth/register?next=${encodeURIComponent(link)}`);
    signUp();
    fireEvent.change(await screen.findByLabelText("Verification code"), {
      target: { value: "482913" },
    });
    expect(await screen.findByText(/invitation/)).toBeInTheDocument();
  });
  it("sends a new code", async () => {
    const api = stubApi({
      "GET /api/auth/me": signedOut,
      "POST /api/auth/register/check": { body: pending },
      "POST /api/auth/register/resend": { status: 202, body: waiting },
    });
    renderAt("/auth/register/verify");
    fireEvent.click(
      await screen.findByRole("button", { name: "Send a new code" }),
    );
    expect(
      await screen.findByText("We sent a new code to ada@example.com."),
    ).toBeInTheDocument();
    expect(api.sent("POST").map((r) => r.url)).toContain(
      "/api/auth/register/resend",
    );
  });
  it("starts again when the sign-up has ended", async () => {
    stubApi({
      "GET /api/auth/me": signedOut,
      "POST /api/auth/register/check": [
        { body: pending },
        {
          status: 401,
          body: { error: "This sign-up has expired. Start again." },
        },
      ],
      "POST /api/auth/register/verify": {
        status: 401,
        body: { error: "Too many wrong codes. Start again." },
      },
    });
    renderAt("/auth/register/verify");
    fireEvent.change(await screen.findByLabelText("Verification code"), {
      target: { value: "000000" },
    });
    expect(
      await screen.findByText("Too many wrong codes. Start again."),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Back to sign up" }),
    ).toHaveAttribute("href", "/auth/register");
  });
  it("says, to everyone, that a blocked address gets no code", async () => {
    const blocked =
      "This address has been blocked. To use it, unblock it from the link in an email we sent, then sign up again.";
    stubApi({
      "GET /api/auth/me": signedOut,
      "POST /api/auth/register/check": [
        { body: pending },
        { status: 401, body: { error: blocked } },
      ],
      // A code sent before the address was blocked.
      "POST /api/auth/register/verify": {
        status: 401,
        body: { error: blocked },
      },
    });
    renderAt("/auth/register/verify");
    expect(
      await screen.findByText(
        "If you blocked this address, no code can reach it: unblock it from the link in our email first.",
      ),
    ).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Verification code"), {
      target: { value: "482913" },
    });
    expect(await screen.findByText(blocked)).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Back to sign up" }),
    ).toHaveAttribute("href", "/auth/register");
  });
  it("starts again when there's no sign-up to resume", async () => {
    stubApi({
      "GET /api/auth/me": signedOut,
      "POST /api/auth/register/check": {
        status: 401,
        body: { error: "This sign-up has expired. Start again." },
      },
    });
    renderAt("/auth/register/verify");
    expect(
      await screen.findByText("This sign-up has expired. Start again."),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("Verification code")).toBeNull();
  });
  it("sends an email that turned out to have an account to sign in", async () => {
    stubApi({
      "GET /api/auth/me": signedOut,
      "POST /api/auth/register/check": { body: pending },
      "POST /api/auth/register/verify": {
        status: 409,
        body: { error: "You already have an account. Sign in instead." },
      },
    });
    renderAt("/auth/register/verify");
    fireEvent.change(await screen.findByLabelText("Verification code"), {
      target: { value: "482913" },
    });
    expect(
      await screen.findByText(/You already have an account. Sign in here, or/),
    ).toBeInTheDocument();
    // The email comes along in navigation state, never the URL.
    expect(screen.getByLabelText("Email")).toHaveValue("ada@example.com");
    expect(
      screen.getByRole("link", { name: "reset your password" }),
    ).toHaveAttribute("href", "/auth/forgot-password");
  });
});
describe("the login page's notices", () => {
  it("shows only the notices it knows, never text from the URL", async () => {
    stubApi({ "GET /api/auth/me": signedOut });
    renderAt("/auth/login?notice=%3Cb%3EHacked%3C%2Fb%3E");
    expect(
      await screen.findByRole("heading", { name: "Sign in to your account" }),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Hacked/)).toBeNull();
    expect(screen.queryByText(/You already have an account/)).toBeNull();
  });
});
