import { isReadyState, isTerminalState } from "../../lib/api/media-upload";

export type RecoveredLessonVideoPhase =
  | "IDLE"
  | "PROCESSING_BACKGROUND"
  | "READY"
  | "FAILED";

export function recoverLessonVideoPhase(
  assetVersionID?: string,
  assetState?: string,
): RecoveredLessonVideoPhase {
  if (!assetVersionID) return "IDLE";
  // Older READY-only authoring responses did not project media state.
  if (!assetState || isReadyState(assetState)) return "READY";
  if (assetState === "UPLOADED" || isTerminalState(assetState)) return "FAILED";
  return "PROCESSING_BACKGROUND";
}

export function isLessonVideoProcessing(assetVersionID?: string, assetState?: string): boolean {
  return recoverLessonVideoPhase(assetVersionID, assetState) === "PROCESSING_BACKGROUND";
}
