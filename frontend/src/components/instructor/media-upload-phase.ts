import { isReadyState, isTerminalState } from "../../lib/api/media-upload";

/**
 * The phase a replaceable media surface should show on first render, derived
 * from what the server said rather than from anything the tab remembers.
 *
 * Lesson video and the public preview share this because they now share a
 * lifecycle: one idempotent request completes the upload and durably selects
 * it, FFmpeg runs on its own, and the browser's observation of that run is a
 * convenience. A reload therefore has to be able to reconstruct the truth from
 * the selected asset identifier and its current media state alone.
 */
export type RecoveredMediaPhase = "IDLE" | "PROCESSING_BACKGROUND" | "READY" | "FAILED";

export function recoverMediaPhase(
  assetVersionID?: string,
  assetState?: string,
): RecoveredMediaPhase {
  if (!assetVersionID) return "IDLE";
  // Older READY-only authoring responses did not project media state.
  if (!assetState || isReadyState(assetState)) return "READY";
  // UPLOADED means the bytes were never completed, so nothing is processing.
  if (assetState === "UPLOADED" || isTerminalState(assetState)) return "FAILED";
  return "PROCESSING_BACKGROUND";
}

export function isMediaProcessing(assetVersionID?: string, assetState?: string): boolean {
  return recoverMediaPhase(assetVersionID, assetState) === "PROCESSING_BACKGROUND";
}
