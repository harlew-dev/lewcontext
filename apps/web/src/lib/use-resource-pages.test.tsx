import type { ReactNode } from "react";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { SWRConfig } from "swr";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Workspace } from "@/types/workspaces";
import { paths } from "./api";
import type { Page } from "./pagination";
import { SessionProvider } from "./session";
import type { Session } from "./types";
import { useResourcePages } from "./use-resource-pages";
const router = vi.hoisted(() => ({
  asPath: "/app/settings",
  replace: vi.fn(),
}));
vi.mock("next/router", () => ({ useRouter: () => router }));
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  router.replace.mockReset();
});
const session: Session = {
  user: { id: "usr_00000000000001", name: "Ada", email: "", createdAt: "" },
  csrfToken: "csrf",
  expiresAt: "",
  signedInAt: "",
};
// A fresh cache per test, like a fresh page load.
function wrapper({ children }: { children: ReactNode }) {
  return (
    <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>
      <SessionProvider session={session}>{children}</SessionProvider>
    </SWRConfig>
  );
}
function workspace(n: number): Workspace {
  const id = `wsp_${String(n).padStart(14, "0")}`;
  return {
    id,
    name: `Workspace ${n}`,
    role: "owner",
    createdAt: "",
    updatedAt: "",
  };
}
// Two pages of two: 1, 2 | 3, 4.
function page(current: number, ...items: number[]): Page<Workspace> {
  return {
    data: items.map(workspace),
    pages: { current, total: 2, perPage: 2, items: 4 },
  };
}
const pathFor = (n: number) => paths.workspaces.index({ page: n, perPage: 2 });
const initial = { path: pathFor(1), data: page(1, 1, 2), error: null };
// Answers each page by URL; `fail` makes page 2 fail with that status until
// `recover()`.
const outage = { status: 0 };
function recover() {
  outage.status = 0;
}
function stubFetch(fail = 0) {
  outage.status = fail;
  const fetch = vi.fn().mockImplementation(async (url: string) => {
    const second = url.includes("page=2");
    if (second && outage.status)
      return new Response(JSON.stringify({ error: "Unavailable" }), {
        status: outage.status,
      });
    // Page 2 repeats workspace 2, as if one was created meanwhile.
    const body = second ? page(2, 2, 3, 4) : page(1, 1, 2);
    return new Response(JSON.stringify(body), { status: 200 });
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
describe("useResourcePages()", () => {
  it("starts from the server-rendered page", () => {
    stubFetch();
    const { result } = renderHook(() => useResourcePages(pathFor, initial), {
      wrapper,
    });
    expect(result.current.items.map((w) => w.name)).toEqual([
      "Workspace 1",
      "Workspace 2",
    ]);
    expect(result.current.hasMore).toBe(true);
    expect(result.current.total).toBe(4);
  });
  it("loads the next page, without repeats, and stops at the last", async () => {
    const fetch = stubFetch();
    const { result } = renderHook(() => useResourcePages(pathFor, initial), {
      wrapper,
    });
    act(() => result.current.loadMore());
    await waitFor(() => expect(result.current.items).toHaveLength(4));
    expect(result.current.items.map((w) => w.id)).toEqual(
      [1, 2, 3, 4].map((n) => workspace(n).id),
    );
    expect(result.current.hasMore).toBe(false);
    expect(fetch.mock.calls.map(([url]) => url)).toContain(
      "/api/workspaces?page=2&perPage=2",
    );
  });
  it("keeps loaded items when a page fails, and retries", async () => {
    stubFetch(503);
    const { result } = renderHook(() => useResourcePages(pathFor, initial), {
      wrapper,
    });
    act(() => result.current.loadMore());
    await waitFor(() => expect(result.current.error).toBe("Unavailable"));
    expect(result.current.items).toHaveLength(2);
    recover();
    act(() => result.current.retry());
    await waitFor(() => expect(result.current.items).toHaveLength(4));
    expect(result.current.error).toBeNull();
  });
  it("sends an expired session to login", async () => {
    stubFetch(401);
    const { result } = renderHook(() => useResourcePages(pathFor, initial), {
      wrapper,
    });
    act(() => result.current.loadMore());
    await waitFor(() =>
      expect(router.replace).toHaveBeenCalledWith(
        "/auth/login?next=%2Fapp%2Fsettings",
      ),
    );
  });
});
