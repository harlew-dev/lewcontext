import { fireEvent, render, screen } from "@testing-library/react";
import { createRoutesStub } from "react-router";
import { SWRConfig } from "swr";
import { describe, expect, it } from "vitest";
import { verifyEmailLoader } from "@/lib/loaders";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import VerifyEmail from "./verify-email";
const unverified = testSession({
  csrfToken: "csrf-7",
  emailVerification: "required",
  user: { emailVerifiedAt: null },
});
function renderPage() {
  const Stub = createRoutesStub([
    {
      path: "/app/verify-email",
      Component: VerifyEmail,
      loader: ({ request }) => verifyEmailLoader(request),
    },
    { path: "/app/settings", Component: () => <p>Account settings</p> },
    { path: "/auth/login", Component: () => <p>Login</p> },
  ]);
  render(
    <SWRConfig value={{ provider: () => new Map() }}>
      <Stub
        initialEntries={[
          { pathname: "/app/verify-email", search: "?next=%2Fapp%2Fsettings" },
        ]}
      />
    </SWRConfig>,
  );
}
describe("the verify-email page", () => {
  it("sends a code only when asked, then goes on to ?next=", async () => {
    const api = stubApi({
      "GET /api/auth/me": { body: unverified },
      "POST /api/auth/email-verification": { status: 202 },
      "PATCH /api/auth/email-verification": {
        body: {
          user: { ...unverified.user, emailVerifiedAt: "2026-10-31T12:00:00Z" },
        },
      },
    });
    renderPage();
    expect(
      await screen.findByText(/You must verify your email to continue/),
    ).toBeInTheDocument();
    expect(api.sent("POST")).toEqual([]);
    fireEvent.click(screen.getByRole("button", { name: "Send code" }));
    fireEvent.change(await screen.findByLabelText("Verification code"), {
      target: { value: "482913" },
    });
    expect(await screen.findByText("Account settings")).toBeInTheDocument();
    expect(api.sent("PATCH")[0]).toMatchObject({
      body: { code: "482913" },
      csrf: "csrf-7",
    });
  });
  it("goes back to sending a code when the last one ended", async () => {
    stubApi({
      "GET /api/auth/me": { body: unverified },
      "POST /api/auth/email-verification": { status: 202 },
      "PATCH /api/auth/email-verification": {
        status: 410,
        body: { error: "Too many wrong codes. Send a new one." },
      },
    });
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "Send code" }));
    fireEvent.change(await screen.findByLabelText("Verification code"), {
      target: { value: "000000" },
    });
    expect(
      await screen.findByText("Too many wrong codes. Send a new one."),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Send code" }),
    ).toBeInTheDocument();
  });
  it("signs out, for the wrong account", async () => {
    const api = stubApi({
      "GET /api/auth/me": { body: unverified },
      "POST /api/auth/logout": { status: 204 },
    });
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "Sign out" }));
    expect(await screen.findByText("Login")).toBeInTheDocument();
    expect(api.sent("POST")[0].url).toBe("/api/auth/logout");
  });
});
