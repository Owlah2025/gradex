import fs from "fs";
import {
  archiveApiLog,
  cleanupRunResources,
  closeApiLogDrain,
  RUN_STATE_FILE_PATH,
  RunState,
} from "../src/lib/api/e2e-infrastructure";

export default async function globalTeardown() {
  if (!fs.existsSync(RUN_STATE_FILE_PATH)) {
    console.log("[E2E Teardown] No run state file found; skipping teardown.");
    return;
  }

  let state: RunState | null = null;
  try {
    const raw = fs.readFileSync(RUN_STATE_FILE_PATH, "utf-8");
    state = JSON.parse(raw);
  } catch (err) {
    console.error("[E2E Teardown] Failed to parse run state file:", err);
  }

  if (!state) {
    try {
      fs.unlinkSync(RUN_STATE_FILE_PATH);
    } catch {}
    return;
  }

  console.log(`[E2E Teardown] Cleaning up resources for run ${state.runId}...`);
  const outcome = await cleanupRunResources({
    runId: state.runId,
    dbName: state.dbName,
    apiPid: state.pid,
  });

  // Close the drain before archiving: the descriptor is released with the run that opened it,
  // and the log is retained as evidence rather than deleted, because per-run limiter proof has to
  // outlive teardown to be observable at all.
  closeApiLogDrain();
  const archived = archiveApiLog(state.runId);
  if (archived) console.log(`[E2E Teardown] Archived run-owned API log to ${archived}`);

  if (outcome.errors.length > 0) {
    console.warn("[E2E Teardown] Cleanup encountered warnings:", outcome.errors);
  } else {
    console.log(`[E2E Teardown] Cleanup completed successfully for run ${state.runId}.`);
  }
}
