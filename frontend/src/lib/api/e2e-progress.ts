import { execFileSync } from "child_process";
import fs from "fs";
import { SEED_BINARY_PATH, RUN_STATE_FILE_PATH } from "./e2e-infrastructure";

/**
 * Test-runner-side Progress evidence.
 *
 * The seeder resolves Progress through the authoritative `progress` table, keyed by the
 * Student's Enrollment and the stable Course Lesson Identity. `found` is load-bearing:
 * a missing row is reported explicitly and must never be consumed as a zero-valued row,
 * because that is exactly how a broken helper lets an assertion pass without proving
 * persistence.
 */
export type ProgressSnapshot = {
  found: boolean;
  max_position_seconds: number;
  position_seconds: number;
  completed: boolean;
  completed_at: string;
  asset_version_id: string;
  updated_at: string;
};

export type ProgressQuery = {
  studentAccountID: string;
  courseID: string;
  lessonIdentityID: string;
};

export function parseProgressSnapshot(raw: string): ProgressSnapshot {
  const trimmed = raw.trim();
  if (trimmed === "") {
    throw new Error("Progress query returned no output; the seeder helper did not run.");
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch {
    throw new Error(`Progress query returned non-JSON output: ${trimmed.slice(0, 200)}`);
  }

  if (typeof parsed !== "object" || parsed === null || typeof (parsed as ProgressSnapshot).found !== "boolean") {
    throw new Error(`Progress query returned an unusable snapshot: ${trimmed.slice(0, 200)}`);
  }

  const snapshot = parsed as Partial<ProgressSnapshot>;
  return {
    found: snapshot.found === true,
    max_position_seconds: snapshot.max_position_seconds ?? 0,
    position_seconds: snapshot.position_seconds ?? 0,
    completed: snapshot.completed === true,
    completed_at: snapshot.completed_at ?? "",
    asset_version_id: snapshot.asset_version_id ?? "",
    updated_at: snapshot.updated_at ?? "",
  };
}

/**
 * Asserts a Progress row the test expects to exist actually exists. A `found:false` here is a
 * hard failure, never a zero-valued default — the defect this replaces silently reported
 * absence for every row because it queried a table that does not exist.
 */
export function requireProgressRow(snapshot: ProgressSnapshot, description: string): ProgressSnapshot {
  if (!snapshot.found) {
    throw new Error(
      `Expected a persisted Progress row for ${description}, but PostgreSQL reported none. ` +
        `A missing row is never evidence of unchanged state.`
    );
  }
  return snapshot;
}

/** Asserts the Student genuinely holds no Progress row for this Lesson. */
export function requireNoProgressRow(snapshot: ProgressSnapshot, description: string): void {
  if (snapshot.found) {
    throw new Error(`Expected no Progress row for ${description}, but PostgreSQL reported one.`);
  }
}

export function queryProgress(query: ProgressQuery): ProgressSnapshot {
  if (!fs.existsSync(RUN_STATE_FILE_PATH)) {
    throw new Error(`E2E run state is missing at ${RUN_STATE_FILE_PATH}; cannot read Progress evidence.`);
  }
  const state = JSON.parse(fs.readFileSync(RUN_STATE_FILE_PATH, "utf-8"));

  const output = execFileSync(
    SEED_BINARY_PATH,
    [
      "-query-progress",
      "-dbname",
      state.dbName,
      "-student",
      query.studentAccountID,
      "-course",
      query.courseID,
      "-lesson",
      query.lessonIdentityID,
    ],
    {
      env: {
        ...process.env,
        GRADEX_E2E_ALLOW_DATABASE_RESET: "1",
        GRADEX_E2E_ADMIN_DB_URL: "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable",
        GRADEX_E2E_TARGET_DB_NAME: state.dbName,
        GRADEX_E2E_TARGET_DB_URL: `postgres://gradex:gradex@localhost:5432/${state.dbName}?sslmode=disable`,
        DATABASE_URL: "postgres://gradex:gradex@localhost:5432/gradex?sslmode=disable",
      },
      encoding: "utf-8",
    }
  );

  return parseProgressSnapshot(output);
}

/**
 * Polls until the Progress row stops changing, so a test can capture a settled baseline after
 * an in-flight write (such as the reporter's legitimate `pagehide` write during navigation)
 * without guessing how long that write takes. Bounded: it fails rather than waiting forever.
 */
export async function waitForStableProgress(
  query: ProgressQuery,
  options: { stableForMs?: number; timeoutMs?: number; intervalMs?: number } = {}
): Promise<ProgressSnapshot> {
  const stableForMs = options.stableForMs ?? 600;
  const timeoutMs = options.timeoutMs ?? 15_000;
  const intervalMs = options.intervalMs ?? 150;
  const deadline = Date.now() + timeoutMs;

  let previous = JSON.stringify(queryProgress(query));
  let stableSince = Date.now();

  for (;;) {
    await new Promise((resolve) => setTimeout(resolve, intervalMs));
    const snapshot = queryProgress(query);
    const serialized = JSON.stringify(snapshot);
    if (serialized === previous) {
      if (Date.now() - stableSince >= stableForMs) return snapshot;
    } else {
      previous = serialized;
      stableSince = Date.now();
    }
    if (Date.now() >= deadline) {
      throw new Error(`Progress row never settled within ${timeoutMs}ms. Last snapshot: ${previous}`);
    }
  }
}

/**
 * Polls until the Progress row satisfies a bounded condition, so a test synchronises on the
 * committed database state instead of on a blind sleep.
 */
export async function waitForProgress(
  query: ProgressQuery,
  condition: (snapshot: ProgressSnapshot) => boolean,
  options: { timeoutMs?: number; intervalMs?: number; description?: string } = {}
): Promise<ProgressSnapshot> {
  const timeoutMs = options.timeoutMs ?? 10_000;
  const intervalMs = options.intervalMs ?? 100;
  const deadline = Date.now() + timeoutMs;

  let latest = queryProgress(query);
  while (!condition(latest)) {
    if (Date.now() >= deadline) {
      throw new Error(
        `Timed out after ${timeoutMs}ms waiting for Progress condition` +
          `${options.description ? ` (${options.description})` : ""}. Last snapshot: ${JSON.stringify(latest)}`
      );
    }
    await new Promise((resolve) => setTimeout(resolve, intervalMs));
    latest = queryProgress(query);
  }
  return latest;
}
