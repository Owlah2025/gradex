import fs from "fs";
import path from "path";
import { defineConfig, devices } from "@playwright/test";
import { apiOrigin, frontendOrigin, runPort, API_PORT_ENV, FRONTEND_PORT_ENV } from "./src/lib/api/e2e-ports";

// Allocated once in the runner process and published through the environment, so worker
// processes re-evaluating this config reuse the same ports instead of allocating new ones.
// Nothing here may assume port 3000: a developer server on 3000 is unrelated to this run.
const externalDeployment = Boolean(process.env.GRADEX_E2E_EXTERNAL_ORIGIN);
const frontendPort = externalDeployment ? 0 : runPort(FRONTEND_PORT_ENV);
const apiPort = externalDeployment ? 0 : runPort(API_PORT_ENV);

/**
 * T076 measures SC-001 against the **built** frontend, not `next dev`.
 *
 * SC-001 is a claim about the shipped application. Measured against the development server the figure
 * is dominated by on-demand compilation and unoptimized, unbundled assets: under the deterministic
 * 4 Mbps profile roughly 86% of the elapsed time passed before the Play control even appeared, and
 * media start itself was ~1.2 s. That measures the dev server, not the product.
 *
 * Production mode is opt-in and affects only an invocation that explicitly asks for it, so T075 and
 * every existing suite keep their current `next dev` behaviour unchanged. `next build` must already
 * have produced output before Playwright starts — the build is not part of the measured interval, and
 * no browser request is issued while it runs. Missing build output fails closed rather than silently
 * falling back to dev mode.
 *
 * The port stays dynamically allocated. The suite deliberately stopped assuming 3000 so a developer
 * server on that port can never be adopted or collided with; forcing 3000 here would reintroduce
 * exactly that hazard. The frontend binds to loopback and the spec records the origin it actually
 * used, rather than asserting a port it does not own.
 */
const productionFrontend = !externalDeployment && process.env.GRADEX_E2E_FRONTEND_MODE === "production";

if (productionFrontend) {
  const buildManifest = path.join(__dirname, ".next", "BUILD_ID");
  if (!fs.existsSync(buildManifest)) {
    throw new Error(
      "GRADEX_E2E_FRONTEND_MODE=production requires a completed production build: " +
        `${buildManifest} is missing. Run \`npm run build\` before invoking Playwright.`,
    );
  }
}

/**
 * In production mode Playwright supervises the run-owned production-origin proxy instead of a bare
 * `next start`.
 *
 * A standalone `next start` is not the deployed topology: the browser client calls relative
 * `/api/v1/...` and `next.config.mjs` deliberately provides rewrites only in development, because the
 * deployed edge fronts the app and the Go API behind one external origin. Served by `next start`
 * alone those calls 404 and the Lesson Player renders "This lesson could not start."
 *
 * The proxy binds the public frontend port this run already allocated — so `baseURL` and every
 * existing origin helper are unchanged — supervises the built server on a separate internal loopback
 * port, and forwards `/api/*` to the run-owned Go API. It exposes the public port only once the
 * internal server is genuinely ready, so `webServer.url` cannot pass early.
 */
const internalNextPort = productionFrontend ? runPort("GRADEX_E2E_INTERNAL_NEXT_PORT") : 0;

const frontendCommand = productionFrontend
  ? "node e2e/production-origin-proxy.mjs"
  : `npm run dev -- --port ${frontendPort} --hostname 127.0.0.1`;

export default defineConfig({
  testDir: "./e2e",
  // The media authoring journey needs real object storage, a running worker,
  // and ffmpeg, so it has its own config and environment.
  testIgnore: ["**/s3-public-catalogue-performance.spec.ts", "**/media-authoring/**"],
  fullyParallel: false,
  globalSetup: externalDeployment ? undefined : "./e2e/global-setup.ts",
  globalTeardown: externalDeployment ? undefined : "./e2e/global-teardown.ts",
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
    launchOptions: process.env.GRADEX_E2E_TLS_SPKI
      ? { args: [`--ignore-certificate-errors-spki-list=${process.env.GRADEX_E2E_TLS_SPKI}`] }
      : undefined,
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: externalDeployment ? undefined : {
    command: frontendCommand,
    url: frontendOrigin(),
    // Never adopt a server this run did not start, on any port.
    reuseExistingServer: false,
    env: {
      GRADEX_API_ORIGIN: apiOrigin(),
      PUBLIC_ORIGIN: frontendOrigin(),
      [FRONTEND_PORT_ENV]: String(frontendPort),
      [API_PORT_ENV]: String(apiPort),
      // Production mode only. Narrowly named and fully specified: the proxy takes no arbitrary
      // command and no per-request target, and validates both upstreams as loopback itself.
      ...(productionFrontend
        ? {
            GRADEX_E2E_FRONTEND_MODE: "production",
            GRADEX_E2E_API_ORIGIN: apiOrigin(),
            GRADEX_E2E_PUBLIC_FRONTEND_PORT: String(frontendPort),
            GRADEX_E2E_INTERNAL_NEXT_PORT: String(internalNextPort),
            GRADEX_E2E_PUBLIC_FRONTEND_ORIGIN: frontendOrigin(),
            GRADEX_E2E_INTERNAL_NEXT_ORIGIN: `http://127.0.0.1:${internalNextPort}`,
          }
        : {}),
    },
  },
});
