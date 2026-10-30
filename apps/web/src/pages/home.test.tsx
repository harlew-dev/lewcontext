import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import Home from "./home";
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
function stubMe(status: number) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response("{}", { status })),
  );
}
describe("homepage", () => {
  it("offers to log in when signed out", async () => {
    stubMe(401);
    render(<Home />, { wrapper: MemoryRouter });
    expect(screen.getByRole("heading")).toHaveTextContent(
      "Your work, in good shape.",
    );
    expect(screen.getByRole("link", { name: "Log in" })).toHaveAttribute(
      "href",
      "/auth/login",
    );
    // It stays that way once the session check has answered.
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(screen.queryByRole("link", { name: "Dashboard" })).toBeNull();
  });
  it("switches to the dashboard once it finds a session", async () => {
    stubMe(200);
    render(<Home />, { wrapper: MemoryRouter });
    expect(
      await screen.findByRole("link", { name: "Dashboard" }),
    ).toHaveAttribute("href", "/app");
    expect(screen.queryByRole("link", { name: "Log in" })).toBeNull();
  });
});
