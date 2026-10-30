import type { ReactNode } from "react";
import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useApi } from "./api";
import {
  SessionProvider,
  useIsGuest,
  useOptionalSession,
  useSession,
} from "./session";
import type { Session } from "./types";
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
const session: Session = {
  user: { id: 7, name: "Ada Lovelace", email: "ada@forma.test", createdAt: "" },
  csrfToken: "csrf-7",
  expiresAt: "",
  signedInAt: "",
};
function provide(value: Session | null) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <SessionProvider session={value}>{children}</SessionProvider>;
  };
}
function stubFetch() {
  const fetch = vi
    .fn()
    .mockImplementation(async () => new Response("{}", { status: 200 }));
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
describe("useSession()", () => {
  it("returns the signed-in session", () => {
    const { result } = renderHook(useSession, { wrapper: provide(session) });
    expect(result.current.user.id).toBe(7);
  });
  it("throws on a signed-out page", () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    expect(() => renderHook(useSession, { wrapper: provide(null) })).toThrow(
      "useSession() needs a signed-in page",
    );
  });
});
describe("useIsGuest() and useOptionalSession()", () => {
  it("tell guest pages whether someone is signed in", () => {
    const guest = renderHook(() => [useIsGuest(), useOptionalSession()], {
      wrapper: provide(null),
    });
    expect(guest.result.current).toEqual([true, null]);
    const user = renderHook(() => [useIsGuest(), useOptionalSession()], {
      wrapper: provide(session),
    });
    expect(user.result.current).toEqual([false, session]);
  });
});
describe("useApi()", () => {
  it("sends the session's CSRF token with mutations", async () => {
    const fetch = stubFetch();
    const { result } = renderHook(useApi, { wrapper: provide(session) });
    await result.current.sessions().destroyOthers();
    expect(fetch.mock.calls[0][1].headers.get("X-CSRF-Token")).toBe("csrf-7");
  });
  it("sends no token when signed out", async () => {
    const fetch = stubFetch();
    const { result } = renderHook(useApi, { wrapper: provide(null) });
    await result.current.auth().login({ email: "a@b.test", password: "x" });
    expect(fetch.mock.calls[0][1].headers.has("X-CSRF-Token")).toBe(false);
  });
  it("keeps the same client until the session changes", () => {
    let value: Session | null = session;
    function Wrapper({ children }: { children: ReactNode }) {
      return <SessionProvider session={value}>{children}</SessionProvider>;
    }
    const { result, rerender } = renderHook(useApi, { wrapper: Wrapper });
    const first = result.current;
    rerender();
    expect(result.current).toBe(first);
    value = { ...session, csrfToken: "rotated" };
    rerender();
    expect(result.current).not.toBe(first);
  });
});
