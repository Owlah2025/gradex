import assert from "node:assert/strict";
import { test } from "node:test";
import {
  blockIsRetryable,
  heartbeatIntervalMS,
  playbackBlockOf,
} from "./playback-conflict";

// The exact shape a ProblemError carries, without importing it: the classifier
// reads structurally, and the test proves that contract rather than the class.
function problem(code: string) {
  return {
    problem: {
      type: `https://api.gradex.com/problems/${code.toLowerCase().replace(/_/g, "-")}`,
      title: "t",
      status: 409,
      code,
    },
  };
}

test("a conflict on another device is classified as its own state", () => {
  assert.equal(
    playbackBlockOf(problem("PLAYBACK_ALREADY_ACTIVE_ON_ANOTHER_DEVICE")),
    "ANOTHER_DEVICE",
  );
});

test("a superseded lease is distinct from a conflict", () => {
  assert.equal(playbackBlockOf(problem("PLAYBACK_LEASE_LOST")), "LEASE_LOST");
});

test("both device-trust refusals send the Student to the same place", () => {
  assert.equal(
    playbackBlockOf(problem("DEVICE_TRUST_REQUIRED")),
    "DEVICE_TRUST_REQUIRED",
  );
  assert.equal(
    playbackBlockOf(problem("DEVICE_ADOPTION_REQUIRED")),
    "DEVICE_TRUST_REQUIRED",
  );
});

test("an unrelated refusal is not turned into a playback message", () => {
  assert.equal(playbackBlockOf(problem("NOT_FOUND")), null);
  assert.equal(playbackBlockOf(new Error("network")), null);
  assert.equal(playbackBlockOf(null), null);
});

test("retrying is offered only where it can succeed", () => {
  assert.equal(blockIsRetryable("ANOTHER_DEVICE"), true);
  assert.equal(blockIsRetryable("LEASE_LOST"), true);
  assert.equal(blockIsRetryable("COORDINATION_UNAVAILABLE"), true);
  // Retrying cannot trust a device; the Student has to go and do that.
  assert.equal(blockIsRetryable("DEVICE_TRUST_REQUIRED"), false);
});

test("the published heartbeat interval is used, within sane bounds", () => {
  assert.equal(heartbeatIntervalMS(25), 25_000);
  assert.equal(heartbeatIntervalMS(undefined), 25_000);
  // A value that would hammer the API, or let the lease lapse between beats,
  // is clamped rather than believed.
  assert.equal(heartbeatIntervalMS(0), 5_000);
  assert.equal(heartbeatIntervalMS(-10), 5_000);
  assert.equal(heartbeatIntervalMS(9_999), 120_000);
  assert.equal(heartbeatIntervalMS(Number.NaN), 5_000);
});
