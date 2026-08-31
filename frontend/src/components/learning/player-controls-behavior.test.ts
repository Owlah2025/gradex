import assert from "node:assert/strict";
import { test } from "node:test";
import {
  createSurfaceGesture,
  gestureClick,
  gestureDoubleClick,
  resetSurfaceGesture,
  restoreMediaPlayback,
  seekBy,
  seekMedia,
  setMediaPlaybackRate,
  setMediaVolume,
  setQualityLevel,
  supportsFullscreen,
  supportsPictureInPicture,
  toggleFullscreen,
  toggleMediaMute,
  toggleMediaPlayback,
  togglePictureInPicture,
} from "./player-controls-behavior";

test("play and pause call the media element and handle rejected play promises generically", async () => {
  let played = 0;
  let paused = 0;
  let failure = 0;
  const target = {
    paused: true,
    ended: false,
    play: async () => { played += 1; throw new Error("browser playback rejection"); },
    pause: () => { paused += 1; },
  };
  assert.equal(toggleMediaPlayback(target, () => { failure += 1; }), "play");
  await Promise.resolve();
  assert.equal(played, 1);
  assert.equal(failure, 1);
  target.paused = false;
  assert.equal(toggleMediaPlayback(target, () => { failure += 1; }), "pause");
  assert.equal(paused, 1);
});

test("seeking is bounded, rejects invalid duration, and never marks completion", () => {
  const target = { currentTime: 12, duration: 100 };
  assert.equal(seekMedia(target, -10), 0);
  assert.equal(target.currentTime, 0);
  assert.equal(seekMedia(target, 120), 100);
  assert.equal(target.currentTime, 100);
  assert.equal(seekMedia({ currentTime: 12, duration: Number.NaN }, 20), null);
});

test("volume and mute use media state and restore the previous audible level", () => {
  const target = { volume: 0.8, muted: false };
  assert.equal(setMediaVolume(target, 0.4), 0.4);
  assert.equal(target.muted, false);
  assert.equal(toggleMediaMute(target, 0.4), 0.4);
  assert.equal(target.muted, true);
  assert.equal(toggleMediaMute(target, 0.4), 0.4);
  assert.equal(target.muted, false);
  assert.equal(setMediaVolume(target, Number.NaN), 0);
  assert.equal(target.muted, true);
});

test("quality selection accepts Auto and only available HLS indexes", () => {
  const target = { currentLevel: 1 };
  assert.equal(setQualityLevel(target, -1, [0, 1]), true);
  assert.equal(target.currentLevel, -1);
  assert.equal(setQualityLevel(target, 0, [0, 1]), true);
  assert.equal(setQualityLevel(target, 3, [0, 1]), false);
  assert.equal(target.currentLevel, 0);
});

test("fullscreen enters, exits, and keeps rejected API calls generic", async () => {
  let entered = 0;
  let exited = 0;
  let failures = 0;
  const target = { requestFullscreen: async () => { entered += 1; } };
  const documentState = {
    fullscreenElement: null as unknown,
    exitFullscreen: async () => { exited += 1; },
  };
  assert.equal(toggleFullscreen(target, documentState, () => { failures += 1; }), "enter");
  await Promise.resolve();
  assert.equal(entered, 1);
  documentState.fullscreenElement = target;
  assert.equal(toggleFullscreen(target, documentState, () => { failures += 1; }), "exit");
  await Promise.resolve();
  assert.equal(exited, 1);
  const rejectedTarget = { requestFullscreen: async () => { throw new Error("fullscreen rejected"); } };
  toggleFullscreen(rejectedTarget, { ...documentState, fullscreenElement: null }, () => { failures += 1; });
  await Promise.resolve();
  assert.equal(failures, 1);
});

test("a skip is bounded by the media, at both ends", () => {
  const target = { currentTime: 5, duration: 100 };
  // Before the start is the start, not a negative position.
  assert.equal(seekBy(target, -10), 0);
  assert.equal(target.currentTime, 0);

  target.currentTime = 40;
  assert.equal(seekBy(target, 10), 50);
  assert.equal(target.currentTime, 50);

  // Past the end is the end. It is never a completion, and never past `duration`.
  target.currentTime = 95;
  assert.equal(seekBy(target, 10), 100);
  assert.equal(target.currentTime, 100);

  // An unknown duration refuses the skip rather than guessing where the end is.
  const unknown = { currentTime: 12, duration: Number.NaN };
  assert.equal(seekBy(unknown, 10), null);
  assert.equal(unknown.currentTime, 12);

  // A non-finite offset never reaches the element.
  const untouched = { currentTime: 20, duration: 100 };
  assert.equal(seekBy(untouched, Number.NaN), null);
  assert.equal(untouched.currentTime, 20);
});

test("playback rate applies only offered rates and reports what the element took", () => {
  const target = { playbackRate: 1 };
  assert.equal(setMediaPlaybackRate(target, 1.5), 1.5);
  assert.equal(target.playbackRate, 1.5);
  assert.equal(setMediaPlaybackRate(target, 0.5), 0.5);
  assert.equal(target.playbackRate, 0.5);
  // An unoffered rate is refused rather than rounded to a neighbour.
  assert.equal(setMediaPlaybackRate(target, 3), null);
  assert.equal(target.playbackRate, 0.5, "a refused rate must not change the element");
  assert.equal(setMediaPlaybackRate(target, Number.NaN), null);
  assert.equal(target.playbackRate, 0.5);
});

test("fullscreen capability is a property of the element, and is false before there is one", () => {
  const fullscreenDocument = { exitFullscreen: async () => {} };
  const container = { requestFullscreen: async () => {} };

  // The Lesson Player renders a placeholder while playback authorisation is in flight. Asked in
  // that commit, the answer is "no" — which is correct, and is why the caller must ask again.
  assert.equal(supportsFullscreen(null, fullscreenDocument), false);
  assert.equal(supportsFullscreen(undefined, fullscreenDocument), false);

  // Asked again once the container is mounted, the same browser answers "yes".
  assert.equal(supportsFullscreen(container, fullscreenDocument), true);

  // A browser without the API is still unsupported with a mounted node.
  assert.equal(supportsFullscreen({}, fullscreenDocument), false);
  assert.equal(supportsFullscreen(container, {}), false);
  assert.equal(supportsFullscreen(container, null), false);
});

test("Picture-in-Picture is offered only when the API, the document and the element all allow it", () => {
  const video = { requestPictureInPicture: async () => {} };
  const pipDocument = {
    pictureInPictureEnabled: true,
    pictureInPictureElement: null as unknown,
    exitPictureInPicture: async () => {},
  };

  assert.equal(supportsPictureInPicture(pipDocument, video), true);
  // No API at all — the control must not be rendered.
  assert.equal(supportsPictureInPicture(pipDocument, {}), false);
  // Disabled by permissions policy.
  assert.equal(supportsPictureInPicture({ ...pipDocument, pictureInPictureEnabled: false }, video), false);
  assert.equal(supportsPictureInPicture({ exitPictureInPicture: async () => {} }, video), false);
  // The element itself opts out.
  assert.equal(supportsPictureInPicture(pipDocument, { ...video, disablePictureInPicture: true }), false);
  // Nothing mounted yet.
  assert.equal(supportsPictureInPicture(pipDocument, null), false);
  assert.equal(supportsPictureInPicture(null, video), false);
});

test("Picture-in-Picture enters, exits, and keeps a refusal from claiming success", async () => {
  let entered = 0;
  let exited = 0;
  let failures = 0;
  const video = { requestPictureInPicture: async () => { entered += 1; } };
  const pipDocument = {
    pictureInPictureEnabled: true,
    pictureInPictureElement: null as unknown,
    exitPictureInPicture: async () => { exited += 1; },
  };

  assert.equal(togglePictureInPicture(video, pipDocument, () => { failures += 1; }), "enter");
  await Promise.resolve();
  assert.equal(entered, 1);

  // The document owns the truth about which element is presented.
  pipDocument.pictureInPictureElement = video;
  assert.equal(togglePictureInPicture(video, pipDocument, () => { failures += 1; }), "exit");
  await Promise.resolve();
  assert.equal(exited, 1);

  // An unsupported browser is told so rather than throwing at the Student.
  assert.equal(togglePictureInPicture({}, pipDocument, () => { failures += 1; }), "unsupported");
  assert.equal(failures, 0);

  const refusing = { requestPictureInPicture: async () => { throw new Error("PiP refused"); } };
  togglePictureInPicture(refusing, { ...pipDocument, pictureInPictureElement: null }, () => { failures += 1; });
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(failures, 1, "a refusal is reported to the caller, not swallowed");
});

/* ------------------------------------- the media-surface click gesture ---- */

/**
 * A media element whose `play()` is genuinely asynchronous.
 *
 * This is the whole point of these cases. A real element does not become unpaused when `play()` is
 * called — it becomes unpaused when the request settles, which is after the `dblclick` handler has
 * already run. `paused` therefore stays `true` for the entire gesture, and any code that decides
 * what to do by reading it back requests play a second time.
 */
function pendingPlayMedia(playing: boolean) {
  let settlePlay: (() => void) | null = null;
  const media = {
    paused: !playing,
    ended: false,
    playRequests: 0,
    pauseCalls: 0,
    play() {
      media.playRequests += 1;
      return new Promise<void>((resolve) => {
        settlePlay = () => {
          media.paused = false;
          resolve();
        };
      });
    },
    pause() {
      media.pauseCalls += 1;
      media.paused = true;
      // A pending play request is aborted by `pause()`, exactly as the media spec requires.
      settlePlay = null;
    },
    /** Let an outstanding play request complete, the way the browser eventually would. */
    async settle() {
      settlePlay?.();
      settlePlay = null;
      await Promise.resolve();
    },
  };
  return media;
}

test("a paused Lesson is still paused after a double-click", async () => {
  const media = pendingPlayMedia(false);
  const gesture = createSurfaceGesture();

  assert.equal(gestureClick(gesture, media, 1, () => {}), "play");
  assert.equal(gesture.playingBeforeGesture, false, "the pre-gesture state is captured before the toggle acts");
  // The element has NOT become unpaused yet — this is the state the old implementation read back.
  assert.equal(media.paused, true);

  assert.equal(gestureClick(gesture, media, 2, () => {}), "ignored", "the second click of a gesture does nothing");
  assert.equal(gesture.playingBeforeGesture, false, "an ignored click must not overwrite the captured state");

  assert.equal(gestureDoubleClick(gesture, media, () => {}), "pause");
  await media.settle();

  assert.equal(media.paused, true, "a double-click on a paused Lesson must leave it paused");
  assert.equal(media.playRequests, 1, "the gesture must not request play a second time");
  assert.equal(media.pauseCalls, 1);
});

test("a playing Lesson is still playing after a double-click", async () => {
  const media = pendingPlayMedia(true);
  const gesture = createSurfaceGesture();

  assert.equal(gestureClick(gesture, media, 1, () => {}), "pause");
  assert.equal(gesture.playingBeforeGesture, true);
  assert.equal(media.paused, true, "the first click paused it, as a single click should");

  assert.equal(gestureDoubleClick(gesture, media, () => {}), "play");
  await media.settle();

  assert.equal(media.paused, false, "a double-click on a running Lesson must leave it running");
  assert.equal(media.playRequests, 1);
});

/**
 * The defect, stated as arithmetic.
 *
 * Toggling again during `dblclick` reads `paused === true` — because the first click's `play()` has
 * not settled — and so requests play a second time. The restore path is given the same in-flight
 * element and still ends paused, because it is driven by the recorded state rather than by the
 * element.
 */
test("an in-flight play from the first click cannot leave a paused-origin double-click playing", async () => {
  const naive = pendingPlayMedia(false);
  toggleMediaPlayback(naive, () => {});
  assert.equal(naive.paused, true, "play() has been requested but has not settled");
  // What toggling a second time would do, and why it is wrong.
  assert.equal(toggleMediaPlayback(naive, () => {}), "play");
  assert.equal(naive.playRequests, 2, "a second toggle requests play again instead of undoing it");
  await naive.settle();
  assert.equal(naive.paused, false, "the naive gesture leaves a Lesson running the Student never started");

  const media = pendingPlayMedia(false);
  const gesture = createSurfaceGesture();
  gestureClick(gesture, media, 1, () => {});
  assert.equal(media.paused, true, "the same in-flight state the naive path misread");
  gestureDoubleClick(gesture, media, () => {});
  await media.settle();
  assert.equal(media.paused, true, "the restore path is not fooled by the in-flight request");
  assert.equal(media.playRequests, 1);
});

test("a single click still toggles playback immediately, with no gesture delay", () => {
  const paused = pendingPlayMedia(false);
  const firstGesture = createSurfaceGesture();
  assert.equal(gestureClick(firstGesture, paused, 1, () => {}), "play");
  assert.equal(paused.playRequests, 1, "play is requested on the click itself, not after a wait");

  const running = pendingPlayMedia(true);
  const secondGesture = createSurfaceGesture();
  assert.equal(gestureClick(secondGesture, running, 1, () => {}), "pause");
  assert.equal(running.paused, true);
  assert.equal(running.pauseCalls, 1);
});

test("the double-click gesture toggles fullscreen and leaves playback where it was", async () => {
  for (const startedPlaying of [false, true]) {
    const media = pendingPlayMedia(startedPlaying);
    const gesture = createSurfaceGesture();
    const container = { requestFullscreen: async () => {} };
    const fullscreenDocument = { fullscreenElement: null as unknown, exitFullscreen: async () => {} };

    // The whole gesture, in the order a browser delivers it.
    gestureClick(gesture, media, 1, () => {});
    gestureClick(gesture, media, 2, () => {});
    gestureDoubleClick(gesture, media, () => {});
    assert.equal(toggleFullscreen(container, fullscreenDocument, () => {}), "enter");
    await media.settle();

    assert.equal(media.paused, !startedPlaying, `double-click changed playback (started playing: ${startedPlaying})`);
  }
});

test("gesture state is spent on completion and cleared by a source replacement", async () => {
  const media = pendingPlayMedia(false);
  const gesture = createSurfaceGesture();

  gestureClick(gesture, media, 1, () => {});
  gestureDoubleClick(gesture, media, () => {});
  assert.equal(gesture.playingBeforeGesture, null, "a completed gesture is spent");

  // A `dblclick` with nothing recorded touches playback at all.
  const before = { play: media.playRequests, pause: media.pauseCalls };
  assert.equal(gestureDoubleClick(gesture, media, () => {}), "unknown");
  assert.equal(media.playRequests, before.play);
  assert.equal(media.pauseCalls, before.pause);

  // A source replacement or unmount ends a gesture that never completed.
  gestureClick(gesture, media, 1, () => {});
  assert.notEqual(gesture.playingBeforeGesture, null);
  resetSurfaceGesture(gesture);
  assert.equal(gesture.playingBeforeGesture, null);
  await media.settle();
});

test("a play rejected while restoring stays a transient control failure", async () => {
  let failures = 0;
  let unavailable = false;
  const media = {
    paused: true,
    ended: false,
    play: async () => { throw new Error("The play() request was interrupted."); },
    pause: () => {},
  };

  assert.equal(restoreMediaPlayback(media, true, () => { failures += 1; }), "play");
  await Promise.resolve();
  await Promise.resolve();

  assert.equal(failures, 1, "the rejection is reported to the caller");
  assert.equal(unavailable, false, "and never condemns the Lesson");
});
