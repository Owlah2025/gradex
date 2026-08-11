import { execFileSync } from "child_process";
import fs from "fs";
import path from "path";
import { E2E_TMP_DIR } from "../../src/lib/api/e2e-infrastructure";
import { MEDIA_DIAGNOSTIC_BINARY_PATH, MEDIA_DIAGNOSTIC_STATE_PATH } from "./worker-process";

const ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

type DiagnosticState = {
  run_id: string;
  asset_version_id: string;
  runtime: Record<"api" | "worker", Record<string, string>>;
  api_log_path: string;
  worker_log_path: string;
};

function readState(): DiagnosticState | null {
  try {
    return JSON.parse(fs.readFileSync(MEDIA_DIAGNOSTIC_STATE_PATH, "utf8")) as DiagnosticState;
  } catch {
    return null;
  }
}

export function recordMediaAssetVersionID(assetVersionID: string): void {
  if (!ID.test(assetVersionID)) return;
  const state = readState();
  if (!state) return;
  state.asset_version_id = assetVersionID;
  fs.writeFileSync(MEDIA_DIAGNOSTIC_STATE_PATH, JSON.stringify(state), { mode: 0o600 });
}

export function captureFailureDiagnostic(): string | null {
  const state = readState();
  if (!state) return null;
  const directory = path.join(E2E_TMP_DIR, "gradex-media-e2e-evidence");
  fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
  const input = path.join(directory, `${state.run_id}-media-failure-input.json`);
  const output = path.join(directory, `${state.run_id}-media-failure.json`);
  fs.writeFileSync(input, JSON.stringify(state), { mode: 0o600 });
  try {
    execFileSync(MEDIA_DIAGNOSTIC_BINARY_PATH, [], {
      stdio: "pipe",
      timeout: 10_000,
      killSignal: "SIGKILL",
      env: { ...process.env, GRADEX_MEDIA_E2E_DIAGNOSTIC_INPUT: input, GRADEX_MEDIA_E2E_DIAGNOSTIC_OUTPUT: output },
    });
    return output;
  } finally {
    try { fs.unlinkSync(input); } catch {}
  }
}

export function clearDiagnosticState(): void {
  try { fs.unlinkSync(MEDIA_DIAGNOSTIC_STATE_PATH); } catch {}
}
