import assert from "node:assert/strict";
import { test } from "node:test";
import {
  seekMedia,
  setMediaVolume,
  setQualityLevel,
  toggleFullscreen,
  toggleMediaMute,
  toggleMediaPlayback,
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
