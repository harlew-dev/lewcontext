import type { ReactNode } from "react";
import { cleanup, renderHook, waitFor } from "@testing-library/react";
import { SWRConfig } from "swr";
import { afterEach, describe, expect, it, vi } from "vitest";
import { paths } from "./api";
import { SessionProvider } from "./session";
import type { Session } from "./types";
import { useResource } from "./use-resource";
const navigate = vi.hoisted(() => vi.fn());
vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  useNavigate: () => navigate,
  useLocation: () => ({
    pathname: "/app/wsp_00000000000001/projects",
    search: "?page=2",
  }),
}));
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  navigate.mockReset();
});
function sessionFor(id: number): Session {
  return {
    user: {
      id: `usr_0000000000000${id}`,
      name: "Ada",
      email: "ada@forma.test",
      createdAt: "",
    },
    csrfToken: "csrf",
    expiresAt: "",
    signedInAt: "",
  };
}
const ws = "wsp_00000000000001";
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
    const path = paths.projects(ws).show("prj_00000000000001");
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
    renderHook(
      () => useResource(paths.projects(ws).show("prj_00000000000002")),
      {
        wrapper: provide(sessionFor(3)),
      },
    );
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith(
        "/auth/login?next=%2Fapp%2Fwsp_00000000000001%2Fprojects%3Fpage%3D2",
        { replace: true },
      ),
    );
  });
});
