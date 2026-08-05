export const progressReportIntervalMilliseconds = 15_000;

export type ProgressReport = {
  position_seconds: number;
  asset_version_id: string;
};

export function progressPath(lessonID: string): string {
  return `/api/v1/learn/lessons/${encodeURIComponent(lessonID)}/progress`;
}

export function progressReport(positionSeconds: number, assetVersionID: string): ProgressReport | null {
  if (!Number.isFinite(positionSeconds) || positionSeconds < 0 || assetVersionID === "") return null;
  return { position_seconds: positionSeconds, asset_version_id: assetVersionID };
}
