import type { ReactNode } from "react";
import { cleanup, renderHook, waitFor } from "@testing-library/react";
import { SWRConfig } from "swr";
import { afterEach, describe, expect, it, vi } from "vitest";
import { paths } from "./api";
import { SessionProvider } from "./session";
import type { Session } from "./types";
import { useResource } from "./use-resource";
const router = vi.hoisted(() => ({
  asPath: "/app/projects?page=2",
  replace: vi.fn(),
}));
vi.mock("next/router", () => ({ useRouter: () => router }));
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  router.replace.mockReset();
});
function sessionFor(id: number): Session {
  return {
    user: { id, name: "Ada", email: "ada@forma.test", createdAt: "" },
    csrfToken: "csrf",
    expiresAt: "",
    signedInAt: "",
  };
}
// One SWR cache shared by every render, like the app's.
const cache = new Map();
function provide(session: Session) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <SWRConfig value={{ provider: () => cache, dedupingInterval: 0 }}>
        <SessionProvider session={session}>{children}</SessionProvider>
      </SWRConfig>
    );
  };
}
function stubFetch(status = 200) {
  const fetch = vi
    .fn()
    .mockImplementation(
      async () =>
        new Response(JSON.stringify({ id: 1, name: "Launch" }), { status }),
    );
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
describe("useResource()", () => {
  it("keeps each user's data apart in the cache", async () => {
    const fetch = stubFetch();
    const path = paths.projects.show(1);
    const first = renderHook(() => useResource(path), {
      wrapper: provide(sessionFor(1)),
    });
    await waitFor(() => expect(first.result.current.data).toBeDefined());
    const second = renderHook(() => useResource(path), {
      wrapper: provide(sessionFor(2)),
    });
    // A different user starts empty and loads their own copy.
    expect(second.result.current.data).toBeUndefined();
    await waitFor(() => expect(second.result.current.data).toBeDefined());
    expect(fetch).toHaveBeenCalledTimes(2);
  });
  it("sends an expired session to login, returning to this page", async () => {
    stubFetch(401);
    renderHook(() => useResource(paths.projects.show(2)), {
      wrapper: provide(sessionFor(3)),
    });
    await waitFor(() =>
      expect(router.replace).toHaveBeenCalledWith(
        "/auth/login?next=%2Fapp%2Fprojects%3Fpage%3D2",
      ),
    );
  });
});
