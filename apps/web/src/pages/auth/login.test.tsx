import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { describe, expect, it } from "vitest";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import Login from "./login";
// Login without its route loader (which only sends signed-in visitors on).
function renderLogin() {
  render(
    <MemoryRouter initialEntries={["/auth/login?next=%2Fapp%2Fsettings"]}>
      <Routes>
        <Route path="/auth/login" element={<Login />} />
        <Route path="/app/settings" element={<p>Account settings</p>} />
      </Routes>
    </MemoryRouter>,
  );
}
function type(label: string, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
}
function signIn() {
  type("Email", "ada@example.com");
  type("Password", "correct horse");
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
}
const signedIn = testSession();
describe("signing in with two-factor authentication", () => {
  it("asks for a code after the password, then goes on to ?next=", async () => {
    const api = stubApi({
      "POST /api/auth/login": { body: { mfaRequired: true } },
      "POST /api/auth/login/mfa": [
        {
          status: 422,
          body: { error: "That code didn’t work.", field: "code" },
        },
        { body: signedIn },
      ],
    });
    renderLogin();
    signIn();
    expect(
      await screen.findByRole("heading", { name: "Two-factor authentication" }),
    ).toBeInTheDocument();
    const field = screen.getByLabelText("Authentication code");
    expect(field).toHaveFocus();
    expect(field).toHaveAttribute("autocomplete", "one-time-code");

    type("Authentication code", "000000");
    fireEvent.click(screen.getByRole("button", { name: "Verify" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "That code didn’t work.",
    );
    type("Authentication code", "123 456");
    fireEvent.click(screen.getByRole("button", { name: "Verify" }));
    expect(await screen.findByText("Account settings")).toBeInTheDocument();
    expect(api.sent("POST").map((r) => r.body)).toEqual([
      { email: "ada@example.com", password: "correct horse" },
      { code: "000000" },
      { code: "123 456" },
    ]);
  });
  it("takes a recovery code instead", async () => {
    const api = stubApi({
      "POST /api/auth/login": { body: { mfaRequired: true } },
      "POST /api/auth/login/mfa": { body: signedIn },
    });
    renderLogin();
    signIn();
    fireEvent.click(
      await screen.findByRole("button", {
        name: "Lost your device? Use a recovery code",
      }),
    );
    type("Recovery code", "K7P2M-X9QFR");
    fireEvent.click(screen.getByRole("button", { name: "Verify" }));
    expect(await screen.findByText("Account settings")).toBeInTheDocument();
    expect(api.sent("POST")[1].body).toEqual({ code: "K7P2M-X9QFR" });
  });
  it("goes back to the password when the sign-in ends, saying why", async () => {
    stubApi({
      "POST /api/auth/login": { body: { mfaRequired: true } },
      "POST /api/auth/login/mfa": {
        status: 401,
        body: { error: "Too many wrong codes. Enter your password again." },
      },
    });
    renderLogin();
    signIn();
    await screen.findByLabelText("Authentication code");
    type("Authentication code", "000000");
    fireEvent.click(screen.getByRole("button", { name: "Verify" }));
    expect(
      await screen.findByText(
        "Too many wrong codes. Enter your password again.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Email")).toHaveValue("ada@example.com");
    expect(screen.getByLabelText("Password")).toHaveValue("");
  });
});
