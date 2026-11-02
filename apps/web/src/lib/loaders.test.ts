import { describe, expect, it } from "vitest";
import { stubApi, type Reply } from "@/test/api";
import { testSession } from "@/test/session";
import { testWorkspace } from "@/test/workspace";
import {
  guestLoader,
  onboardingLoader,
  signedInLoader,
  verifyEmailLoader,
  workspaceHomeLoader,
} from "./loaders";
const session = testSession();
const workspace = testWorkspace();
function list(...items: (typeof workspace)[]) {
  return {
    data: items,
    pages: { current: 1, total: 1, perPage: 20, items: items.length },
  };
}
const signedOut: Reply = {
  status: 401,
  body: { error: "Please sign in to continue." },
};
// Answers /api/auth/me and /api/workspaces; `network` makes every request
// fail as if the API were down.
function stubLoaderApi({
  me = { body: session },
  workspaces = { body: list(workspace) },
  network = false,
}: {
  me?: Reply;
  workspaces?: Reply;
  network?: boolean;
}) {
  if (network) return stubApi({ "*": { network: true } });
  return stubApi({ "GET /api/auth/me": me, "GET /api/workspaces": workspaces });
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
    stubLoaderApi({});
    const loaded = await signedInLoader(request("/app/settings"));
    expect(loaded).toEqual({ session, workspaces: list(workspace) });
  });
  it("sends an expired session to login, returning to this page", async () => {
    stubLoaderApi({ me: signedOut, workspaces: signedOut });
    const path = "/app/wsp_2yEHYLwiwPVhLW/projects?page=2";
    expect(await redirectOf(signedInLoader(request(path)))).toBe(
      `/auth/login?next=${encodeURIComponent(path)}`,
    );
  });
  it("sends an account without a workspace to onboarding", async () => {
    stubLoaderApi({ workspaces: { body: list() } });
    expect(await redirectOf(signedInLoader(request("/app/settings")))).toBe(
      "/app/onboarding",
    );
  });
  it("reports an unreachable API instead of redirecting", async () => {
    stubLoaderApi({ network: true });
    expect(await signedInLoader(request("/app/settings"))).toEqual({
      unavailable: true,
    });
  });
  it("keeps a 500's reference for the outage page", async () => {
    stubLoaderApi({
      me: {
        status: 500,
        body: {
          error: "Something went wrong. Please try again.",
          requestId: "req_2yEHYLwiwPVhLW",
        },
      },
    });
    expect(await signedInLoader(request("/app/settings"))).toEqual({
      unavailable: true,
      requestId: "req_2yEHYLwiwPVhLW",
    });
  });
});
describe("guest loader", () => {
  it("renders the form for signed-out visitors", async () => {
    stubLoaderApi({ me: signedOut });
    expect(await guestLoader(request("/auth/login"))).toBeNull();
  });
  it("still renders the form when the API is down", async () => {
    stubLoaderApi({ network: true });
    expect(await guestLoader(request("/auth/login"))).toBeNull();
  });
  it("sends signed-in visitors on to an allowed ?next= or /app", async () => {
    stubLoaderApi({});
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
    stubLoaderApi({});
    expect(await redirectOf(workspaceHomeLoader(request("/app")))).toBe(
      "/app/wsp_2yEHYLwiwPVhLW/dashboard",
    );
    stubLoaderApi({ workspaces: { body: list() } });
    expect(await redirectOf(workspaceHomeLoader(request("/app")))).toBe(
      "/app/onboarding",
    );
  });
  it("sends an expired session to login", async () => {
    stubLoaderApi({ workspaces: signedOut });
    expect(await redirectOf(workspaceHomeLoader(request("/app")))).toBe(
      "/auth/login?next=%2Fapp",
    );
  });
});
describe("onboarding loader", () => {
  it("lets an account without a workspace create one", async () => {
    stubLoaderApi({ workspaces: { body: list() } });
    expect(await onboardingLoader()).toEqual({ session });
  });
  it("sends accounts that have a workspace on to /app", async () => {
    stubLoaderApi({});
    expect(await redirectOf(onboardingLoader())).toBe("/app");
  });
});

// EMAIL_VERIFICATION_EXISTING=required: the API refuses an unverified
// account everything but its session and verifying.
const mustVerify: Reply = {
  status: 403,
  body: { error: "Verify your email to continue.", verificationRequired: true },
};
describe("an account that must verify its email", () => {
  const unverified = testSession({
    emailVerification: "required",
    user: { emailVerifiedAt: null },
  });
  it("goes to the verify page from every signed-in page, returning after", async () => {
    stubLoaderApi({ me: { body: unverified }, workspaces: mustVerify });
    const path = "/app/wsp_2yEHYLwiwPVhLW/projects";
    expect(await redirectOf(signedInLoader(request(path)))).toBe(
      `/app/verify-email?next=${encodeURIComponent(path)}`,
    );
    expect(await redirectOf(workspaceHomeLoader(request("/app")))).toBe(
      "/app/verify-email",
    );
    expect(await redirectOf(onboardingLoader())).toBe("/app/verify-email");
  });
  it("stays on the verify page only while it must verify", async () => {
    stubLoaderApi({ me: { body: unverified } });
    expect(await verifyEmailLoader(request("/app/verify-email"))).toEqual({
      session: unverified,
    });
    stubLoaderApi({ me: { body: testSession() } });
    const next = encodeURIComponent("/app/settings");
    expect(
      await redirectOf(
        verifyEmailLoader(request(`/app/verify-email?next=${next}`)),
      ),
    ).toBe("/app/settings");
    // Another site isn't a place to go on to.
    expect(
      await redirectOf(
        verifyEmailLoader(
          request("/app/verify-email?next=https%3A%2F%2Fevil.example"),
        ),
      ),
    ).toBe("/app");
    stubLoaderApi({ me: signedOut });
    expect(
      await redirectOf(verifyEmailLoader(request("/app/verify-email"))),
    ).toBe("/auth/login?next=%2Fapp");
  });
});
