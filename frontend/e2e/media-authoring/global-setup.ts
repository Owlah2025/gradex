import { execSync, spawn } from "child_process";
import { createHash } from "crypto";
import fs from "fs";
import path from "path";
import http from "http";
import {
  acquireEnvironmentLock,
  writeRunState,
  cleanupRunResources,
  closeApiLogDrain,
  startApiLogDrain,
  e2eDatabaseEnvironment,
  API_BINARY_PATH,
  SEED_BINARY_PATH,
  RUN_STATE_FILE_PATH,
  E2E_TMP_DIR,
  RunState,
} from "../../src/lib/api/e2e-infrastructure";
import { apiOrigin, assertPortIsFree, frontendOrigin, runPort, API_PORT_ENV, FRONTEND_PORT_ENV } from "../../src/lib/api/e2e-ports";
import { MEDIA_DIAGNOSTIC_BINARY_PATH, MEDIA_DIAGNOSTIC_STATE_PATH, WORKER_BINARY_PATH, WORKER_STATE_FILE_PATH, mediaStackEnvironment, terminateWorker } from "./worker-process";

/**
 * Environment for the real media authoring journey.
 *
 * It differs from the shared E2E setup in exactly the ways the media pipeline
 * needs and no others:
 *
 *  - object storage is the developer Compose MinIO, not the read-only HLS
 *    fixture server, because the browser genuinely PUTs bytes and the API
 *    genuinely re-reads and hashes the stored object version;
 *  - a real worker process runs, because scanning and transcoding are its job;
 *  - APP_ENV is development with MEDIA_SCANNER_MODE=DEVELOPMENT_NO_OP, the only
 *    configuration in which the unresolved LG-014 scanner boundary can be
 *    exercised end to end. Production still refuses that mode outright.
 */

const backendDir = path.resolve(__dirname, "../../../backend");

const runtimeMediaConfigurationKeys = new Set([
  "MEDIA_SCANNER_MODE",
  "MEDIA_OPERATING_MODE",
  "APP_ENV",
  "REDIS_ADDR",
  "S3_ENDPOINT",
  "S3_BUCKET",
]);

function logEffectiveMediaConfiguration(role: "api" | "worker", pid: number): Record<string, string> {
  const environment = fs.readFileSync(`/proc/${pid}/environ`, "utf8");
  const resolved = Object.fromEntries(
    environment
      .split("\0")
      .filter((entry) => entry !== "")
      .map((entry) => entry.split(/=(.*)/s, 2))
      .filter(([key]) => runtimeMediaConfigurationKeys.has(key)),
  );
  const missing = [...runtimeMediaConfigurationKeys].filter((key) => resolved[key] === undefined);
  if (missing.length > 0) {
    throw new Error(`[Media E2E Setup] ${role} runtime configuration omitted ${missing.join(", ")}`);
  }
  console.log(`[Media E2E Runtime] ${role} ${JSON.stringify(resolved)}`);
  return resolved;
}

async function waitForHealth(url: string, timeoutMs: number): Promise<void> {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    const ok = await new Promise<boolean>((resolve) => {
      const request = http.get(url, (response) => resolve(response.statusCode === 200));
      request.on("error", () => resolve(false));
      request.end();
    });
    if (ok) return;
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(`Timeout waiting for Go API health probe at ${url}`);
}

export default async function globalSetup() {
  const runId = (Date.now().toString(36) + Math.random().toString(36).substring(2, 10))
    .toLowerCase()
    .replace(/[^a-z0-9]/g, "")
    .substring(0, 16);
  const dbName = `gradex_playwright_e2e_${runId}`;

  let dbCreated = false;
  let apiProcess: any = null;
  let workerProcess: any = null;
  let stateWritten = false;

  try {
    console.log(`[Media E2E Setup] Acquiring environment lock for run ${runId}...`);
    acquireEnvironmentLock(runId);

    const port = runPort(API_PORT_ENV);
    const frontendPort = runPort(FRONTEND_PORT_ENV);
    await assertPortIsFree(port, "The run-owned Go API");

    console.log("[Media E2E Setup] Compiling Go API, worker, seeder, and diagnostic binaries...");
    execSync(`go build -o ${API_BINARY_PATH} ./cmd/api`, { cwd: backendDir, stdio: "inherit" });
    execSync(`go build -o ${WORKER_BINARY_PATH} ./cmd/worker`, { cwd: backendDir, stdio: "inherit" });
    execSync(`go test -c -o ${SEED_BINARY_PATH} ./cmd/e2e-seed`, { cwd: backendDir, stdio: "inherit" });
    execSync(`go build -o ${MEDIA_DIAGNOSTIC_BINARY_PATH} ./cmd/e2e-media-diagnostic`, { cwd: backendDir, stdio: "inherit" });

    dbCreated = true;
    console.log(`[Media E2E Setup] Seeding ${dbName}...`);
    execSync(SEED_BINARY_PATH, {
      cwd: backendDir,
      stdio: "inherit",
      env: { ...process.env, ...e2eDatabaseEnvironment(dbName) },
    });

    const targetDSN = e2eDatabaseEnvironment(dbName).GRADEX_E2E_TARGET_DB_URL;
    const backendEnvironment = mediaStackEnvironment({
      databaseURL: targetDSN,
      publicOrigin: frontendOrigin(),
      limiterKey: createHash("sha256").update(`gradex-media-e2e-limiter-${runId}`).digest("hex"),
    });
    // The canonical seed owns the existing Lab Material association. These
    // deterministic private bytes make its real Student download verifiable
    // without creating a Lab authoring workflow in this Resource-only slice.
    console.log("[Media E2E Setup] Writing deterministic private Resource/Lab fixture bytes...");
    execSync("go run ./cmd/e2e-material-fixture", {
      cwd: backendDir,
      stdio: "inherit",
      env: { ...process.env, ...backendEnvironment, GRADEX_E2E_SEED_PRIVATE_MATERIALS: "true" },
    });
    const env = { ...process.env, ...backendEnvironment, PORT: String(port), SERVICE_ROLE: "api" };

    // The session-issuance helper builds the same production configuration the
    // API does, so it needs the same settings in its environment. These are
    // development fixture values for a throwaway database; they stay in process
    // memory, are never written to the run-state file, and never reach browser
    // JavaScript.
    for (const [key, value] of Object.entries(backendEnvironment)) {
      process.env[key] = value;
    }

    console.log(
      `[Media E2E Setup] Starting Go API on port ${port} against MinIO ${backendEnvironment.S3_ENDPOINT}...`,
    );
    apiProcess = spawn(API_BINARY_PATH, [], { env, stdio: "pipe" });
    if (!apiProcess.pid) throw new Error("[Media E2E Setup] Failed to spawn Go API process");
    const apiRuntime = logEffectiveMediaConfiguration("api", apiProcess.pid);
    const apiLogPath = startApiLogDrain(apiProcess, runId);
    console.log(`[Media E2E Setup] Draining Go API output to ${apiLogPath}`);

    console.log("[Media E2E Setup] Starting media worker...");
    workerProcess = spawn(WORKER_BINARY_PATH, [], {
      env: { ...env, SERVICE_ROLE: "worker" },
      stdio: ["ignore", "pipe", "pipe"],
    });
    if (!workerProcess.pid) throw new Error("[Media E2E Setup] Failed to spawn media worker process");
    const workerRuntime = logEffectiveMediaConfiguration("worker", workerProcess.pid);
    const workerLog = fs.createWriteStream(`${E2E_TMP_DIR}/gradex-media-e2e-worker-${runId}.log`, { mode: 0o600 });
    workerProcess.stdout?.pipe(workerLog, { end: false });
    workerProcess.stderr?.pipe(workerLog, { end: false });
    const workerLogPath = `${E2E_TMP_DIR}/gradex-media-e2e-worker-${runId}.log`;
    fs.writeFileSync(WORKER_STATE_FILE_PATH, JSON.stringify({ runId, pid: workerProcess.pid }), { mode: 0o600 });

    fs.writeFileSync(MEDIA_DIAGNOSTIC_STATE_PATH, JSON.stringify({
      run_id: runId,
      asset_version_id: "",
      runtime: { api: apiRuntime, worker: workerRuntime },
      api_log_path: apiLogPath,
      worker_log_path: workerLogPath,
    }), { mode: 0o600 });

    const runState: RunState = {
      runId,
      pid: apiProcess.pid,
      port,
      apiExecPath: API_BINARY_PATH,
      apiListenAddr: `127.0.0.1:${port}`,
      frontendPort,
      processStartTime: new Date().toISOString(),
      dbName,
      lockOwner: runId,
      startedAt: new Date().toISOString(),
    };
    writeRunState(runState);
    stateWritten = true;

    process.env.GRADEX_API_ORIGIN = apiOrigin();
    await waitForHealth(`http://127.0.0.1:${port}/readyz`, 20000);
    console.log(`[Media E2E Setup] Go API readiness probe passed on port ${port}.`);
  } catch (err: any) {
    console.error("[Media E2E Setup Failure] Cleaning up acquired resources...", err);
    closeApiLogDrain();
    terminateWorker();
    await cleanupRunResources({
      runId,
      dbName: dbCreated ? dbName : null,
      apiPid: apiProcess?.pid || null,
      stateFilePath: stateWritten ? RUN_STATE_FILE_PATH : undefined,
    });
    if (workerProcess?.pid) {
      try {
        process.kill(workerProcess.pid, "SIGTERM");
      } catch {}
    }
    throw err;
  }
}
