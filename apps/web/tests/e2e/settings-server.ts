import { expect } from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import { fileURLToPath } from "node:url";
// A second server for specs that need other settings than the main one's:
// the same bin/api on the run's database (scripts/serve-test.sh,
// E2E_SHARED_DATABASE=1), on the port e2e-preflight.sh reserved for it
// (E2E_SETTINGS_PORT). Both share the run's APP_KEY. Specs run one at a
// time (workers: 1), so they take turns with the port.
export const settingsPort = process.env.E2E_SETTINGS_PORT;
const root = fileURLToPath(new URL("../../../..", import.meta.url));
let server: ChildProcess | undefined;
// Starts it with `settings` over .env.testing's and waits until it
// answers; returns its origin.
export async function startServer(settings: Record<string, string>) {
  server = spawn("scripts/serve-test.sh", [], {
    cwd: root,
    env: {
      ...process.env,
      ...settings,
      E2E_PORT: settingsPort,
      E2E_LOCK_HELD: "1",
      E2E_SHARED_DATABASE: "1",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  for (const stream of [server.stdout, server.stderr])
    stream?.on("data", (line) =>
      process.stdout.write(`[settings server] ${line}`),
    );
  const origin = `http://localhost:${settingsPort}`;
  await expect
    .poll(
      async () => {
        try {
          return (await fetch(`${origin}/api/health`)).status;
        } catch {
          return 0;
        }
      },
      { message: `the settings server on ${origin}`, timeout: 30000 },
    )
    .toBe(200);
  return origin;
}
// Stops it, if it's running, and waits until it has exited.
export async function stopServer() {
  const running = server;
  if (!running || running.exitCode !== null) return;
  const exited = new Promise((done) => running.once("exit", done));
  running.kill("SIGTERM");
  await exited;
  server = undefined;
}
