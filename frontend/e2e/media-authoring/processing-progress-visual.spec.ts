import { execFileSync } from "child_process";
import fs from "fs";
import os from "os";
import path from "path";
import { test, expect, request as playwrightRequest, type Browser, type Page } from "@playwright/test";
import { issueRotatingSession } from "../rotating-students";
import { frontendOrigin } from "../../src/lib/api/e2e-ports";
import { openAuthoringSections } from "../authoring-sections";

/**
 * D-098 visual evidence for real video processing progress.
 *
 * Separate from the S12 journey because it needs a source video long enough
 * that the transcode genuinely takes time. S12 uses a tiny fixture, which is
 * right for a journey — but a transcode that finishes in under a second has no
 * observable middle, and a spec that waited for one there would be a timing
 * race rather than a test.
 *
 * Nothing here is faked. The video is real, the upload is real, the worker
 * really encodes four HLS rungs, and every percentage screenshotted below was
 * produced by FFmpeg's own progress stream against the ffprobe duration.
 */

const INSTRUCTOR = { email: "instructor@example.test", accountID: "a0000000-0000-0000-0000-000000000003" };
const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };

/**
 * A real 45-second 720p H.264/AAC MP4.
 *
 * Long enough that encoding the ladder takes tens of seconds of genuine work,
 * which is what makes an intermediate percentage exist to be photographed.
 */
function makeLongSampleMP4(): string {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gradex-progress-mp4-"));
  const file = path.join(directory, "long-lesson.mp4");
  execFileSync(
    "ffmpeg",
    [
      "-y",
      "-f", "lavfi", "-i", "testsrc=size=1280x720:rate=30:duration=45",
      "-f", "lavfi", "-i", "sine=frequency=440:duration=45",
      "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
      "-c:a", "aac", "-shortest", "-movflags", "+faststart",
      file,
    ],
    { stdio: "ignore" },
  );
  return file;
}

async function signedInInstructorPage(browser: Browser, locale: "en" | "ar"): Promise<Page> {
  const session = issueRotatingSession(INSTRUCTOR);
  const origin = new URL(frontendOrigin());
  const context = await browser.newContext({ locale: locale === "ar" ? "ar-KW" : "en-US" });
  await context.addInitScript((language: string) => {
    window.localStorage.setItem("gradex.locale", language);
  }, locale);
  await context.addCookies([
    {
      name: session.cookie_name,
      value: session.cookie_value,
      domain: origin.hostname,
      path: "/",
      httpOnly: true,
      secure: true,
      sameSite: "Strict",
    },
  ]);
  return context.newPage();
}

test("processing progress is real, visible, accessible, and localized", async ({ browser }, testInfo) => {
  test.setTimeout(5 * 60 * 1000);

  const adminSession = issueRotatingSession(ADMIN);
  const admin = await playwrightRequest.newContext({
    baseURL: frontendOrigin(),
    extraHTTPHeaders: {
      Accept: "application/json, application/problem+json",
      Origin: frontendOrigin(),
      Cookie: `${adminSession.cookie_name}=${adminSession.cookie_value}`,
      "X-CSRF-Token": adminSession.csrf_token,
    },
  });

  // The media suite runs on its own freshly seeded database, so the canonical
  // Academic context the Instructor flow requires is created here.
  const institution = await admin.post("/api/v1/admin/academic/institutions", {
    data: {
      country_code: "KW", slug: `progress-university-${Date.now()}`,
      name_ar: "جامعة اختبار الوسائط", name_en: "Media Test University",
      max_academic_level: 4,
    },
  });
  expect(institution.status(), await institution.text()).toBe(201);
  const institutionID = ((await institution.json()) as { id: string }).id;
  const subject = await admin.post(`/api/v1/admin/academic/institutions/${institutionID}/subjects`, {
    data: { official_code: "CS101", title_ar: "برمجة", title_en: "Programming" },
  });
  expect(subject.status(), await subject.text()).toBe(201);

  const mp4 = makeLongSampleMP4();
  const page = await signedInInstructorPage(browser, "en");
  await page.goto("/en/instructor/courses");
  await expect(page.locator("h1")).toContainText("Course Authoring Studio");

  await page.getByTestId("toggle-new-course").click();
  await page.getByTestId("new-course-institution").selectOption({ label: "Media Test University" });
  await page.getByTestId("new-course-subject-search").fill("CS101");
  await expect(page.getByTestId("new-course-subject-result")).toBeVisible();
  await page.getByTestId("new-course-subject-result").click();
  const courseTitleEn = `Processing Progress Course ${Date.now()}`;
  await page.getByTestId("new-course-title-ar").fill("دورة تقدم المعالجة");
  await page.getByTestId("new-course-title-en").fill(courseTitleEn);
  await page.getByTestId("new-course-description-ar").fill("وصف");
  await page.getByTestId("new-course-description-en").fill("Processing progress evidence");
  await page.getByTestId("create-course").click();
  await expect(page.getByTestId("authoring-notice")).toContainText("Course created");
  await openAuthoringSections(page);
  const courseID = (await page.getByTestId("selected-course-context").getAttribute("data-course-id"))!;

  // A never-published Course offers the Admin gate, in those words.
  const submissionPanel = page.getByTestId("submission-panel");
  await expect(submissionPanel).toHaveAttribute("data-publication-mode", "FIRST_PUBLICATION");
  await expect(page.getByTestId("submit-for-review")).toHaveText("Submit for review");
  await expect(page.getByTestId("first-publication-note")).toContainText(
    "An administrator must approve this course before its first publication",
  );
  await submissionPanel.screenshot({ path: testInfo.outputPath("publication-first-submit-for-review.png") });

  await page.getByTestId("section-title-ar").fill("القسم");
  await page.getByTestId("section-title-en").fill("Progress Section");
  await page.getByTestId("add-section").click();
  const sectionRow = page.locator('[data-testid^="section-"]').first();
  await expect(sectionRow).toBeVisible();
  await expect(sectionRow).toContainText("Progress Section");
  const sectionID = (await sectionRow.getAttribute("data-testid"))!.replace("section-", "");
  await page.getByTestId(`lesson-title-ar-${sectionID}`).fill("الدرس");
  await page.getByTestId(`lesson-title-en-${sectionID}`).fill("Progress Lesson");
  await page.getByTestId(`add-lesson-${sectionID}`).click();
  const lessonID = (await page
    .locator('[data-testid^="lesson-video-upload-"]')
    .first()
    .getAttribute("data-testid"))!.replace("lesson-video-upload-", "");

  const phase = page.getByTestId(`lesson-video-phase-${lessonID}`);
  await page.getByTestId(`lesson-video-file-${lessonID}`).setInputFiles(mp4);

  // 1. Uploading, then the moment just after: the worker has taken over but has
  //    not measured anything yet, so the bar is indeterminate and shows no
  //    percentage rather than a fabricated zero. Against local object storage
  //    the upload itself finishes faster than a screenshot, so what this frame
  //    reliably captures is that honest indeterminate state.
  await expect(phase).toContainText("Uploading", { timeout: 60_000 });
  await page.getByTestId(`lesson-video-upload-${lessonID}`).screenshot({
    path: testInfo.outputPath("processing-indeterminate.png"),
  });

  // 2. Processing — the worker's measured percentage, mid-run. The wait is on
  //    a genuine intermediate value, which exists because the source is long
  //    enough for the ladder to take real time.
  await expect(phase).toContainText("Processing", { timeout: 2 * 60 * 1000 });
  await expect
    .poll(
      async () => Number((await phase.getAttribute("data-processing-percent")) ?? "-1"),
      { timeout: 3 * 60 * 1000, message: "the worker must report a measured intermediate percentage" },
    )
    .toBeGreaterThan(0);

  const midPercent = Number(await phase.getAttribute("data-processing-percent"));
  expect(midPercent).toBeGreaterThan(0);
  expect(midPercent).toBeLessThan(100);
  expect(["TRANSCODING", "PACKAGING"]).toContain(await phase.getAttribute("data-processing-stage"));
  await expect(phase).toContainText(`${midPercent}%`);
  await expect(page.getByTestId(`lesson-video-phase-${lessonID}-stage`)).toContainText(
    /Transcoding and preparing playback|Preparing playback/,
  );

  // Accessibility: a determinate bar reports the value it is actually showing.
  const bar = page.getByTestId(`lesson-video-upload-${lessonID}`).getByRole("progressbar");
  await expect(bar).toHaveAttribute("aria-valuemin", "0");
  await expect(bar).toHaveAttribute("aria-valuemax", "100");
  await expect(bar).toHaveAttribute("aria-valuenow", String(midPercent));
  await page.getByTestId(`lesson-video-upload-${lessonID}`).screenshot({
    path: testInfo.outputPath("processing-midway.png"),
  });

  // 3. A reload during processing recovers the run from the server. Nothing in
  //    the tab remembers it, so what comes back is the persisted observation.
  await page.reload();
  await page.getByTestId(`owned-course-${courseID}`).click();
  await openAuthoringSections(page);
  const recovered = page.getByTestId(`lesson-video-phase-${lessonID}`);
  await expect(recovered).toContainText(/Processing|Ready/, { timeout: 60_000 });
  if ((await recovered.textContent())?.includes("Processing")) {
    const recoveredPercent = Number((await recovered.getAttribute("data-processing-percent")) ?? "-1");
    expect(
      recoveredPercent,
      "after a reload the studio must show the server's own progress, not zero",
    ).toBeGreaterThanOrEqual(midPercent);
    await page.getByTestId(`lesson-video-upload-${lessonID}`).screenshot({
      path: testInfo.outputPath("processing-after-reload.png"),
    });
  }

  // 4. Ready.
  await expect(recovered).toContainText("Ready", { timeout: 4 * 60 * 1000 });
  await page.getByTestId(`lesson-video-upload-${lessonID}`).screenshot({
    path: testInfo.outputPath("processing-ready.png"),
  });

  // 5. Arabic and mobile, on a second real upload into the same Lesson so the
  //    processing surface is genuinely live in both.
  const arabicPage = await signedInInstructorPage(browser, "ar");
  await arabicPage.setViewportSize({ width: 390, height: 844 });
  await arabicPage.goto(`/ar/instructor/courses`);
  await arabicPage.getByTestId(`owned-course-${courseID}`).click();
  await openAuthoringSections(arabicPage);
  await expect(arabicPage.locator("html")).toHaveAttribute("dir", "rtl");
  await arabicPage.getByTestId(`lesson-video-file-${lessonID}`).setInputFiles(mp4);
  const arabicPhase = arabicPage.getByTestId(`lesson-video-phase-${lessonID}`);
  await expect(arabicPhase).toContainText("جارٍ المعالجة", { timeout: 3 * 60 * 1000 });
  await expect
    .poll(
      async () => Number((await arabicPhase.getAttribute("data-processing-percent")) ?? "-1"),
      { timeout: 3 * 60 * 1000 },
    )
    .toBeGreaterThan(0);
  await expect(arabicPage.getByTestId(`lesson-video-phase-${lessonID}-stage`)).toContainText("جارٍ");
  await arabicPage.getByTestId(`lesson-video-upload-${lessonID}`).screenshot({
    path: testInfo.outputPath("processing-arabic-mobile.png"),
  });
  await arabicPage.screenshot({ path: testInfo.outputPath("processing-arabic-mobile-page.png"), fullPage: true });

  await arabicPage.context().close();
  await page.context().close();
  await admin.dispose();
});
