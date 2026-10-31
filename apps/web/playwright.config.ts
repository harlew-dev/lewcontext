import { defineConfig, devices } from "@playwright/test";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { parseEnv } from "node:util";

// The repository root, where .env.testing and the Makefile are.
const root = fileURLToPath(new URL("../..", import.meta.url));

// Every setting in .env.testing replaces the environment's, however
// Playwright is started, so a run never uses .env's values. Only the run's
// E2E_PORT can come from the caller (make test-e2e E2E_PORT=…). The server's
// address and its database (lewcontext_test_<port>) follow from it, as
// scripts/serve-test.sh works them out. Mail is read from Mailpit
// (MAILPIT_URL), which every run shares.
function loadTestEnv(): string {
  const callerPort = process.env.E2E_PORT;
  const settings = parseEnv(
    readFileSync(path.join(root, ".env.testing"), "utf8"),
  );
  Object.assign(process.env, settings);
  const port = callerPort || process.env.E2E_PORT || "3100";
  if (!/^\d+$/.test(port))
    throw new Error(
      `E2E_PORT=${port}: give a port, or run make test-e2e E2E_PORT=auto to pick one`,
    );
  const database = new URL(process.env.DATABASE_URL!);
  database.pathname = `${database.pathname}_${port}`;
  process.env.E2E_PORT = port;
  process.env.APP_ORIGIN = `http://localhost:${port}`;
  process.env.DATABASE_URL = database.toString();
  return process.env.APP_ORIGIN;
}

const origin = loadTestEnv();

export default defineConfig({
  testDir: "./tests/e2e",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 90000,
  expect: { timeout: 15000 },
  use: {
    baseURL: origin,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  // Builds and starts the server for this run. It never reuses one already
  // on the port, which may be another run's, unless E2E_REUSE=1 asks to.
  webServer: {
    command: "make serve-test",
    cwd: root,
    url: `${origin}/auth/login`,
    reuseExistingServer: process.env.E2E_REUSE === "1",
    timeout: 180000,
    stdout: "pipe",
    stderr: "pipe",
  },
  projects: [
    {
      name: "chromium",
      use: {
        ...devices["Desktop Chrome"],
        launchOptions: {
          executablePath: process.env.CHROMIUM_PATH || "/usr/bin/chromium",
          args: ["--no-sandbox"],
        },
      },
    },
  ],
  reporter: "list",
});
