import { test, expect } from "@playwright/test";
import type { PlaybackAuthorization } from "../src/lib/api/learning";
import { queryProgress } from "../src/lib/api/e2e-progress";
import { authenticateRotatingStudent, studentFor, PROGRESS_TEST_SLOT } from "./rotating-students";

const courseID = "c0000000-0000-0000-0000-000000000001";
const lessonID = "30000000-0000-0000-0000-000000000001";
const lessonPath = `/en/learn/courses/${courseID}/lessons/${lessonID}`;

test("paused playback crosses real capability expiry, resumes its position and preserves one lease", async ({ context, page }, testInfo) => {
  const student = studentFor(testInfo, PROGRESS_TEST_SLOT);
  const session = await authenticateRotatingStudent(context, student);
  const firstResponse = page.waitForResponse((response) => response.url().endsWith(`/lessons/${lessonID}/playback`) && response.request().method() === "POST");
  await page.goto(lessonPath);
  const first = await (await firstResponse).json() as PlaybackAuthorization;
  const leaseRequest = (path: string, playbackSession: string) => page.evaluate(async (request) => {
    const response = await fetch(request.path, {
      method: "POST", credentials: "same-origin",
      headers: { "X-CSRF-Token": request.csrf, "Content-Type": "application/json" },
      body: JSON.stringify({ playback_session: request.playbackSession }),
    });
    return { status: response.status, code: response.status === 204 ? null : (await response.json()).code };
  }, { path, playbackSession, csrf: session.csrf_token });
  await page.waitForFunction(() => (document.querySelector("video")?.readyState ?? 0) >= 2);
  // Browser fetch is necessary here: the APIRequest client does not send
  // Secure __Host- cookies over the loopback HTTP test origin.
  expect(await page.evaluate(async (url) => (await fetch(url, { credentials: "same-origin" })).status, first.manifest_url)).toBe(200);
  await page.evaluate(() => {
    const video = document.querySelector("video")!;
    video.pause();
    video.currentTime = 8;
    video.dispatchEvent(new Event("seeked"));
  });
  const progressQuery = { studentAccountID: student.accountID, courseID, lessonIdentityID: lessonID };
  await expect.poll(() => queryProgress(progressQuery).position_seconds).toBeCloseTo(8, 0);
  const expiryWait = Date.parse(first.expires_at) - Date.now() + 200;
  expect(expiryWait).toBeLessThan(40_000);
  expect(expiryWait).toBeGreaterThan(0);
  await page.waitForTimeout(expiryWait);
  // The old capability is actually expired on the API, not just in a UI mock.
  const expired = await page.evaluate(async (url) => {
    const response = await fetch(url, { credentials: "same-origin" });
    return { status: response.status, code: (await response.json()).code };
  }, first.manifest_url);
  expect(expired).toEqual({ status: 404, code: "NOT_FOUND" });

  const refreshedResponse = page.waitForResponse((response) => response.url().endsWith(`/lessons/${lessonID}/playback`) && response.request().method() === "POST");
  await page.evaluate(() => { void document.querySelector("video")!.play(); });
  const refreshed = await (await refreshedResponse).json() as PlaybackAuthorization;
  expect(refreshed.playback_session).not.toBe(first.playback_session);
  await expect.poll(() => page.evaluate(() => document.querySelector("video")?.currentTime ?? 0)).toBeGreaterThan(8.5);
  await expect(page.getByTestId("lesson-media-unavailable")).toHaveCount(0);
  await expect.poll(() => queryProgress(progressQuery).max_position_seconds).toBeGreaterThanOrEqual(8);

  // A delayed release of the old lease cannot revoke the refreshed player.
  const oldRelease = await leaseRequest("/api/v1/media/playback-releases", first.playback_session);
  expect(oldRelease).toEqual({ status: 404, code: "NOT_FOUND" });
  const heartbeat = await leaseRequest("/api/v1/media/playback-heartbeats", refreshed.playback_session);
  expect(heartbeat.status).toBe(200);
  const releaseResponse = page.waitForResponse((response) =>
    response.url().endsWith("/media/playback-releases") &&
    response.request().postDataJSON()?.playback_session === refreshed.playback_session);
  await page.getByRole("link", { name: "My courses", exact: true }).first().click();
  await page.waitForURL("**/en/learn/dashboard");
  expect((await releaseResponse).status()).toBe(204);
  await expect.poll(async () => {
    return (await leaseRequest("/api/v1/media/playback-heartbeats", refreshed.playback_session)).code;
  }).toBe("PLAYBACK_LEASE_LOST");
});

test("fatal manifest 403s exhaust recovery and an explicit retry can play", async ({ context, page }, testInfo) => {
  await authenticateRotatingStudent(context, studentFor(testInfo, PROGRESS_TEST_SLOT));
  let authorizations = 0;
  let refuse = true;
  page.on("response", (response) => {
    if (response.url().endsWith(`/lessons/${lessonID}/playback`) && response.request().method() === "POST" && response.status() === 200) authorizations += 1;
  });
  await page.route("**/media/playback-manifests/**", (route) => refuse
    ? route.fulfill({ status: 403, contentType: "application/problem+json", body: JSON.stringify({ code: "MEDIA_ACCESS_DENIED" }) })
    : route.continue());
  await page.goto(lessonPath);
  await expect(page.getByTestId("lesson-media-unavailable")).toBeVisible();
  expect(authorizations).toBe(3);
  await page.waitForTimeout(1000);
  expect(authorizations).toBe(3);
  refuse = false;
  await page.getByTestId("lesson-playback-retry").click();
  await page.waitForFunction(() => (document.querySelector("video")?.readyState ?? 0) >= 2);
  expect(authorizations).toBe(4);
});

test("a terminal media element error refreshes and restores a paused position", async ({ context, page }, testInfo) => {
  await authenticateRotatingStudent(context, studentFor(testInfo, PROGRESS_TEST_SLOT));
  await page.goto(lessonPath);
  await page.waitForFunction(() => (document.querySelector("video")?.readyState ?? 0) >= 2);
  const refresh = page.waitForResponse((response) => response.url().endsWith(`/lessons/${lessonID}/playback`) && response.request().method() === "POST");
  await page.evaluate(() => {
    const video = document.querySelector("video")!;
    video.pause();
    video.currentTime = 12;
    video.dispatchEvent(new Event("error"));
  });
  expect((await refresh).status()).toBe(200);
  await expect.poll(() => page.evaluate(() => document.querySelector("video")?.currentTime ?? 0)).toBeCloseTo(12, 0);
  expect(await page.evaluate(() => document.querySelector("video")?.paused)).toBe(true);
});
