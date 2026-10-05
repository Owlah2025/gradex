import assert from "node:assert/strict";
import test from "node:test";
import { createPlaybackRecovery, isExpiredHLSError } from "./playback-recovery";

test("only fatal network authorization refusals require a fresh authorization", () => {
  const future = "2999-01-01T00:00:00Z";
  for (const code of [401, 403]) {
    assert.equal(isExpiredHLSError({ fatal: true, type: "networkError", response: { code } }, future), true);
    assert.equal(isExpiredHLSError({ fatal: false, type: "networkError", response: { code } }, future), false);
  }
  for (const code of [0, 404, 500]) {
    assert.equal(isExpiredHLSError({ fatal: true, type: "networkError", response: { code } }, future), false);
  }
  assert.equal(isExpiredHLSError({ fatal: true, type: "mediaError", response: { code: 403 } }, future), false);
  assert.equal(isExpiredHLSError({ fatal: true, type: "networkError" }, future), false);
  assert.equal(isExpiredHLSError({ fatal: true, type: "networkError", response: { code: 404 } }, "2000-01-01T00:00:00Z"), true);
});

test("duplicate and stale events cannot mint authorizations and the retry budget survives refresh", () => {
  const recovery = createPlaybackRecovery();
  recovery.bind("first");
  assert.equal(recovery.begin("first"), "refresh");
  assert.equal(recovery.begin("first"), "ignore");
  recovery.bind("second");
  assert.equal(recovery.begin("first"), "ignore");
  // An old effect cleanup must not deactivate a newer source.
  recovery.unbind("first");
  assert.equal(recovery.begin("second"), "refresh");
  recovery.bind("third");
  assert.equal(recovery.begin("third"), "exhausted");
  recovery.reset();
  assert.equal(recovery.begin("third"), "refresh");
});

test("teardown refuses callbacks even while recovery is pending", () => {
  const recovery = createPlaybackRecovery();
  recovery.bind("source");
  assert.equal(recovery.begin("source"), "refresh");
  recovery.unbind("source");
  assert.equal(recovery.begin("source"), "ignore");
  recovery.bind("replacement");
  assert.equal(recovery.begin("replacement"), "refresh");
});
