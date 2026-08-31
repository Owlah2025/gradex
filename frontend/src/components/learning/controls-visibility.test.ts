import assert from "node:assert/strict";
import { test } from "node:test";
import { CONTROLS_IDLE_MS, controlsVisible, pointerHidden } from "./controls-visibility";

test("a paused player always shows its controls", () => {
  assert.equal(controlsVisible({ playing: false, recentActivity: false, interactionHeld: false }), true);
  assert.equal(controlsVisible({ playing: false, recentActivity: true, interactionHeld: false }), true);
  assert.equal(pointerHidden({ playing: false, recentActivity: false, interactionHeld: false }), false);
});

test("controls withdraw only while the media is running and nothing is happening", () => {
  assert.equal(controlsVisible({ playing: true, recentActivity: false, interactionHeld: false }), false);
  assert.equal(pointerHidden({ playing: true, recentActivity: false, interactionHeld: false }), true);
});

test("recent pointer, touch or keyboard activity brings them back", () => {
  assert.equal(controlsVisible({ playing: true, recentActivity: true, interactionHeld: false }), true);
  assert.equal(pointerHidden({ playing: true, recentActivity: true, interactionHeld: false }), false);
});

/**
 * The case a plain idle timer gets wrong.
 *
 * Focus inside the bar, the pointer resting on it, or an open menu are all the Student holding the
 * controls open. Withdrawing under any of them takes away the control being operated — a menu list
 * disappears mid-read, or a keyboard user tabs into a bar that is no longer on screen.
 */
test("controls stay while the Student is holding them open, however long they take", () => {
  assert.equal(controlsVisible({ playing: true, recentActivity: false, interactionHeld: true }), true);
  assert.equal(pointerHidden({ playing: true, recentActivity: false, interactionHeld: true }), false);
});

test("the idle window is long enough to reach a control and short enough to get out of the way", () => {
  assert.equal(Number.isInteger(CONTROLS_IDLE_MS), true);
  assert.ok(CONTROLS_IDLE_MS >= 1500, `${CONTROLS_IDLE_MS}ms withdraws before a Student can cross the bar`);
  assert.ok(CONTROLS_IDLE_MS <= 5000, `${CONTROLS_IDLE_MS}ms leaves the overlay sitting on the Lesson`);
});
