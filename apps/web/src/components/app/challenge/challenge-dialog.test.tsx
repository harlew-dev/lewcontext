import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useEffect, useState } from "react";
import { ApiError } from "@/lib/api/http";
import { ChallengeProvider, useChallenge } from "@/lib/challenge/challenge";
import * as providers from "@/lib/challenge/providers";
// A stub of window.turnstile: the test says what the widget does.
type Options = Record<string, (value?: string) => void>;
let widget: Options | undefined;
const turnstile = {
  render: vi.fn((_el: HTMLElement, options: Options) => {
    widget = options;
    return "w1";
  }),
  remove: vi.fn(),
};
afterEach(() => {
  vi.restoreAllMocks();
  widget = undefined;
  delete window.turnstile;
});
// A page asking for challenges, showing how each ended.
function Asker({ provider = "turnstile", count = 1 }) {
  const solve = useChallenge()!;
  const [results, setResults] = useState<string[]>([]);
  useEffect(() => {
    for (let i = 0; i < count; i++)
      solve({ provider, siteKey: "site", action: "login" }).then(
        (token) => setResults((r) => [...r, `token ${token}`]),
        (e: ApiError) => setResults((r) => [...r, `${e.status} ${e.message}`]),
      );
  }, [solve, provider, count]);
  return <p data-testid="results">{results.join(", ")}</p>;
}
function renderAsker(props: { provider?: string; count?: number } = {}) {
  render(
    <ChallengeProvider>
      <Asker {...props} />
    </ChallengeProvider>,
  );
}
describe("ChallengeDialog", () => {
  it("checks, then resolves with the widget's token", async () => {
    window.turnstile = turnstile;
    vi.spyOn(providers, "loadScript").mockResolvedValue();
    renderAsker();
    expect(
      await screen.findByRole("dialog", { name: "Quick security check" }),
    ).toBeInTheDocument();
    await waitFor(() => expect(widget).toBeDefined());
    expect(screen.getByRole("status")).toHaveTextContent("Checking…");
    expect(turnstile.render.mock.calls[0][1]).toMatchObject({
      sitekey: "site",
      action: "login",
      appearance: "interaction-only",
    });
    act(() => widget!.callback("abc"));
    await waitFor(() =>
      expect(screen.getByTestId("results")).toHaveTextContent("token abc"),
    );
  });
  it("says when the script can't load, and tries again", async () => {
    const load = vi
      .spyOn(providers, "loadScript")
      .mockRejectedValueOnce(new Error("blocked"));
    renderAsker();
    expect(
      await screen.findByText(/Couldn’t load the check/),
    ).toBeInTheDocument();
    window.turnstile = turnstile;
    load.mockResolvedValue();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(widget).toBeDefined());
  });
  it("shows the widget's error with Retry, and resets on expiry", async () => {
    window.turnstile = turnstile;
    vi.spyOn(providers, "loadScript").mockResolvedValue();
    renderAsker();
    await waitFor(() => expect(widget).toBeDefined());
    act(() => widget!["error-callback"]());
    expect(
      await screen.findByText("The check couldn't be completed."),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(turnstile.render).toHaveBeenCalledTimes(2));
    act(() => widget!["expired-callback"]());
    expect(screen.getByRole("status")).toHaveTextContent("Checking…");
  });
  it("cancels as a 428 the form can show", async () => {
    window.turnstile = turnstile;
    vi.spyOn(providers, "loadScript").mockResolvedValue();
    renderAsker();
    fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));
    await waitFor(() =>
      expect(screen.getByTestId("results")).toHaveTextContent(
        "428 Check cancelled.",
      ),
    );
  });
  it("says an unknown provider isn't supported", async () => {
    const load = vi.spyOn(providers, "loadScript");
    renderAsker({ provider: "hcaptcha" });
    expect(
      await screen.findByText(/This check isn’t supported/),
    ).toBeInTheDocument();
    expect(load).not.toHaveBeenCalled();
  });
  it("queues two challenges, one after the other", async () => {
    window.turnstile = turnstile;
    vi.spyOn(providers, "loadScript").mockResolvedValue();
    renderAsker({ count: 2 });
    await waitFor(() => expect(widget).toBeDefined());
    act(() => widget!.callback("one"));
    await waitFor(() => expect(turnstile.render).toHaveBeenCalledTimes(2));
    act(() => widget!.callback("two"));
    await waitFor(() =>
      expect(screen.getByTestId("results")).toHaveTextContent(
        "token one, token two",
      ),
    );
  });
});
describe("ChallengeProvider", () => {
  it("settles a challenge once, and never drops the next", async () => {
    window.turnstile = turnstile;
    vi.spyOn(providers, "loadScript").mockResolvedValue();
    renderAsker({ count: 2 });
    await waitFor(() => expect(widget).toBeDefined());
    const first = widget!;
    // The token arrives, then Cancel and the token again before the
    // dialog moves on: only the first counts.
    act(() => {
      first.callback("one");
      fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
      first.callback("again");
    });
    await waitFor(() => expect(turnstile.render).toHaveBeenCalledTimes(2));
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    act(() => widget!.callback("two"));
    await waitFor(() =>
      expect(screen.getByTestId("results")).toHaveTextContent(
        "token one, token two",
      ),
    );
  });
  it("supports only the providers it has, not an object's own keys", async () => {
    const load = vi.spyOn(providers, "loadScript");
    renderAsker({ provider: "constructor" });
    expect(
      await screen.findByText(/This check isn’t supported/),
    ).toBeInTheDocument();
    expect(load).not.toHaveBeenCalled();
  });
});
