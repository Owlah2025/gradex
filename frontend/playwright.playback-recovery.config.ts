import { defineConfig } from "@playwright/test";
import base from "./playwright.config";

// The protected capability lasts trusted duration + this short grace period.
// Only this local recovery lane changes timings; authorization remains real.
process.env.PLAYBACK_URL_EXPIRY = "2s";

export default defineConfig({
  ...base,
  testIgnore: [],
  testMatch: "**/s5-playback-recovery.spec.ts",
  timeout: 120_000,
  projects: base.projects?.filter((project) => project.name === "chromium"),
});
