import { describe, expect, it, vi } from "vitest";
import { ApiError } from "./http";
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
describe("challenges (428)", () => {
  const challenge = { provider: "turnstile", siteKey: "site", action: "login" };
  const asked = () =>
    new Response(
      JSON.stringify({ error: "Complete the check to continue.", challenge }),
      { status: 428 },
    );
  function replies(...responses: Response[]) {
    const fetch = vi.fn();
    for (const r of responses) fetch.mockResolvedValueOnce(r);
    vi.stubGlobal("fetch", fetch);
    return fetch;
  }
  it("solves it and resends once, with the token and the same body", async () => {
    const fetch = replies(
      asked(),
      new Response('{"ok":true}', { status: 200 }),
    );
    const solve = vi.fn().mockResolvedValue("token-1");
    expect(
      await browserHttp("csrf", solve).send("POST", "/api/auth/login", {
        a: 1,
      }),
    ).toEqual({ ok: true });
    expect(solve).toHaveBeenCalledWith(challenge);
    const [first, second] = fetch.mock.calls;
    expect(first[1].headers.has("X-Challenge-Token")).toBe(false);
    expect(second[0]).toBe("/api/auth/login");
    expect(second[1].method).toBe("POST");
    expect(second[1].body).toBe('{"a":1}');
    expect(second[1].headers.get("X-Challenge-Token")).toBe("token-1");
    expect(second[1].headers.get("X-CSRF-Token")).toBe("csrf");
  });
  it("throws a second 428 rather than asking again", async () => {
    replies(asked(), asked());
    const solve = vi.fn().mockResolvedValue("token-1");
    await expect(
      browserHttp("csrf", solve).send("POST", "/api/auth/login", {}),
    ).rejects.toMatchObject({ status: 428 });
    expect(solve).toHaveBeenCalledTimes(1);
  });
  it("throws the cancellation, as any failure", async () => {
    const fetch = replies(asked());
    const solve = vi
      .fn()
      .mockRejectedValue(new ApiError("Check cancelled.", 428));
    await expect(
      browserHttp("csrf", solve).send("POST", "/api/auth/login", {}),
    ).rejects.toEqual(new ApiError("Check cancelled.", 428));
    expect(fetch).toHaveBeenCalledTimes(1);
  });
  it("never solves an ordinary 429, or without a solver", async () => {
    replies(new Response('{"error":"Too many attempts."}', { status: 429 }));
    const solve = vi.fn();
    await expect(
      browserHttp("csrf", solve).send("POST", "/api/auth/login", {}),
    ).rejects.toMatchObject({ status: 429 });
    expect(solve).not.toHaveBeenCalled();
    replies(asked());
    await expect(
      browserHttp().send("POST", "/api/auth/login", {}),
    ).rejects.toMatchObject({
      status: 428,
      message: "Complete the check to continue.",
    });
  });
});
