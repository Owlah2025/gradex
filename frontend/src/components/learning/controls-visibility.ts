/**
 * When the control overlay is on screen.
 *
 * The rule has to hold three things at once, and each of them is a way a real player annoys the
 * person using it: controls that never leave sit on top of the Lesson; controls that leave while
 * the Student is reaching for them cannot be clicked; and controls that leave while a menu is open
 * take the menu with them.
 *
 * So visibility is not a timer, it is a decision about three facts — whether the media is running,
 * whether the Student has touched the player recently, and whether they are currently holding the
 * controls open. The timer only decides when the second of those stops being true, which is what
 * keeps this rule provable without one.
 */
export type ControlsActivity = {
  /** Media is running. A paused player has nothing to get out of the way of. */
  playing: boolean;
  /** Pointer, touch, or keyboard activity inside the idle window. */
  recentActivity: boolean;
  /**
   * The Student is holding the controls open: focus is inside them, the pointer is over them, or
   * one of their menus is open. None of these may be interrupted by an idle timer.
   */
  interactionHeld: boolean;
};

/** How long the player stays quiet before the overlay withdraws. */
export const CONTROLS_IDLE_MS = 2600;

export function controlsVisible(activity: ControlsActivity): boolean {
  if (!activity.playing) return true;
  if (activity.interactionHeld) return true;
  return activity.recentActivity;
}

/**
 * Whether the pointer should be hidden along with the controls.
 *
 * The cursor is part of the overlay. Leaving it floating over a full-screen Lesson is the one part
 * of "controls hidden" that browsers do not do for you.
 */
export function pointerHidden(activity: ControlsActivity): boolean {
  return !controlsVisible(activity);
}
