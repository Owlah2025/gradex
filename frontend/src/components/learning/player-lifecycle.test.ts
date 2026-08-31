import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import { ar } from "../../lib/i18n/dictionaries/ar";
import { en } from "../../lib/i18n/dictionaries/en";

/**
 * The Lesson Player's lifecycle and presentation contract.
 *
 * These are source assertions in the same style as the S5 payload contract, and they exist for the
 * defects that are invisible in a green run: a capability decided against a node that does not
 * exist yet, a keyboard listener that reaches beyond the player, a control overlay quietly
 * unmounted instead of faded, a storage URL built on the client. Each one is a rule the player
 * already follows; what is written here is what stops the next edit from undoing it silently.
 */

function frontendRoot(): string {
  return process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend");
}

function shipped(relativePath: string): string {
  return fs.readFileSync(path.join(frontendRoot(), relativePath), "utf8");
}

/** The source with its comments removed — several rules here are documented in prose beside them. */
function code(relativePath: string): string {
  return shipped(relativePath)
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/^\s*\/\/.*$/gm, "");
}

const PLAYER = "src/components/learning/lesson-player.tsx";
const CONTROLS = "src/components/learning/player-controls.tsx";

/** The dependency array of the effect that contains a given statement. */
function effectDependencies(source: string, statement: string): string {
  const start = source.indexOf(statement);
  assert.notEqual(start, -1, `the player no longer contains \`${statement}\``);
  const dependencies = /\}, \[([^\]]*)\]\);/.exec(source.slice(start));
  assert.ok(dependencies, `no dependency array follows \`${statement}\``);
  return dependencies![1].trim();
}

// --- Capabilities are decided against the node they are about ----------------

/**
 * The fullscreen lifecycle defect, pinned.
 *
 * The player renders a placeholder while playback authorisation is in flight, so its container does
 * not exist in its first commit. Support was previously computed in an effect with an empty
 * dependency array, which therefore ran against a `null` ref and recorded "unsupported" on every
 * browser — permanently, because nothing ever asked again. The capability must be keyed to the
 * mounted node, so it is re-decided the moment the node is really there.
 */
test("fullscreen support is decided against the mounted container, not the first commit", () => {
  const player = code(PLAYER);
  assert.match(player, /supportsFullscreen\(playerElement, document\)/);
  assert.equal(
    effectDependencies(player, "setFullscreenSupported(supportsFullscreen"),
    "playerElement",
    "fullscreen capability must be keyed to the container node",
  );
  // The node reaches state through a callback ref, which is what makes it a dependency at all.
  assert.match(player, /ref=\{setPlayerElement\}/);
  assert.match(player, /const \[playerElement, setPlayerElement\] = useState<HTMLDivElement \| null>\(null\)/);
});

test("fullscreen presents the whole player, and follows the document rather than assuming", () => {
  const player = code(PLAYER);
  assert.match(player, /toggleFullscreenBehavior\(playerElement, document/, "fullscreen must target the player container");
  assert.doesNotMatch(player, /toggleFullscreenBehavior\(video/, "fullscreening the bare video loses the controls");
  assert.match(player, /document\.addEventListener\("fullscreenchange", syncFullscreen\)/);
  assert.match(player, /document\.removeEventListener\("fullscreenchange", syncFullscreen\)/);
  assert.match(player, /document\.fullscreenElement === playerElement/);
  // Double-click is a fullscreen gesture where fullscreen exists, and nothing else.
  assert.match(player, /onDoubleClick=\{surfaceDoubleClicked\}/);
});

/**
 * The gesture restores a recorded state; it never re-reads the element.
 *
 * `play()` is asynchronous, so between the first click of a double-click and the `dblclick` the
 * element still reports `paused`. A second toggle driven by that in-flight state requests play
 * again and leaves a Lesson running that the Student never started, which is why the handler must
 * go through the gesture controller and must not call the plain toggle a second time.
 */
test("a double-click restores the pre-gesture playback state instead of toggling again", () => {
  const player = code(PLAYER);
  const doubleClick = player.slice(player.indexOf("const surfaceDoubleClicked = useCallback("));
  const body = doubleClick.slice(0, doubleClick.indexOf("}, ["));
  assert.match(body, /gestureDoubleClick\(surfaceGestureRef\.current, video, \(\) => \{\}\)/);
  assert.match(body, /toggleFullscreen\(\)/, "the gesture must still toggle fullscreen");
  assert.doesNotMatch(body, /togglePlayPause\(\)/, "toggling again re-reads an in-flight play request");

  // The first click records the state before it acts, and the second click is ignored by `detail`.
  assert.match(player, /gestureClick\(surfaceGestureRef\.current, video, event\.detail, \(\) => \{\}\)/);
  // And no gesture survives a source replacement or an unmount.
  assert.match(player, /resetSurfaceGesture\(surfaceGesture\)/);
  // The fix introduces no click discriminator timer: the idle countdown is still the only one.
  assert.equal((player.match(/setTimeout\(/g) ?? []).length, 1);
});

test("Picture-in-Picture capability and state are keyed to the mounted media element", () => {
  const player = code(PLAYER);
  assert.equal(
    effectDependencies(player, "setPictureInPictureSupported(supportsPictureInPicture"),
    "videoElement",
    "Picture-in-Picture capability must be keyed to the video node",
  );
  // The browser can be left from its own window, so its events are the only honest state.
  assert.match(player, /addEventListener\("enterpictureinpicture", entered\)/);
  assert.match(player, /addEventListener\("leavepictureinpicture", left\)/);
  assert.match(player, /removeEventListener\("enterpictureinpicture", entered\)/);
  assert.match(player, /removeEventListener\("leavepictureinpicture", left\)/);
});

/**
 * No capability is ever waited for.
 *
 * A timeout would have hidden the fullscreen defect instead of fixing it, and would have made the
 * control's appearance depend on how fast the machine was. The only timer in the player is the
 * control overlay's idle countdown, which is a presentation decision rather than a capability one.
 */
test("no capability is decided on a timer", () => {
  const player = code(PLAYER);
  const timers = player.match(/setTimeout\(/g) ?? [];
  assert.equal(timers.length, 1, "the idle countdown is the only timer the player is allowed");
  assert.match(player, /idleTimerRef\.current = setTimeout\(/);
  assert.match(player, /clearTimeout\(idleTimerRef\.current\)/, "the idle timer is cleared on unmount");
});

// --- The player's keyboard stays inside the player ---------------------------

test("the player listens on itself, never on the document or the window", () => {
  const player = code(PLAYER);
  assert.match(player, /onKeyDown=\{handleKeyDown\}/);
  for (const global of ["document", "window"]) {
    assert.doesNotMatch(
      player,
      new RegExp(`${global}\\.addEventListener\\(\\s*"key`),
      `a ${global}-level key listener takes keystrokes from the rest of the Lesson`,
    );
  }
  // The browser default is suppressed only after a shortcut has been claimed.
  assert.match(player, /if \(!shortcut\) return;\s*\n\s*event\.preventDefault\(\);/);
});

// --- The protected playback flow is unchanged --------------------------------

test("the player still loads only the manifest the server authorised", () => {
  const player = code(PLAYER);
  assert.match(player, /requestPlayback\(lessonID, locale, currentCSRFToken\(\)\)/);
  assert.match(player, /hls\.loadSource\(playback\.manifest_url\)/);
  assert.match(player, /video\.src = playback\.manifest_url/);
  // Native HLS — Safari and iOS — is still reached, and still through the same authorised URL.
  assert.match(player, /video\.canPlayType\("application\/vnd\.apple\.mpegurl"\)/);
  // Nothing about storage is the client's business.
  assert.doesNotMatch(player, /https?:\/\//, "the player builds a URL of its own");
  assert.doesNotMatch(player, /\.m3u8|\.ts["'`]|bucket|r2\.|amazonaws|X-Amz/i);
});

test("progress reporting and the resume position survive the rebuild", () => {
  const player = code(PLAYER);
  assert.match(player, /useProgressReporter\(videoRef, lessonID, playback\?\.asset_version_id \?\? null\)/);
  assert.match(player, /clampMediaValue\(initialPositionSeconds/, "the saved position is still restored");
  assert.match(player, /addEventListener\("loadedmetadata", seekToSavedPosition, \{ once: true \}\)/);
  assert.match(player, /removeEventListener\("loadedmetadata", seekToSavedPosition\)/);
  // Every media subscription this mount made is removed with it.
  assert.match(player, /mediaEvents\.forEach\(\(eventName\) => video\.removeEventListener\(eventName, syncMediaState\)\)/);
  assert.match(player, /hls\.destroy\(\)/);
});

test("a transient control failure still never makes the Lesson unavailable", () => {
  const player = code(PLAYER);
  // `setFailed(true)` belongs to unplayable media only: denied authorisation, unsupported HLS, and
  // the element's own `error` event.
  const failures = player.match(/setFailed\(true\)/g) ?? [];
  assert.equal(failures.length, 3, "only unplayable media may make the Lesson unavailable");
  for (const control of ["toggleMediaPlayback", "toggleFullscreenBehavior", "togglePictureInPictureBehavior"]) {
    assert.match(
      player,
      new RegExp(`${control}\\([^)]*\\(\\) => \\{\\}\\)`),
      `${control} must report failure to a handler that does not condemn the Lesson`,
    );
  }
});

// --- A player overlay, not a form -------------------------------------------

test("the controls are an overlay on the media, and are faded rather than unmounted", () => {
  const controls = code(CONTROLS);
  assert.match(controls, /data-player-controls/);
  assert.match(controls, /absolute inset-x-0 bottom-0/, "the bar sits on the media it operates");
  assert.match(controls, /bg-gradient-to-t from-gx-navy/, "a gradient keeps the bar legible over any frame");
  // Hidden is still mounted and still focusable, so a keyboard user can always reach the controls.
  assert.match(controls, /visible \? "opacity-100" : "pointer-events-none opacity-0"/);
  assert.doesNotMatch(controls, /\{visible \? \(/, "the bar must not be unmounted when it withdraws");
});

test("every control on the bar is a real, named, keyboard-operable element", () => {
  const controls = code(CONTROLS);
  // No div-with-onClick controls: the bar is buttons, ranges and selects.
  assert.doesNotMatch(controls, /<div[^>]*onClick=/);
  assert.match(controls, /<button type="button"[^>]*aria-label=\{label\}/);
  // Both menus are native selects, which are keyboard-operable and announce as comboboxes.
  assert.match(controls, /data-quality-mode=\{quality\.selection\.mode\}/);
  assert.match(controls, /data-playback-rate=\{playbackRate\}/);
  // Focus is visible on every surface the bar offers.
  for (const surface of ["CONTROL_SURFACE", "SELECT_SURFACE"]) {
    const declaration = controls.slice(controls.indexOf(`const ${surface} =`));
    assert.match(declaration.slice(0, 600), /focus-visible:outline-ring/, `${surface} has no visible focus`);
  }
  // Controls the media cannot honour say so rather than failing silently.
  assert.match(controls, /disabled=\{safeDuration === 0\}/);
});

/**
 * WCAG 2.5.5 target size, at the breakpoint where it matters.
 *
 * A Lesson is watched on a phone more often than anywhere else, and a bar of 32px controls is a bar
 * a thumb cannot use. 44px on touch, 40px from the small breakpoint upward where a pointer is doing
 * the aiming.
 */
test("touch targets are large enough on a phone", () => {
  const controls = code(CONTROLS);
  assert.match(controls, /inline-flex size-11 /, "control buttons are 44px on touch");
  assert.match(controls, /"h-11 /, "the menus are 44px on touch");
  assert.match(controls, /sm:size-10/);
  // The volume slider is the one control that gives way on a phone; mute carries the capability.
  assert.match(controls, /hidden items-center sm:flex/);
});

test("the bar carries no stock palette and stays on the design system", () => {
  const stock =
    /\b(?:bg|text|border|ring|from|to|via)-(?:slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-\d{2,3}\b/;
  for (const file of [PLAYER, CONTROLS]) {
    const offender = shipped(file).match(stock);
    assert.equal(offender, null, `${file} uses the stock palette class ${offender?.[0]}`);
  }
});

// --- Time is not language ----------------------------------------------------

test("the timeline and the elapsed reading run the way time runs, in both languages", () => {
  const controls = code(CONTROLS);
  assert.match(controls, /dir="ltr"/, "the transport bar is an instrument, not a paragraph");
  // The words in it still carry the Student's own direction.
  assert.match(controls, /const textDirection = locale === "ar" \? "rtl" : "ltr"/);
  assert.match(controls, /dir=\{textDirection\}/);
});

// --- Both languages, and nothing invented ------------------------------------

test("every control the player added is named in both languages", () => {
  const added = ["speed", "rewind", "forward", "pictureInPicture", "exitPictureInPicture", "buffering"] as const;
  for (const key of added) {
    assert.equal(typeof en.player[key], "string", `English player.${key}`);
    assert.equal(typeof ar.player[key], "string", `Arabic player.${key}`);
    assert.notEqual(en.player[key].trim(), "", `English player.${key} is empty`);
    assert.notEqual(ar.player[key].trim(), "", `Arabic player.${key} is empty`);
    // Arabic byte-identical to English is untranslated copy, not a translation.
    assert.notEqual(ar.player[key], en.player[key], `player.${key} was never translated`);
  }
  assert.deepEqual(Object.keys(en.player), Object.keys(ar.player), "the two dictionaries drifted apart");
});

/**
 * Captions are not in this tranche.
 *
 * There is no subtitle track in the media contract, no caption asset in the authoring pipeline and
 * nothing in the manifest to select — so a captions control would be a button that turns on
 * nothing. It is left out until there is a real pipeline behind it rather than shipped as an
 * affordance the product cannot honour.
 */
test("the player invents no captions control", () => {
  const surfaces = [shipped(PLAYER), shipped(CONTROLS), JSON.stringify(en.player), JSON.stringify(ar.player)].join("\n");
  assert.doesNotMatch(surfaces, /caption|subtitle|texttrack|<track|webvtt|\.vtt/i);
});
