import { useState } from "react";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { SWRConfig } from "swr";
import { expect, it, vi } from "vitest";
import { Button } from "@/components/ui/button";
import { SessionProvider, useSession } from "@/lib/session";
import { gate, stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import { ChangeEmailDialog } from "./change-email-dialog";

const initial = testSession({ csrfToken: "old-csrf" });
const changed = {
  ...initial,
  csrfToken: "rotated-csrf",
  user: { ...initial.user, email: "new@example.test" },
};
const pending = {
  email: "new@example.test",
  expiresAt: "2030-01-01T12:15:00Z",
  state: "code",
};

function renderDialog(current = initial, initialEmail?: string) {
  const done = vi.fn();
  function Demo() {
    const [open, setOpen] = useState(true);
    const session = useSession();
    return (
      <>
        <p>
          {session.user.email} / {session.csrfToken}
        </p>
        <Button onClick={() => setOpen(true)}>Open change</Button>
        <ChangeEmailDialog
          open={open}
          onClose={() => setOpen(false)}
          onChanged={done}
          initialEmail={initialEmail}
        />
      </>
    );
  }
  render(
    <MemoryRouter>
      <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>
        <SessionProvider session={current}>
          <Demo />
        </SessionProvider>
      </SWRConfig>
    </MemoryRouter>,
  );
  return done;
}
// The dialog stays open on a success screen: nothing is reported to the page
// (which may navigate away) until it's dismissed. Escape and a backdrop click
// run the same close as the button (the e2e spec presses Escape for real).
async function dismissSuccess(done: ReturnType<typeof vi.fn>) {
  expect(
    await screen.findByRole("heading", { name: "Email address changed" }),
  ).toBeInTheDocument();
  expect(screen.getByText(/Your sign-in email is now/)).toHaveTextContent(
    "new@example.test",
  );
  expect(screen.getByRole("dialog")).toBeInTheDocument();
  expect(done).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  await waitFor(() =>
    expect(done).toHaveBeenCalledExactlyOnceWith("new@example.test"),
  );
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
}
function type(label: string, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
}

it("requests with fresh proof, checks only the inbox code, and renews the session", async () => {
  const api = stubApi({
    "GET /api/auth/email-change": { body: { pending: null } },
    "POST /api/auth/email-change": { status: 202, body: { pending } },
    "PATCH /api/auth/email-change": [
      { status: 422, body: { error: "Wrong email code.", field: "code" } },
      { body: changed },
    ],
  });
  const done = renderDialog();
  await screen.findByLabelText("New email address");
  type("New email address", "new@example.test");
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  type("Current password", "my current password");
  fireEvent.click(screen.getByRole("button", { name: "Send code" }));
  await screen.findByLabelText("Email change code");
  expect(api.sent("POST")[0].body).toEqual({
    email: "new@example.test",
    currentPassword: "my current password",
  });
  type("Email change code", "123456");
  expect(await screen.findByText("Wrong email code.")).toBeInTheDocument();
  type("Email change code", "654321");
  await dismissSuccess(done);
  expect(api.sent("PATCH").map((sent) => sent.body)).toEqual([
    { code: "123456" },
    { code: "654321" },
  ]);
  expect(
    screen.getByText("new@example.test / rotated-csrf"),
  ).toBeInTheDocument();
});

it("resumes a pending request at its code and requires fresh proof to resend", async () => {
  const api = stubApi({
    "GET /api/auth/email-change": { body: { pending } },
    "POST /api/auth/email-change": { status: 202, body: { pending } },
    "DELETE /api/auth/email-change": { status: 204 },
  });
  renderDialog();
  // No "active request" screen: straight to the code it is waiting for.
  await screen.findByLabelText("Email change code");
  expect(screen.queryByText(/active change request/)).toBeNull();
  expect(api.sent("POST")).toHaveLength(0);
  fireEvent.click(screen.getByRole("button", { name: "Send a new code" }));
  expect(screen.getByLabelText("Current password")).toHaveValue("");
  expect(api.sent("POST")).toHaveLength(0);
  type("Current password", "fresh password");
  fireEvent.click(screen.getByRole("button", { name: "Send code" }));
  await screen.findByLabelText("Email change code");
  fireEvent.click(screen.getByRole("button", { name: "Cancel change" }));
  // Cancelling ends on its own screen with only a Close button.
  expect(
    await screen.findByRole("heading", {
      name: "Your change has been cancelled",
    }),
  ).toBeInTheDocument();
  expect(api.sent("DELETE")).toHaveLength(1);
  expect(
    within(screen.getByRole("dialog"))
      .getAllByRole("button")
      .map((button) => button.textContent),
  ).toEqual(["Close"]);
  expect(screen.queryByLabelText("New email address")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Close" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
});

it("closes a resumed request without deleting it, or cancels it from the same screen", async () => {
  const api = stubApi({
    "GET /api/auth/email-change": { body: { pending } },
    "DELETE /api/auth/email-change": { status: 204 },
  });
  renderDialog();
  await screen.findByLabelText("Email change code");
  // Close · Cancel change · Change email address.
  expect(
    screen.getAllByRole("button").map((button) => button.textContent),
  ).toEqual(
    expect.arrayContaining(["Close", "Cancel change", "Change email address"]),
  );
  fireEvent.click(screen.getByRole("button", { name: "Close" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(api.sent("DELETE")).toHaveLength(0);
  fireEvent.click(screen.getByRole("button", { name: "Open change" }));
  await screen.findByLabelText("Email change code");
  fireEvent.click(screen.getByRole("button", { name: "Cancel change" }));
  await screen.findByRole("heading", {
    name: "Your change has been cancelled",
  });
  expect(api.sent("DELETE")).toHaveLength(1);
});

it("starts a typed address at account access and replaces a pending request without mentioning it", async () => {
  const replacement = {
    ...pending,
    email: "another@example.test",
    expiresAt: "2030-01-01T12:20:00Z",
  };
  const api = stubApi({
    "GET /api/auth/email-change": { body: { pending } },
    "POST /api/auth/email-change": {
      status: 202,
      body: { pending: replacement },
    },
    "DELETE /api/auth/email-change": { status: 204 },
  });
  renderDialog(initial, "another@example.test");
  await screen.findByLabelText("Current password");
  type("Current password", "my password");
  expect(screen.getByText("another@example.test")).toBeInTheDocument();
  expect(screen.queryByText(/active|already|request/i)).toBeNull();
  expect(screen.queryByRole("button", { name: "Cancel change" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Send code" }));
  await screen.findByLabelText("Email change code");
  // Confirming replaced it on the server: one POST, no separate cancel.
  expect(api.sent("POST").map((sent) => sent.body)).toEqual([
    { email: "another@example.test", currentPassword: "my password" },
  ]);
  expect(api.sent("DELETE")).toHaveLength(0);
  expect(screen.getByText(/another@example.test/)).toBeInTheDocument();
  expect(screen.queryByText(/new@example.test/)).toBeNull();
});

it("doesn't wait on, or show a failed read of, the waiting request for a typed address", async () => {
  stubApi({
    "GET /api/auth/email-change": {
      status: 500,
      body: { error: "Status unavailable." },
    },
    "POST /api/auth/email-change": { status: 202, body: { pending } },
  });
  renderDialog(initial, pending.email);
  type("Current password", "my password");
  expect(screen.queryByText("Status unavailable.")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Send code" }));
  expect(await screen.findByLabelText("Email change code")).toBeInTheDocument();
  expect(screen.queryByText("Status unavailable.")).toBeNull();
});

it("skips inbox codes when verification is off, including the MFA recovery flow", async () => {
  const api = stubApi({
    "GET /api/auth/email-change": { body: { pending: null } },
    "POST /api/auth/email-change": {
      body: { ...changed, emailVerification: "off" },
    },
  });
  const done = renderDialog({
    ...initial,
    emailVerification: "off",
    user: { ...initial.user, mfaEnabledAt: "2026-01-01T00:00:00Z" },
  });
  await screen.findByLabelText("New email address");
  type("New email address", "new@example.test");
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  fireEvent.click(screen.getByRole("button", { name: /Use a recovery code/ }));
  type("Recovery code", "k7p2m-x9qfr");
  fireEvent.click(screen.getByRole("button", { name: "Change email address" }));
  await dismissSuccess(done);
  expect(api.sent("POST")[0].body).toEqual({
    email: "new@example.test",
    code: "k7p2m-x9qfr",
  });
  expect(api.sent("PATCH")).toHaveLength(0);
});

it("preserves a pending request after a failure and resets after expiry", async () => {
  stubApi({
    "GET /api/auth/email-change": { body: { pending } },
    "PATCH /api/auth/email-change": [
      { status: 429, body: { error: "Try again in 12 minutes." } },
      { status: 410, body: { error: "This email change has ended." } },
    ],
  });
  renderDialog();
  await screen.findByLabelText("Email change code");
  type("Email change code", "123456");
  await screen.findByText("Try again in 12 minutes.");
  expect(screen.getByLabelText("Email change code")).toBeInTheDocument();
  type("Email change code", "654321");
  await screen.findByText("This email change has ended.");
  expect(screen.getByLabelText("New email address")).toBeInTheDocument();
});

it("submits once and prevents dismissal while fresh proof is in flight", async () => {
  const held = gate();
  const api = stubApi({
    "GET /api/auth/email-change": { body: { pending: null } },
    "POST /api/auth/email-change": {
      status: 202,
      body: { pending },
      gate: held.promise,
    },
  });
  renderDialog();
  await screen.findByLabelText("New email address");
  type("New email address", pending.email);
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  type("Current password", "current password");
  const form = screen.getByLabelText("Current password").closest("form")!;
  fireEvent.submit(form);
  fireEvent.submit(form);
  expect(api.sent("POST")).toHaveLength(1);
  expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
  expect(screen.getByLabelText("Current password")).toHaveValue("");
  await act(async () => held.release());
  await screen.findByLabelText("Email change code");
});

it("does not turn a completed direct change into failure when cache refresh fails", async () => {
  stubApi({
    "GET /api/auth/email-change": [
      { body: { pending: null } },
      { status: 500, body: { error: "Refresh unavailable." } },
    ],
    "POST /api/auth/email-change": {
      body: { ...changed, emailVerification: "off" },
    },
  });
  const done = renderDialog({ ...initial, emailVerification: "off" });
  await screen.findByLabelText("New email address");
  type("New email address", changed.user.email);
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  type("Current password", "current password");
  fireEvent.click(screen.getByRole("button", { name: "Change email address" }));
  await dismissSuccess(done);
  expect(
    screen.getByText("new@example.test / rotated-csrf"),
  ).toBeInTheDocument();
});
