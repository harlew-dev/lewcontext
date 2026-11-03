import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { describe, expect, it } from "vitest";
import { gate, stubApi } from "@/test/api";
import EmailBlockShow from "./[token]";
// The block link's page, without a loader: it has none.
function renderAt(pathname: string) {
  render(
    <MemoryRouter initialEntries={[pathname]}>
      <Routes>
        <Route path="/auth/email-blocks/:token" element={<EmailBlockShow />} />
      </Routes>
    </MemoryRouter>,
  );
}
const token = "aB3_-".repeat(8) + "xyz"; // 43 base64url characters
const page = `/auth/email-blocks/${token}`;
const link = { email: "bob@example.com", registered: false, blocked: false };
const deadLink = {
  status: 422,
  body: { error: "This link has expired or isn’t valid.", field: "token" },
};
describe("a block link", () => {
  it("blocks the address, then unblocks it, sending the token only in bodies", async () => {
    const api = stubApi({
      "POST /api/email-blocks/check": { body: link },
      "POST /api/email-blocks": { status: 204 },
      "DELETE /api/email-blocks": { status: 204 },
    });
    renderAt(page);
    expect(
      await screen.findByRole("heading", { name: "Block this address?" }),
    ).toBeInTheDocument();
    expect(screen.getByText("bob@example.com").tagName).toBe("STRONG");

    fireEvent.click(screen.getByRole("button", { name: "Block this address" }));
    expect(
      await screen.findByRole("heading", { name: "This address is blocked" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Address blocked.");
    expect(screen.getByText(/Keep this email/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Unblock" }));
    expect(
      await screen.findByRole("heading", { name: "Block this address?" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Address unblocked.");

    expect(
      api.sent().map(({ method, url, body }) => ({ method, url, body })),
    ).toEqual([
      { method: "POST", url: "/api/email-blocks/check", body: { token } },
      { method: "POST", url: "/api/email-blocks", body: { token } },
      { method: "DELETE", url: "/api/email-blocks", body: { token } },
    ]);
  });

  it("shows the pending state and blocks once", async () => {
    const held = gate();
    const api = stubApi({
      "POST /api/email-blocks/check": { body: link },
      "POST /api/email-blocks": { status: 204, gate: held.promise },
    });
    renderAt(page);
    const button = await screen.findByRole("button", {
      name: "Block this address",
    });
    fireEvent.click(button);
    fireEvent.click(button);
    // The spinner shows once the request has taken 200 ms.
    await waitFor(() => expect(button).toHaveAttribute("aria-busy", "true"));
    held.release();
    expect(
      await screen.findByRole("heading", { name: "This address is blocked" }),
    ).toBeInTheDocument();
    expect(
      api.sent("POST").filter((r) => r.url === "/api/email-blocks"),
    ).toHaveLength(1);
  });

  it("keeps the page when blocking fails", async () => {
    stubApi({
      "POST /api/email-blocks/check": { body: link },
      "POST /api/email-blocks": {
        status: 429,
        body: { error: "Too many attempts. Try again in 12 minutes." },
      },
    });
    renderAt(page);
    fireEvent.click(
      await screen.findByRole("button", { name: "Block this address" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Too many attempts. Try again in 12 minutes.",
    );
    expect(
      screen.getByRole("heading", { name: "Block this address?" }),
    ).toBeInTheDocument();
  });

  it("starts from the blocked state with Unblock", async () => {
    stubApi({
      "POST /api/email-blocks/check": { body: { ...link, blocked: true } },
    });
    renderAt(page);
    expect(
      await screen.findByRole("heading", { name: "This address is blocked" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Unblock" })).toBeInTheDocument();
  });

  it("tells an address with an account to delete it first", async () => {
    stubApi({
      "POST /api/email-blocks/check": { body: { ...link, registered: true } },
    });
    renderAt(page);
    expect(
      await screen.findByRole("heading", {
        name: "This address has an account",
      }),
    ).toBeInTheDocument();
    expect(screen.getByText(/delete your account first/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Sign in" })).toHaveAttribute(
      "href",
      "/auth/login",
    );
    expect(
      screen.queryByRole("button", { name: "Block this address" }),
    ).toBeNull();
  });

  it("switches to the account's state when one appeared meanwhile", async () => {
    stubApi({
      "POST /api/email-blocks/check": { body: link },
      "POST /api/email-blocks": {
        status: 409,
        body: {
          error: "This address has an account. Delete your account first.",
        },
      },
    });
    renderAt(page);
    fireEvent.click(
      await screen.findByRole("button", { name: "Block this address" }),
    );
    expect(
      await screen.findByRole("heading", {
        name: "This address has an account",
      }),
    ).toBeInTheDocument();
  });

  it("renders the address as text", async () => {
    stubApi({
      "POST /api/email-blocks/check": {
        body: { ...link, email: "<em>bob</em>@example.com" },
      },
    });
    renderAt(page);
    expect(
      await screen.findByText("<em>bob</em>@example.com"),
    ).toBeInTheDocument();
    expect(document.querySelector("em")).toBeNull();
  });

  it("says a dead link has expired, and checks no malformed one", async () => {
    stubApi({ "POST /api/email-blocks/check": deadLink });
    renderAt(page);
    expect(
      await screen.findByRole("heading", { name: "This link has expired" }),
    ).toBeInTheDocument();

    const api = stubApi({});
    renderAt("/auth/email-blocks/not-a-token");
    expect(
      screen.getAllByRole("heading", { name: "This link has expired" }),
    ).toHaveLength(2);
    expect(api.sent()).toEqual([]);
  });

  it("offers Try again when the API can't be reached", async () => {
    stubApi({
      "POST /api/email-blocks/check": [{ network: true }, { body: link }],
    });
    renderAt(page);
    fireEvent.click(await screen.findByRole("button", { name: "Try again" }));
    expect(
      await screen.findByRole("heading", { name: "Block this address?" }),
    ).toBeInTheDocument();
  });
});
