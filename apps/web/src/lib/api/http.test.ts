import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./http";
import { browserHttp, serverHttp } from "./http";
import { path } from "./paths";
afterEach(() => vi.unstubAllGlobals());
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
      new ApiError("Session expired", 401),
    );
  });
  it("handles an empty 204 response", async () => {
    stubFetch(new Response(null, { status: 204 }));
    await expect(
      browserHttp("csrf").send("POST", "/api/auth/logout", {}),
    ).resolves.toBeUndefined();
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
describe("server transport", () => {
  it("calls the fixed API URL and forwards only the request cookie", async () => {
    const fetch = stubFetch(new Response("{}", { status: 200 }));
    await serverHttp("http://127.0.0.1:8080", "forma_session=abc").get(
      path("/api/auth/me"),
    );
    const [url, options] = fetch.mock.calls[0];
    expect(url).toBe("http://127.0.0.1:8080/api/auth/me");
    expect([...options.headers.keys()]).toEqual(["cookie"]);
    expect(options.headers.get("cookie")).toBe("forma_session=abc");
    expect(options.credentials).toBeUndefined();
  });
});
