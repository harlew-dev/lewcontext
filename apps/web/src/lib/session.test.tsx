import type { ReactNode } from "react";
import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import { useApi } from "./api";
import {
  SessionProvider,
  useIsGuest,
  useOptionalSession,
  useRenewSession,
  useSession,
} from "./session";
import type { Session } from "./types";
const session = testSession({
  csrfToken: "csrf-7",
  user: { id: "usr_00000000000007" },
});
function provide(value: Session | null) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <SessionProvider session={value}>{children}</SessionProvider>;
  };
}
const stubFetch = () => stubApi({ "*": { body: {} } });
describe("useSession()", () => {
  it("returns the signed-in session", () => {
    const { result } = renderHook(useSession, { wrapper: provide(session) });
    expect(result.current.user.id).toBe("usr_00000000000007");
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
    const api = stubFetch();
    const { result } = renderHook(useApi, { wrapper: provide(session) });
    await result.current.sessions().destroyOthers();
    expect(api.sent()[0].csrf).toBe("csrf-7");
  });
  it("sends no token when signed out", async () => {
    const api = stubFetch();
    const { result } = renderHook(useApi, { wrapper: provide(null) });
    await result.current.auth().login({ email: "a@b.test", password: "x" });
    expect(api.sent()[0].csrf).toBeNull();
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
describe("useRenewSession()", () => {
  const rotated: Session = { ...session, csrfToken: "csrf-8" };
  it("replaces the session, so mutations send the new CSRF token", async () => {
    const api = stubFetch();
    const { result } = renderHook(
      () => ({ api: useApi(), renew: useRenewSession() }),
      { wrapper: provide(session) },
    );
    act(() => result.current.renew(rotated));
    await result.current.api.sessions().destroyOthers();
    expect(api.sent()[0].csrf).toBe("csrf-8");
  });
  it("gives way to a session the loader provides later", () => {
    let value: Session = session;
    function Wrapper({ children }: { children: ReactNode }) {
      return <SessionProvider session={value}>{children}</SessionProvider>;
    }
    const { result, rerender } = renderHook(
      () => ({ session: useSession(), renew: useRenewSession() }),
      { wrapper: Wrapper },
    );
    act(() => result.current.renew(rotated));
    expect(result.current.session.csrfToken).toBe("csrf-8");
    value = { ...session, csrfToken: "csrf-9" };
    rerender();
    expect(result.current.session.csrfToken).toBe("csrf-9");
  });
});
