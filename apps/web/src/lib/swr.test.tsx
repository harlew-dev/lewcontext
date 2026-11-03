import type { ReactNode } from "react";
import {
  fireEvent,
  render,
  renderHook,
  screen,
  waitFor,
} from "@testing-library/react";
import { SWRConfig } from "swr";
import { describe, expect, it, vi } from "vitest";
import { ErrorNotice } from "@/components/ui/feedback";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import { paths, useApi } from "./api";
import { SessionProvider } from "./session";
import { swrConfig } from "./swr";
import { useAction } from "./use-action";
import { useResource } from "./use-resource";
vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  useNavigate: () => vi.fn(),
  useLocation: () => ({ pathname: "/app", search: "" }),
}));
const tooMany = "Too many requests. Try again in 12 minutes.";
const ws = "wsp_00000000000001";
// The app's SWR settings, with retries a few milliseconds apart.
function Providers({ children }: { children: ReactNode }) {
  return (
    <SWRConfig
      value={{
        ...swrConfig,
        provider: () => new Map(),
        dedupingInterval: 0,
        errorRetryInterval: 5,
      }}
    >
      <SessionProvider session={testSession()}>{children}</SessionProvider>
    </SWRConfig>
  );
}
const wait = (ms: number) => new Promise((done) => setTimeout(done, ms));
describe("swrConfig", () => {
  it("doesn't retry a read the API refused, and shows its message", async () => {
    const api = stubApi({ "*": { status: 429, body: { error: tooMany } } });
    const { result } = renderHook(
      () => useResource(paths.projects(ws).index()),
      { wrapper: Providers },
    );
    await waitFor(() => expect(result.current.error).toBe(tooMany));
    await wait(100);
    expect(api.sent()).toHaveLength(1);
    // Retry asks again.
    result.current.refresh();
    await waitFor(() => expect(api.sent()).toHaveLength(2));
  });
  it("retries a read that failed on the server twice", async () => {
    const api = stubApi({ "*": { status: 500, body: { error: "Down." } } });
    renderHook(() => useResource(paths.projects(ws).index()), {
      wrapper: Providers,
    });
    await waitFor(() => expect(api.sent()).toHaveLength(3));
    await wait(100);
    expect(api.sent()).toHaveLength(3);
  });
  it("shows a refused mutation's message", async () => {
    stubApi({ "*": { status: 429, body: { error: tooMany } } });
    function CreateWorkspace() {
      const api = useApi();
      const create = useAction(() => api.workspaces().store({ name: "Acme" }));
      return (
        <>
          <ErrorNotice error={create.error} />
          <button onClick={() => void create.run()}>Create</button>
        </>
      );
    }
    render(<CreateWorkspace />, { wrapper: Providers });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    expect(await screen.findByText(tooMany)).toBeInTheDocument();
  });
});
