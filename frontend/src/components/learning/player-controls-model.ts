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

/**
 * The step a Student skips by.
 *
 * Ten seconds is the interval both the buttons and the keyboard shortcuts move, so a skip is the
 * same distance whichever way it was asked for. It lives here rather than in the two call sites,
 * because "the skip button and the skip shortcut agree" is the guarantee, not the number.
 */
export const SEEK_STEP_SECONDS = 10;

/**
 * The rates a Lesson may be watched at.
 *
 * The set is fixed and ordered slowest-first, because the control renders it in order and the
 * shortcut set does not offer a rate the menu cannot show. `1` is the rate every source starts at
 * and the one a Student returns to; it is a member of the set rather than a special case.
 */
export const PLAYBACK_RATES = [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2] as const;

export const DEFAULT_PLAYBACK_RATE = 1;

/** Whether a number is one of the offered rates. Anything else is refused rather than rounded. */
export function isPlaybackRate(value: number): boolean {
  return PLAYBACK_RATES.some((rate) => rate === value);
}

/**
 * The rate as the Student reads it.
 *
 * The multiplication sign, not the letter "x": the value is a number and a unit, so it stays in
 * Latin digits and is laid out left-to-right in both languages, exactly as the resolution labels
 * ("1080p") and the elapsed/duration reading already are.
 */
export function playbackRateLabel(rate: number): string {
  return `${rate}×`;
}

/** The value the rate `<select>` renders. */
export function playbackRateSelectValue(rate: number): string {
  return `rate-${rate}`;
}

/** Maps a `<select>` value back to a rate without trusting the control to supply a real one. */
export function playbackRateFromSelectValue(value: string): number | null {
  const match = /^rate-(\d+(?:\.\d+)?)$/.exec(value);
  if (!match) return null;
  const rate = Number(match[1]);
  return isPlaybackRate(rate) ? rate : null;
}
