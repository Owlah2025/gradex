import { defineConfig } from "@playwright/test";
import path from "path";
import base from "./playwright.config";
import { E2E_TMP_DIR } from "./src/lib/api/e2e-infrastructure";

// The protected capability lasts trusted duration + this short grace period.
// Only this local recovery lane changes timings; authorization remains real.
process.env.PLAYBACK_URL_EXPIRY = "2s";
// Setup and workers evaluate config separately. The run-owned directory and
// existing single-run environment lock give this marker a stable identity.
process.env.GRADEX_E2E_SEGMENT_FAILURE_FILE = path.join(E2E_TMP_DIR, "gradex-playback-segment-failure");

export default defineConfig({
  ...base,
  testIgnore: [],
  testMatch: "**/s5-playback-recovery.spec.ts",
  timeout: 120_000,
  projects: base.projects?.filter((project) => project.name === "chromium"),
});
