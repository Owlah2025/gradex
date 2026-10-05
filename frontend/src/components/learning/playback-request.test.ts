import assert from "node:assert/strict";
import test from "node:test";
import { createPlaybackRequest } from "./playback-request";
import type { PlaybackAuthorization } from "@/lib/api/learning";

const authorization: PlaybackAuthorization = {
  playback_session: "local-lease", manifest_url: "/authorized-manifest",
  asset_version_id: "local-version", expires_at: "2026-10-06T12:00:00Z",
};

test("effect replay shares a lease while an abandoned pending request releases its result", async () => {
  let complete!: (value: PlaybackAuthorization) => void;
  const pending = new Promise<PlaybackAuthorization>((resolve) => { complete = resolve; });
  const released: PlaybackAuthorization[] = [];
  const request = createPlaybackRequest(() => pending, (value) => released.push(value));
  const stopFirst = request.retain();
  stopFirst();
  const stopReplay = request.retain();
  complete(authorization);
  assert.equal(await request.promise, authorization);
  await Promise.resolve();
  assert.equal(released.length, 0);
  stopReplay();
  await new Promise<void>((resolve) => queueMicrotask(resolve));
  assert.deepEqual(released, [authorization]);

  let lateComplete!: (value: PlaybackAuthorization) => void;
  const late = createPlaybackRequest(
    () => new Promise((resolve) => { lateComplete = resolve; }),
    (value) => released.push(value),
  );
  late.retain()();
  lateComplete({ ...authorization, playback_session: "abandoned-lease" });
  await late.promise;
  await new Promise<void>((resolve) => queueMicrotask(resolve));
  assert.equal(released[1].playback_session, "abandoned-lease");
});

test("a hung authorization reaches a bounded error and late success releases its lease", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  let complete!: (value: PlaybackAuthorization) => void;
  const released: PlaybackAuthorization[] = [];
  const request = createPlaybackRequest(
    () => new Promise((resolve) => { complete = resolve; }),
    (value) => released.push(value),
  );
  const failed = assert.rejects(request.promise, /timed out/);
  t.mock.timers.tick(15_000);
  await failed;
  complete(authorization);
  await Promise.resolve();
  assert.deepEqual(released, [authorization]);
});
