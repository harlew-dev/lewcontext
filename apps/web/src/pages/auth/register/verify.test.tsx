import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { createRoutesStub, useLocation } from "react-router";
import { SWRConfig } from "swr";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ChallengeProvider } from "@/lib/challenge/challenge";
import * as providers from "@/lib/challenge/providers";
import { registerVerifyLoader } from "@/lib/loaders";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import Login from "../login/index";
import Register from "./index";
import RegisterVerify from "./verify/index";
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
// Register's loader data with the policies off (not published).
const offPolicies = () => ({ policies: { enabled: false as const } });
function renderAt(entry: string) {
  const Stub = createRoutesStub([
    { path: "/auth/register", Component: Register, loader: offPolicies },
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
  // ChallengeProvider, as root.tsx mounts it, for a challenged request.
  render(
    <SWRConfig value={{ provider: () => new Map() }}>
      <ChallengeProvider>
        <Stub initialEntries={[{ pathname, search: search && `?${search}` }]} />
      </ChallengeProvider>
    </SWRConfig>,
  );
}
afterEach(() => {
  vi.restoreAllMocks();
  delete window.turnstile;
});
function type(label: string, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
}
async function signUp() {
  await screen.findByRole("heading", { name: "Create your account" });
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
    await signUp();
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
    // Back on sign-up and on the code page keep the invitation in ?next=.
    expect(await screen.findByRole("link", { name: "Back" })).toHaveAttribute(
      "href",
      `/auth/login?next=${encodeURIComponent(link)}`,
    );
    await signUp();
    await screen.findByLabelText("Verification code");
    expect(screen.getByRole("link", { name: "Back" })).toHaveAttribute(
      "href",
      `/auth/register?next=${encodeURIComponent(link)}`,
    );
    fireEvent.change(screen.getByLabelText("Verification code"), {
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
    // A dialog says what to check first, then sends one on Resend code.
    fireEvent.click(
      await screen.findByRole("button", { name: "Didn’t receive the email?" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Didn’t receive the email?",
    });
    expect(dialog).toHaveTextContent("Check your spam or junk folder");
    expect(api.sent("POST").map((r) => r.url)).not.toContain(
      "/api/auth/register/resend",
    );
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Resend code" }),
    );
    expect(
      await screen.findByText("We sent a new code to ada@example.com."),
    ).toBeInTheDocument();
    expect(api.sent("POST").map((r) => r.url)).toContain(
      "/api/auth/register/resend",
    );
  });
  it("answers a challenge on a new code, waiting until it's solved", async () => {
    // A stub Turnstile widget: the test gives its token.
    let widget: Record<string, (token: string) => void> | undefined;
    window.turnstile = {
      render: (_el: HTMLElement, options: Record<string, unknown>) => {
        widget = options as typeof widget;
        return "w1";
      },
      remove: () => {},
    };
    vi.spyOn(providers, "loadScript").mockResolvedValue();
    const api = stubApi({
      "GET /api/auth/me": signedOut,
      "POST /api/auth/register/check": { body: pending },
      "POST /api/auth/register/resend": [
        {
          status: 428,
          body: {
            error: "Complete the check to continue.",
            challenge: {
              provider: "turnstile",
              siteKey: "site",
              action: "register",
            },
          },
        },
        { status: 202, body: waiting },
      ],
    });
    renderAt("/auth/register/verify");
    fireEvent.click(
      await screen.findByRole("button", { name: "Didn’t receive the email?" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Didn’t receive the email?",
    });
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Resend code" }),
    );
    expect(
      await screen.findByRole("dialog", { name: "Quick security check" }),
    ).toBeInTheDocument();
    // The resend stays pending under the check (loading shows after
    // useAction's 200 ms): no second send, no Cancel.
    await vi.waitFor(() =>
      expect(
        within(dialog).getByRole("button", { name: "Cancel", hidden: true }),
      ).toBeDisabled(),
    );
    await vi.waitFor(() => expect(widget).toBeDefined());
    act(() => widget!.callback("tok"));
    expect(
      await screen.findByText("We sent a new code to ada@example.com."),
    ).toBeInTheDocument();
    const resends = api.fetch.mock.calls.filter(
      ([url]) => url === "/api/auth/register/resend",
    );
    expect(resends).toHaveLength(2);
    expect(new Headers(resends[1][1]?.headers).get("X-Challenge-Token")).toBe(
      "tok",
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
    // Fixed text in the dialog, whatever the address: it may be blocked.
    fireEvent.click(
      await screen.findByRole("button", { name: "Didn’t receive the email?" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Didn’t receive the email?",
    });
    expect(dialog).toHaveTextContent(
      "Did you block this address? If you ever used “Block this address” in one of our emails, we can’t send anything to it",
    );
    // Text to read, not a field: no (disabled) fieldset around it.
    expect(within(dialog).queryByRole("group")).toBeNull();
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
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
