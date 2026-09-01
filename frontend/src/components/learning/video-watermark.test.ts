import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import {
  WATERMARK_CLOCK_TICK_MS,
  WATERMARK_FADE_MS,
  WATERMARK_MAX_HOLD_MS,
  WATERMARK_MIN_HOLD_MS,
  WATERMARK_ZONE_CLASSES,
  WATERMARK_ZONES,
  watermarkClock,
  watermarkDetailLine,
  watermarkHoldMS,
  watermarkSeed,
  watermarkZoneAt,
} from "./video-watermark-model";

/**
 * The Student watermark: what it must show, where it may sit, and how rarely it may move.
 *
 * The movement and cadence are pure functions precisely so they can be proved here rather than
 * waited out — a test that slept for a real 45-second interval would be both slow and flaky. The
 * rendering rules that cannot be expressed as a function are asserted against the shipped source,
 * in the same style as the rest of the player's contract.
 *
 * The feature is deterrence and leak attribution. None of these tests claim it prevents capture.
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

const WATERMARK = "src/components/learning/video-watermark.tsx";
const PLAYER = "src/components/learning/lesson-player.tsx";

/** A few codes in the server's real shape, to exercise both stride directions. */
const CODES = ["7K2F", "A1B2", "ZZZZ", "0000", "9QW3", "MN4P"];

// --- Where it rests ----------------------------------------------------------

test("the watermark never rests over the centre of the lecture", () => {
  // Six edge zones and no centre. The centre is where the slide, the code and the speaker are.
  assert.equal(WATERMARK_ZONES.length, 6);
  for (const zone of WATERMARK_ZONES) {
    assert.match(zone, /^(top|mid|bottom)-(start|end)$/, `${zone} is not an edge zone`);
  }
  assert.equal(Object.keys(WATERMARK_ZONE_CLASSES).length, WATERMARK_ZONES.length);
});

/**
 * The bottom zones stop well clear of the control bar.
 *
 * The bar is `absolute inset-x-0 bottom-0` with a `pt-12` gradient above it, which is roughly a
 * quarter of the picture at the smallest inline player size and far less in fullscreen. Resting at
 * 28% from the bottom is above it at every size, so the watermark never covers a control and never
 * takes a click that belonged to one.
 */
test("no zone sits in the control bar's band", () => {
  for (const zone of WATERMARK_ZONES) {
    const classes = WATERMARK_ZONE_CLASSES[zone];
    const bottom = /bottom-\[(\d+)%\]/.exec(classes);
    if (bottom) {
      assert.ok(
        Number(bottom[1]) >= 26,
        `${zone} rests ${bottom[1]}% from the bottom, inside the control bar's band`,
      );
    }
    const top = /top-\[(\d+)%\]/.exec(classes);
    if (top) {
      assert.ok(Number(top[1]) <= 45, `${zone} rests ${top[1]}% down, which is not an edge`);
    }
  }
});

/** Both Gradex layouts. A left/right inset would sit in the wrong margin in Arabic. */
test("the zones follow the reading direction rather than the screen", () => {
  for (const zone of WATERMARK_ZONES) {
    const classes = WATERMARK_ZONE_CLASSES[zone];
    assert.match(classes, /(start|end)-\[\d+%\]/, `${zone} has no logical horizontal inset`);
    assert.doesNotMatch(classes, /\b(left|right)-/, `${zone} pins itself to a physical side`);
  }
});

// --- How it moves ------------------------------------------------------------

test("the watermark never moves to the position it is already in", () => {
  for (const seedCode of CODES) {
    const seed = watermarkSeed(seedCode);
    for (let step = 0; step < 40; step += 1) {
      assert.notEqual(
        watermarkZoneAt(seed, step),
        watermarkZoneAt(seed, step + 1),
        `${seedCode} repeated a zone at step ${step}`,
      );
    }
  }
});

/**
 * Cropping one corner must not remove the watermark from a recording.
 *
 * Six consecutive positions visit all six zones, so a recording long enough to matter carries the
 * identity in both top corners, both middles and both lower margins. Cutting one region removes one
 * sixth of the occurrences.
 */
test("six moves visit every zone", () => {
  for (const seedCode of CODES) {
    const seed = watermarkSeed(seedCode);
    for (let start = 0; start < 12; start += 1) {
      const visited = new Set(
        Array.from({ length: WATERMARK_ZONES.length }, (_unused, offset) =>
          watermarkZoneAt(seed, start + offset),
        ),
      );
      assert.equal(visited.size, WATERMARK_ZONES.length, `${seedCode} left a zone unused`);
    }
  }
});

test("movement is deterministic in the Student's own code", () => {
  const seed = watermarkSeed("7K2F");
  for (let step = 0; step < 20; step += 1) {
    assert.equal(watermarkZoneAt(seed, step), watermarkZoneAt(watermarkSeed("7K2F"), step));
    assert.equal(watermarkHoldMS(seed, step), watermarkHoldMS(watermarkSeed("7K2F"), step));
  }
  // Which is what lets the component render without `Math.random`, and therefore without a
  // hydration mismatch between the server pass and the client pass.
  assert.doesNotMatch(code("src/components/learning/video-watermark-model.ts"), /Math\.random/);
  assert.doesNotMatch(code(WATERMARK), /Math\.random/);
});

test("two Students do not move in lockstep", () => {
  const cadences = CODES.map((seedCode) => {
    const seed = watermarkSeed(seedCode);
    return Array.from({ length: 8 }, (_unused, step) => `${watermarkZoneAt(seed, step)}@${watermarkHoldMS(seed, step)}`).join("|");
  });
  assert.equal(new Set(cadences).size, cadences.length, "two codes produced identical behaviour");
});

// --- How often it moves ------------------------------------------------------

test("a position is held between 35 and 55 seconds, and the interval varies", () => {
  assert.equal(WATERMARK_MIN_HOLD_MS, 35_000);
  assert.equal(WATERMARK_MAX_HOLD_MS, 55_000);
  for (const seedCode of CODES) {
    const seed = watermarkSeed(seedCode);
    const holds = Array.from({ length: 30 }, (_unused, step) => watermarkHoldMS(seed, step));
    for (const hold of holds) {
      assert.ok(
        hold >= WATERMARK_MIN_HOLD_MS && hold <= WATERMARK_MAX_HOLD_MS,
        `${seedCode} held a position for ${hold}ms`,
      );
      assert.equal(hold % 1000, 0, "a hold is a whole number of seconds");
    }
    // A fixed beat is one an editor can cut around and one a Student starts to notice.
    assert.ok(new Set(holds).size > 1, `${seedCode} used a single fixed interval`);
  }
});

/** Short enough to read as the watermark settling, not as an animation running. */
test("the move is a brief fade rather than a bounce or a drift", () => {
  assert.ok(WATERMARK_FADE_MS > 0 && WATERMARK_FADE_MS <= 1000);
  const watermark = code(WATERMARK);
  assert.match(watermark, /transition-opacity/, "the move is a fade");
  for (const forbidden of ["animate-bounce", "animate-ping", "animate-pulse", "requestAnimationFrame"]) {
    assert.doesNotMatch(watermark, new RegExp(forbidden), `the watermark uses ${forbidden}`);
  }
});

// --- The coarse clock --------------------------------------------------------

test("the clock is coarse, and reads as local hours and minutes", () => {
  assert.equal(watermarkClock(new Date(2026, 8, 1, 3, 42, 17)), "03:42");
  assert.equal(watermarkClock(new Date(2026, 8, 1, 23, 5, 59)), "23:05");
  assert.equal(watermarkClock(new Date(2026, 8, 1, 0, 0, 0)), "00:00");
});

/**
 * Once a minute, not once a second.
 *
 * A per-second clock is a re-render per second for a value that only moves once a minute, and it
 * turns a watermark a Student forgets about into a ticking thing they cannot stop looking at. The
 * poll is coarse and the rendered string is identical between polls within the same minute, which
 * React discards without re-rendering.
 */
test("the clock is polled coarsely", () => {
  assert.ok(WATERMARK_CLOCK_TICK_MS >= 30_000, "the clock is polled at most twice a minute");
  const sameMinute = new Date(2026, 8, 1, 3, 42, 0);
  const laterInThatMinute = new Date(2026, 8, 1, 3, 42, 59);
  assert.equal(watermarkClock(sameMinute), watermarkClock(laterInThatMinute));
});

/** The browser's clock is a visible deterrent, never evidence sent anywhere. */
test("the clock is never reported to the backend", () => {
  const watermark = code(WATERMARK);
  for (const forbidden of ["fetch(", "authenticatedRequest", "navigator.sendBeacon", "XMLHttpRequest"]) {
    assert.doesNotMatch(watermark, new RegExp(forbidden.replace(/[.(]/g, "\\$&")), `the watermark calls ${forbidden}`);
  }
});

// --- What it says ------------------------------------------------------------

test("the detail line carries the masked address, the code, and the clock once read", () => {
  assert.equal(watermarkDetailLine("ah***@example.com", "7K2F", "03:42"), "ah***@example.com • 7K2F • 03:42");
  // Absent on the very first render, because the clock is read in an effect rather than in render.
  assert.equal(watermarkDetailLine("ah***@example.com", "7K2F", null), "ah***@example.com • 7K2F");
});

/**
 * The identity is the server's, not the browser's.
 *
 * Everything drawn comes off the playback authorization. If the component could reach the signed-in
 * session or a client-side profile it would be able to draw an identity the server never issued,
 * which is the whole property this feature depends on.
 */
test("the watermark renders only what the server issued", () => {
  const watermark = code(WATERMARK);
  assert.match(watermark, /function VideoWatermark\(\{ watermark \}: \{ watermark: PlaybackWatermark \}\)/);
  assert.match(watermark, /watermark\.masked_identifier/);
  assert.match(watermark, /watermark\.code/);
  assert.match(watermark, /watermark\.display_name/);
  for (const clientIdentity of ["currentCSRFToken", "lib/identity/session", "localStorage", "sessionStorage", "document.cookie"]) {
    assert.doesNotMatch(
      watermark,
      new RegExp(clientIdentity.replace(/[./]/g, "\\$&")),
      `the watermark reads ${clientIdentity} instead of the authorization`,
    );
  }
});

// --- What it must not disturb ------------------------------------------------

test("the watermark is inert to the pointer, to selection, and to assistive technology", () => {
  const watermark = code(WATERMARK);
  assert.match(watermark, /pointer-events-none/, "the watermark must never take a click");
  assert.match(watermark, /select-none/, "the watermark must not be selectable");
  assert.match(watermark, /aria-hidden/, "the watermark is decoration to a screen reader");
  // It is not in the tab order and owns no keyboard.
  assert.doesNotMatch(watermark, /tabIndex/, "the watermark must not be focusable");
  assert.doesNotMatch(watermark, /onKeyDown|onClick|onFocus/, "the watermark must not handle input");
});

test("every timer the watermark starts is cleared", () => {
  const watermark = code(WATERMARK);
  const timeouts = watermark.match(/setTimeout\(/g) ?? [];
  const intervals = watermark.match(/setInterval\(/g) ?? [];
  assert.equal(timeouts.length, 2, "the hold timer and the fade timer are the only timeouts");
  assert.equal(intervals.length, 1, "the clock is the only interval");
  assert.match(watermark, /clearTimeout\(holdTimer\)/);
  assert.match(watermark, /if \(fadeTimer\) clearTimeout\(fadeTimer\)/);
  assert.match(watermark, /clearInterval\(clockTimer\)/);
  // A timer that fires after unmount must not write to a dead component.
  assert.match(watermark, /cancelled = true/);
  assert.match(watermark, /if \(cancelled\) return;/);
});

// --- How the player mounts it ------------------------------------------------

/**
 * Inside the fullscreen surface, and only when there is an authorization to draw from.
 *
 * The player's container is the element it hands to `requestFullscreen`, so anything mounted inside
 * it is presented in fullscreen too. Mounting the watermark beside the player instead of within it
 * would leave every fullscreen recording unmarked — which is the way a Lesson is most likely to be
 * recorded.
 */
test("the player mounts the watermark inside the surface it makes fullscreen", () => {
  const player = code(PLAYER);
  assert.match(player, /\{playback\.watermark \? <VideoWatermark watermark=\{playback\.watermark\} \/> : null\}/);

  // The container passed to fullscreen is the one held in `playerElement`.
  assert.match(player, /supportsFullscreen\(playerElement, document\)/);
  assert.match(player, /toggleFullscreenBehavior\(playerElement, document/);

  // And the mount point is inside that container, above the controls in the source order.
  const container = player.indexOf("ref={setPlayerElement}");
  const mount = player.indexOf("<VideoWatermark");
  const controls = player.indexOf("<PlayerControls");
  assert.ok(container !== -1 && mount !== -1 && controls !== -1);
  assert.ok(container < mount, "the watermark is mounted outside the fullscreen container");
  assert.ok(mount < controls, "the watermark is not on the media surface");
});

/**
 * No authorization, no watermark.
 *
 * The player returns a placeholder while playback authorisation is in flight and resets `playback`
 * to null whenever the Lesson changes, so the watermark subtree is unmounted between Lessons and
 * cannot carry one Student's identity onto another authorization's video.
 */
test("the watermark exists only while an authorization does", () => {
  const player = code(PLAYER);
  assert.match(player, /setPlayback\(null\);/, "a Lesson change clears the authorization");
  assert.match(player, /if \(!playback\) \{/, "no authorization renders the placeholder instead");
  assert.match(player, /\}, \[lessonID, locale\]\);/, "the authorization is re-fetched per Lesson");
});

// --- Deterrence, and its honest limits ---------------------------------------

/**
 * The deterrents that are allowed, and the ones that are not.
 *
 * Suppressing the context menu over the picture and refusing a drag removes the casual one-click
 * save. Neither is a security boundary, and the things that would pretend to be one — trapping F12,
 * polling for DevTools, clearing the console, blocking keys across the page — are absent and must
 * stay absent: they break real Students' browsers and stop nobody.
 */
test("the player deters the casual save without pretending to be a boundary", () => {
  const player = code(PLAYER);
  assert.match(player, /onContextMenu=\{\(event\) => event\.preventDefault\(\)\}/);
  assert.match(player, /draggable=\{false\}/);
  assert.match(player, /controlsList="nodownload"/);
  assert.match(player, /controls=\{false\}/, "the native control set stays off");

  for (const theatre of ["devtools", "debugger", "console.clear", "F12", "keyCode === 123"]) {
    assert.doesNotMatch(
      player,
      new RegExp(theatre.replace(/[.]/g, "\\$&"), "i"),
      `the player ships anti-DevTools theatre: ${theatre}`,
    );
  }
  // The context menu is suppressed on the media surface only, never across the Lesson page.
  assert.equal((player.match(/onContextMenu=/g) ?? []).length, 1);
  assert.doesNotMatch(player, /document\.addEventListener\(\s*"contextmenu/);
});
