import { StrictMode } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import { createRoutesStub, data } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { resetClientErrorReports } from "@/lib/client-errors";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import { ErrorBoundary } from "./root";
// The root error boundary: the outage page for a crash, which it reports
// once (signed in), and the not-found page for a 404, which it doesn't.
// The browser's address is the page's, as in the app (the report reads its
// route from it); the stub routes in memory.
function renderCrash(loader: () => unknown) {
  window.history.replaceState(null, "", "/app/settings?q=ada");
  const Stub = createRoutesStub([
    { path: "/app/settings", loader, Component: () => null, ErrorBoundary },
  ]);
  render(
    <StrictMode>
      <Stub initialEntries={["/app/settings"]} />
    </StrictMode>,
  );
}
beforeEach(() => resetClientErrorReports());
afterEach(() => window.history.replaceState(null, "", "/"));
describe("the root error boundary", () => {
  it("reports a crash once, by its route pattern", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const api = stubApi({
      "GET /api/auth/me": { body: testSession() },
      "POST /api/client-errors": { status: 202 },
    });
    renderCrash(() => {
      throw new TypeError("project.name is undefined");
    });
    await screen.findByRole("button", { name: "Try again" });
    await waitFor(() => expect(api.sent("POST")).toHaveLength(1));
    expect(api.sent("POST")[0].body).toMatchObject({
      message: "TypeError: project.name is undefined",
      route: "/app/settings",
    });
    // Strict mode ran its effect twice; still one report.
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(api.sent("POST")).toHaveLength(1);
  });
  it("doesn't report a 404", async () => {
    const api = stubApi({ "*": { status: 202 } });
    renderCrash(() => {
      throw data(null, { status: 404 });
    });
    await screen.findByText("This page has wandered off.");
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(api.fetch).not.toHaveBeenCalled();
  });
});
