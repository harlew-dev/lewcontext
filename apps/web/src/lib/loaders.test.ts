import { afterEach, describe, expect, it, vi } from "vitest";
import {
  guestLoader,
  onboardingLoader,
  signedInLoader,
  workspaceHomeLoader,
} from "./loaders";
afterEach(() => vi.unstubAllGlobals());
const session = {
  user: { id: "usr_00000000000001", name: "Ada", email: "", createdAt: "" },
  csrfToken: "csrf",
  expiresAt: "",
  signedInAt: "",
};
const workspace = {
  id: "wsp_2yEHYLwiwPVhLW",
  name: "Acme",
  role: "owner",
  createdAt: "",
  updatedAt: "",
};
function list(...items: (typeof workspace)[]) {
  return {
    data: items,
    pages: { current: 1, total: 1, perPage: 20, items: items.length },
  };
}
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status });
const signedOut = () => json({ error: "Please sign in to continue." }, 401);
// Answers /api/auth/me and /api/workspaces; `network` makes every request
// fail as if the API were down.
function stubApi({
  me = () => json(session),
  workspaces = () => json(list(workspace)),
  network = false,
}: {
  me?: () => Response;
  workspaces?: () => Response;
  network?: boolean;
}) {
  const fetch = vi.fn(async (url: string) => {
    if (network) throw new TypeError("Failed to fetch");
    return url.startsWith("/api/auth/me") ? me() : workspaces();
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
const request = (path: string) => new Request(`http://localhost${path}`);
// The Location of a redirect a loader threw.
async function redirectOf(load: Promise<unknown>): Promise<string | null> {
  try {
    await load;
    return null;
  } catch (thrown) {
    if (thrown instanceof Response) return thrown.headers.get("Location");
    throw thrown;
  }
}
describe("signed-in layout loader", () => {
  it("loads the session and the first page of workspaces", async () => {
    stubApi({});
    const loaded = await signedInLoader(request("/app/settings"));
    expect(loaded).toEqual({ session, workspaces: list(workspace) });
  });
  it("sends an expired session to login, returning to this page", async () => {
    stubApi({ me: signedOut, workspaces: signedOut });
    const path = "/app/wsp_2yEHYLwiwPVhLW/projects?page=2";
    expect(await redirectOf(signedInLoader(request(path)))).toBe(
      `/auth/login?next=${encodeURIComponent(path)}`,
    );
  });
  it("sends an account without a workspace to onboarding", async () => {
    stubApi({ workspaces: () => json(list()) });
    expect(await redirectOf(signedInLoader(request("/app/settings")))).toBe(
      "/app/onboarding",
    );
  });
  it("reports an unreachable API instead of redirecting", async () => {
    stubApi({ network: true });
    expect(await signedInLoader(request("/app/settings"))).toEqual({
      unavailable: true,
    });
  });
});
describe("guest loader", () => {
  it("renders the form for signed-out visitors", async () => {
    stubApi({ me: signedOut });
    expect(await guestLoader(request("/auth/login"))).toBeNull();
  });
  it("still renders the form when the API is down", async () => {
    stubApi({ network: true });
    expect(await guestLoader(request("/auth/login"))).toBeNull();
  });
  it("sends signed-in visitors on to an allowed ?next= or /app", async () => {
    stubApi({});
    const next = encodeURIComponent("/app/settings");
    expect(
      await redirectOf(guestLoader(request(`/auth/login?next=${next}`))),
    ).toBe("/app/settings");
    const outside = encodeURIComponent("https://evil.example/");
    expect(
      await redirectOf(guestLoader(request(`/auth/login?next=${outside}`))),
    ).toBe("/app");
  });
});
describe("/app loader", () => {
  it("opens the newest workspace, or onboarding without one", async () => {
    stubApi({});
    expect(await redirectOf(workspaceHomeLoader(request("/app")))).toBe(
      "/app/wsp_2yEHYLwiwPVhLW/dashboard",
    );
    stubApi({ workspaces: () => json(list()) });
    expect(await redirectOf(workspaceHomeLoader(request("/app")))).toBe(
      "/app/onboarding",
    );
  });
  it("sends an expired session to login", async () => {
    stubApi({ workspaces: signedOut });
    expect(await redirectOf(workspaceHomeLoader(request("/app")))).toBe(
      "/auth/login?next=%2Fapp",
    );
  });
});
describe("onboarding loader", () => {
  it("lets an account without a workspace create one", async () => {
    stubApi({ workspaces: () => json(list()) });
    expect(await onboardingLoader()).toEqual({ session });
  });
  it("sends accounts that have a workspace on to /app", async () => {
    stubApi({});
    expect(await redirectOf(onboardingLoader())).toBe("/app");
  });
});
