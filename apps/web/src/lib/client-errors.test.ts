import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import type { RouteConfigEntry } from "@react-router/dev/routes";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { ApiError } from "@/lib/api";
import { testSession } from "@/test/session";
import { stubApi } from "@/test/api";
import routes from "../routes";
import {
  clientRoutePatterns,
  listenForClientErrors,
  reportClientError,
  resetClientErrorReports,
  routePattern,
} from "./client-errors";
// Vitest runs from apps/web.
const go = readFileSync(
  resolve(
    process.cwd(),
    "../api/internal/httpapi/endpoints/account/client_errors.go",
  ),
  "utf8",
);
const session = testSession();
const signedIn = () =>
  stubApi({
    "GET /api/auth/me": { body: session },
    "POST /api/client-errors": { status: 202 },
  });
// Lets a report's two requests finish.
const settled = () => new Promise((resolve) => setTimeout(resolve, 0));
beforeEach(() => resetClientErrorReports());
afterEach(() => window.history.replaceState(null, "", "/"));
// Every page's full pattern, from the route config (index and layout routes
// add no segment).
function patterns(entries: RouteConfigEntry[], parent = ""): string[] {
  return entries.flatMap((entry) => {
    const path = entry.path ? `${parent}/${entry.path}` : parent;
    const own = entry.path || entry.index ? [path || "/"] : [];
    return [...own, ...patterns(entry.children ?? [], path)];
  });
}
describe("route patterns", () => {
  it("name the page's route, never its path", () => {
    expect(routePattern("/")).toBe("/");
    expect(routePattern("/app/settings")).toBe("/app/settings");
    expect(
      routePattern("/app/wsp_2yEHYLwiwPVhLW/projects/prj_j292s2S2j7Qx4B"),
    ).toBe("/app/:workspace/projects/:project");
    expect(routePattern("/app/wsp_2yEHYLwiwPVhLW/settings/members")).toBe(
      "/app/:workspace/settings/members",
    );
    expect(routePattern("/auth/forgot-password/s3cr3t-token")).toBe(
      "/auth/forgot-password/:token",
    );
    expect(routePattern("/nowhere/at/all")).toBe("*");
  });
  it("match the app build's pages and the API's list", () => {
    const pages = patterns(routes).filter((p) => p !== "/*");
    expect([...clientRoutePatterns].sort()).toEqual([...new Set(pages)].sort());
    const listed = [...go.matchAll(/^\t"([^"]+)":\s+true,$/gm)].map(
      (m) => m[1],
    );
    expect(listed.sort()).toEqual([...clientRoutePatterns, "*"].sort());
  });
});
describe("reporting an error", () => {
  it("sends the message, stack and route pattern with the session's CSRF token, never the URL", async () => {
    window.history.replaceState(
      null,
      "",
      "/auth/forgot-password/s3cr3t-token?next=%2Fapp&email=ada%40example.com",
    );
    const api = signedIn();
    const error = new TypeError("x is undefined");
    await reportClientError(error);
    const [sent] = api.sent("POST");
    expect(sent.url).toBe("/api/client-errors");
    expect(sent.csrf).toBe(session.csrfToken);
    expect(sent.body).toEqual({
      message: "TypeError: x is undefined",
      stack: error.stack,
      route: "/auth/forgot-password/:token",
      release: "",
    });
    const text = JSON.stringify(api.sent());
    for (const value of ["s3cr3t", "ada", "next="])
      expect(text).not.toContain(value);
  });
  it("sends nothing signed out", async () => {
    const api = stubApi({
      "GET /api/auth/me": {
        status: 401,
        body: { error: "Please sign in to continue." },
      },
    });
    await reportClientError(new Error("boom"));
    expect(api.sent("POST")).toEqual([]);
    expect(api.sent("GET")).toHaveLength(1);
  });
  it("sends each distinct error once per page load, ten at most", async () => {
    const api = signedIn();
    // Thrown again from the same place, as a re-render would.
    const fail = () => new Error("once");
    await reportClientError(fail());
    await reportClientError(fail());
    expect(api.sent("POST")).toHaveLength(1);
    await reportClientError(new Error("once")); // from elsewhere
    expect(api.sent("POST")).toHaveLength(2);
    for (let i = 0; i < 20; i++) await reportClientError(`problem ${i}`);
    expect(api.sent("POST")).toHaveLength(10);
  });
  it("ignores what isn't the web app's bug", async () => {
    const api = signedIn();
    const timeout = new Error("signal timed out");
    timeout.name = "TimeoutError";
    for (const value of [
      new ApiError("Something went wrong.", 500),
      new ApiError("Not found.", 404),
      new TypeError("Failed to fetch"),
      timeout,
      "Script error.",
      undefined,
      null,
    ])
      await reportClientError(value);
    expect(api.fetch).not.toHaveBeenCalled();
  });
  it("names a value that isn't an Error only by its type", async () => {
    const api = signedIn();
    await reportClientError({ email: "ada@example.com" });
    expect(api.sent("POST")[0].body).toMatchObject({
      message: "A object was thrown",
      stack: "",
    });
  });
  it("leaves out the text JSON.parse quotes", async () => {
    const api = signedIn();
    const error = new SyntaxError(
      `Unexpected token 'o', "{"name":"Ada Lovelace","email":"ada@example.com"}" is not valid JSON`,
    );
    await reportClientError(error);
    const body = api.sent("POST")[0].body as { message: string; stack: string };
    expect(body.message).toBe(
      `SyntaxError: Unexpected token 'o', "…" is not valid JSON`,
    );
    expect(JSON.stringify(body)).not.toContain("Ada");
  });
  it("stops for the page load once the API refuses for a limit", async () => {
    const api = stubApi({
      "GET /api/auth/me": { body: session },
      "POST /api/client-errors": {
        status: 429,
        body: { error: "Too many requests." },
      },
    });
    await reportClientError(new Error("first"));
    await reportClientError(new Error("second"));
    await reportClientError(new Error("third"));
    expect(api.sent("POST")).toHaveLength(1);
    expect(api.sent("GET")).toHaveLength(1);
  });
  it("cuts a long report to fit the API's 4 KiB", async () => {
    const api = signedIn();
    const error = new Error("é".repeat(5000));
    error.stack = "    at f (https://h/a.js:1:2)\n".repeat(400);
    await reportClientError(error);
    const body = api.sent("POST")[0].body;
    expect(new TextEncoder().encode(JSON.stringify(body)).length).toBeLessThan(
      4096,
    );
  });
  it("hears errors and rejections nothing caught, until told to stop", async () => {
    const api = signedIn();
    const stop = listenForClientErrors();
    window.dispatchEvent(
      new ErrorEvent("error", { error: new Error("uncaught"), message: "x" }),
    );
    const rejection = new Event("unhandledrejection") as Event & {
      reason?: unknown;
    };
    rejection.reason = new Error("unhandled");
    window.dispatchEvent(rejection);
    await settled();
    await settled();
    stop();
    window.dispatchEvent(new ErrorEvent("error", { message: "late" }));
    await settled();
    expect(
      api.sent("POST").map((s) => (s.body as { message: string }).message),
    ).toEqual(["Error: uncaught", "Error: unhandled"]);
  });
});
