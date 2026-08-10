import fs from "fs";
import {
  archiveApiLog,
  cleanupRunResources,
  closeApiLogDrain,
  RUN_STATE_FILE_PATH,
  RunState,
} from "../../src/lib/api/e2e-infrastructure";
import { terminateWorker } from "./worker-process";

export default async function globalTeardown() {
  // The worker is stopped first: it holds the same database this teardown is
  // about to drop.
  terminateWorker();

  if (!fs.existsSync(RUN_STATE_FILE_PATH)) {
    console.log("[Media E2E Teardown] No run state file found; skipping teardown.");
    return;
  }

  let state: RunState | null = null;
  try {
    state = JSON.parse(fs.readFileSync(RUN_STATE_FILE_PATH, "utf-8"));
  } catch (err) {
    console.error("[Media E2E Teardown] Failed to parse run state file:", err);
  }
  if (!state) {
    try {
      fs.unlinkSync(RUN_STATE_FILE_PATH);
    } catch {}
    return;
  }

  const outcome = await cleanupRunResources({ runId: state.runId, dbName: state.dbName, apiPid: state.pid });
  closeApiLogDrain();
  const archived = archiveApiLog(state.runId);
  if (archived) console.log(`[Media E2E Teardown] Archived run-owned API log to ${archived}`);
  if (outcome.errors.length > 0) {
    console.warn("[Media E2E Teardown] Cleanup encountered warnings:", outcome.errors);
  }
}
