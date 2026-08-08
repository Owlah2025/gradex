import fs from "fs";
import path from "path";
import { execFileSync } from "child_process";

/**
 * The API's output must be drained continuously.
 *
 * A child spawned with piped stdio whose output nobody reads blocks on its own write once the
 * ~64 KB pipe buffer fills. When that write happens inside a request handler the server wedges:
 * it keeps its listening socket, so it still looks alive, while every request hangs up or returns
 * 500. That is invisible in a short run and fatal in a repeated one. Draining to a run-owned file
 * removes the failure mode and keeps the API's own diagnostics available as evidence.
 *
 * The API logs structured request lines only — method, route template, status, duration, limiter
 * outcome. It never logs session cookies, CSRF tokens, database credentials, signed media URLs,
 * or Asset Version identifiers, so the drained file carries no credential-bearing value.
 */
/**
 * Base directory for every transient run-owned path: the compiled API and seeder binaries, the
 * environment lock, the run-state file, and the drained API log.
 *
 * It is deliberately **outside the repository** — a compiled binary inside the working tree would
 * be a repository-local executable — and it is overridable so a CI runner can point it at its own
 * job-scoped temporary directory, which the runner destroys with itself. The default is the value
 * this constant has always had, so an unset environment behaves exactly as before for every
 * existing E2E suite.
 */
export const E2E_TMP_DIR = process.env.GRADEX_E2E_TMP_DIR || "/var/tmp";

export const API_LOG_DIR = E2E_TMP_DIR;
/** Run-owned log naming pattern: `<tmp>/gradex-s5-e2e-api-<runId>.log`, mode 0600. */
export function apiLogPath(runId: string): string {
  return `${API_LOG_DIR}/gradex-s5-e2e-api-${runId}.log`;
}
/** Teardown archives the drained log here rather than deleting it, so limiter evidence survives. */
export const API_LOG_ARCHIVE_DIR = `${E2E_TMP_DIR}/gradex-s5-e2e-evidence`;

export const LOCK_FILE_PATH = `${E2E_TMP_DIR}/gradex-s5-e2e-environment.lock`;
export const RUN_STATE_FILE_PATH = `${E2E_TMP_DIR}/gradex-s5-e2e-run-state.json`;
export const API_BINARY_PATH = `${E2E_TMP_DIR}/gradex-s5-playwright-api`;
export const SEED_BINARY_PATH = `${E2E_TMP_DIR}/gradex-s5-e2e-seed`;

// External staging runs keep using the same safety-gated seed/query binary,
// but their PostgreSQL endpoint is supplied by the deployment harness rather
// than assumed to be the local developer Compose port.
export function e2eDatabaseEnvironment(dbName: string): Record<string, string> {
  return {
    GRADEX_E2E_ALLOW_DATABASE_RESET: "1",
    GRADEX_E2E_ADMIN_DB_URL:
      process.env.GRADEX_E2E_ADMIN_DB_URL || "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable",
    GRADEX_E2E_TARGET_DB_NAME: dbName,
    GRADEX_E2E_TARGET_DB_URL:
      process.env.GRADEX_E2E_TARGET_DB_URL || `postgres://gradex:gradex@localhost:5432/${dbName}?sslmode=disable`,
    DATABASE_URL:
      process.env.GRADEX_E2E_APPLICATION_DB_URL || "postgres://gradex:gradex@localhost:5432/gradex?sslmode=disable",
  };
}

export type LockInfo = {
  ownerPid: number;
  ownerCmd: string;
  processStartTime: string;
  runId: string;
  stateFilePath: string;
  createdAt: string;
};

export type RunState = {
  runId: string;
  pid: number;
  port: number;
  apiExecPath: string;
  apiListenAddr: string;
  /** The per-run Next.js port. A port number is run-owned, non-secret state. */
  frontendPort: number;
  processStartTime: string;
  dbName: string;
  lockOwner: string;
  startedAt: string;
};

export type CleanupResources = {
  runId: string;
  lockFilePath?: string;
  stateFilePath?: string;
  dbName?: string | null;
  apiPid?: number | null;
  seedBinaryPath?: string;
  adminDSN?: string;
};

export type CleanupOutcome = {
  apiTerminated: boolean;
  dbDropped: boolean;
  stateFileRemoved: boolean;
  lockReleased: boolean;
  errors: string[];
};

type ApiLogDrain = { runId: string; path: string; stream: fs.WriteStream };

// Module state, shared because Playwright evaluates globalSetup and globalTeardown in the same
// runner process. Holding the stream is what lets both teardown and setup-failure cleanup close
// it explicitly instead of relying on process exit to release the descriptor.
let activeDrain: ApiLogDrain | null = null;

/**
 * Attaches a continuous drain from the API child's stdout and stderr to a run-owned file. Both
 * streams are consumed, so neither pipe can fill.
 */
export function startApiLogDrain(
  child: { stdout: NodeJS.ReadableStream | null; stderr: NodeJS.ReadableStream | null },
  runId: string
): string {
  closeApiLogDrain();
  const path = apiLogPath(runId);
  const stream = fs.createWriteStream(path, { mode: 0o600 });
  // `end: false` so neither stream closes the file when it ends; the file is closed exactly once,
  // by closeApiLogDrain.
  child.stdout?.pipe(stream, { end: false });
  child.stderr?.pipe(stream, { end: false });
  child.stdout?.resume();
  child.stderr?.resume();
  activeDrain = { runId, path, stream };
  return path;
}

/** Closes the drained log, releasing its descriptor. Safe to call when no drain is active. */
export function closeApiLogDrain(): void {
  if (!activeDrain) return;
  try {
    activeDrain.stream.end();
  } catch {}
  activeDrain = null;
}

/** The active drain's path, or null. */
export function activeApiLogPath(): string | null {
  return activeDrain?.path ?? null;
}

/**
 * Moves the run's drained log into the evidence directory. Retaining it — rather than deleting —
 * is what makes per-run limiter evidence observable after teardown; it is run-owned, secret-free,
 * and never blocks a later run.
 */
export function archiveApiLog(runId: string): string | null {
  const source = apiLogPath(runId);
  if (!fs.existsSync(source)) return null;
  try {
    fs.mkdirSync(API_LOG_ARCHIVE_DIR, { recursive: true, mode: 0o700 });
    const destination = `${API_LOG_ARCHIVE_DIR}/gradex-s5-e2e-api-${runId}.log`;
    fs.renameSync(source, destination);
    return destination;
  } catch {
    return null;
  }
}

export function getProcessStartTime(pid: number): string | null {
  if (!pid || pid <= 0) return null;
  try {
    const statPath = `/proc/${pid}/stat`;
    if (!fs.existsSync(statPath)) return null;
    const stat = fs.readFileSync(statPath, "utf-8");
    const rightParenIndex = stat.lastIndexOf(")");
    if (rightParenIndex === -1) return null;
    const rest = stat.substring(rightParenIndex + 2).trim().split(/\s+/);
    return rest[19] || null; // 22nd field in /proc/<pid>/stat
  } catch {
    return null;
  }
}

export function verifyProcessOwnership(
  pid: number,
  expectedKeywords: string | string[],
  recordedStartTime?: string | null
): boolean {
  if (!pid || pid <= 0) return false;
  try {
    process.kill(pid, 0);
  } catch {
    return false; // Process does not exist
  }

  // If recordedStartTime is provided, verify process start identity has not changed (PID recycled)
  if (recordedStartTime) {
    const currentStartTime = getProcessStartTime(pid);
    if (currentStartTime && currentStartTime !== recordedStartTime) {
      return false; // PID recycled, stale ownership
    }
  }

  const keywords = Array.isArray(expectedKeywords) ? expectedKeywords : [expectedKeywords];

  // On Linux, inspect /proc/<pid>/cmdline to ensure process identity
  const cmdlinePath = `/proc/${pid}/cmdline`;
  if (fs.existsSync(cmdlinePath)) {
    try {
      const cmdline = fs.readFileSync(cmdlinePath, "utf-8");
      const matchesKeyword = keywords.some((kw) => cmdline.includes(kw));
      if (!matchesKeyword) {
        return false;
      }
    } catch {
      return false;
    }
  }
  return true;
}

export function acquireEnvironmentLock(
  runId: string,
  stateFilePath: string = RUN_STATE_FILE_PATH,
  lockFilePath: string = LOCK_FILE_PATH
): LockInfo {
  const startTime = getProcessStartTime(process.pid) || new Date().toISOString();
  const lockInfo: LockInfo = {
    ownerPid: process.pid,
    ownerCmd: process.argv.join(" "),
    processStartTime: startTime,
    runId,
    stateFilePath,
    createdAt: new Date().toISOString(),
  };

  const payload = JSON.stringify(lockInfo, null, 2);

  try {
    const fd = fs.openSync(lockFilePath, "wx", 0o600);
    fs.writeFileSync(fd, payload);
    fs.closeSync(fd);
    return lockInfo;
  } catch (err: any) {
    if (err.code !== "EEXIST") {
      throw new Error(`Failed to acquire environment lock at ${lockFilePath}: ${err.message}`);
    }
  }

  // Lock file exists. Read existing lock to inspect owner.
  let existingLock: LockInfo | null = null;
  try {
    const raw = fs.readFileSync(lockFilePath, "utf-8");
    existingLock = JSON.parse(raw);
  } catch {
    // Corrupt lock file. Safely reclaim.
  }

  if (existingLock && existingLock.ownerPid) {
    const isAlive = verifyProcessOwnership(
      existingLock.ownerPid,
      ["playwright", "s5-lesson-player", "global-setup", "e2e", "node"],
      existingLock.processStartTime
    );
    if (isAlive && existingLock.runId !== runId) {
      throw new Error(
        `E2E environment is locked by active process PID ${existingLock.ownerPid} (runId: ${existingLock.runId}, created: ${existingLock.createdAt})`
      );
    }
  }

  // Recorded owner is dead or file was corrupt or PID recycled. Reclaim lock safely.
  try {
    fs.unlinkSync(lockFilePath);
  } catch {}

  const fd = fs.openSync(lockFilePath, "wx", 0o600);
  fs.writeFileSync(fd, payload);
  fs.closeSync(fd);
  return lockInfo;
}

export function releaseEnvironmentLock(
  runId: string,
  lockFilePath: string = LOCK_FILE_PATH
): boolean {
  if (!fs.existsSync(lockFilePath)) return false;
  try {
    const raw = fs.readFileSync(lockFilePath, "utf-8");
    const lockInfo: LockInfo = JSON.parse(raw);
    if (lockInfo.runId === runId) {
      fs.unlinkSync(lockFilePath);
      return true;
    }
    return false; // Lock owned by a different runId
  } catch {
    return false;
  }
}

export function writeRunState(
  runState: RunState,
  stateFilePath: string = RUN_STATE_FILE_PATH
): void {
  // Ensure mode 0o600 (owner read/write only)
  const payload = JSON.stringify(runState, null, 2);
  fs.writeFileSync(stateFilePath, payload, { mode: 0o600 });
}

export function auditStateFileSecrecy(stateFilePath: string = RUN_STATE_FILE_PATH): {
  isSecretFree: boolean;
  foundSecrets: string[];
} {
  if (!fs.existsSync(stateFilePath)) {
    return { isSecretFree: true, foundSecrets: [] };
  }

  const content = fs.readFileSync(stateFilePath, "utf-8");
  const foundSecrets: string[] = [];

  // Patterns for passwords, secret keys, tokens, session cookies, credential-bearing DSNs
  const secretPatterns = [
    /password/i,
    /secret/i,
    /cookie/i,
    /token/i,
    /postgres:\/\/[^:]+:[^@]+@/i,
    /mysql:\/\/[^:]+:[^@]+@/i,
  ];

  for (const pattern of secretPatterns) {
    if (pattern.test(content)) {
      foundSecrets.push(pattern.toString());
    }
  }

  return {
    isSecretFree: foundSecrets.length === 0,
    foundSecrets,
  };
}

export function safeTerminateApiProcess(pid: number, apiExecPath: string = API_BINARY_PATH): boolean {
  if (!pid || pid <= 0) return false;
  const isOwned = verifyProcessOwnership(pid, "gradex-s5-playwright-api") || verifyProcessOwnership(pid, apiExecPath);
  if (!isOwned) {
    console.warn(`[E2E Safety] Refusing to signal unverified or stale process PID ${pid}`);
    return false;
  }

  try {
    process.kill(pid, "SIGTERM");
    let attempts = 0;
    while (attempts < 20) {
      try {
        process.kill(pid, 0);
        execFileSync("sleep", ["0.1"]);
        attempts++;
      } catch {
        return true; // Process exited gracefully
      }
    }
    // Bounded escalation to SIGKILL if still running
    try {
      process.kill(pid, "SIGKILL");
    } catch {}
    return true;
  } catch {
    return true;
  }
}

export async function cleanupRunResources(resources: CleanupResources): Promise<CleanupOutcome> {
  const outcome: CleanupOutcome = {
    apiTerminated: false,
    dbDropped: false,
    stateFileRemoved: false,
    lockReleased: false,
    errors: [],
  };

  const lockPath = resources.lockFilePath || LOCK_FILE_PATH;
  const statePath = resources.stateFilePath || RUN_STATE_FILE_PATH;
  const seedBin = resources.seedBinaryPath || SEED_BINARY_PATH;
  const adminDSN = resources.adminDSN || "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable";

  // 1. Terminate API process if owned
  if (resources.apiPid) {
    try {
      outcome.apiTerminated = safeTerminateApiProcess(resources.apiPid);
    } catch (e: any) {
      outcome.errors.push(`API termination error: ${e.message}`);
    }
  }

  // 2. Drop per-run isolated database if created
  if (resources.dbName && resources.dbName.startsWith("gradex_playwright_e2e_")) {
    try {
      execFileSync(seedBin, ["-drop", "-dbname", resources.dbName], {
        stdio: "pipe",
        env: {
          ...process.env,
          GRADEX_E2E_ADMIN_DB_URL: adminDSN,
          GRADEX_E2E_TARGET_DB_NAME: resources.dbName,
          GRADEX_E2E_TARGET_DB_URL: `postgres://gradex:gradex@localhost:5432/${resources.dbName}?sslmode=disable`,
          GRADEX_E2E_ALLOW_DATABASE_RESET: "1",
          DATABASE_URL: "postgres://gradex:gradex@localhost:5432/gradex?sslmode=disable",
        },
      });
      outcome.dbDropped = true;
    } catch (e: any) {
      outcome.errors.push(`Database drop error: ${e.message}`);
    }
  }

  // 3. Remove state file
  if (fs.existsSync(statePath)) {
    try {
      fs.unlinkSync(statePath);
      outcome.stateFileRemoved = true;
    } catch (e: any) {
      outcome.errors.push(`State file remove error: ${e.message}`);
    }
  }

  // 4. Release environment lock
  try {
    outcome.lockReleased = releaseEnvironmentLock(resources.runId, lockPath);
  } catch (e: any) {
    outcome.errors.push(`Lock release error: ${e.message}`);
  }

  return outcome;
}
