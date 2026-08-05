export type QualityLevel = {
  height: number;
  width?: number;
  bitrate?: number;
};

export type QualityOption = {
  levelIndex: number;
  label: string;
};

export function clampMediaValue(value: number, maximum: number): number {
  if (!Number.isFinite(value)) return 0;
  if (!Number.isFinite(maximum) || maximum <= 0) return 0;
  return Math.min(maximum, Math.max(0, value));
}

export function formatMediaTime(value: number): string {
  const safeValue = Number.isFinite(value) && value > 0 ? Math.floor(value) : 0;
  const hours = Math.floor(safeValue / 3600);
  const minutes = Math.floor((safeValue % 3600) / 60);
  const seconds = safeValue % 60;
  const paddedMinutes = hours > 0 ? String(minutes).padStart(2, "0") : String(minutes);
  const paddedSeconds = String(seconds).padStart(2, "0");
  return hours > 0 ? `${hours}:${paddedMinutes}:${paddedSeconds}` : `${paddedMinutes}:${paddedSeconds}`;
}

export function qualityOptions(levels: readonly QualityLevel[]): QualityOption[] {
  const seenLabels = new Map<string, number>();
  return levels.flatMap((level, levelIndex) => {
    if (!Number.isFinite(level.height) || level.height <= 0) return [];
    const baseLabel = `${Math.round(level.height)}p`;
    const occurrence = (seenLabels.get(baseLabel) ?? 0) + 1;
    seenLabels.set(baseLabel, occurrence);
    return [{ levelIndex, label: occurrence === 1 ? baseLabel : `${baseLabel} ${occurrence}` }];
  });
}
