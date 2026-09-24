import { execFileSync } from "child_process";
import fs from "fs";
import os from "os";
import path from "path";
import {
  test,
  expect,
  request as playwrightRequest,
  type APIRequestContext,
} from "@playwright/test";
import { issueRotatingSession } from "../rotating-students";
import { frontendOrigin } from "../../src/lib/api/e2e-ports";
import { captureFailureDiagnostic } from "./diagnostics";
import { openAuthoringSections } from "../authoring-sections";

/**
 * The anonymous Lesson preview journey, with nothing simulated.
 *
 * # WHY THIS SPEC EXISTS
 *
 * The Lesson preview model is proved at integration level against a real database: the
 * authorization chain, the separate token domain, the replay refusals, and the proof that
 * marking a Lesson previewable creates no media of its own. None of that proves the thing a
 * visitor actually cares about — that pressing the control yields *moving pictures*, from the
 * same transcode a paying Student gets, to a browser holding no session at all.
 *
 * So this runs on the media stack: real MinIO, a real worker, real ffmpeg. A genuine MP4 is
 * uploaded through the real presigned intent, the worker scans and transcodes it, the Instructor
 * ticks the new control, an Administrator sees what they are approving and approves it, and then
 * a browser with **no cookies** plays it and is required to report a decoded frame size and a
 * duration.
 *
 * # WHAT IT ALSO REFUSES TO ASSUME
 *
 * The boundaries are re-checked through the real router and the real browser, not only in a
 * package test: the preview is unreachable before approval, the Lesson's protected Student
 * playback still refuses the anonymous visitor, the Lesson's resources stay protected, and the
 * manifest the page plays is an application route rather than a storage URL.
 */

const INSTRUCTOR = {
  email: "instructor@example.test",
  accountID: "a0000000-0000-0000-0000-000000000003",
};
const ADMIN = {
  email: "admin@example.test",
  accountID: "a0000000-0000-0000-0000-000000000000",
};

const UUID_PATTERN = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i;

type Session = ReturnType<typeof issueRotatingSession>;

function apiContextFor(session: Session): Promise<APIRequestContext> {
  return playwrightRequest.newContext({
    baseURL: frontendOrigin(),
    extraHTTPHeaders: {
      Accept: "application/json, application/problem+json",
      Origin: frontendOrigin(),
      Cookie: `${session.cookie_name}=${session.cookie_value}`,
      "X-CSRF-Token": session.csrf_token,
    },
  });
}

/** A small but genuine H.264/AAC MP4 — real bytes with a real container. */
function makeSampleMP4(): string {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gradex-lesson-preview-mp4-"));
  const file = path.join(directory, "lesson.mp4");
  execFileSync(
    "ffmpeg",
    [
      "-y",
      "-f", "lavfi", "-i", "testsrc=size=320x240:rate=15",
      "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100",
      "-t", "2",
      "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
      "-c:a", "aac", "-b:a", "64k",
      "-movflags", "+faststart",
      file,
    ],
    { stdio: "ignore" },
  );
  return file;
}

test.afterEach(async ({}, testInfo) => {
  if (testInfo.status === testInfo.expectedStatus) return;
  try {
    const artifact = captureFailureDiagnostic();
    if (artifact) console.error(`[Media E2E Failure] sanitized diagnostic artifact: ${artifact}`);
  } catch {
    console.error("[Media E2E Failure] diagnostic collector could not complete");
  }
});

test("a free Lesson is offered by its Instructor, approved by an Admin, and watched by a visitor with no account", async ({
  browser,
}, testInfo) => {
  test.setTimeout(12 * 60 * 1000);
  const admin = await apiContextFor(issueRotatingSession(ADMIN));

  // The media suite has its own isolated database, so it creates the minimum canonical Academic
  // Catalog context the Instructor flow requires.
  const institution = await admin.post("/api/v1/admin/academic/institutions", {
    data: {
      country_code: "KW",
      slug: "lesson-preview-university",
      name_ar: "جامعة معاينة الدرس",
      name_en: "Lesson Preview University",
      max_academic_level: 4,
    },
  });
  expect(institution.status(), await institution.text()).toBe(201);
  const institutionID = ((await institution.json()) as { id: string }).id;
  const subject = await admin.post(
    `/api/v1/admin/academic/institutions/${institutionID}/subjects`,
    { data: { official_code: "CS201", title_ar: "خوارزميات", title_en: "Algorithms" } },
  );
  expect(subject.status(), await subject.text()).toBe(201);

  const context = await browser.newContext({ locale: "en-US" });
  const instructorSession = issueRotatingSession(INSTRUCTOR);
  const origin = new URL(frontendOrigin());
  await context.addInitScript(() => {
    window.localStorage.setItem("gradex.locale", "en");
  });
  await context.addCookies([
    {
      name: instructorSession.cookie_name,
      value: instructorSession.cookie_value,
      domain: origin.hostname,
      path: "/",
      httpOnly: true,
      secure: true,
      sameSite: "Strict",
    },
  ]);
  const page = await context.newPage();

  // 1. Course, section, lesson.
  await page.goto("/en/instructor/courses");
  await expect(page.locator("h1")).toContainText("Course Authoring Studio");
  await page.getByTestId("toggle-new-course").click();
  await page
    .getByTestId("new-course-institution")
    .selectOption({ label: "Lesson Preview University" });
  await page.getByTestId("new-course-subject-search").fill("CS201");
  await expect(page.getByTestId("new-course-subject-result")).toBeVisible();
  await page.getByTestId("new-course-subject-result").click();
  await page.getByTestId("new-course-title-ar").fill("دورة المعاينة المجانية");
  const courseTitleEn = `Free Preview Course ${Date.now()}`;
  await page.getByTestId("new-course-title-en").fill(courseTitleEn);
  await page.getByTestId("new-course-description-ar").fill("وصف");
  await page.getByTestId("new-course-description-en").fill("Free lesson preview journey");
  await page.getByTestId("create-course").click();
  await expect(page.getByTestId("authoring-notice")).toContainText("Course created");
  await openAuthoringSections(page);
  const courseID = (await page
    .getByTestId("selected-course-context")
    .getAttribute("data-course-id"))!;
  expect(courseID).toMatch(UUID_PATTERN);

  await page.getByTestId("section-title-ar").fill("القسم");
  await page.getByTestId("section-title-en").fill("Preview Section");
  await page.getByTestId("add-section").click();
  const sectionRow = page.locator('[data-testid^="section-"]').first();
  await expect(sectionRow).toBeVisible();
  const sectionID = (await sectionRow.getAttribute("data-testid"))!.replace("section-", "");

  const freeLessonTitle = "Free Lesson";
  await page.getByTestId(`lesson-title-ar-${sectionID}`).fill("الدرس المجاني");
  await page.getByTestId(`lesson-title-en-${sectionID}`).fill(freeLessonTitle);
  await page.getByTestId(`add-lesson-${sectionID}`).click();
  await expect(sectionRow).toContainText(freeLessonTitle);
  const lessonID = (await page
    .locator('[data-testid^="lesson-video-upload-"]')
    .first()
    .getAttribute("data-testid"))!.replace("lesson-video-upload-", "");

  // 2. The control refuses a Lesson with nothing to show, before any video exists.
  const toggleRow = page
    .locator('[data-testid="lesson-public-preview-toggle"]')
    .first();
  await expect(toggleRow).toBeDisabled();
  await expect(page.getByTestId("lesson-public-preview-needs-video").first()).toBeVisible();

  // 3. A real MP4 through the real upload contract, to READY.
  const mp4Path = makeSampleMP4();
  await page.getByTestId(`lesson-video-file-${lessonID}`).setInputFiles(mp4Path);
  const phase = page.getByTestId(`lesson-video-phase-${lessonID}`);
  await expect(phase).toContainText(/Preparing|Uploading|Processing/, { timeout: 30_000 });
  await expect(phase).toContainText("Ready", { timeout: 6 * 60 * 1000 });

  // 4. Now the Instructor offers it free. This is the new control, on the real page.
  await expect(toggleRow).toBeEnabled();
  await expect(toggleRow).not.toBeChecked();
  await toggleRow.check();
  await expect(toggleRow).toBeChecked();

  // The helper text states the one thing this control could most easily lie about.
  await expect(page.getByText("after an administrator approves this version").first()).toBeVisible();

  // It survives a reload, because it is durable revision state rather than local UI state.
  await page.reload();
  await openAuthoringSections(page);
  await expect(page.locator('[data-testid="lesson-public-preview-toggle"]').first()).toBeChecked();

  // 5. Before approval the preview is unreachable to everyone. The Course is not even published.
  const anonymousBeforeApproval = await playwrightRequest.newContext({
    baseURL: frontendOrigin(),
  });
  const refusedEarly = await anonymousBeforeApproval.post(
    `/api/v1/media/courses/${courseID}/lessons/${lessonID}/preview-authorizations`,
    { headers: { Origin: frontendOrigin() } },
  );
  expect(
    refusedEarly.ok(),
    "an unapproved Lesson preview must stay unreachable anonymously",
  ).toBeFalsy();
  await anonymousBeforeApproval.dispose();

  // 6. The Course needs a price before it can be approved. Pricing is an Admin
  // authority and has nothing to do with preview; it is set here only because a
  // Course with no price is not publishable, and this journey needs a published one.
  const priced = await admin.put(`/api/v1/admin/courses/${courseID}/price`, {
    data: { price_minor_units: 25000, reason: "Lesson preview journey pricing" },
  });
  expect(priced.status(), await priced.text()).toBe(200);

  // 7. Submit, and read what the Administrator is being asked to approve.
  await page.getByTestId("submit-for-review").click();
  await page.getByTestId("submit-confirm").getByTestId("confirm-accept").click();
  await expect(page.getByTestId("authoring-notice")).toContainText(
    "Submitted. An administrator will review it",
  );

  const queue = await admin.get("/api/v1/admin/review/queue");
  expect(queue.status()).toBe(200);
  const queued = (await queue.json()) as Array<{
    course_id?: string;
    id?: string;
    revision_id?: string;
  }>;
  const item = queued.find((entry) => entry.course_id === courseID || entry.id === courseID);
  expect(item, `submitted Course ${courseID} must appear in the Admin review queue`).toBeTruthy();
  const revisionID = item!.revision_id!;

  // The submitted revision payload carries the intent, so the review surface can show it.
  const reviewed = await admin.get(
    `/api/v1/admin/review/courses/${courseID}/revisions/${revisionID}`,
  );
  expect(reviewed.status(), await reviewed.text()).toBe(200);
  const graph = (await reviewed.json()) as {
    editable_revision?: {
      sections?: Array<{ lessons?: Array<{ id: string; allow_public_preview?: boolean }> }>;
    };
  };
  const reviewedLesson = (graph.editable_revision?.sections ?? [])
    .flatMap((section) => section.lessons ?? [])
    .find((lesson) => lesson.id === lessonID);
  expect(
    reviewedLesson?.allow_public_preview,
    "the Administrator must be able to see which Lessons become publicly watchable",
  ).toBe(true);

  // And the Admin *screen* says so too, in words that name the consequence.
  const adminContext = await browser.newContext({ locale: "en-US" });
  const adminSession = issueRotatingSession(ADMIN);
  await adminContext.addCookies([
    {
      name: adminSession.cookie_name,
      value: adminSession.cookie_value,
      domain: origin.hostname,
      path: "/",
      httpOnly: true,
      secure: true,
      sameSite: "Strict",
    },
  ]);
  const adminPage = await adminContext.newPage();
  await adminPage.goto(`/en/admin/courses/${courseID}/review`);
  const badge = adminPage.getByTestId(`submitted-lesson-public-preview-${lessonID}`);
  await expect(badge).toBeVisible({ timeout: 30_000 });
  await expect(badge).toContainText("anyone will be able to watch this lesson");
  await adminPage.screenshot({ path: testInfo.outputPath("admin-review-free-lesson.png") });

  // 8. Approve. This is the moment the intent becomes public.
  const approved = await admin.post(
    `/api/v1/admin/review/courses/${courseID}/revisions/${revisionID}/approve`,
  );
  expect(approved.status(), await approved.text()).toBe(200);

  // 9. A visitor with no account at all.
  const visitorContext = await browser.newContext({ locale: "en-US" });
  const visitor = await visitorContext.newPage();
  await visitor.goto(`/en/catalog/${courseID}`);

  // The public projection first, so a failure here names the layer that broke rather than timing
  // out on a locator and leaving the cause ambiguous.
  const publicAPI = await playwrightRequest.newContext({ baseURL: frontendOrigin() });
  const detail = await publicAPI.get(`/api/v1/catalog/courses/${courseID}`, {
    headers: { "Accept-Language": "en" },
  });
  expect(detail.status(), await detail.text()).toBe(200);
  const detailBody = (await detail.json()) as {
    has_preview?: boolean;
    sections?: Array<{
      lesson_count?: number;
      lessons?: Array<{ id: string; title: string }>;
    }>;
  };
  await publicAPI.dispose();
  const projected = (detailBody.sections ?? []).flatMap((section) => section.lessons ?? []);
  expect(
    projected.map((lesson) => lesson.id),
    `the live projection must offer the free Lesson: ${JSON.stringify(detailBody)}`,
  ).toContain(lessonID);
  expect(detailBody.has_preview, "has_preview is derived from the free Lesson").toBe(true);

  const previewLesson = visitor.getByTestId("course-curriculum-preview-lesson").first();
  await expect(previewLesson).toBeVisible({ timeout: 30_000 });
  await expect(previewLesson).toContainText(freeLessonTitle);
  await expect(
    previewLesson.getByTestId("course-curriculum-preview-badge"),
  ).toContainText("Free preview");

  // The outline still reports the whole section, not just its free lesson.
  await expect(visitor.getByTestId("course-curriculum-section").first()).toContainText("1 lessons");

  // Nothing has been requested yet: the capability is minted on activation.
  const previewRequests: string[] = [];
  visitor.on("request", (request) => {
    const { pathname } = new URL(request.url());
    if (pathname.includes("/preview-authorizations") || pathname.includes("/lesson-previews/"))
      previewRequests.push(`${request.method()} ${pathname}`);
  });
  expect(previewRequests).toHaveLength(0);

  await previewLesson.getByTestId("lesson-preview-play").click();

  const video = visitor.getByTestId("lesson-preview-video");
  await expect(video).toBeVisible({ timeout: 60_000 });

  // Moving pictures, not a valid URL that resolves to nothing.
  await expect
    .poll(
      () => video.evaluate((element: HTMLVideoElement) => element.videoWidth),
      { timeout: 60_000, message: "the anonymous preview must decode real video" },
    )
    .toBeGreaterThan(0);
  await expect
    .poll(() => video.evaluate((element: HTMLVideoElement) => element.duration), {
      timeout: 60_000,
    })
    .toBeGreaterThan(0);
  await visitor.screenshot({ path: testInfo.outputPath("anonymous-lesson-preview.png") });

  // The element plays an application route. A storage URL here would mean the anonymous path had
  // reached the original uploaded object.
  const playedSource = await video.evaluate(
    (element: HTMLVideoElement) => element.currentSrc || element.src,
  );
  expect(playedSource).not.toMatch(/quarantine|\.mp4(\?|$)/i);

  const authorizationCalls = previewRequests.filter((entry) =>
    entry.includes("/preview-authorizations"),
  );
  expect(
    authorizationCalls.length,
    "exactly one capability per activation",
  ).toBe(1);
  expect(
    previewRequests.some((entry) => entry.includes("/lesson-previews/")),
    "the player must fetch the application manifest route",
  ).toBeTruthy();

  // 10. And the paid surface still refuses this visitor.
  const anonymous = await playwrightRequest.newContext({ baseURL: frontendOrigin() });
  const protectedPlayback = await anonymous.post("/api/v1/media/playback-authorizations", {
    headers: { Origin: frontendOrigin() },
    data: { lesson_id: lessonID, asset_version_id: "00000000-0000-0000-0000-000000000000" },
  });
  expect(
    protectedPlayback.ok(),
    "anonymous protected Student playback must stay refused",
  ).toBeFalsy();

  // A Lesson offered free opens its video and nothing else.
  const resourceEntry = await anonymous.get(`/api/v1/media/lessons/${lessonID}/materials/resource`);
  expect(
    resourceEntry.ok(),
    "Lesson resources stay entitlement-protected on a previewable Lesson",
  ).toBeFalsy();
  await anonymous.dispose();

  // 11. Arabic and a phone viewport, on the same live Course.
  await visitor.goto(`/ar/catalog/${courseID}`);
  const arabicPreview = visitor.getByTestId("course-curriculum-preview-lesson").first();
  await expect(arabicPreview).toBeVisible({ timeout: 30_000 });
  await expect(
    arabicPreview.getByTestId("course-curriculum-preview-badge"),
  ).toContainText("معاينة مجانية");
  await expect(visitor.locator("html")).toHaveAttribute("dir", "rtl");
  await visitor.setViewportSize({ width: 390, height: 844 });
  await arabicPreview.scrollIntoViewIfNeeded();
  await expect(arabicPreview.getByTestId("lesson-preview-play")).toBeVisible();
  await visitor.screenshot({ path: testInfo.outputPath("anonymous-lesson-preview-arabic-mobile.png") });

  await visitorContext.close();
  await adminContext.close();
  await context.close();
  await admin.dispose();
});
