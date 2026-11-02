import { fireEvent, render, screen } from "@testing-library/react";
import useSWR, { SWRConfig, type Cache } from "swr";
import useSWRInfinite from "swr/infinite";
import { describe, expect, it } from "vitest";
import { useClearCache } from "./use-clear-cache";
// A plain key and an infinite list, both cached.
function Cached() {
  const one = useSWR(["/api/auth/me", "usr_1"], () => "me");
  const pages = useSWRInfinite(
    (index) => [`/api/workspaces?page=${index + 1}`, "usr_1"],
    () => "page",
  );
  const clear = useClearCache();
  return (
    <button onClick={clear}>
      {one.data && pages.data ? "Loaded" : "Loading"}
    </button>
  );
}
describe("useClearCache", () => {
  it("forgets every cached response, infinite lists included", async () => {
    const cache: Cache = new Map();
    render(
      <SWRConfig value={{ provider: () => cache }}>
        <Cached />
      </SWRConfig>,
    );
    await screen.findByText("Loaded");
    expect(Array.from(cache.keys()).some((k) => k.startsWith("$inf$"))).toBe(
      true,
    );
    fireEvent.click(screen.getByRole("button"));
    expect(Array.from(cache.keys())).toEqual([]);
  });
});
