import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  testMatch: "s3-public-catalogue-performance.spec.ts",
  reporter: "list",
  use: {
    baseURL: "http://127.0.0.1:3001",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    ...devices["Desktop Chrome"],
  },
  webServer: {
    command: "npm run start -- --hostname 127.0.0.1 --port 3001",
    url: "http://127.0.0.1:3001",
    reuseExistingServer: false,
  },
});
