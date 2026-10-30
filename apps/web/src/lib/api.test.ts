import { afterEach, describe, expect, it, vi } from "vitest";
import { request, ApiError } from "./api";
afterEach(() => vi.unstubAllGlobals());
describe("API client", () => {
  it("sends CSRF token and cookie credentials on a mutation", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ id: 1 }), { status: 201 }),
      );
    vi.stubGlobal("fetch", fetch);
    expect(
      await request("/api/projects", { method: "POST", body: "{}" }, "csrf"),
    ).toEqual({ id: 1 });
    const options = fetch.mock.calls[0][1];
    expect(options.credentials).toBe("same-origin");
    expect(options.headers.get("X-CSRF-Token")).toBe("csrf");
    expect(options.headers.get("Content-Type")).toBe("application/json");
  });
  it("surfaces safe API error messages with status", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: "Session expired" }), {
          status: 401,
        }),
      ),
    );
    await expect(request("/api/auth/me")).rejects.toEqual(
      new ApiError("Session expired", 401),
    );
  });
  it("handles an empty logout response", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 204 })),
    );
    await expect(request("/api/auth/logout")).resolves.toBeUndefined();
  });
  it("handles non-JSON upstream errors", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response("bad gateway", { status: 502 })),
    );
    await expect(request("/api/projects")).rejects.toMatchObject({
      status: 502,
      message: "Unable to connect. Please try again.",
    });
  });
});
