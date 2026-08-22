import { test, expect, type Browser, type BrowserContext, type Page } from "@playwright/test";
import { AUTHORIZATION_FLAG, expectAbsent } from "./authority-leak";
import { queryProgress, requireProgressRow, waitForProgress } from "../src/lib/api/e2e-progress";
import {
  authenticateRotatingStudent,
  studentFor,
  DASHBOARD_RESUME_AR_TEST_SLOT,
  DASHBOARD_RESUME_EN_TEST_SLOT,
  type RotatingStudent,
} from "./rotating-students";

/**
 * MVP-F15 / ST-12 — the continue-learning pointer, proved as a Student journey.
 *
 * The Progress row is created the only way the product creates one: by the real Lesson Player
 * reporting a real position through `PUT /api/v1/learn/lessons/:lessonId/progress`. Nothing is
 * seeded into `progress`, and no request is synthesised — the earlier attempt at a synthetic write
 * failed because Playwright's Node-side request client withholds the `Secure` `__Host-` session
 * cookie over the run's plain-HTTP loopback origin, so those calls arrived unauthenticated and were
 * refused by the inventory-safe 404 that protects every unauthenticated protected read.
 *
 * The Dashboard is then driven as a Student would: read the pointer, click the actual control, and
 * land on the Lesson. No final Lesson URL is ever constructed by the test.
 */

const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const LESSON_ID = "30000000-0000-0000-0000-000000000001";

const COURSE_TITLE_EN = "CS101: Introduction to Programming";
const COURSE_TITLE_AR = "مقدمة في البرمجة CS101";
const LESSON_TITLE_EN = "Lesson 1: Introduction";
const LESSON_TITLE_AR = "الدرس الأول: مرحباً بك";

/** The position the journey persists. Well under the 90% completion threshold of the 30 s fixture. */
const RESUME_POSITION_SECONDS = 11;
const POSITION_TOLERANCE_SECONDS = 0.5;

// Real media, a real HLS load and real database round trips, plus first-compile cost for three
// routes under `next dev`. Generous so a slow-but-correct run reports its assertion, not a timeout.
test.describe.configure({ timeout: 120_000 });

function progressQuery(student: RotatingStudent) {
  return { studentAccountID: student.accountID, courseID: COURSE_ID, lessonIdentityID: LESSON_ID };
}

/**
 * Drives one genuine Progress write through the production reporter and resolves once its exact
 * HTTP response has been observed, so the Dashboard read that follows is deterministic.
 */
async function persistProgressThroughTheRealPlayer(page: Page, locale: "en" | "ar"): Promise<void> {
  const playback = page.waitForResponse(
    (r) => r.url().includes(`/learn/lessons/${LESSON_ID}/playback`) && r.request().method() === "POST",
  );
  await page.goto(`/${locale}/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);

  // The player obtained real S4 playback authorization; the Asset Version the reporter binds to
  // comes from that response and from nowhere else.
  expect((await playback).status()).toBe(200);

  // Real media metadata, with a seekable range wide enough that the seek lands where it is asked.
  await page.waitForFunction(() => {
    const video = document.querySelector("video");
    if (!video) return null;
    if (video.readyState < 1) return null;
    if (!Number.isFinite(video.duration) || video.duration <= 0) return null;
    if (video.seekable.length === 0 || video.seekable.end(0) < 20) return null;
    return video.duration;
  });

  const written = page.waitForResponse((response) => {
    if (!response.url().includes(`/learn/lessons/${LESSON_ID}/progress`)) return false;
    if (response.request().method() !== "PUT") return false;
    const body = response.request().postData();
    if (!body) return false;
    try {
      return (
        Math.abs(JSON.parse(body).position_seconds - RESUME_POSITION_SECONDS) <= POSITION_TOLERANCE_SECONDS
      );
    } catch {
      return false;
    }
  });

  await page.evaluate((seconds) => {
    const video = document.querySelector("video");
    if (!video) throw new Error("No video element mounted, so no Progress could be reported.");
    // Pause first so playback cannot advance the position between the seek and the report.
    video.pause();
    video.currentTime = seconds;
    video.dispatchEvent(new Event("seeked"));
  }, RESUME_POSITION_SECONDS);

  const response = await written;
  const sent = JSON.parse(response.request().postData() ?? "{}") as { asset_version_id?: string };
  expect(response.status(), "the production Progress write was accepted").toBeLessThan(400);
  // Presence only — the Asset Version identifier itself is never logged or asserted by value.
  expect(typeof sent.asset_version_id === "string" && sent.asset_version_id.length > 0).toBe(true);

  // Stop the media before leaving, so nothing advances the stored position afterwards.
  await page.evaluate(() => document.querySelector("video")?.pause());
}

/**
 * The Dashboard is a protected surface. It names a Course and a Lesson and nothing else: no
 * authority internals, no media identity, no signed-target parameters.
 */
async function expectNoAuthorityLeak(page: Page): Promise<void> {
  const bodyHtml = await page.content();
  const prohibited: (string | RegExp)[] = [
    "asset_version_id",
    "entitlement_id",
    "enrollment_id",
    "revision_id",
    "can_play",
    "can_update_progress",
    AUTHORIZATION_FLAG,
    "AWSAccessKeyId",
    "X-Amz-Signature",
  ];
  for (const token of prohibited) {
    expectAbsent(bodyHtml, token);
  }
}

async function newStudentContext(
  browser: Browser,
  student: RotatingStudent,
): Promise<{ context: BrowserContext; page: Page }> {
  const context = await browser.newContext();
  await authenticateRotatingStudent(context, student);
  return { context, page: await context.newPage() };
}

test.describe("S15 Dashboard resume — MVP-F15 continue learning", () => {
  test("English: a real player write becomes a Dashboard Continue pointer that reaches the Lesson", async ({
    browser,
  }, testInfo) => {
    const student = studentFor(testInfo, DASHBOARD_RESUME_EN_TEST_SLOT);
    const { context, page } = await newStudentContext(browser, student);

    try {
      // 1–3. Active access, a real Lesson, and Progress persisted through the canonical path.
      await persistProgressThroughTheRealPlayer(page, "en");

      const persisted = requireProgressRow(
        await waitForProgress(
          progressQuery(student),
          (snapshot) =>
            snapshot.found &&
            Math.abs(snapshot.position_seconds - RESUME_POSITION_SECONDS) <= POSITION_TOLERANCE_SECONDS,
          { description: `the production write landing at ${RESUME_POSITION_SECONDS}s` },
        ),
        "the Student's Progress after the real player write",
      );
      expect(persisted.completed).toBe(false);

      // 4–5. The Student leaves the Lesson and opens the Dashboard as a fresh navigation.
      await page.goto(`/en/learn/dashboard`);

      // 6–9. The pointer is present, names the right Course and Lesson, and offers to continue
      // rather than to start.
      const block = page.getByTestId("continue-learning");
      await expect(block).toBeVisible();
      await expect(block).toContainText("Continue learning");
      await expect(page.getByTestId("continue-learning-course")).toHaveText(COURSE_TITLE_EN);
      await expect(page.getByTestId("continue-learning-lesson")).toContainText(LESSON_TITLE_EN);
      const action = page.getByTestId("continue-learning-action");
      await expect(action).toHaveText("Continue");
      await expect(action).not.toHaveText("Start");

      // The Dashboard is a protected surface: it must not leak authorization vocabulary.
      await expectNoAuthorityLeak(page);

      // 10–11. The Student clicks the real control. The destination is whatever the product put in
      // the link — the test never builds a Lesson URL of its own.
      await action.click();
      await page.waitForURL((url) => url.pathname.startsWith("/en/learn/courses/"));
      const landed = new URL(page.url());
      expect(landed.pathname).toBe(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);
      await expect(page.locator("h1")).toContainText(LESSON_TITLE_EN);

      // 12. The existing progress/resume contract still sees the persisted position: the player
      // resumes from it rather than restarting.
      const resumedAt = await page
        .waitForFunction(
          (target) => {
            const video = document.querySelector("video");
            if (!video || video.readyState < 1) return null;
            return Math.abs(video.currentTime - target) <= 1.5 ? video.currentTime : null;
          },
          RESUME_POSITION_SECONDS,
        )
        .then((handle) => handle.jsonValue() as Promise<number>);
      expect(Math.abs(resumedAt - RESUME_POSITION_SECONDS)).toBeLessThanOrEqual(1.5);
      await page.evaluate(() => document.querySelector("video")?.pause());

      // The stored row is unchanged in identity by the round trip.
      const afterReturn = requireProgressRow(
        queryProgress(progressQuery(student)),
        "the Student's Progress after following the pointer",
      );
      expect(afterReturn.completed).toBe(false);

      // The pointer survives a fresh page rather than being a one-render artefact.
      await page.goto(`/en/learn/dashboard`);
      await expect(page.getByTestId("continue-learning-lesson")).toContainText(LESSON_TITLE_EN);
      await expect(page.getByTestId("continue-learning-action")).toHaveText("Continue");
    } finally {
      await context.close();
    }
  });

  test("Arabic: the same journey renders real Arabic copy and reaches the Arabic Lesson route", async ({
    browser,
  }, testInfo) => {
    // Its own Student, so this proves the Arabic surface rather than inheriting English state.
    const student = studentFor(testInfo, DASHBOARD_RESUME_AR_TEST_SLOT);
    const { context, page } = await newStudentContext(browser, student);

    try {
      await persistProgressThroughTheRealPlayer(page, "ar");

      requireProgressRow(
        await waitForProgress(
          progressQuery(student),
          (snapshot) =>
            snapshot.found &&
            Math.abs(snapshot.position_seconds - RESUME_POSITION_SECONDS) <= POSITION_TOLERANCE_SECONDS,
          { description: `the Arabic journey's production write landing at ${RESUME_POSITION_SECONDS}s` },
        ),
        "the Student's Progress after the real player write",
      );

      await page.goto(`/ar/learn/dashboard`);
      await expect(page.locator("main")).toHaveAttribute("dir", "rtl");

      // Real Arabic copy, not merely a right-to-left direction attribute.
      const block = page.getByTestId("continue-learning");
      await expect(block).toBeVisible();
      await expect(block).toContainText("تابع التعلّم");
      await expect(page.getByTestId("continue-learning-course")).toHaveText(COURSE_TITLE_AR);
      await expect(page.getByTestId("continue-learning-lesson")).toContainText(LESSON_TITLE_AR);

      const action = page.getByTestId("continue-learning-action");
      await expect(action).toHaveText("تابع");
      await expectNoAuthorityLeak(page);

      // The real Arabic action, clicked, must land on the Arabic learning route.
      await action.click();
      await page.waitForURL((url) => url.pathname.startsWith("/ar/learn/courses/"));
      expect(new URL(page.url()).pathname).toBe(`/ar/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);
      await expect(page.locator("h1")).toContainText(LESSON_TITLE_AR);
      await page.evaluate(() => document.querySelector("video")?.pause());
    } finally {
      await context.close();
    }
  });
});
