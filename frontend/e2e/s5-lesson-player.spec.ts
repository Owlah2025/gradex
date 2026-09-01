import { test, expect, type Page } from "@playwright/test";
import { AxeBuilder } from "@axe-core/playwright";
import { AUTHORIZATION_FLAG, expectAbsent } from "./authority-leak";
import {
  queryProgress,
  requireNoProgressRow,
  requireProgressRow,
  waitForProgress,
  waitForStableProgress,
  type ProgressSnapshot,
} from "../src/lib/api/e2e-progress";
import {
  authenticateRotatingStudent,
  expiredStudentFor,
  expiredTestSlot,
  genericTestSlot,
  lifecycleTestSlot,
  playerTestSlot,
  studentFor,
  PROGRESS_TEST_SLOT,
  type RotatingStudent,
} from "./rotating-students";

const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const LESSON_ID = "30000000-0000-0000-0000-000000000001";
const LESSON_B_ID = "30000000-0000-0000-0000-000000000002";
const UNENTITLED_COURSE_ID = "c9999999-9999-9999-9999-999999999999";
const ACTIVE_STUDENT_ID = "a0000000-0000-0000-0000-000000000001";
const OTHER_STUDENT_ID = "a0000000-0000-0000-0000-000000000002";

/** The trusted duration of the seeded Asset Version, in seconds. Completion is >= 90% of it. */
const TRUSTED_DURATION_SECONDS = 30;
const COMPLETION_THRESHOLD_SECONDS = TRUSTED_DURATION_SECONDS * 0.9;

/** The Progress identity for one rotating Student and one stable Lesson. */
function progressQuery(student: RotatingStudent, lessonIdentityID: string) {
  return { studentAccountID: student.accountID, courseID: COURSE_ID, lessonIdentityID };
}

// The neighbouring Student whose row must never move. It is a seeded Student with real Progress
// rows, deliberately not drawn from the rotating pool, so "unchanged" is asserted against a row
// that genuinely exists rather than against an absent one.
const otherStudentLessonAQuery = {
  studentAccountID: OTHER_STUDENT_ID,
  courseID: COURSE_ID,
  lessonIdentityID: LESSON_ID,
};

/**
 * Drives one real Progress write through the production reporter and resolves once the exact
 * `PUT /api/v1/learn/lessons/:lessonId/progress` response has been observed. Synchronising on
 * the response — rather than on a sleep — is what makes the database assertions that follow
 * deterministic.
 */
async function reportProgressAndAwaitResponse(
  page: Page,
  lessonID: string,
  targetSeconds: number
): Promise<{ status: number; sentPositionSeconds: number; sentAssetVersionBound: boolean }> {
  // Match on the payload, not merely the URL. The element emits its own media events, so an
  // unrelated in-flight report would otherwise be mistaken for this one and the assertions
  // would describe a position the test never asked for.
  const responsePromise = page.waitForResponse((response) => {
    if (!response.url().includes(`/learn/lessons/${lessonID}/progress`)) return false;
    if (response.request().method() !== "PUT") return false;
    const body = response.request().postData();
    if (!body) return false;
    try {
      return Math.abs(JSON.parse(body).position_seconds - targetSeconds) <= POSITION_TOLERANCE_SECONDS;
    } catch {
      return false;
    }
  });

  const seekedAt = await page.evaluate((seconds) => {
    const video = document.querySelector("video");
    if (!video) return null;
    // Pause first so playback cannot advance the position between the seek and the report.
    video.pause();
    video.currentTime = seconds;
    video.dispatchEvent(new Event("seeked"));
    return video.currentTime;
  }, targetSeconds);

  if (seekedAt === null) throw new Error("No video element was mounted, so no Progress write could be driven.");

  const response = await responsePromise;
  const payload = response.request().postData();
  if (!payload) throw new Error("The Progress request carried no body.");
  const sent = JSON.parse(payload) as { position_seconds: number; asset_version_id: string };

  return {
    status: response.status(),
    sentPositionSeconds: sent.position_seconds,
    // The Asset Version identifier itself is never logged; only its presence is evidence.
    sentAssetVersionBound: typeof sent.asset_version_id === "string" && sent.asset_version_id.length > 0,
  };
}

/**
 * Waits until the player has real media metadata and a seekable range wide enough for every
 * position this suite reports, so a seek lands where the test asked rather than being clamped.
 */
async function waitForPlayableMedia(page: Page): Promise<number> {
  return page
    .waitForFunction(() => {
      const video = document.querySelector("video");
      if (!video) return null;
      if (video.readyState < 1) return null;
      if (!Number.isFinite(video.duration) || video.duration <= 0) return null;
      if (video.seekable.length === 0 || video.seekable.end(0) < 20) return null;
      return video.duration;
    })
    .then((handle) => handle.jsonValue() as Promise<number>);
}

function expectSnapshotsEqual(before: ProgressSnapshot, after: ProgressSnapshot, description: string) {
  expect(after, description).toEqual(before);
}

/**
 * `position_seconds` is stored as NUMERIC(10,3) and sourced from `video.currentTime`, which the
 * browser may snap to a frame boundary. The element also emits its own native `seeked` alongside
 * the dispatched one, so the value that finally lands is the requested position to within a
 * fraction of a second rather than a bit-exact float.
 */
const POSITION_TOLERANCE_SECONDS = 0.5;

function positionMatches(snapshot: ProgressSnapshot, targetSeconds: number): boolean {
  return snapshot.found && Math.abs(snapshot.position_seconds - targetSeconds) <= POSITION_TOLERANCE_SECONDS;
}

const VIEWPORTS = [
  { name: "phone", width: 390, height: 844 },
  { name: "tablet", width: 768, height: 1024 },
  { name: "laptop", width: 1280, height: 900 },
  { name: "desktop", width: 1440, height: 1000 },
];

const LOCALES = ["en", "ar"] as const;

// Real media, real HLS loading, and real database round trips are legitimately slower than the
// 30 s default — especially on the first compile of a route in Next.js dev mode. The budget is
// generous so that a slow-but-correct run reports the assertion that actually failed instead of
// a timeout that hides it.
test.describe.configure({ timeout: 120_000 });

// Every execution authenticates as its own rotating Student. Sharing one Student across a
// repeated suite exhausts the production playback-issuance budget — 30 per 10 minutes — which is
// a limit this suite respects rather than relaxes. Sessions are issued through the real session
// repository rather than the login route, so the login endpoint's own per-network budget stays
// intact; the infrastructure smoke test continues to exercise the full HTTP login flow.
test.describe("T061 — S5 Lesson Player E2E Test Suite", () => {
  for (const [viewportIndex, vp] of VIEWPORTS.entries()) {
    test.describe(`Viewport: ${vp.name} (${vp.width}x${vp.height})`, () => {
      test.use({ viewport: { width: vp.width, height: vp.height } });

      for (const [localeIndex, locale] of LOCALES.entries()) {
        const isRTL = locale === "ar";
        const targetUrl = `/${locale}/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`;

        test(`Active Student (${locale.toUpperCase()}): Custom player controls, real S4 playback authorization, HLS playback, keyboard operation, WCAG AA scan & no leaks`, async ({
          context,
          page,
        }, testInfo) => {
          await authenticateRotatingStudent(
            context,
            studentFor(testInfo, playerTestSlot(viewportIndex, localeIndex, LOCALES.length))
          );

          const networkRequests: string[] = [];
          page.on("request", (req) => networkRequests.push(req.url()));

          // Awaiting the real playback response is the synchronisation point. `networkidle` is
          // not one here: HLS keeps the network busy, so idleness is neither necessary nor a
          // reliable signal that the client has issued its authorization request yet.
          const playbackResponse = page.waitForResponse(
            (r) => r.url().includes(`/learn/lessons/${LESSON_ID}/playback`) && r.request().method() === "POST"
          );
          await page.goto(targetUrl);

          // 1. Direction and header assertions
          const main = page.locator("main");
          await expect(main).toHaveAttribute("dir", isRTL ? "rtl" : "ltr");
          await expect(page.getByRole("heading", { level: 1 })).toContainText(
            isRTL ? "الدرس الأول: مرحباً بك" : "Lesson 1: Introduction"
          );

          // 2. Real network authenticity: the client obtained genuine S4 playback authorization.
          expect((await playbackResponse).status()).toBe(200);
          expect(networkRequests.some((url) => url.includes("/playback"))).toBe(true);

          // 3. Custom player controls presence & no native controls leak
          const playerControls = page.locator("[data-player-controls]");
          await expect(playerControls).toBeVisible();

          const video = page.locator("video");
          await expect(video).toBeVisible();
          await expect(video).not.toHaveAttribute("controls");

          // 4. Play / Pause button interaction and state updates
          const playButton = page.getByRole("button", { name: isRTL ? /تشغيل/i : /play/i });
          await expect(playButton).toBeVisible();

          // Focus play button and activate
          await playButton.focus();
          await playButton.click();

          // Verify state updates to Pause / إيقاف مؤقت
          const pauseButton = page.getByRole("button", { name: isRTL ? /إيقاف مؤقت/i : /pause/i });
          await expect(pauseButton).toBeVisible({ timeout: 5000 });

          // Stop playback immediately. Left running, the 30 s fixture reaches its end during the
          // accessibility scan and the reporter's `ended`/`pause` write crosses the server-side
          // 90% completion threshold — writing a write-once completion that every later test in
          // the run would then inherit.
          await page.evaluate(() => document.querySelector("video")?.pause());

          // 5. Seek slider keyboard operation & PostgreSQL progress mutation verification
          const seekSlider = page.getByRole("slider", { name: isRTL ? /البحث في الفيديو/i : /seek/i });
          await expect(seekSlider).toBeVisible();
          await seekSlider.focus();
          await page.keyboard.press("ArrowRight");
          await page.keyboard.press("ArrowRight");

          // The seek lands in real media. Progress persistence is deliberately NOT asserted per
          // viewport: this matrix exists for responsive, bilingual, keyboard, and WCAG evidence,
          // and eight extra writes to the same (Student, Lesson) trip the production
          // 12-writes-per-minute limiter, which turns a rendering suite into a rate-limit test.
          // The dedicated persistence test below owns that evidence.
          await waitForPlayableMedia(page);
          const seekedTo = await page.evaluate(() => {
            const video = document.querySelector("video")!;
            video.pause();
            video.currentTime = 15;
            return video.currentTime;
          });
          expect(Math.abs(seekedTo - 15)).toBeLessThanOrEqual(POSITION_TOLERANCE_SECONDS);

          // 6. Volume & Mute keyboard operation
          const muteButton = page.getByRole("button", { name: isRTL ? /كتم الصوت/i : /mute/i });
          await expect(muteButton).toBeVisible();
          await muteButton.focus();
          await page.keyboard.press("Enter");
          await expect(page.getByRole("button", { name: isRTL ? /إلغاء كتم الصوت/i : /unmute/i })).toBeVisible();

          // Unmute back
          await page.keyboard.press("Enter");
          await expect(muteButton).toBeVisible();

          // 7. Quality select dropdown (if available from HLS master playlist). The control
          // reports the Student's selected mode, which is a distinct thing from the level HLS is
          // currently rendering: it opens in Auto, a manual choice sticks, and returning to Auto
          // restores adaptive selection.
          const qualitySelect = page.getByRole("combobox", { name: /quality|الجودة/i });
          if (await qualitySelect.isVisible()) {
            await expect(qualitySelect).toHaveAttribute("data-quality-mode", "auto");
            await expect(qualitySelect).toHaveValue("auto");

            await qualitySelect.selectOption("level-0");
            await expect(qualitySelect).toHaveAttribute("data-quality-mode", "manual");
            await expect(qualitySelect).toHaveValue("level-0");

            // An adaptive LEVEL_SWITCHED must not silently replace the Student's choice.
            await expect
              .poll(() => qualitySelect.evaluate((node) => (node as HTMLSelectElement).value), {
                timeout: 3000,
                intervals: [300, 300, 300],
              })
              .toBe("level-0");

            // Accessible naming stays in resolutions; internal hls.js indexes are never exposed.
            const accessibleName = await qualitySelect.getAttribute("aria-label");
            expect(accessibleName).not.toMatch(/level-\d|index/i);

            await qualitySelect.selectOption("auto");
            await expect(qualitySelect).toHaveAttribute("data-quality-mode", "auto");
            await expect(qualitySelect).toHaveValue("auto");
          }

          // 8. Navigation links presence & keyboard focus
          const nextLessonLink = page.getByRole("link", { name: isRTL ? /الدرس التالي/i : /next lesson/i });
          if (await nextLessonLink.isVisible()) {
            await nextLessonLink.focus();
            await expect(nextLessonLink).toBeFocused();
          }

          // 9. WCAG 2.2 AA Accessibility Scan on complete Lesson-player screen (<main>) with zero violations
          const accessibilityScanResults = await new AxeBuilder({ page })
            .include("main")
            .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
            .analyze();
          expect(accessibilityScanResults.violations).toEqual([]);

          // 10. Information leak audit in rendered DOM (14 sensitive tokens)
          const bodyHtml = await page.content();
          const PROHIBITED_TOKENS: (string | RegExp)[] = [
            "asset_version_id",
            "entitlement_id",
            "enrollment_id",
            "revision_id",
            "can_play",
            "can_update_progress",
            AUTHORIZATION_FLAG,
            "internal evaluator reason",
            "object key",
            "storage bucket",
            "session secret",
            "signed manifest URL",
            "AWSAccessKeyId",
            "X-Amz-Signature",
            "60000000-0000-0000-0000-000000000001",
            "test/master.m3u8",
          ];
          for (const token of PROHIBITED_TOKENS) {
            expectAbsent(bodyHtml, token);
          }

          // 11. Browser storage information-leak audit (localStorage / sessionStorage)
          const storageSecrets = await page.evaluate(() => {
            const prohibited = ["asset_version_id", "X-Amz-Signature", "AWSAccessKeyId", "test/master.m3u8"];
            const found: string[] = [];
            const checkStore = (store: Storage, name: string) => {
              for (let i = 0; i < store.length; i++) {
                const k = store.key(i) || "";
                const v = store.getItem(k) || "";
                for (const p of prohibited) {
                  if (k.includes(p) || v.includes(p)) found.push(`${name}:${p}`);
                }
              }
            };
            checkStore(localStorage, "localStorage");
            checkStore(sessionStorage, "sessionStorage");
            return found;
          });
          expect(storageSecrets).toEqual([]);
        });
      }

      test("Expired Student: Localized expired message and NO player or playback requests", async ({ context, page }, testInfo) => {
        await authenticateRotatingStudent(context, expiredStudentFor(testInfo, expiredTestSlot(viewportIndex)));

        const networkRequests: string[] = [];
        page.on("request", (req) => networkRequests.push(req.url()));

        await page.goto(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);
        await page.waitForLoadState("networkidle");

        // Expired indicator rendered in card body
        await expect(page.locator("p", { hasText: /Access expired/i })).toBeVisible();

        // Player controls NOT mounted
        await expect(page.locator("[data-player-controls]")).not.toBeVisible();
        await expect(page.locator("video")).not.toBeVisible();

        // Assert NO playback authorization request was issued
        const hasPlaybackReq = networkRequests.some((url) => url.includes("/playback"));
        expect(hasPlaybackReq).toBe(false);
      });

      test("Generic Unavailable: Unentitled course returns generic error without revealing internal cause", async ({ context, page }, testInfo) => {
        await authenticateRotatingStudent(context, studentFor(testInfo, genericTestSlot(viewportIndex)));

        await page.goto(`/en/learn/courses/${UNENTITLED_COURSE_ID}/lessons/${LESSON_ID}`);
        await page.waitForLoadState("networkidle");

        await expect(page.getByRole("heading", { name: "Learning is unavailable" })).toBeVisible();
        await expect(page.getByText("This learning content is not available right now.")).toBeVisible();

        // No internal error details or player rendered
        await expect(page.locator("video")).not.toBeVisible();
        await expect(page.locator("[data-player-controls]")).not.toBeVisible();
      });

      test("Media & lifecycle cleanup: navigating from Lesson A to Lesson B disposes old reporter and resets quality state", async ({ browser }, testInfo) => {
        // A dedicated rotating Student and a dedicated BrowserContext per repetition: no cookie
        // jar, storage, timer, network-array state, or Progress row is inherited from any other
        // execution, so ordering is irrelevant and no repetition inherits the previous one's
        // Progress.
        const student = studentFor(testInfo, lifecycleTestSlot(viewportIndex));
        const lessonAQuery = progressQuery(student, LESSON_ID);
        const lifecycleContext = await browser.newContext({ viewport: { width: vp.width, height: vp.height } });
        await authenticateRotatingStudent(lifecycleContext, student);
        const page = await lifecycleContext.newPage();

        try {
          const requests: { url: string; method: string; at: number }[] = [];
          page.on("request", (req) => requests.push({ url: req.url(), method: req.method(), at: Date.now() }));

          const lessonAProgressRequests = () =>
            requests.filter((r) => r.url.includes(`/learn/lessons/${LESSON_ID}/progress`));
          const hlsRequests = () => requests.filter((r) => r.url.includes(".m3u8") || r.url.includes(".ts"));

          // 1. Lesson A obtains playback authorization — awaited, never sampled from a log.
          const playbackA = page.waitForResponse(
            (r) => r.url().includes(`/learn/lessons/${LESSON_ID}/playback`) && r.request().method() === "POST"
          );
          // 2. Lesson A loads real media: the HLS master manifest is a real network request.
          const manifestA = page.waitForResponse((r) => r.url().includes(".m3u8"));

          await page.goto(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);
          expect((await playbackA).status()).toBe(200);
          expect((await manifestA).status()).toBeLessThan(400);

          await expect(page.locator("video")).toBeVisible();
          await expect(page.locator("[data-player-controls]")).toBeVisible();
          await waitForPlayableMedia(page);

          // Lesson A selects a manual quality, so quality reset on Lesson B is observable
          // rather than vacuous.
          const qualitySelectA = page.getByRole("combobox", { name: /quality|الجودة/i });
          const hasQualityLevels = await qualitySelectA.isVisible();
          let lessonAOptionCount = 0;
          if (hasQualityLevels) {
            // The player opens in Auto, which is the Student's selection and not merely whichever
            // level HLS happens to be rendering.
            await expect(qualitySelectA).toHaveAttribute("data-quality-mode", "auto");
            await expect(qualitySelectA).toHaveValue("auto");

            // Pin a real level on Lesson A, so a carry-over into Lesson B would be observable.
            await qualitySelectA.selectOption("level-0");
            await expect(qualitySelectA).toHaveAttribute("data-quality-mode", "manual");
            await expect(qualitySelectA).toHaveValue("level-0");

            // The pin survives adaptive `LEVEL_SWITCHED` events. Before the fix this control was
            // re-synced to whatever level HLS was rendering, which silently discarded the
            // Student's choice — and reported Auto as manual whenever ABR switched.
            await expect
              .poll(() => qualitySelectA.evaluate((node) => (node as HTMLSelectElement).value), {
                timeout: 3000,
                intervals: [300, 300, 300],
              })
              .toBe("level-0");

            lessonAOptionCount = await qualitySelectA.evaluate((node) => (node as HTMLSelectElement).options.length);
            expect(lessonAOptionCount).toBeGreaterThan(1);
          }

          // Held so disposal of Lesson A's player is provable by detachment, not inferred.
          const playerElementA = await page.locator("[data-lesson-player]").elementHandle();
          expect(playerElementA).not.toBeNull();

          // 3–4. Lesson A issues a real Progress write and it settles against its exact response.
          const reportedA = await reportProgressAndAwaitResponse(page, LESSON_ID, 9);
          expect(reportedA.status).toBeLessThan(400);
          expect(reportedA.sentPositionSeconds).toBeLessThan(COMPLETION_THRESHOLD_SECONDS);
          await waitForProgress(lessonAQuery, (snapshot) => positionMatches(snapshot, 9), {
            description: "Lesson A write settling before navigation",
          });

          // Pause so no periodic tick can advance the position underneath the comparison.
          await page.evaluate(() => document.querySelector("video")?.pause());

          const lessonAProgressRequestsBeforeNavigation = lessonAProgressRequests().length;
          const hlsRequestsBeforeNavigation = hlsRequests().length;

          // 6–8. Navigate to Lesson B, awaiting both its read model and a *fresh* playback
          // authorization. Awaiting the response is the fix for the sampled-log race: the
          // previous assertion read a snapshot of the request array immediately after
          // `networkidle`, which can settle before hydration has issued the POST — worst on the
          // first viewports, where Next.js dev-mode compiles the route on demand.
          const playbackB = page.waitForResponse(
            (r) => r.url().includes(`/learn/lessons/${LESSON_B_ID}/playback`) && r.request().method() === "POST"
          );
          await page.goto(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_B_ID}`);
          await expect(page.getByRole("heading", { level: 1, name: "Lesson 2: Variables" })).toBeVisible();
          expect((await playbackB).status()).toBe(200);

          // 9–10. Lesson B's quality state is rebuilt from its own fresh HLS instance rather than
          // carried over. The previous player element is detached, and the new control is
          // repopulated from Lesson B's manifest — the cleanup path clears `availableQualities`
          // and `selectedQuality`, so a surviving control would be the old one.
          await expect(page.locator("video")).toBeVisible();
          await waitForPlayableMedia(page);
          expect(await playerElementA!.evaluate((node) => node.isConnected).catch(() => false)).toBe(false);

          const qualitySelectB = page.getByRole("combobox", { name: /quality|الجودة/i });
          if (hasQualityLevels) {
            await expect(qualitySelectB).toBeVisible();
            // Lesson B starts at Auto. Lesson A's manual pin belongs to Lesson A's media and must
            // not carry across a source replacement.
            await expect(qualitySelectB).toHaveAttribute("data-quality-mode", "auto");
            await expect(qualitySelectB).toHaveValue("auto");

            const rebuilt = await qualitySelectB.evaluate((node) => {
              const select = node as HTMLSelectElement;
              return {
                optionCount: select.options.length,
                autoPresent: Array.from(select.options).some((option) => option.value === "auto"),
                valueIsOwnOption: Array.from(select.options).some((option) => option.value === select.value),
                exposesHlsIndexes: Array.from(select.options).some((option) => /level|index/i.test(option.label)),
              };
            });
            // Auto remains offered and the value is one of Lesson B's own options — never a
            // stale index left over from Lesson A's manifest.
            expect(rebuilt.autoPresent).toBe(true);
            expect(rebuilt.optionCount).toBe(lessonAOptionCount);
            expect(rebuilt.valueIsOwnOption).toBe(true);
            // Accessible text names resolutions, never internal hls.js level indexes.
            expect(rebuilt.exposesHlsIndexes).toBe(false);
          }

          // 5. Capture the settled Lesson A baseline. The reporter's `pagehide` write during
          // navigation is legitimate production behaviour, so the baseline is taken once the
          // row stops changing rather than at an arbitrary instant.
          const settledA = requireProgressRow(
            await waitForStableProgress(lessonAQuery),
            "active Student, Lesson A after navigation"
          );
          const lessonAProgressRequestsAfterNavigation = lessonAProgressRequests().length;

          // 11. Cross a real reporter lifecycle boundary. A live Lesson A reporter would fire a
          // Lesson A write here; a disposed one cannot. This is the same boundary the periodic
          // tick would exercise, reached deterministically instead of by waiting out 15 s.
          await page.evaluate(() => {
            Object.defineProperty(document, "visibilityState", { value: "hidden", configurable: true });
            document.dispatchEvent(new Event("visibilitychange"));
          });
          const boundaryProgressB = page.waitForResponse(
            (r) => r.url().includes(`/learn/lessons/${LESSON_B_ID}/progress`) && r.request().method() === "PUT",
            { timeout: 5000 }
          );
          // Lesson B's live reporter responds to the boundary, which proves the boundary really
          // fired — so Lesson A's silence is evidence of disposal, not of an inert trigger.
          expect((await boundaryProgressB).status()).toBeLessThan(400);

          // 12. No Progress request targeted Lesson A after the navigation settled.
          expect(lessonAProgressRequests().length).toBe(lessonAProgressRequestsAfterNavigation);
          expect(lessonAProgressRequestsAfterNavigation).toBeGreaterThanOrEqual(
            lessonAProgressRequestsBeforeNavigation
          );

          // 13. Lesson A's PostgreSQL Progress is unchanged after disposal, field for field.
          expectSnapshotsEqual(
            settledA,
            queryProgress(lessonAQuery),
            "Lesson A Progress after Lesson B lifecycle boundary"
          );

          // 14. No late Lesson A callback altered Lesson B's UI.
          await expect(page.getByRole("heading", { level: 1, name: "Lesson 2: Variables" })).toBeVisible();
          if (hasQualityLevels) {
            await expect(qualitySelectB).toBeVisible();
            expect(
              await qualitySelectB.evaluate((node) => {
                const select = node as HTMLSelectElement;
                return Array.from(select.options).some((option) => option.value === select.value);
              })
            ).toBe(true);
            // 22. Lesson A's manual quality selection still has not carried into Lesson B, and no
            // stale level event from the destroyed instance has changed the new player's mode.
            await expect(qualitySelectB).toHaveAttribute("data-quality-mode", "auto");
            await expect(qualitySelectB).toHaveValue("auto");
          }

          // 15. Lesson A's HLS source is shut down: all manifest, variant, and segment activity
          // observed after the navigation belongs to Lesson B's freshly attached instance, and
          // the old instance adds none of its own once the boundary has been crossed.
          const hlsAfterBoundary = hlsRequests().length;
          expect(hlsAfterBoundary).toBeGreaterThan(hlsRequestsBeforeNavigation);
          await page.evaluate(() => document.querySelector("video")?.pause());
          const hlsSettled = hlsRequests().length;
          await expect
            .poll(() => hlsRequests().length, { timeout: 3000, intervals: [500, 500, 500] })
            .toBe(hlsSettled);
        } finally {
          await lifecycleContext.close();
        }
      });
    });
  }

  test("Progress persistence: a real production write advances the active Student's row and leaves another Student's row untouched", async ({
    browser,
  }, testInfo) => {
    // A Student this execution alone owns, so the starting state is known rather than inherited
    // from a previous repetition.
    const student = studentFor(testInfo, PROGRESS_TEST_SLOT);
    const lessonAQuery = progressQuery(student, LESSON_ID);
    const context = await browser.newContext();
    await authenticateRotatingStudent(context, student);
    const page = await context.newPage();

    try {
      const progressRequests: string[] = [];
      page.on("request", (req) => {
        if (req.url().includes("/progress")) progressRequests.push(req.url());
      });

      const playback = page.waitForResponse(
        (r) => r.url().includes(`/learn/lessons/${LESSON_ID}/playback`) && r.request().method() === "POST"
      );
      await page.goto(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);
      expect((await playback).status()).toBe(200);
      await waitForPlayableMedia(page);

      // Pause first: a periodic tick from a playing video must not move the position underneath
      // the before/after comparison.
      await page.evaluate(() => document.querySelector("video")?.pause());

      // 2–3. The designed initial state for a rotating Student is a genuine absence: the pool is
      // seeded with no Progress, and this Student belongs to this execution alone. Absence is
      // asserted explicitly rather than read as a zero-valued row.
      const before = await waitForStableProgress(lessonAQuery);
      requireNoProgressRow(before, `rotating Student ${student.index}, Lesson A before any write`);

      // The neighbouring Student's row must exist, so "unchanged" is asserted against real data.
      const otherBefore = requireProgressRow(
        queryProgress(otherStudentLessonAQuery),
        "other Student, Lesson A initial row"
      );

      // 4–5. Two real production Progress writes, each synchronised on its exact HTTP response.
      const firstTarget = 6;
      const secondTarget = 18;

      const first = await reportProgressAndAwaitResponse(page, LESSON_ID, firstTarget);
      expect(first.status).toBeLessThan(400);
      expect(first.sentAssetVersionBound).toBe(true);
      const afterFirst = requireProgressRow(
        await waitForProgress(lessonAQuery, (snapshot) => positionMatches(snapshot, firstTarget), {
          description: `first write landing at ${firstTarget}s`,
        }),
        "active Student, Lesson A after first write"
      );

      const second = await reportProgressAndAwaitResponse(page, LESSON_ID, secondTarget);
      expect(second.status).toBeLessThan(400);
      const afterSecond = requireProgressRow(
        await waitForProgress(lessonAQuery, (snapshot) => positionMatches(snapshot, secondTarget), {
          description: `second write landing at ${secondTarget}s`,
        }),
        "active Student, Lesson A after second write"
      );

      // 9. The row the writes created now exists and the position advanced as the requests asked.
      expect(second.sentPositionSeconds).toBeGreaterThan(first.sentPositionSeconds);
      expect(afterSecond.position_seconds).toBeGreaterThan(afterFirst.position_seconds);

      // 10. The monotonic maximum never regresses and reflects the greatest position reported.
      expect(afterSecond.max_position_seconds).toBeGreaterThanOrEqual(afterFirst.max_position_seconds);
      expect(afterSecond.max_position_seconds).toBeGreaterThanOrEqual(afterSecond.position_seconds);
      expect(Math.abs(afterSecond.max_position_seconds - secondTarget)).toBeLessThanOrEqual(
        POSITION_TOLERANCE_SECONDS
      );
      expect(afterSecond.updated_at).not.toBe(afterFirst.updated_at);

      // 11. Completion is not reached because the production threshold was never crossed, and a
      // Student who began with no row cannot have acquired a completion instant.
      expect(second.sentPositionSeconds).toBeLessThan(COMPLETION_THRESHOLD_SECONDS);
      expect(afterSecond.completed).toBe(false);
      expect(afterSecond.completed_at).toBe("");

      // 12. The stored Asset Version binding is written only by completion, so an incomplete
      // row must still carry none — even though every write was bound to an Asset Version.
      expect(first.sentAssetVersionBound).toBe(true);
      expect(afterSecond.asset_version_id).toBe("");

      // Every Progress request targeted the intended Lesson only.
      expect(progressRequests.length).toBeGreaterThan(0);
      for (const url of progressRequests) {
        expect(url).toContain(`/learn/lessons/${LESSON_ID}/progress`);
        expect(url).not.toContain(LESSON_B_ID);
      }

      // 1–6 (other Student). The neighbouring row is byte-for-byte equivalent afterwards.
      expectSnapshotsEqual(
        otherBefore,
        requireProgressRow(queryProgress(otherStudentLessonAQuery), "other Student, Lesson A after write"),
        "other Student's Progress after the active Student's writes"
      );
    } finally {
      await context.close();
    }
  });
});

/**
 * Visible Progress follows the server without a reload.
 *
 * The Progress write used to answer 204, so a successful report told the
 * browser only that it had been accepted. Every surface showing completion or a
 * Course percentage therefore kept rendering whatever it had at page load, and
 * a Student who finished a Lesson watched it stay "in progress" until they
 * reloaded the page.
 *
 * What is proved here is the whole chain in a real browser: a real production
 * write, the canonical state on its response, and the rendered Lesson state and
 * Course contents following it — with no navigation, no reload, and no second
 * page load of any kind.
 */
test("visible Progress follows a real write without any reload", async ({ browser }, testInfo) => {
  const student = studentFor(testInfo, PROGRESS_TEST_SLOT);
  const context = await browser.newContext();
  await authenticateRotatingStudent(context, student);
  const page = await context.newPage();

  try {
    // Any navigation at all would make the assertion meaningless: a fresh
    // server render would show the new state whether or not the page ever
    // learned it. This fails the test if one happens.
    const navigations: string[] = [];
    page.on("framenavigated", (frame) => {
      if (frame === page.mainFrame()) navigations.push(frame.url());
    });

    await page.goto(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);
    await waitForPlayableMedia(page);
    await page.evaluate(() => document.querySelector("video")?.pause());
    const loadedAt = navigations.length;

    const lessonState = page.getByTestId("lesson-state");
    await expect(lessonState).toBeVisible();
    await expect(lessonState).not.toHaveAttribute("data-lesson-state", "completed");

    // A partial position: started, not finished. The rendered state has to move
    // off "not started" without the page being reloaded.
    const partial = await reportProgressAndAwaitResponse(page, LESSON_ID, 6);
    expect(partial.status).toBe(200);
    await expect(lessonState).toHaveAttribute("data-lesson-state", "in-progress");

    // Past the completion threshold. Completion is the server's decision and
    // arrives on the write's response; the browser renders it.
    const finished = await reportProgressAndAwaitResponse(
      page,
      LESSON_ID,
      Math.ceil(COMPLETION_THRESHOLD_SECONDS) + 1,
    );
    expect(finished.status).toBe(200);
    await expect(lessonState).toHaveAttribute("data-lesson-state", "completed");

    // The Course contents beside the player follow the same confirmation, so
    // the outline and the Lesson cannot disagree about what has been finished.
    const contents = page.getByTestId("course-contents-sidebar");
    await expect(contents).toBeVisible();
    await expect(
      contents.locator(`[data-lesson-id="${LESSON_ID}"][data-lesson-state="completed"]`),
    ).toHaveCount(1);

    // A rewind after completion must not un-complete the Lesson: completion is
    // write-once server-side, and the rendered state must not claim otherwise.
    const rewound = await reportProgressAndAwaitResponse(page, LESSON_ID, 3);
    expect(rewound.status).toBe(200);
    await expect(lessonState).toHaveAttribute("data-lesson-state", "completed");

    expect(
      navigations.length,
      "the page navigated, so this proves a fresh render rather than a live update",
    ).toBe(loadedAt);
  } finally {
    await context.close();
  }
});
