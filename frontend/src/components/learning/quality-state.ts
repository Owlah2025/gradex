import { qualityOptions, type QualityLevel, type QualityOption } from "./player-controls-model";

/**
 * The Student's selected quality mode is a distinct thing from the level HLS is currently
 * rendering.
 *
 * hls.js emits `LEVEL_SWITCHED` for every adaptive switch it makes on its own. Folding that
 * event into the selection — the defect this module replaces — silently converted an Auto
 * selection into a manual one the Student never made, and then reported that manual level back
 * as though it were their choice. Adaptive mode was still running underneath, so the control
 * claimed a level was pinned while nothing was pinned.
 *
 * Selection changes only when the Student changes it, or when the source is replaced. Active
 * level changes only from player events. Neither writes the other.
 */
export type QualitySelection = { mode: "auto" } | { mode: "manual"; levelIndex: number };

export const autoSelection: QualitySelection = { mode: "auto" };

export type QualityState = {
  /** What the Student chose. Auto until they choose otherwise. */
  selection: QualitySelection;
  /** What HLS is rendering right now, or -1 when not yet known. Never authoritative for selection. */
  activeLevelIndex: number;
  options: QualityOption[];
  /**
   * Identifies the HLS instance the state belongs to. A Lesson or source replacement mints a new
   * key, so an in-flight event from the destroyed instance cannot reach the new player's state.
   */
  sourceKey: string;
};

export function initialQualityState(sourceKey: string): QualityState {
  return { selection: autoSelection, activeLevelIndex: -1, options: [], sourceKey };
}

/** A source or Lesson replacement always returns the Student to Auto — a manual pin belongs to the media it was chosen for. */
export function sourceReplaced(sourceKey: string): QualityState {
  return initialQualityState(sourceKey);
}

/** Levels parsed for a source. Selection is untouched: parsing levels is not a Student action. */
export function levelsLoaded(state: QualityState, levels: readonly QualityLevel[], sourceKey: string): QualityState {
  if (sourceKey !== state.sourceKey) return state;
  return { ...state, options: qualityOptions(levels) };
}

/**
 * An adaptive or manual switch HLS has completed. This records what is playing and nothing else —
 * in particular it never promotes Auto to manual.
 */
export function levelSwitched(state: QualityState, levelIndex: number, sourceKey: string): QualityState {
  if (sourceKey !== state.sourceKey) return state;
  if (!Number.isInteger(levelIndex) || levelIndex < 0) return { ...state, activeLevelIndex: -1 };
  return { ...state, activeLevelIndex: levelIndex };
}

/** The Student chose Auto: adaptive selection resumes and the active level is left to the player. */
export function selectAuto(state: QualityState): QualityState {
  return { ...state, selection: autoSelection };
}

/** The Student pinned a level. Rejected when the level is not one of the offered options. */
export function selectManual(state: QualityState, levelIndex: number): QualityState {
  if (!state.options.some((option) => option.levelIndex === levelIndex)) return state;
  return { ...state, selection: { mode: "manual", levelIndex } };
}

/** The level hls.js must be driven to: -1 is its adaptive sentinel, which is what Auto means. */
export function intendedHlsLevel(state: QualityState): number {
  return state.selection.mode === "auto" ? -1 : state.selection.levelIndex;
}

export type QualityTextLabels = { quality: string; auto: string };

/**
 * The accessible value of the control. It names the Student's selection, never an internal hls.js
 * level index, and when Auto is selected it also names the level actually being rendered — so the
 * control can be honest about both without ever implying a level is pinned.
 */
export function qualityValueText(state: QualityState, labels: QualityTextLabels): string {
  const selection = state.selection;
  if (selection.mode === "manual") {
    const pinned = state.options.find((option) => option.levelIndex === selection.levelIndex);
    return pinned ? pinned.label : labels.auto;
  }
  const active = state.options.find((option) => option.levelIndex === state.activeLevelIndex);
  return active ? `${labels.auto} (${active.label})` : labels.auto;
}

/** The value the `<select>` renders. Auto is its own entry, distinct from every level. */
export function qualitySelectValue(state: QualityState): string {
  return state.selection.mode === "auto" ? "auto" : `level-${state.selection.levelIndex}`;
}

/** Maps a `<select>` value back to a selection without trusting the string to be a level index. */
export function applySelectValue(state: QualityState, value: string): QualityState {
  if (value === "auto") return selectAuto(state);
  const match = /^level-(\d+)$/.exec(value);
  if (!match) return state;
  return selectManual(state, Number(match[1]));
}
