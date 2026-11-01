import { describe, expect, it, vi } from "vitest";
import { ApiError, requestIdOf } from "./http";
import { browserHttp } from "./http";
import { path } from "./paths";
function stubFetch(response: Response) {
  const fetch = vi.fn().mockResolvedValue(response);
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
describe("browser transport", () => {
  it("sends the CSRF token, cookie credentials and a JSON body on a mutation", async () => {
    const fetch = stubFetch(
      new Response(JSON.stringify({ id: 1 }), { status: 201 }),
    );
    expect(
      await browserHttp("csrf").send("POST", "/api/projects", { a: 1 }),
    ).toEqual({ id: 1 });
    const [url, options] = fetch.mock.calls[0];
    expect(url).toBe("/api/projects");
    expect(options.method).toBe("POST");
    expect(options.body).toBe('{"a":1}');
    expect(options.credentials).toBe("same-origin");
    expect(options.headers.get("X-CSRF-Token")).toBe("csrf");
    expect(options.headers.get("Content-Type")).toBe("application/json");
  });
  it("sends no CSRF header or content type on a read without a session", async () => {
    const fetch = stubFetch(new Response("{}", { status: 200 }));
    await browserHttp().get(path<object>("/api/auth/me"));
    const [, options] = fetch.mock.calls[0];
    expect(options.method).toBe("GET");
    expect(options.headers.has("X-CSRF-Token")).toBe(false);
    expect(options.headers.has("Content-Type")).toBe(false);
  });
  it("surfaces safe API error messages with status", async () => {
    stubFetch(
      new Response(JSON.stringify({ error: "Session expired" }), {
        status: 401,
      }),
    );
    await expect(browserHttp().get(path("/api/auth/me"))).rejects.toEqual(
      new ApiError("Session expired", 401, undefined, {
        error: "Session expired",
      }),
    );
  });
  it("keeps the rest of an error's body", async () => {
    const workspaces = [{ id: "wsp_2yEHYLwiwPVhLW", name: "Acme" }];
    stubFetch(
      new Response(JSON.stringify({ error: "Delete them first", workspaces }), {
        status: 409,
      }),
    );
    const error = await browserHttp("csrf")
      .send("DELETE", "/api/auth/me", { password: "x" })
      .catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).details.workspaces).toEqual(workspaces);
  });
  it("handles an empty 204 response", async () => {
    stubFetch(new Response(null, { status: 204 }));
    await expect(
      browserHttp("csrf").send("POST", "/api/auth/logout", {}),
    ).resolves.toBeUndefined();
  });
  it("handles an empty 202 response", async () => {
    stubFetch(new Response(null, { status: 202 }));
    await expect(
      browserHttp().send("POST", "/api/auth/password-reset", {}),
    ).resolves.toBeUndefined();
  });
  it("keeps a 500's request ID as its reference, only in the API's shape", async () => {
    stubFetch(
      new Response(
        JSON.stringify({
          error: "Something went wrong. Please try again.",
          requestId: "req_2yEHYLwiwPVhLW",
        }),
        { status: 500 },
      ),
    );
    const error = await browserHttp()
      .get(path("/api/projects"))
      .catch((e: unknown) => e);
    expect(requestIdOf(error)).toBe("req_2yEHYLwiwPVhLW");
    for (const odd of ["has space", "x".repeat(65), 7, null]) {
      expect(
        new ApiError("x", 500, undefined, { requestId: odd }).requestId,
      ).toBeUndefined();
    }
    expect(requestIdOf(new Error("x"))).toBeUndefined();
  });
  it("handles non-JSON upstream errors", async () => {
    stubFetch(new Response("bad gateway", { status: 502 }));
    await expect(
      browserHttp().get(path("/api/projects")),
    ).rejects.toMatchObject({
      status: 502,
      message: "Unable to connect. Please try again.",
    });
  });
});
