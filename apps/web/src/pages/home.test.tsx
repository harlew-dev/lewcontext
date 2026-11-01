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
      "Context for AI agents.",
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
describe("policy links", () => {
  it("shows them below the card once the API says they're published", async () => {
    stubApi({
      "GET /api/auth/me": { status: 401, body: {} },
      "GET /api/policies": {
        body: {
          enabled: true,
          termsOfService: "2026-11-01",
          privacyPolicy: "2026-11-01",
        },
      },
    });
    render(<Home />, { wrapper: MemoryRouter });
    expect(
      await screen.findByRole("link", { name: "Terms of Service" }),
    ).toHaveAttribute("href", "/policy/terms-of-service");
    expect(screen.getByRole("contentinfo")).toHaveTextContent(
      "Terms of Service·Privacy Policy",
    );
  });
  it("leaves them out while they aren't", async () => {
    stubApi({
      "GET /api/auth/me": { status: 401, body: {} },
      "GET /api/policies": { body: { enabled: false } },
    });
    render(<Home />, { wrapper: MemoryRouter });
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(screen.queryByRole("link", { name: "Terms of Service" })).toBeNull();
  });
});
