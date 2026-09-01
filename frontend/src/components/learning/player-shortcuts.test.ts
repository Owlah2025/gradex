import assert from "node:assert/strict";
import { test } from "node:test";
import { playerShortcutFor, targetOwnsKeyboard, VOLUME_STEP } from "./player-shortcuts";

/** The player's own surface: a focusable container with no role of its own. */
const PLAYER = { tagName: "DIV", isContentEditable: false, role: null };

test("the player's keys do what the player says they do", () => {
  const bindings: [string, string][] = [
    [" ", "playPause"],
    ["k", "playPause"],
    ["K", "playPause"],
    ["ArrowLeft", "seekBackward"],
    ["j", "seekBackward"],
    ["ArrowRight", "seekForward"],
    ["l", "seekForward"],
    ["ArrowUp", "volumeUp"],
    ["ArrowDown", "volumeDown"],
    ["m", "toggleMute"],
    ["f", "toggleFullscreen"],
  ];
  for (const [key, shortcut] of bindings) {
    assert.equal(playerShortcutFor({ key, target: PLAYER }), shortcut, `${key} must be ${shortcut}`);
  }
  assert.equal(VOLUME_STEP > 0 && VOLUME_STEP < 1, true, "a volume step is a fraction of full volume");
});

test("keys the player has no business with are left to the page", () => {
  for (const key of ["Tab", "Enter", "Escape", "a", "1", "PageDown", "Home", "End", "p"]) {
    assert.equal(playerShortcutFor({ key, target: PLAYER }), null, `${key} must not be claimed`);
  }
});

/**
 * `I` was the Picture-in-Picture key, and it must stay unbound.
 *
 * Protected Student playback refuses Picture-in-Picture because the browser presents the bare
 * `<video>` element without the DOM watermark drawn over it. Taking the control off the bar without
 * unbinding the key would leave the same hole open from the keyboard, so this asserts the key is
 * claimed as nothing at all rather than merely mapped to a control that is no longer rendered.
 */
test("the Picture-in-Picture key is not claimed by the protected player", () => {
  for (const key of ["i", "I"]) {
    assert.equal(
      playerShortcutFor({ key, target: PLAYER }),
      null,
      `${key} must not reach Picture-in-Picture`,
    );
  }
});

/**
 * The rule that keeps the player from stealing keys.
 *
 * Every one of these is a control where the keystroke already means something: Space activates a
 * focused button, ArrowRight scrubs a focused range by one step, ArrowDown opens a `<select>`, and
 * every printable key belongs to a text field. A player that claimed them would break its own
 * controls first — the play button and the seek slider are exactly the elements listed here.
 */
test("a control that owns its keyboard keeps it", () => {
  const interactive = [
    { tagName: "INPUT" },
    { tagName: "input" },
    { tagName: "TEXTAREA" },
    { tagName: "SELECT" },
    { tagName: "BUTTON" },
    { tagName: "OPTION" },
    { tagName: "A" },
    { tagName: "SUMMARY" },
    { tagName: "IFRAME" },
    { tagName: "DIV", isContentEditable: true },
    { tagName: "DIV", contentEditable: "true" },
    { tagName: "DIV", role: "textbox" },
    { tagName: "DIV", role: "slider" },
    { tagName: "DIV", role: "combobox" },
    { tagName: "DIV", role: "menuitem" },
    { tagName: "SPAN", role: "BUTTON" },
  ];
  for (const target of interactive) {
    assert.equal(targetOwnsKeyboard(target), true, `${JSON.stringify(target)} owns its keyboard`);
    for (const key of [" ", "ArrowRight", "ArrowLeft", "ArrowUp", "ArrowDown", "k", "m", "f", "i"]) {
      assert.equal(
        playerShortcutFor({ key, target }),
        null,
        `${key} on ${JSON.stringify(target)} must reach the control, not the player`,
      );
    }
  }
});

test("the player's own surface is not an interactive control", () => {
  assert.equal(targetOwnsKeyboard(PLAYER), false);
  assert.equal(targetOwnsKeyboard({ tagName: "DIV", contentEditable: "false" }), false);
  assert.equal(targetOwnsKeyboard({ tagName: "DIV", contentEditable: "inherit" }), false);
  assert.equal(targetOwnsKeyboard({ tagName: "SPAN", role: "presentation" }), false);
  assert.equal(targetOwnsKeyboard(null), false);
  assert.equal(targetOwnsKeyboard(undefined), false);
});

test("a modified keystroke belongs to the browser, never to the player", () => {
  for (const modifier of ["altKey", "ctrlKey", "metaKey"] as const) {
    for (const key of [" ", "k", "f", "m", "ArrowRight", "i"]) {
      assert.equal(
        playerShortcutFor({ key, target: PLAYER, [modifier]: true }),
        null,
        `${modifier}+${key} must not be claimed`,
      );
    }
  }
  // Shift only changes the letter, so it does not disqualify the keystroke.
  assert.equal(playerShortcutFor({ key: "K", target: PLAYER }), "playPause");
});
