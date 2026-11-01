import { act, render, renderHook, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ApiError } from "@/lib/api";
import { useAction } from "@/lib/use-action";
import { ErrorNotice } from "./feedback";
// What useAction keeps after an action failed with error.
async function failedWith(error: unknown) {
  const { result } = renderHook(() => useAction(() => Promise.reject(error)));
  await act(async () => {
    await result.current.run();
  });
  return result.current.error;
}
describe("error notice", () => {
  it("shows a 500's reference under its message", async () => {
    const error = await failedWith(
      new ApiError("Something went wrong. Please try again.", 500, undefined, {
        error: "Something went wrong. Please try again.",
        requestId: "req_2yEHYLwiwPVhLW",
      }),
    );
    render(<ErrorNotice error={error} />);
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Something went wrong. Please try again.Reference: req_2yEHYLwiwPVhLW",
    );
  });
  it("shows no reference without one, or for one in another shape", async () => {
    const plain = await failedWith(new ApiError("Name is taken.", 422));
    const { unmount } = render(<ErrorNotice error={plain} />);
    expect(screen.getByRole("alert")).toHaveTextContent("Name is taken.");
    expect(screen.queryByText(/Reference/)).toBeNull();
    unmount();
    const odd = await failedWith(
      new ApiError("Oops", 500, undefined, {
        requestId: "<b>not an id</b>",
      }),
    );
    render(<ErrorNotice error={odd} />);
    expect(screen.queryByText(/Reference/)).toBeNull();
  });
});
