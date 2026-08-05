import { defineConfig, devices } from "@playwright/test";
import { apiOrigin, frontendOrigin, runPort, API_PORT_ENV, FRONTEND_PORT_ENV } from "./src/lib/api/e2e-ports";

// Allocated once in the runner process and published through the environment, so worker
// processes re-evaluating this config reuse the same ports instead of allocating new ones.
// Nothing here may assume port 3000: a developer server on 3000 is unrelated to this run.
const frontendPort = runPort(FRONTEND_PORT_ENV);
const apiPort = runPort(API_PORT_ENV);

export default defineConfig({
  testDir: "./e2e",
  testIgnore: "**/s3-public-catalogue-performance.spec.ts",
  fullyParallel: false,
  globalSetup: "./e2e/global-setup.ts",
  globalTeardown: "./e2e/global-teardown.ts",
  // The HTML report is the retained artifact of record, so its destination is overridable: the CI
  // evidence job writes it straight into the directory it uploads rather than into the ignored
  // local default. Unset — which is every existing local run — behaves exactly as before.
  reporter: [
    ["list"],
    ["html", { outputFolder: process.env.GRADEX_PLAYWRIGHT_HTML_DIR || "playwright-report", open: "never" }],
  ],
  use: {
    baseURL: frontendOrigin(),
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: `npm run dev -- --port ${frontendPort} --hostname 127.0.0.1`,
    url: frontendOrigin(),
    // Never adopt a server this run did not start, on any port.
    reuseExistingServer: false,
    env: {
      GRADEX_API_ORIGIN: apiOrigin(),
      [FRONTEND_PORT_ENV]: String(frontendPort),
      [API_PORT_ENV]: String(apiPort),
    },
  },
});
