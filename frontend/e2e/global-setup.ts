import { execSync, spawn } from "child_process";
import { createHash } from "crypto";
import path from "path";
import fs from "fs";
import http from "http";
import {
  acquireEnvironmentLock,
  writeRunState,
  cleanupRunResources,
  closeApiLogDrain,
  startApiLogDrain,
  API_BINARY_PATH,
  SEED_BINARY_PATH,
  WORKER_BINARY_PATH,
  RUN_STATE_FILE_PATH,
  RunState,
} from "../src/lib/api/e2e-infrastructure";
import { startLocalMediaServer } from "../src/lib/api/e2e-media-server";
import { apiOrigin, assertPortIsFree, frontendOrigin, runPort, API_PORT_ENV, FRONTEND_PORT_ENV } from "../src/lib/api/e2e-ports";

const backendDir = path.resolve(__dirname, "../../backend");

async function waitForHealth(url: string, timeoutMs: number): Promise<void> {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    try {
      const ok = await new Promise<boolean>((resolve) => {
        const req = http.get(url, (res) => {
          resolve(res.statusCode === 200);
        });
        req.on("error", () => resolve(false));
        req.end();
      });
      if (ok) return;
    } catch {}
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Error(`Timeout waiting for Go API health probe at ${url}`);
}

/**
 * Refuses to run this single-tenant harness under concurrency.
 *
 * The config pins `workers: 1`, but `--workers=N` on the command line silently overrides it. Every
 * spec shares one seeded database, one API, one Next server, and one set of fixed `/var/tmp`
 * singletons, and several specs mutate shared authority rows by id mid-run. Under more than one
 * worker the failing set stops being a property of the code (GAP-05), which is exactly the
 * condition this gate exists to prevent — so it fails loudly instead of producing an untrustworthy
 * green.
 */
function assertSingleWorker(config: { workers?: number }): void {
  const workers = config.workers ?? 1;
  if (workers <= 1) return;
  throw new Error(
    `[E2E Setup] Refusing to run with ${workers} workers. This harness shares one seeded database, ` +
      `one API process, one Next server, and fixed /var/tmp run-state — and specs mutate shared ` +
      `authority rows by id. Concurrency makes the failing set depend on execution order rather ` +
      `than on the code (GAP-05). Run with one worker, or build per-worker isolation first.`,
  );
}

export default async function globalSetup(config?: { workers?: number }) {
  assertSingleWorker(config ?? {});

  const runId = (Date.now().toString(36) + Math.random().toString(36).substring(2, 10)).toLowerCase().replace(/[^a-z0-9]/g, "").substring(0, 16);
  const dbName = `gradex_playwright_e2e_${runId}`;

  let dbCreated = false;
  let apiProcess: any = null;
  let workerProcess: any = null;
  let stateWritten = false;

  try {
    console.log(`[E2E Setup] Acquiring environment lock for run ${runId}...`);
    acquireEnvironmentLock(runId);

    // Both ports were allocated by the config module and published through the environment, so
    // the Next.js server this run started is already pointing at this API port. Neither is a
    // fixed number, and neither may belong to a process this run does not own.
    const port = runPort(API_PORT_ENV);
    const frontendPort = runPort(FRONTEND_PORT_ENV);
    await assertPortIsFree(port, "The run-owned Go API");

    console.log(`[E2E Setup] Run ID: ${runId}`);
    console.log(`[E2E Setup] Allocated API Port: ${port}`);
    console.log(`[E2E Setup] Allocated Frontend Port: ${frontendPort}`);
    console.log(`[E2E Setup] Isolated Database Name: ${dbName}`);

    console.log("[E2E Setup] Compiling Go API binary to external output path...");
    execSync(`go build -o ${API_BINARY_PATH} ./cmd/api`, { cwd: backendDir, stdio: "inherit" });

    console.log("[E2E Setup] Compiling Go worker binary to external output path...");
    execSync(`go build -o ${WORKER_BINARY_PATH} ./cmd/worker`, { cwd: backendDir, stdio: "inherit" });

    console.log("[E2E Setup] Compiling E2E seeder binary to external output path...");
    execSync(`go test -c -o ${SEED_BINARY_PATH} ./cmd/e2e-seed`, { cwd: backendDir, stdio: "inherit" });

    const adminDSN = "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable";
    const targetDSN = `postgres://gradex:gradex@localhost:5432/${dbName}?sslmode=disable`;

    // Marked before the seeder runs, not after. The seeder creates the database itself, so a
    // seeder that fails part-way still leaves one behind; flagging it only on success leaked a
    // database that then required manual cleanup.
    dbCreated = true;
    console.log(`[E2E Setup] Running database seeder for ${dbName}...`);
    execSync(SEED_BINARY_PATH, {
      cwd: backendDir,
      stdio: "inherit",
      env: {
        ...process.env,
        GRADEX_E2E_ADMIN_DB_URL: adminDSN,
        GRADEX_E2E_TARGET_DB_NAME: dbName,
        GRADEX_E2E_TARGET_DB_URL: targetDSN,
        GRADEX_E2E_ALLOW_DATABASE_RESET: "1",
        DATABASE_URL: "postgres://gradex:gradex@localhost:5432/gradex?sslmode=disable",
      },
    });

    console.log("[E2E Setup] Starting local media server for HLS test fixture...");
    const mediaServer = await startLocalMediaServer();
    console.log(`[E2E Setup] Local media server running at ${mediaServer.origin}`);

    console.log(`[E2E Setup] Starting Go API server on port ${port}...`);
    const env = {
      ...process.env,
      DATABASE_URL: targetDSN,
      PORT: String(port),
      APP_ENV: "development",
      SERVICE_ROLE: "api",
      REDIS_ADDR: "localhost:6379",
      S3_ENDPOINT: mediaServer.origin,
      S3_BUCKET: "gradex-test",
      S3_ACCESS_KEY: "gradexminio",
      S3_SECRET_KEY: "gradexminio",
      AUTH_FAKE_MODE: "false",
      STUDENT_REGISTRATION_ENABLED: "true",
      REGISTRATION_POLICY_SET_ID: "e2e-policy-v1",
      PASSWORD_SCREEN_MODE: "deterministic",
      SESSION_CSRF_KEY: "0123456789abcdef0123456789abcdef",
      ANONYMOUS_COOKIE_SIGNING_KEY: "1123456789abcdef0123456789abcdef",
      ANONYMOUS_CSRF_KEY: "2123456789abcdef0123456789abcdef",
      // The HMAC key that keeps a six-digit verification code unsearchable in
      // the database. Registration refuses to start without it, deliberately:
      // a deployment that could only store an unkeyed digest of a
      // million-value space must not come up quietly.
      IDENTITY_OTP_PEPPER: "3123456789abcdef0123456789abcdef",
      // Per-run limiter key namespace.
      //
      // Redis is shared between runs even though each run gets its own PostgreSQL database, and
      // limiter counters key on the trusted source address — which is `127.0.0.1` for every run
      // on this host. Two runs launched back to back therefore share live counters, and the
      // second inherits the first's consumption.
      //
      // The limiter derives every Redis key by HMAC over (policy, dimension, value) with this
      // key, so a per-run value gives each run its own counter namespace. Nothing about the
      // policy changes: every limit, window, burst, and dimension is enforced in full within the
      // run. This isolates test state exactly as the per-run database does; it does not relax a
      // production limit, and a run that genuinely exceeds a ceiling still gets 429.
      ADMISSION_LIMITER_HMAC_KEY: createHash("sha256").update(`gradex-s5-e2e-limiter-${runId}`).digest("hex"),
      PLAYBACK_TOKEN_SECRET: "4123456789abcdef0123456789abcdef",
      OUTBOX_PROTECTED_PAYLOAD_KEY: "5123456789abcdef0123456789abcdef",
      OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION: "dev-v1",
      PUBLIC_ORIGIN: frontendOrigin(),
      CORS_ALLOWED_ORIGINS: frontendOrigin(),
      CORS_ALLOW_CREDENTIALS: "true",
    };

    // The session-issuance helper builds the production configuration the same way the API does,
    // so it needs the same configuration in its environment. These are development fixture values
    // for a throwaway database; they are held in process memory only, are never written to the
    // run-state file, and never reach browser JavaScript.
    for (const key of ["APP_ENV", "SERVICE_ROLE", "SESSION_CSRF_KEY", "ANONYMOUS_COOKIE_SIGNING_KEY",
      "ANONYMOUS_CSRF_KEY", "ADMISSION_LIMITER_HMAC_KEY", "PLAYBACK_TOKEN_SECRET",
      "OUTBOX_PROTECTED_PAYLOAD_KEY", "OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION", "REDIS_ADDR",
      "S3_ENDPOINT", "S3_BUCKET", "S3_ACCESS_KEY", "S3_SECRET_KEY", "AUTH_FAKE_MODE",
      "STUDENT_REGISTRATION_ENABLED", "REGISTRATION_POLICY_SET_ID", "PASSWORD_SCREEN_MODE",
      // The seeder's verification query builds the production configuration
      // the same way the API does, and registration refuses to load without
      // the OTP pepper.
      "IDENTITY_OTP_PEPPER",
      "PUBLIC_ORIGIN", "CORS_ALLOWED_ORIGINS", "CORS_ALLOW_CREDENTIALS"]) {
      process.env[key] = (env as Record<string, string>)[key];
    }

    apiProcess = spawn(API_BINARY_PATH, [], { env, stdio: "pipe" });
    if (!apiProcess.pid) {
      throw new Error("[E2E Setup] Failed to spawn Go API process");
    }

    // Both API pipes are drained continuously; neither can fill and wedge the server.
    const apiLogPath = startApiLogDrain(apiProcess, runId);
    console.log(`[E2E Setup] Draining Go API output to ${apiLogPath}`);

    // The transactional email path is a product capability, not a test fixture: the API writes an
    // outbox row and a worker renders and sends it. A run that never starts a worker can only prove
    // the row was written, so the run owns a worker whose SMTP target is the development Mailpit
    // instance. `EMAIL_PROVIDER=mailpit` is refused outside `APP_ENV=development` by configuration,
    // so this cannot select a development sender in production.
    //
    // Redis and object storage are the worker's other dependencies. The bucket check runs against
    // the developer MinIO because the run-local media server serves fixture bytes rather than an S3
    // API; no media work is exercised here, and the email dispatcher touches neither.
    const workerEnv = {
      ...env,
      SERVICE_ROLE: "worker",
      PORT: String(port + 1),
      S3_ENDPOINT: process.env.GRADEX_E2E_WORKER_S3_ENDPOINT || "http://127.0.0.1:9000",
      S3_BUCKET: process.env.GRADEX_E2E_WORKER_S3_BUCKET || "gradex-video",
      S3_ACCESS_KEY: "gradexminio",
      S3_SECRET_KEY: "gradexminio",
      S3_USE_PATH_STYLE: "true",
      MEDIA_OPERATING_MODE: "SCANNER",
      MEDIA_SCANNER_MODE: "DEVELOPMENT_NO_OP",
      EMAIL_ENABLED: "true",
      EMAIL_PROVIDER: "mailpit",
      EMAIL_SMTP_ADDR: process.env.GRADEX_E2E_MAILPIT_SMTP || "127.0.0.1:1025",
      EMAIL_FROM_ADDRESS: "no-reply@gradex.test",
      EMAIL_FROM_NAME: "Gradex",
    };

    console.log("[E2E Setup] Starting Go worker (transactional email dispatcher)...");
    workerProcess = spawn(WORKER_BINARY_PATH, [], { env: workerEnv, stdio: "pipe" });
    if (!workerProcess.pid) {
      throw new Error("[E2E Setup] Failed to spawn Go worker process");
    }
    const workerLogPath = `${API_BINARY_PATH}-worker-${runId}.log`;
    const workerLog = fs.createWriteStream(workerLogPath, { flags: "a" });
    workerProcess.stdout.pipe(workerLog);
    workerProcess.stderr.pipe(workerLog);
    workerProcess.on("exit", (code: number | null) => {
      console.warn(`[E2E Setup] Worker process exited with code ${code}. See ${workerLogPath}`);
    });
    console.log(`[E2E Setup] Draining worker output to ${workerLogPath}`);

    const runState: RunState = {
      runId,
      pid: apiProcess.pid,
      workerPid: workerProcess.pid,
      workerExecPath: WORKER_BINARY_PATH,
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
    console.log(`[E2E Setup] Recorded run state to ${RUN_STATE_FILE_PATH}`);

    process.env.GRADEX_API_ORIGIN = `http://127.0.0.1:${port}`;
    await waitForHealth(`http://127.0.0.1:${port}/readyz`, 15000);
    console.log(`[E2E Setup] Go API readiness probe passed on port ${port}.`);
  } catch (err: any) {
    console.error("[E2E Setup Failure] Setup failed. Cleaning up acquired resources...", err);
    // A failed setup releases the log descriptor too; the drain must not outlive the run that
    // opened it.
    closeApiLogDrain();
    await cleanupRunResources({
      runId,
      dbName: dbCreated ? dbName : null,
      apiPid: apiProcess?.pid || null,
      workerPid: workerProcess?.pid || null,
      stateFilePath: stateWritten ? RUN_STATE_FILE_PATH : undefined,
    });
    throw err;
  }
}
