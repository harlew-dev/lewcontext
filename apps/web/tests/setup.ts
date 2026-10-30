import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, vi } from "vitest";
// Every test starts from an empty page and the real globals (fetch,
// navigator), whatever the last one rendered or stubbed.
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
