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
 * DEVELOPMENT SHARDS
 *   One `next dev` serving the whole development lane grew from ~2.7 GB to ~5.5 GB RSS over 640
 *   tests. On a workstation that pushed the host into swap exhaustion, and Chromium then failed
 *   unrelated tests with `net::ERR_INSUFFICIENT_RESOURCES` and `Page.captureScreenshot` protocol
 *   errors. The application has no unbounded cache; the growth is the development compiler.
 *
 *   The development lane therefore runs as sequential Playwright shards (`--shard=i/N`, default
 *   N = 4, `GRADEX_E2E_DEVELOPMENT_SHARDS` overrides). Each shard is a complete invocation with its
 *   own database, API, worker, media server, `next dev`, output and report directories, still one
 *   worker. Shards never overlap: the next one starts only after the previous one's processes are
 *   gone. Before anything runs, the shard lists are checked to be disjoint and to cover exactly the
 *   lane's `playwright test --list`; afterwards every shard must have reported exactly its own
 *   tests. A test missing from every shard, or run twice, fails the canonical result.
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
// Checks the shard partition and exits without building or running anything.
const planOnly = process.argv.includes("--plan-only");

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
    shards: developmentShardCount(),
  },
];

function developmentShardCount() {
  const raw = process.env.GRADEX_E2E_DEVELOPMENT_SHARDS ?? "4";
  const count = Number(raw);
  if (!Number.isInteger(count) || count < 1 || count > 32) {
    console.error(`[e2e:canonical] GRADEX_E2E_DEVELOPMENT_SHARDS must be an integer from 1 to 32, got "${raw}".`);
    process.exit(1);
  }
  return count;
}

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

/** Runs a command in the frontend directory and resolves with its exit code and captured stdout. */
function runCapture(command, args, extraEnv = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd: FRONTEND_DIR,
      env: { ...process.env, ...extraEnv },
      stdio: ["ignore", "pipe", "inherit"],
    });
    const chunks = [];
    child.stdout.on("data", (chunk) => chunks.push(chunk));
    child.on("error", reject);
    child.on("exit", (code, signal) =>
      resolve({ code: signal ? 130 : (code ?? 1), stdout: Buffer.concat(chunks).toString("utf-8") }),
    );
  });
}

/** Every test in a Playwright JSON report, keyed by spec id and project, with its readable title. */
function testsInReport(report) {
  const tests = new Map();
  const walk = (suite, trail) => {
    const here = suite.title ? [...trail, suite.title] : trail;
    for (const spec of suite.specs ?? []) {
      for (const testCase of spec.tests ?? []) {
        tests.set(`${spec.id}|${testCase.projectName ?? ""}`, [...here, spec.title].filter(Boolean).join(" › "));
      }
    }
    for (const child of suite.suites ?? []) walk(child, here);
  };
  for (const suite of report.suites ?? []) walk(suite, []);
  return tests;
}

/** The tests Playwright would run for a lane (or one shard of it), without running anything. */
async function listTests(laneEnv, shardArgs = []) {
  const { code, stdout } = await runCapture("npx", ["playwright", "test", "--list", "--reporter=json", ...shardArgs], laneEnv);
  if (code !== 0) throw new Error(`playwright --list ${shardArgs.join(" ")} exited ${code}`);
  const start = stdout.indexOf("{");
  if (start < 0) throw new Error(`playwright --list ${shardArgs.join(" ")} printed no JSON report`);
  return testsInReport(JSON.parse(stdout.slice(start)));
}

/**
 * Proves the shards partition the lane: no test is listed by two shards, and together they list
 * exactly the lane's authoritative `--list`. Returns each shard's expected test keys.
 */
async function planShards(lane) {
  const all = await listTests(lane.env);
  const shardLists = [];
  const owner = new Map();
  const problems = [];
  for (let index = 1; index <= lane.shards; index += 1) {
    const tests = await listTests(lane.env, [`--shard=${index}/${lane.shards}`]);
    for (const [key, title] of tests) {
      if (owner.has(key)) problems.push(`listed by shards ${owner.get(key)} and ${index}: ${title}`);
      owner.set(key, index);
      if (!all.has(key)) problems.push(`shard ${index} lists a test outside the lane: ${title}`);
    }
    shardLists.push(tests);
  }
  for (const [key, title] of all) {
    if (!owner.has(key)) problems.push(`no shard lists: ${title}`);
  }
  const shardTotal = shardLists.reduce((sum, tests) => sum + tests.size, 0);
  log(
    `${lane.id} lane: ${all.size} tests listed; shards list ${shardLists.map((tests) => tests.size).join(" + ")} = ${shardTotal}`,
  );
  if (problems.length > 0 || shardTotal !== all.size) {
    for (const problem of problems) console.error(`[e2e:canonical] shard plan: ${problem}`);
    throw new Error(`${lane.id} shards do not partition the lane (${shardTotal} listed by shards, ${all.size} in the lane)`);
  }
  return { total: all.size, shardLists };
}

/** Process ids whose working directory is the frontend, other than this runner and its ancestors. */
function processesInFrontend() {
  const ancestors = new Set();
  let pid = process.pid;
  while (pid > 1 && !ancestors.has(pid)) {
    ancestors.add(pid);
    try {
      pid = Number(fs.readFileSync(`/proc/${pid}/stat`, "utf-8").split(") ")[1].split(" ")[1]);
    } catch {
      break;
    }
  }
  const found = [];
  for (const entry of fs.readdirSync("/proc")) {
    if (!/^\d+$/.test(entry) || ancestors.has(Number(entry))) continue;
    try {
      if (fs.readlinkSync(`/proc/${entry}/cwd`) === FRONTEND_DIR) {
        found.push(`${entry} ${fs.readFileSync(`/proc/${entry}/cmdline`, "utf-8").replace(/\0/g, " ").trim().slice(0, 120)}`);
      }
    } catch {
      /* exited, or not ours to read */
    }
  }
  return found;
}

/**
 * A shard is over only when everything it started is gone. Playwright's teardown stops them; this
 * waits for that to finish and refuses to continue rather than killing anything itself.
 */
async function awaitShardProcessesGone(label) {
  const deadline = Date.now() + 60_000;
  let live = processesInFrontend();
  while (live.length > 0 && Date.now() < deadline) {
    await new Promise((resolve) => setTimeout(resolve, 1_000));
    live = processesInFrontend();
  }
  if (live.length > 0) {
    throw new Error(`${label} left processes running in ${FRONTEND_DIR}:\n  ${live.join("\n  ")}`);
  }
  if (fs.existsSync(RUN_STATE_FILE_PATH)) {
    const state = JSON.parse(fs.readFileSync(RUN_STATE_FILE_PATH, "utf-8"));
    for (const pid of [state.pid, state.workerPid].filter(Boolean)) {
      try {
        process.kill(pid, 0);
        throw new Error(`${label} left run ${state.runId} process ${pid} running`);
      } catch (error) {
        if (error.code !== "ESRCH") throw error;
      }
    }
  }
}

/** Available memory and swap in use, from /proc/meminfo, in MiB. */
function memorySnapshot() {
  const info = Object.fromEntries(
    fs
      .readFileSync("/proc/meminfo", "utf-8")
      .split("\n")
      .filter(Boolean)
      .map((line) => {
        const [key, value] = line.split(":");
        return [key, Number.parseInt(value, 10)];
      }),
  );
  return {
    mem_available_mib: Math.round(info.MemAvailable / 1024),
    swap_used_mib: Math.round((info.SwapTotal - info.SwapFree) / 1024),
  };
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
    tests: testsInReport(report),
  };
}

/** A result as written to summary.json: the per-test map is for coverage checks, not the summary. */
function withoutTests(counts) {
  if (!counts) return {};
  const { tests: _tests, ...rest } = counts;
  return rest;
}

/** Sums shard results into one lane result. */
function combineShardResults(shards) {
  const combined = { passed: 0, failed: 0, flaky: 0, skipped: 0, durationMs: 0, skippedTitles: [], tests: new Map() };
  for (const { counts } of shards) {
    combined.passed += counts.passed;
    combined.failed += counts.failed;
    combined.flaky += counts.flaky;
    combined.skipped += counts.skipped;
    combined.durationMs += counts.durationMs;
    combined.skippedTitles.push(...counts.skippedTitles);
    for (const [key, title] of counts.tests) combined.tests.set(key, title);
  }
  return combined;
}

/** Problems with a shard's report: a listed test it did not report, or a test it was never given. */
function shardCoverageProblems(label, expected, counts) {
  const problems = [];
  for (const [key, title] of expected) {
    if (!counts.tests.has(key)) problems.push(`${label} did not report: ${title}`);
  }
  for (const [key, title] of counts.tests) {
    if (!expected.has(key)) problems.push(`${label} reported a test it was not assigned: ${title}`);
  }
  const reported = counts.passed + counts.failed + counts.flaky + counts.skipped;
  if (reported !== expected.size) problems.push(`${label} reported ${reported} outcomes for ${expected.size} assigned tests`);
  return problems;
}

async function main() {
  if (planOnly) {
    for (const lane of LANES.filter((candidate) => (candidate.shards ?? 1) > 1)) await planShards(lane);
    log("shard plan verified; nothing was run (--plan-only)");
    return;
  }

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
    log(`── ${lane.title} ──`);
    const shardCount = lane.shards ?? 1;
    const plan = shardCount > 1 ? await planShards(lane) : null;
    const shardResults = [];
    for (let index = 1; index <= shardCount; index += 1) {
      const id = shardCount > 1 ? `${lane.id}-shard-${index}-of-${shardCount}` : lane.id;
      const jsonPath = path.join(RESULT_DIR, `${id}.json`);
      try {
        fs.unlinkSync(jsonPath);
      } catch {
        /* first run */
      }

      await awaitShardProcessesGone(`before ${id}`);
      const memoryBefore = memorySnapshot();
      log(`${id}: starting; available ${memoryBefore.mem_available_mib} MiB, swap used ${memoryBefore.swap_used_mib} MiB`);
      const exitCode = await run("npx", ["playwright", "test", ...(shardCount > 1 ? [`--shard=${index}/${shardCount}`] : [])], {
        ...lane.env,
        GRADEX_PLAYWRIGHT_HTML_DIR: path.join("playwright-report", id),
        // Each shard gets its own output directory: Playwright clears it at start, so a shared one
        // would delete the previous shard's failure artifacts.
        GRADEX_PLAYWRIGHT_OUTPUT_DIR: shardCount > 1 ? path.join("test-results", lane.id, `shard-${index}-of-${shardCount}`) : path.join("test-results", lane.id),
        GRADEX_PLAYWRIGHT_JSON_FILE: jsonPath,
      });
      await awaitShardProcessesGone(`after ${id}`);
      const memoryAfter = memorySnapshot();
      log(`${id}: exit ${exitCode}; processes gone; available ${memoryAfter.mem_available_mib} MiB, swap used ${memoryAfter.swap_used_mib} MiB`);

      const counts = readLaneResult(jsonPath);
      if (!counts) console.error(`[e2e:canonical] ${id} produced no JSON report at ${jsonPath}`);
      const problems = counts && plan ? shardCoverageProblems(id, plan.shardLists[index - 1], counts) : [];
      for (const problem of problems) console.error(`[e2e:canonical] ${problem}`);
      shardResults.push({ id, exitCode, counts, problems, memoryBefore, memoryAfter });
    }

    const complete = shardResults.every(({ counts }) => counts);
    const counts = complete ? (shardCount > 1 ? combineShardResults(shardResults) : shardResults[0].counts) : null;
    const problems = shardResults.flatMap((shard) => shard.problems);
    if (counts && plan && counts.tests.size !== plan.total) {
      problems.push(`${lane.id} reported ${counts.tests.size} distinct tests, the lane lists ${plan.total}`);
    }
    const exitCode = shardResults.find((shard) => shard.exitCode !== 0)?.exitCode ?? (problems.length > 0 ? 1 : 0);
    results.push({ lane, exitCode, counts, problems, plan, shards: shardCount > 1 ? shardResults : [] });
  }

  const total = { passed: 0, failed: 0, flaky: 0, skipped: 0 };
  console.log("\n════════ canonical E2E result ════════");
  for (const { lane, exitCode, counts, problems, plan, shards } of results) {
    for (const shard of shards) {
      const line = shard.counts
        ? `passed ${shard.counts.passed}  failed ${shard.counts.failed}  flaky ${shard.counts.flaky}  skipped ${shard.counts.skipped}`
        : "NO REPORT";
      console.log(
        `${"".padEnd(12)} ${shard.id}: ${line}  (exit ${shard.exitCode}; available ` +
          `${shard.memoryBefore.mem_available_mib}→${shard.memoryAfter.mem_available_mib} MiB)`,
      );
    }
    for (const problem of problems) console.log(`${lane.id.padEnd(12)} COVERAGE: ${problem}`);
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
        `flaky ${counts.flaky}  skipped ${counts.skipped}  (${(counts.durationMs / 1000).toFixed(1)}s, exit ${exitCode})` +
        (plan ? `  [${counts.tests.size} of ${plan.total} listed tests reported]` : ""),
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
    lanes: results.map(({ lane, exitCode, counts, problems, plan, shards }) => ({
      lane: lane.id,
      exit_code: exitCode,
      ...withoutTests(counts),
      ...(plan ? { listed_tests: plan.total, reported_tests: counts?.tests.size ?? 0 } : {}),
      coverage_problems: problems,
      shards: shards.map((shard) => ({
        shard: shard.id,
        exit_code: shard.exitCode,
        assigned_tests: plan.shardLists[shards.indexOf(shard)].size,
        ...withoutTests(shard.counts),
        coverage_problems: shard.problems,
        memory_before: shard.memoryBefore,
        memory_after: shard.memoryAfter,
      })),
    })),
    aggregate: total,
  };
  fs.writeFileSync(path.join(RESULT_DIR, "summary.json"), `${JSON.stringify(summary, null, 2)}\n`);

  // Non-zero if any lane or shard failed, produced no report, reported a failure, or left a listed
  // test unreported. No swallowed errors.
  const failed = results.some(
    ({ exitCode, counts, problems }) => exitCode !== 0 || !counts || counts.failed > 0 || problems.length > 0,
  );
  process.exit(failed ? 1 : 0);
}

main().catch((error) => {
  console.error("[e2e:canonical]", error);
  process.exit(1);
});
