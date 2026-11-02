import { act, render, renderHook, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ErrorNotice } from "@/components/ui/feedback";
import { ApiError } from "./api";
import { useAction } from "./use-action";
// An action that waits until the test settles it.
function deferred<T>() {
  let resolve: (value: T) => void = () => {};
  let reject: (error: unknown) => void = () => {};
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
describe("useAction()", () => {
  it("is busy while the action runs and resolves with its value", async () => {
    const pending = deferred<string>();
    const { result } = renderHook(() => useAction(() => pending.promise));
    let run: Promise<unknown> = Promise.resolve();
    act(() => {
      run = result.current.run();
    });
    expect(result.current.busy).toBe(true);
    await act(async () => {
      pending.resolve("saved");
      await run;
    });
    expect(result.current.busy).toBe(false);
    await expect(run).resolves.toEqual({ ok: true, value: "saved" });
  });
  it("shows loading only once the action has run for 200 ms", async () => {
    vi.useFakeTimers();
    try {
      const pending = deferred<void>();
      const { result } = renderHook(() => useAction(() => pending.promise));
      let run: Promise<unknown> = Promise.resolve();
      act(() => {
        run = result.current.run();
      });
      expect(result.current.loading).toBe(false);
      act(() => {
        vi.advanceTimersByTime(200);
      });
      expect(result.current.loading).toBe(true);
      await act(async () => {
        pending.resolve();
        await run;
      });
      expect(result.current.loading).toBe(true);
      act(() => {
        vi.advanceTimersByTime(400);
      });
      expect(result.current.loading).toBe(false);
    } finally {
      vi.useRealTimers();
    }
  });
  it("ignores a second run while one is in flight", async () => {
    const pending = deferred<void>();
    let calls = 0;
    const { result } = renderHook(() =>
      useAction(() => {
        calls += 1;
        return pending.promise;
      }),
    );
    let first: Promise<unknown> = Promise.resolve();
    let second: Promise<unknown> = Promise.resolve();
    // Both in the same tick, before React re-renders: a double click.
    act(() => {
      first = result.current.run();
      second = result.current.run();
    });
    await expect(second).resolves.toEqual({ ok: false });
    await act(async () => {
      pending.resolve();
      await first;
    });
    expect(calls).toBe(1);
  });
  it("keeps the failure for the notice, and clears it on success", async () => {
    let fail = true;
    const { result } = renderHook(() =>
      useAction(async () => {
        if (fail) throw new ApiError("Too many attempts.", 429);
      }),
    );
    await act(async () => {
      await expect(result.current.run()).resolves.toEqual({ ok: false });
    });
    expect(result.current.error.message).toBe("Too many attempts.");
    expect(result.current.busy).toBe(false);
    fail = false;
    await act(async () => {
      await result.current.run();
    });
    expect(result.current.error.message).toBe("");
  });
  it("leaves failures that onError handles out of the error", async () => {
    const handled: unknown[] = [];
    const { result } = renderHook(() =>
      useAction(
        async () => {
          throw new ApiError("Current password is incorrect.", 422, "password");
        },
        {
          onError: (e) => {
            handled.push(e);
            return e instanceof ApiError && e.field !== undefined;
          },
        },
      ),
    );
    await act(async () => {
      await result.current.run();
    });
    expect(handled).toHaveLength(1);
    expect(result.current.error.message).toBe("");
  });
});
describe("<ErrorNotice>", () => {
  it("shows the failure as an alert, and nothing without one", () => {
    const { rerender } = render(
      <ErrorNotice error={{ message: "", key: 0 }} />,
    );
    expect(screen.queryByRole("alert")).toBeNull();
    rerender(<ErrorNotice error={{ message: "Unable to connect.", key: 1 }} />);
    expect(screen.getByRole("alert")).toHaveTextContent("Unable to connect.");
  });
  it("announces the same message again after another failure", () => {
    const { rerender } = render(
      <ErrorNotice error={{ message: "Unable to connect.", key: 1 }} />,
    );
    const first = screen.getByRole("alert");
    rerender(<ErrorNotice error={{ message: "Unable to connect.", key: 2 }} />);
    expect(screen.getByRole("alert")).not.toBe(first);
  });
});
