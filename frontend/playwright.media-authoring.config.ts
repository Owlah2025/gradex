import { defineConfig, devices } from "@playwright/test";
import { apiOrigin, frontendOrigin, runPort, API_PORT_ENV, FRONTEND_PORT_ENV } from "./src/lib/api/e2e-ports";

/**
 * Real Instructor media authoring journey.
 *
 * Separate from the shared suite because it needs infrastructure the shared
 * suite deliberately does not have: the developer Compose MinIO as real object
 * storage, a running media worker, and ffmpeg. Keeping it separate leaves the
 * S5/S6/S11 evidence environments untouched.
 *
 * Prerequisites: `docker compose up -d` in `backend/` (PostgreSQL, Redis, and
 * a version-enabled MinIO bucket) and ffmpeg on PATH.
 */
const frontendPort = runPort(FRONTEND_PORT_ENV);
const apiPort = runPort(API_PORT_ENV);

export default defineConfig({
  testDir: "./e2e/media-authoring",
  fullyParallel: false,
  // Transcoding a real file is slower than any UI assertion in the suite.
  timeout: 5 * 60 * 1000,
  globalSetup: "./e2e/media-authoring/global-setup.ts",
  globalTeardown: "./e2e/media-authoring/global-teardown.ts",
  reporter: [
    ["list"],
    [
      "html",
      {
        outputFolder: process.env.GRADEX_PLAYWRIGHT_HTML_DIR || "playwright-report-media-authoring",
        open: "never",
      },
    ],
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
    reuseExistingServer: false,
    env: {
      GRADEX_API_ORIGIN: apiOrigin(),
      PUBLIC_ORIGIN: frontendOrigin(),
      [FRONTEND_PORT_ENV]: String(frontendPort),
      [API_PORT_ENV]: String(apiPort),
    },
  },
});
