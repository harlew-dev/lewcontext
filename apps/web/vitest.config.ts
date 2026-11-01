import { defineConfig } from "vitest/config";
import { fileURLToPath } from "node:url";
export default defineConfig({
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  test: {
    environment: "jsdom",
    setupFiles: ["./tests/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
    // Above the 5s waits in tests/setup.ts, so a slow machine fails on the
    // thing waited for, not on the clock.
    testTimeout: 15_000,
  },
});
