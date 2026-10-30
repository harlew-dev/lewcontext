import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it } from "vitest";
import { stubApi } from "@/test/api";
import Home from "./home";
const stubMe = (status: number) =>
  stubApi({ "GET /api/auth/me": { status, body: {} } });
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
