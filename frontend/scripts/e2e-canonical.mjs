#!/usr/bin/env node
/**
 * The canonical whole-suite E2E entrypoint (SY-07).
 *
 * WHY THIS EXISTS
 *   The canonical suite contains two runtime-mode classes, not one. Almost every spec exercises the
 *   development harness the Playwright config is built around. `s5-playback-performance` is a claim
 *   about the *built* application and asserts that precondition itself, so under a single
 *   `npx playwright test` it failed on its first viewport and took the remaining three with it —
 *   a red result produced entirely by launching the wrong frontend mode, not by the product.
 *
 *   `playwright.config.ts` already knew how to run either mode. What was missing was one supported
 *   path that runs *both*, in the mode each contract requires, and returns a single verdict. That
 *   orchestration is this file.
 *
 * WHAT IT DOES
 *   1. Pre-flight: refuses to start if a previous E2E run still owns live processes. It never kills
 *      anything — a Gradex stack it does not own (s12, manual acceptance, a developer server) is not
 *      this script's to terminate, so it reports and exits instead.
 *   2. Builds the frontend for production from the current worktree, so the production lane can
 *      never measure a stale `.next`.
 *   3. Runs the production lane, then the development lane, sequentially. Each lane is a full
 *      Playwright invocation with its own `globalSetup`/`globalTeardown`, so each owns and disposes
 *      of its own database, Go API, worker, media server and frontend.
 *   4. Aggregates both lanes into one summary and exits non-zero if either failed.
 *
 * ORDER
 *   Production first, on purpose. `next dev` writes into the same `.next` directory the production
 *   build produces, so building and then running the development lane first would leave the
 *   production lane measuring a directory the dev server had since rewritten.
 *
 * OWNERSHIP
 *   This script owns exactly one child process at a time — the Playwright run — and forwards
 *   termination signals to it rather than killing by name, so Playwright's own teardown disposes of
 *   the per-run database, API, worker, media server and frontend even on interrupt or failure.
 */

import { spawn } from "child_process";
import fs from "fs";
import path from "path";

const FRONTEND_DIR = path.resolve(import.meta.dirname, "..");
const RESULT_DIR = path.join(FRONTEND_DIR, "playwright-report", "canonical");
const E2E_TMP_DIR = process.env.GRADEX_E2E_TMP_DIR || "/var/tmp";
const RUN_STATE_FILE_PATH = path.join(E2E_TMP_DIR, "gradex-s5-e2e-run-state.json");

const skipBuild = process.argv.includes("--skip-build");

/**
 * The lanes. `mode` is the only difference in application runtime; `playwright.config.ts` derives
 * which specs each lane discovers from that same value, so the classification lives in one place
 * and this runner does not restate it as a file list.
 */
const LANES = [
  {
    id: "production",
    title: "Production lane (built frontend)",
    env: { GRADEX_E2E_FRONTEND_MODE: "production" },
  },
  {
    id: "development",
    title: "Development lane (next dev)",
    env: {},
  },
];

function log(message) {
  console.log(`[e2e:canonical] ${message}`);
}

/** Runs a command in the frontend directory, streaming its output, and resolves with its exit code. */
function run(command, args, extraEnv = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd: FRONTEND_DIR,
      env: { ...process.env, ...extraEnv },
      stdio: "inherit",
    });

    // Forward termination rather than killing anything ourselves, so Playwright's globalTeardown
    // runs and the run-owned database, API, worker and servers are disposed of.
    const forward = (signal) => () => {
      if (!child.killed) child.kill(signal);
    };
    const onInt = forward("SIGINT");
    const onTerm = forward("SIGTERM");
    process.on("SIGINT", onInt);
    process.on("SIGTERM", onTerm);

    child.on("error", (error) => {
      process.off("SIGINT", onInt);
      process.off("SIGTERM", onTerm);
      reject(error);
    });
    child.on("exit", (code, signal) => {
      process.off("SIGINT", onInt);
      process.off("SIGTERM", onTerm);
      resolve(signal ? 130 : (code ?? 1));
    });
  });
}

/**
 * SY-07 is a claim about starting from a clean state, so a leftover run is a hard stop rather than
 * something to clean up implicitly. Only processes this harness recorded as its own are inspected;
 * no pattern ever matches another Gradex stack.
 */
function assertCleanStart() {
  if (!fs.existsSync(RUN_STATE_FILE_PATH)) {
    log(`clean start: no previous run state at ${RUN_STATE_FILE_PATH}`);
    return;
  }

  let state;
  try {
    state = JSON.parse(fs.readFileSync(RUN_STATE_FILE_PATH, "utf-8"));
  } catch {
    log(`previous run state at ${RUN_STATE_FILE_PATH} is unreadable; Playwright's setup will reclaim it`);
    return;
  }

  const live = [];
  for (const [label, pid] of [
    ["API", state.pid],
    ["worker", state.workerPid],
  ]) {
    if (!pid) continue;
    try {
      process.kill(pid, 0);
      live.push(`${label} pid ${pid}`);
    } catch {
      /* not running */
    }
  }

  if (live.length > 0) {
    console.error(
      `[e2e:canonical] Refusing to start: run ${state.runId} still owns ${live.join(", ")}.\n` +
        `[e2e:canonical] This script never terminates processes it did not start. Stop that run, ` +
        `then delete ${RUN_STATE_FILE_PATH}.`,
    );
    process.exit(1);
  }

  log(`previous run ${state.runId} left state behind but owns no live process; Playwright will reclaim it`);
}

/** Reads a lane's JSON report into the counts the summary is built from. */
function readLaneResult(jsonPath) {
  if (!fs.existsSync(jsonPath)) return null;
  let report;
  try {
    report = JSON.parse(fs.readFileSync(jsonPath, "utf-8"));
  } catch {
    return null;
  }

  const stats = report.stats ?? {};
  const skippedTitles = [];
  const walk = (suite, trail) => {
    const here = suite.title ? [...trail, suite.title] : trail;
    for (const spec of suite.specs ?? []) {
      for (const testCase of spec.tests ?? []) {
        // `status` is the test's outcome across retries; "skipped" covers both an explicit skip and
        // a test Playwright never reached because an earlier hook failed.
        if (testCase.status === "skipped") {
          skippedTitles.push([...here, spec.title].filter(Boolean).join(" › "));
        }
      }
    }
    for (const child of suite.suites ?? []) walk(child, here);
  };
  for (const suite of report.suites ?? []) walk(suite, []);

  return {
    passed: stats.expected ?? 0,
    failed: stats.unexpected ?? 0,
    flaky: stats.flaky ?? 0,
    skipped: stats.skipped ?? 0,
    durationMs: Math.round(stats.duration ?? 0),
    skippedTitles,
  };
}

async function main() {
  fs.mkdirSync(RESULT_DIR, { recursive: true });

  assertCleanStart();

  if (skipBuild) {
    // Only for iterating on the harness itself. A build that does not correspond to the current
    // worktree makes the production lane's measurement meaningless, so say so loudly.
    log("WARNING: --skip-build — the production lane will measure whatever .next already contains");
    const buildId = path.join(FRONTEND_DIR, ".next", "BUILD_ID");
    if (!fs.existsSync(buildId)) {
      console.error(`[e2e:canonical] --skip-build was given but ${buildId} does not exist.`);
      process.exit(1);
    }
  } else {
    log("building the frontend for production from the current worktree...");
    const buildCode = await run("npm", ["run", "build"]);
    if (buildCode !== 0) {
      console.error(`[e2e:canonical] production build failed (exit ${buildCode}). No lane was run.`);
      process.exit(buildCode);
    }
    log("production build complete");
  }

  const results = [];
  for (const lane of LANES) {
    const jsonPath = path.join(RESULT_DIR, `${lane.id}.json`);
    try {
      fs.unlinkSync(jsonPath);
    } catch {
      /* first run */
    }

    log(`── ${lane.title} ──`);
    const exitCode = await run("npx", ["playwright", "test"], {
      ...lane.env,
      GRADEX_PLAYWRIGHT_HTML_DIR: path.join("playwright-report", lane.id),
      GRADEX_PLAYWRIGHT_OUTPUT_DIR: path.join("test-results", lane.id),
      GRADEX_PLAYWRIGHT_JSON_FILE: jsonPath,
    });

    const counts = readLaneResult(jsonPath);
    results.push({ lane, exitCode, counts });
    if (!counts) {
      console.error(`[e2e:canonical] ${lane.id} lane produced no JSON report at ${jsonPath}`);
    }
  }

  const total = { passed: 0, failed: 0, flaky: 0, skipped: 0 };
  console.log("\n════════ canonical E2E result ════════");
  for (const { lane, exitCode, counts } of results) {
    if (!counts) {
      console.log(`${lane.id.padEnd(12)} NO REPORT (exit ${exitCode})`);
      continue;
    }
    total.passed += counts.passed;
    total.failed += counts.failed;
    total.flaky += counts.flaky;
    total.skipped += counts.skipped;
    console.log(
      `${lane.id.padEnd(12)} passed ${counts.passed}  failed ${counts.failed}  ` +
        `flaky ${counts.flaky}  skipped ${counts.skipped}  (${(counts.durationMs / 1000).toFixed(1)}s, exit ${exitCode})`,
    );
    for (const title of counts.skippedTitles) console.log(`${"".padEnd(12)}   skipped: ${title}`);
  }
  console.log(
    `${"aggregate".padEnd(12)} passed ${total.passed}  failed ${total.failed}  ` +
      `flaky ${total.flaky}  skipped ${total.skipped}`,
  );
  console.log(`reports: ${RESULT_DIR} and playwright-report/{production,development}`);
  console.log("══════════════════════════════════════\n");

  const summary = {
    schema: "gradex.sy07.canonical-e2e/1",
    generated_at_utc: new Date().toISOString(),
    build: skipBuild ? "reused (--skip-build)" : "next build from the current worktree",
    lanes: results.map(({ lane, exitCode, counts }) => ({ lane: lane.id, exit_code: exitCode, ...(counts ?? {}) })),
    aggregate: total,
  };
  fs.writeFileSync(path.join(RESULT_DIR, "summary.json"), `${JSON.stringify(summary, null, 2)}\n`);

  // Non-zero if any lane failed, produced no report, or reported a failure. No swallowed errors.
  const failed = results.some(({ exitCode, counts }) => exitCode !== 0 || !counts || counts.failed > 0);
  process.exit(failed ? 1 : 0);
}

main().catch((error) => {
  console.error("[e2e:canonical]", error);
  process.exit(1);
});
