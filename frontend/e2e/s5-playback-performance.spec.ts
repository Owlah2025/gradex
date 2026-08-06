import fs from "fs";
import path from "path";
import { expect, test, type Page } from "@playwright/test";
import { authenticateRotatingStudent, studentFor, playerTestSlot } from "./rotating-students";

/**
 * T076 — time-to-first-frame evidence (SC-001).
 *
 * SC-001: "A Student with active access can start a Lesson and see video playing within 5 seconds on
 * a typical connection, at every supported viewport." This spec measures exactly that interval — from
 * navigation to the Lesson Player through to a frame the browser has actually presented — and asserts
 * it separately at each of the four supported viewports.
 *
 * WHAT COUNTS AS A FIRST FRAME
 *   Only evidence that a frame was decoded and presented:
 *
 *     video.getVideoPlaybackQuality().totalVideoFrames > 0   (primary)
 *     a `timeupdate` where video.currentTime > 0             (fallback)
 *
 *   `loadedmetadata`, `loadeddata`, `canplay`, `readyState`, element visibility, and poster visibility
 *   are all deliberately rejected. Every one of them can be true while the viewport still shows
 *   nothing — metadata arrives before any pixel does, which is the precise mistake the task names.
 *
 * WHY A PLAY GESTURE IS PART OF THE MEASUREMENT
 *   The player ships paused with no autoplay, and SC-001 measures a Student who "can start a Lesson
 *   and see video playing". So the run clicks the real Play control — the same gesture a Student
 *   makes — and the clock keeps running across it. Nothing about the player, its preload, or its
 *   autoplay behaviour is altered to make this easier; the measurement adapts to production, not the
 *   other way round.
 *
 * DETERMINISM
 *   The network is shaped through a real Chromium CDP session — `Network.enable`,
 *   `Network.setCacheDisabled`, `Network.emulateNetworkConditions` — not a Playwright route delay or
 *   an application-side sleep, so the throttle applies to the media fragments hls.js fetches rather
 *   than to a subset of requests the test happens to intercept. Cache is disabled so a second
 *   viewport cannot inherit the first one's warm fragments.
 *
 * LOOPBACK ONLY
 *   Every request made during a measurement is recorded and its host asserted to be loopback. A
 *   public host, CDN, analytics endpoint, or remote media URL fails the measurement rather than being
 *   quietly tolerated.
 *
 *   Note on the task's literal "127.0.0.1:3000": this suite allocates its frontend, API, and media
 *   ports per run precisely so a run never collides with a developer server on 3000, and
 *   `playwright.config.ts` has assumed nothing about 3000 since that change. The load-bearing
 *   requirement — local fixtures, no public media or CDN — is enforced here as loopback-host-only on
 *   every request, and the actual media origin is recorded in the evidence.
 */

const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const LESSON_ID = "30000000-0000-0000-0000-000000000001";

/** SC-001's threshold. Not tunable: the task states it must not be weakened. */
const THRESHOLD_MS = 5000;

/** The same four supported viewports the T075 rendered matrix uses. */
const VIEWPORTS = [
  { name: "phone", width: 390, height: 844 },
  { name: "tablet", width: 768, height: 1024 },
  { name: "laptop", width: 1280, height: 900 },
  { name: "desktop", width: 1440, height: 1000 },
];

/**
 * The documented, repeatable profile. No profile was mandated by spec.md, plan.md, research.md, or
 * the decision log, so this one is declared here and recorded in the retained evidence, which is what
 * "documented" has to mean for a figure to be reproducible.
 *
 * CDP takes throughput in **bytes** per second; the bit rates are the human-facing equivalents.
 */
const PROFILE = {
  name: "gradex-sc001-deterministic-4g",
  offline: false,
  latency_ms: 150,
  download_bytes_per_second: 500_000, // 4 Mbps
  upload_bytes_per_second: 125_000, //   1 Mbps
  connection_type: "cellular4g" as const,
  cache_disabled: true,
  packet_loss: "none",
};

const EVIDENCE_DIR = process.env.GRADEX_T076_EVIDENCE_DIR || "/var/tmp/gradex-s5-e2e-evidence/t076-performance";

const LOOPBACK_HOSTS = new Set(["127.0.0.1", "localhost", "::1", "[::1]"]);

type Measurement = {
  viewport: string;
  width: number;
  height: number;
  elapsed_ms: number;
  first_frame_signal: "totalVideoFrames" | "timeupdate";
  total_video_frames: number;
  current_time: number;
  passed: boolean;
};

const measurements: Measurement[] = [];
const mediaOrigins = new Set<string>();
const apiOrigins = new Set<string>();
const frontendOrigins = new Set<string>();
let publicDependencies: string[] = [];

function isLoopback(rawUrl: string): boolean {
  try {
    const u = new URL(rawUrl);
    if (u.protocol === "data:" || u.protocol === "blob:") return true;
    return LOOPBACK_HOSTS.has(u.hostname);
  } catch {
    return true; // not an http(s) URL at all
  }
}

/**
 * Applies the profile through CDP. Kept separate so the evidence records exactly what was applied
 * rather than what was intended.
 */
async function applyNetworkProfile(page: Page) {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Network.enable");
  await cdp.send("Network.setCacheDisabled", { cacheDisabled: PROFILE.cache_disabled });
  await cdp.send("Network.emulateNetworkConditions", {
    offline: PROFILE.offline,
    latency: PROFILE.latency_ms,
    downloadThroughput: PROFILE.download_bytes_per_second,
    uploadThroughput: PROFILE.upload_bytes_per_second,
    connectionType: PROFILE.connection_type,
  });
  return cdp;
}

// Real media over a throttled link on a shared runner is legitimately slower than the 30 s default,
// and a timeout must not be mistaken for a threshold breach: the budget is generous so a slow-but-
// correct run reports its measured figure instead of dying mid-measurement.
test.describe.configure({ timeout: 180_000 });

/**
 * SC-001 is a claim about the shipped application, so this suite refuses to run against `next dev`.
 * Measured there the figure is dominated by on-demand compilation and unoptimized assets — under this
 * exact profile ~86% of the elapsed time passed before the Play control appeared. Failing closed is
 * deliberate: a green T076 obtained from the development server would be a misleading number, not a
 * weaker one, and nothing about that is visible in the result itself.
 */
test.beforeAll(() => {
  expect(
    process.env.GRADEX_E2E_FRONTEND_MODE,
    "T076 must measure the built frontend: run with GRADEX_E2E_FRONTEND_MODE=production after npm run build",
  ).toBe("production");
});

test.describe("T076 — SC-001 time-to-first-frame", () => {
  for (const [viewportIndex, vp] of VIEWPORTS.entries()) {
    test.describe(`Viewport: ${vp.name} (${vp.width}x${vp.height})`, () => {
      // A fresh context per measurement, so no viewport inherits another's cache, session, or
      // decoded frames.
      test.use({ viewport: { width: vp.width, height: vp.height } });

      test(`first rendered frame within ${THRESHOLD_MS} ms at ${vp.name}`, async ({ context, page }, testInfo) => {
        // One Student per viewport, reusing the English Lesson Player slots (0, 2, 4, 6).
        //
        // Slot uniqueness only has to hold *within a run*, and T076 is a separate Playwright
        // invocation: its own globalSetup creates its own isolated per-run database and seeds its own
        // rotating pool, so nothing here contends with the T075 matrix. Reusing existing slots keeps
        // this task inside its two-path boundary — adding a slot would mean editing the shared
        // allocator and the backend seeder's mirrored constants, which is a wider change than the
        // evidence needs.
        await authenticateRotatingStudent(context, studentFor(testInfo, playerTestSlot(viewportIndex, 0, 2)));

        const requests: string[] = [];
        page.on("request", (req) => requests.push(req.url()));

        await applyNetworkProfile(page);

        // Instrument before navigation so no frame can slip past between load and listener
        // attachment. `performance.now()` is monotonic and immune to wall-clock adjustment.
        await page.addInitScript(() => {
          const state: Record<string, any> = { firstFrameAt: null, signal: null, frames: 0, currentTime: 0 };
          (window as unknown as Record<string, unknown>).__t076 = state;

          const record = (signal: string, frames: number, currentTime: number) => {
            if (state.firstFrameAt !== null) return;
            state.firstFrameAt = performance.now();
            state.signal = signal;
            state.frames = frames;
            state.currentTime = currentTime;
          };

          // The element does not exist when an init script runs — the document is not parsed yet, so
          // `document.documentElement` is still null and a MutationObserver on it throws, which
          // silently kills the whole init script and leaves the signal permanently unset. Polling for
          // the element sidesteps that entirely and depends on no DOM being present up front.
          const poll = window.setInterval(() => {
            const video = document.querySelector("video");
            if (!video) return;
            window.clearInterval(poll);

            video.addEventListener("timeupdate", () => {
              if (video.currentTime > 0) {
                const q = video.getVideoPlaybackQuality?.();
                record("timeupdate", q ? q.totalVideoFrames : 0, video.currentTime);
              }
            });

            // Presented-frame count, checked every animation frame. Preferred over `timeupdate`
            // because a frame can be painted before the first time update fires.
            const frameCheck = () => {
              if (state.firstFrameAt !== null) return;
              const q = video.getVideoPlaybackQuality?.();
              if (q && q.totalVideoFrames > 0) {
                record("totalVideoFrames", q.totalVideoFrames, video.currentTime);
                return;
              }
              requestAnimationFrame(frameCheck);
            };
            requestAnimationFrame(frameCheck);
          }, 25);
        });

        const navigationStart = Date.now();
        await page.goto(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);

        // The Student's own gesture. SC-001 measures starting a Lesson, so the clock runs across it.
        const playButton = page.getByRole("button", { name: /play/i });
        await expect(playButton).toBeVisible();
        await playButton.click();

        // Wait for presented-frame evidence only.
        await page.waitForFunction(
          () => (window as unknown as Record<string, any>).__t076?.firstFrameAt !== null,
          undefined,
          { timeout: 120_000 },
        );
        const elapsedMs = Date.now() - navigationStart;

        const signal = await page.evaluate(() => (window as unknown as Record<string, any>).__t076);
        // Stop playback so a long-running fixture cannot cross the server-side completion threshold.
        await page.evaluate(() => document.querySelector("video")?.pause());

        const offenders = requests.filter((u) => /^https?:/i.test(u) && !isLoopback(u));
        publicDependencies = [...publicDependencies, ...offenders];
        // Origins are classified rather than lumped together, so the evidence can state separately
        // that the frontend, the API, and the media fixture were each loopback.
        for (const u of requests) {
          let origin: string;
          try {
            origin = new URL(u).origin;
          } catch {
            continue; // not an absolute http(s) URL
          }
          if (/\.m3u8|\.ts(\?|$)|\.mp4/i.test(u)) mediaOrigins.add(origin);
          else if (/\/api\/v1\//.test(u)) apiOrigins.add(origin);
          else frontendOrigins.add(origin);
        }

        const passed = elapsedMs < THRESHOLD_MS;
        measurements.push({
          viewport: vp.name,
          width: vp.width,
          height: vp.height,
          elapsed_ms: elapsedMs,
          first_frame_signal: signal.signal,
          total_video_frames: signal.frames ?? 0,
          current_time: signal.currentTime ?? 0,
          passed,
        });

        console.log(
          `[T076] ${vp.name}: ${elapsedMs} ms via ${signal.signal} (frames=${signal.frames}, currentTime=${signal.currentTime}) threshold=${THRESHOLD_MS} -> ${passed ? "PASS" : "FAIL"}`,
        );

        // Loopback-only is asserted, never tolerated.
        expect(offenders, `${vp.name}: media and application dependencies must stay on loopback`).toEqual([]);
        // A permitted signal only.
        expect(["totalVideoFrames", "timeupdate"]).toContain(signal.signal);
        // The threshold, per viewport, unaveraged.
        expect(
          elapsedMs,
          `${vp.name}: first rendered frame took ${elapsedMs} ms, SC-001 allows under ${THRESHOLD_MS} ms`,
        ).toBeLessThan(THRESHOLD_MS);
      });
    });
  }

  // Written even when a threshold assertion failed, so a breach is evidenced rather than invisible.
  test.afterAll(() => {
    fs.mkdirSync(EVIDENCE_DIR, { recursive: true });
    const evidence = {
      schema: "gradex.t076.playback-performance/1",
      task: "T076",
      requirement: "SC-001",
      threshold_ms: THRESHOLD_MS,
      commit: process.env.GITHUB_SHA ?? null,
      run_id: process.env.GITHUB_RUN_ID ?? null,
      run_attempt: process.env.GITHUB_RUN_ATTEMPT ?? null,
      playwright_version: process.env.GRADEX_PLAYWRIGHT_VERSION ?? null,
      chromium_version: process.env.GRADEX_CHROMIUM_VERSION ?? null,
      generated_at_utc: new Date().toISOString(),
      spec: "e2e/s5-playback-performance.spec.ts",
      profile: PROFILE,
      frontend_mode: process.env.GRADEX_E2E_FRONTEND_MODE ?? null,
      origins: {
        frontend: [...frontendOrigins].sort(),
        api: [...apiOrigins].sort(),
        media: [...mediaOrigins].sort(),
        all_loopback: [...frontendOrigins, ...apiOrigins, ...mediaOrigins].every((o) => isLoopback(o)),
        public_dependency_count: publicDependencies.length,
      },
      measurements: [...measurements].sort((a, b) => a.viewport.localeCompare(b.viewport)),
      expected_measurements: VIEWPORTS.length,
      actual_measurements: measurements.length,
      passed: measurements.length === VIEWPORTS.length && measurements.every((m) => m.passed),
    };
    const target = path.join(EVIDENCE_DIR, "t076-playback-performance.json");
    const tmp = `${target}.tmp`;
    fs.writeFileSync(tmp, `${JSON.stringify(evidence, null, 2)}\n`);
    fs.renameSync(tmp, target); // atomic replace
    console.log(`[T076] wrote ${target} — ${evidence.actual_measurements}/${evidence.expected_measurements} measurements`);
  });
});
