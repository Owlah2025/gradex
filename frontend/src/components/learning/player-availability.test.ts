import assert from "node:assert/strict";
import { test } from "node:test";
import { toggleFullscreen, toggleMediaPlayback } from "./player-controls-behavior";

/**
 * Which failures may render a Lesson unavailable.
 *
 * The Lesson Player collapses to a single `role="alert"` unavailable message when its `failed`
 * state is set — the entire control set is unmounted. That state belongs to a Lesson that has no
 * playable media: denied playback authorisation, unsupported HLS, or the media element's own
 * `error` event.
 *
 * It previously also fired when `video.play()` rejected. A rejected play promise is an ordinary,
 * transient outcome of one interaction — an interrupted play, an autoplay refusal — and the media
 * is still loaded and still authorised. Treating it as fatal destroyed a working player after a
 * single click, which is exactly how the Lesson Player's controls intermittently vanished
 * mid-test: the play/pause click at one step removed the seek slider the next step needed.
 *
 * These cases pin the boundary: a transient control failure is reported to its caller, and the
 * player-level availability decision is not one of the things that caller may do.
 */

/** Mirrors the Lesson Player's availability state and how each control failure feeds it. */
function playerAvailability() {
  let unavailable = false;
  return {
    get unavailable() {
      return unavailable;
    },
    /** Denied playback authorisation, unsupported HLS, or a media `error` event. */
    mediaIsUnplayable() {
      unavailable = true;
    },
    /** What the Lesson Player now passes as the play/pause and fullscreen failure handler. */
    transientControlFailure() {},
  };
}

test("a rejected play promise does not make the Lesson unavailable", async () => {
  const player = playerAvailability();
  const target = {
    paused: true,
    ended: false,
    play: async () => {
      throw new Error("The play() request was interrupted.");
    },
    pause: () => {},
  };

  assert.equal(toggleMediaPlayback(target, player.transientControlFailure), "play");
  await Promise.resolve();
  await Promise.resolve();

  assert.equal(
    player.unavailable,
    false,
    "a transient play rejection must not unmount the player and its controls"
  );
});

test("a refused fullscreen request does not make the Lesson unavailable", async () => {
  const player = playerAvailability();
  const container = {
    requestFullscreen: async () => {
      throw new Error("Permissions check failed");
    },
  };
  const fullscreenDocument = { fullscreenElement: null, exitFullscreen: async () => {} };

  assert.equal(toggleFullscreen(container, fullscreenDocument, player.transientControlFailure), "enter");
  await Promise.resolve();
  await Promise.resolve();

  assert.equal(player.unavailable, false, "a refused presentation change says nothing about the media");
});

test("genuinely unplayable media still makes the Lesson unavailable", () => {
  const player = playerAvailability();
  // The media element's `error` event, unsupported HLS, and denied playback authorisation all
  // route here, so real failure detection is preserved.
  player.mediaIsUnplayable();
  assert.equal(player.unavailable, true);
});

test("a successful play reports no failure at all", async () => {
  const player = playerAvailability();
  let played = 0;
  const target = {
    paused: true,
    ended: false,
    play: async () => {
      played += 1;
    },
    pause: () => {},
  };

  toggleMediaPlayback(target, player.transientControlFailure);
  await Promise.resolve();
  assert.equal(played, 1);
  assert.equal(player.unavailable, false);
});
