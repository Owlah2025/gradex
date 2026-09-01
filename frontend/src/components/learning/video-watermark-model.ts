/**
 * The watermark's movement, cadence and clock, as pure functions.
 *
 * All of it is deterministic in the Student's own attribution code, which is why none of it needs
 * `Math.random`: a random position chosen during render differs between the server pass and the
 * client pass and produces a hydration mismatch, and a random cadence cannot be tested. Seeding
 * from the code instead gives every Student a different resting place and a different rhythm while
 * keeping each Student's own behaviour reproducible.
 *
 * This is a deterrent, not a security boundary. Anyone can read this file; knowing where the
 * watermark will move next does not make a recording of it stop carrying the account that made it.
 */

/**
 * The resting places, named logically rather than left/right.
 *
 * Gradex renders in both Arabic and English, so `start`/`end` follow the reading direction and the
 * watermark sits in the near or far margin in both. The centre is deliberately not a zone: it is
 * where the lecture is. The bottom zones stop well clear of the control bar.
 */
export const WATERMARK_ZONES = [
  "top-start",
  "top-end",
  "mid-start",
  "mid-end",
  "bottom-start",
  "bottom-end",
] as const;

export type WatermarkZone = (typeof WATERMARK_ZONES)[number];

/**
 * Where each zone actually sits, as utility classes on the media surface.
 *
 * The bottom pair rests at 28% from the bottom. The control bar is roughly a quarter of the
 * picture at the smallest player size and much less than that in fullscreen, so the watermark is
 * above it at every size and never covers a control. The horizontal inset is a percentage so the
 * margin scales with the player instead of crowding a phone.
 */
export const WATERMARK_ZONE_CLASSES: Record<WatermarkZone, string> = {
  "top-start": "top-[5%] start-[4%] text-start",
  "top-end": "top-[5%] end-[4%] text-end",
  "mid-start": "top-[45%] start-[4%] text-start",
  "mid-end": "top-[45%] end-[4%] text-end",
  "bottom-start": "bottom-[28%] start-[4%] text-start",
  "bottom-end": "bottom-[28%] end-[4%] text-end",
};

/** The shortest and longest a single resting position may last. */
export const WATERMARK_MIN_HOLD_MS = 35_000;
export const WATERMARK_MAX_HOLD_MS = 55_000;

/** The cross-fade between two positions. Short enough not to read as animation. */
export const WATERMARK_FADE_MS = 600;

/**
 * How often the clock is re-read.
 *
 * The rendered value is coarse — hours and minutes — so a 30-second poll changes the text once a
 * minute and produces the identical string the other half of the time, which React discards
 * without re-rendering. Polling per second would be a re-render per second for a value that moves
 * sixty times more slowly.
 */
export const WATERMARK_CLOCK_TICK_MS = 30_000;

/**
 * A stable 32-bit seed for one Student, from their attribution code (FNV-1a).
 *
 * The code is already server-derived and per-Account, so it is the natural seed: two Students get
 * different movement, and the same Student gets the same movement on every Lesson and every
 * reload. Nothing here is reversible into the code, and nothing needs to be.
 */
export function watermarkSeed(code: string): number {
  let hash = 0x811c9dc5;
  for (let index = 0; index < code.length; index += 1) {
    hash ^= code.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash >>> 0;
}

/** An avalanche mix of a seed and a step, so consecutive steps are not consecutive values. */
function mix(seed: number, step: number): number {
  let hash = (seed ^ Math.imul(step + 1, 0x9e3779b9)) >>> 0;
  hash = Math.imul(hash ^ (hash >>> 16), 0x85ebca6b) >>> 0;
  hash = Math.imul(hash ^ (hash >>> 13), 0xc2b2ae35) >>> 0;
  return (hash ^ (hash >>> 16)) >>> 0;
}

/**
 * The zone for a given step.
 *
 * The step advances by a stride coprime with the number of zones, so six moves visit all six zones
 * and no move ever lands where the last one was. That is what makes cropping ineffective: a
 * recording long enough to matter contains the watermark in every corner and both middles, so
 * removing one region removes only one sixth of the occurrences.
 */
export function watermarkZoneAt(seed: number, step: number): WatermarkZone {
  const count = WATERMARK_ZONES.length;
  // 1 and 5 are the strides coprime with 6 that are not the identity rotation in both directions;
  // the seed chooses which way round this Student's cycle runs.
  const stride = seed % 2 === 0 ? 1 : count - 1;
  const base = seed % count;
  return WATERMARK_ZONES[(base + step * stride) % count];
}

/**
 * How long the position at `step` is held, in whole seconds between the two bounds.
 *
 * Varying it is the point: a fixed 45-second beat is a beat an editor can cut around, and it is
 * also the kind of metronome a Student starts to notice. Whole seconds keep the timer cheap and
 * the tests exact.
 */
export function watermarkHoldMS(seed: number, step: number): number {
  const spanSeconds = (WATERMARK_MAX_HOLD_MS - WATERMARK_MIN_HOLD_MS) / 1000;
  return WATERMARK_MIN_HOLD_MS + (mix(seed, step) % (spanSeconds + 1)) * 1000;
}

/**
 * The coarse wall-clock reading, as `HH:MM` in the Student's own local time.
 *
 * Formatted by hand rather than through `toLocaleTimeString` so the digits are Western in both
 * locales and the string is stable enough to assert on. This is a visible deterrent only — the
 * browser's clock is never sent to the backend and is never treated as evidence of anything.
 */
export function watermarkClock(now: Date): string {
  const hours = String(now.getHours()).padStart(2, "0");
  const minutes = String(now.getMinutes()).padStart(2, "0");
  return `${hours}:${minutes}`;
}

/**
 * The technical half of the watermark: masked address, attribution code, and the coarse clock when
 * one has been read yet.
 *
 * The clock is absent on the very first render because it is read in an effect rather than during
 * render, which is what keeps the server pass and the first client pass identical.
 */
export function watermarkDetailLine(
  maskedIdentifier: string,
  code: string,
  clock: string | null,
): string {
  const parts = [maskedIdentifier, code];
  if (clock) parts.push(clock);
  return parts.join(" • ");
}
