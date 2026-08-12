import { execFileSync } from "child_process";
import fs from "fs";
import os from "os";
import path from "path";
import { test, expect, request as playwrightRequest, type APIRequestContext } from "@playwright/test";
import { issueRotatingSession } from "../rotating-students";
import { frontendOrigin } from "../../src/lib/api/e2e-ports";
import { captureFailureDiagnostic, recordMediaAssetVersionID } from "./diagnostics";

/**
 * Real Instructor video journey.
 *
 * Nothing here is simulated. A genuine MP4 is produced by ffmpeg, the browser
 * uploads it directly to private object storage through the presigned intent,
 * the API verifies the exact stored object version, the worker scans and
 * transcodes it, and only then is the resulting Asset Version attached to the
 * Lesson. The attachment is re-read after a full page reload, and the complete
 * Course is submitted and observed in the Admin review queue.
 */

const INSTRUCTOR = { email: "instructor@example.test", accountID: "a0000000-0000-0000-0000-000000000003" };
const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };

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
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gradex-authoring-mp4-"));
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

test("C an Instructor uploads a real MP4, the worker makes it READY, and the attachment survives a reload", async ({
  browser,
}) => {
  const admin = await apiContextFor(issueRotatingSession(ADMIN));

  // Taxonomy is Admin-owned, so the Admin creates the terms the Instructor
  // will later assign. Submission validation requires both dimensions.
  const major = await admin.post("/api/v1/admin/taxonomy/terms", {
    // An academic code belongs to a SUBJECT only; the API refuses one on a MAJOR.
    data: { kind: "MAJOR", label_ar: "هندسة", label_en: "Engineering" },
  });
  expect(major.status(), await major.text()).toBe(201);
  const subject = await admin.post("/api/v1/admin/taxonomy/terms", {
    data: { kind: "SUBJECT", label_ar: "برمجة", label_en: "Programming", academic_code: "CS101" },
  });
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
  page.on("response", async (response) => {
    if (response.request().method() !== "POST" || new URL(response.url()).pathname !== "/api/v1/media/uploads" || response.status() !== 201) return;
    try {
      const body = await response.json() as { asset_version_id?: unknown };
      if (typeof body.asset_version_id === "string") recordMediaAssetVersionID(body.asset_version_id);
    } catch {}
  });
  await page.goto("/en/instructor/courses");
  await expect(page.locator("h1")).toContainText("Course Authoring Studio");

  // 1. Course
  await page.getByTestId("toggle-new-course").click();
  await page.getByTestId("new-course-title-ar").fill("دورة الفيديو الحقيقية");
  const courseTitleEn = `Real Video Course ${Date.now()}`;
  await page.getByTestId("new-course-title-en").fill(courseTitleEn);
  await page.getByTestId("new-course-description-ar").fill("وصف");
  await page.getByTestId("new-course-description-en").fill("Real media journey");
  await page.getByTestId("create-course").click();
  await expect(page.getByTestId("authoring-notice")).toContainText("Course created on the server");
  const courseID = (await page.getByTestId("selected-course-id").innerText()).match(UUID_PATTERN)![0];

  // 2. Section
  await page.getByTestId("section-title-ar").fill("القسم");
  await page.getByTestId("section-title-en").fill("Media Section");
  await page.getByTestId("add-section").click();
  await expect(page.getByText("Media Section")).toBeVisible();
  const sectionID = (await page
    .locator('[data-testid^="section-"]')
    .first()
    .getAttribute("data-testid"))!.replace("section-", "");

  // 3. Lesson
  await page.getByTestId(`lesson-title-ar-${sectionID}`).fill("الدرس");
  await page.getByTestId(`lesson-title-en-${sectionID}`).fill("Media Lesson");
  await page.getByTestId(`add-lesson-${sectionID}`).click();
  await expect(page.getByText("Media Lesson")).toBeVisible();
  const lessonID = (await page
    .locator('[data-testid^="lesson-video-upload-"]')
    .first()
    .getAttribute("data-testid"))!.replace("lesson-video-upload-", "");

  // 4. Real MP4 through the real upload contract.
  const mp4Path = makeSampleMP4();
  await page.getByTestId(`lesson-video-file-${lessonID}`).setInputFiles(mp4Path);

  const phase = page.getByTestId(`lesson-video-phase-${lessonID}`);
  await expect(phase).toContainText(/Preparing|Uploading|Processing/, { timeout: 30_000 });
  await expect(phase).toContainText("Ready", { timeout: 4 * 60 * 1000 });
  await expect(page.getByTestId(`lesson-video-ref-${lessonID}`)).toBeVisible();

  // 5. The attachment is server state, not a rendering of what just happened.
  await page.reload();
  await page.getByTestId(`owned-course-${courseID}`).click();
  const attached = page.getByTestId(`lesson-video-ref-${lessonID}`);
  await expect(attached).toBeVisible();
  const assetVersionID = (await attached.innerText()).match(UUID_PATTERN)![0];

  const instructorAPI = await apiContextFor(instructorSession);
  const assetStatus = await instructorAPI.get(`/api/v1/media/assets/${assetVersionID}`);
  expect(assetStatus.status()).toBe(200);
  expect((await assetStatus.json()).state).toBe("READY");

  // 6. Taxonomy and study year, then submission.
  // The taxonomy panel names the revision it writes to, so its Course must be
  // chosen before the dimension selects become usable.
  await page.getByTestId("taxonomy-course").selectOption({ label: courseTitleEn });
  await page.getByLabel("Major").selectOption({ label: "Engineering" });
  await page.getByLabel("Subject").selectOption({ label: "Programming (CS101)" });
  await page.getByRole("button", { name: "Save Taxonomy" }).click();
  await expect(page.getByText("Taxonomy saved for the named revision")).toBeVisible();

  await page.getByTestId("revision-study-year").selectOption("YEAR_1");
  await page.getByTestId("save-revision").click();
  await expect(page.getByTestId("authoring-notice")).toContainText("Revision details saved");

  await page.getByTestId("submit-for-review").click();
  await expect(page.getByTestId("authoring-notice")).toContainText("Course submitted for Admin review");

  // 7. The Admin review surface sees the submitted revision.
  const queue = await admin.get("/api/v1/admin/review/queue");
  expect(queue.status()).toBe(200);
  const queued = (await queue.json()) as Array<{ course_id?: string; id?: string; revision_id?: string; state?: string }>;
  const submittedQueueItem = queued.find((item) => item.course_id === courseID || item.id === courseID);
  expect(
    submittedQueueItem,
    `submitted Course ${courseID} must appear in the Admin review queue`,
  ).toBeTruthy();
  expect(submittedQueueItem?.revision_id, "the queue must carry the submitted revision ID").toBeTruthy();
  const submittedRevisionID = submittedQueueItem!.revision_id!;

  // 8. And the Admin *screen* sees it too, not only the API. The Admin Catalog
  // reads the same queue, so a real submission is what appears there — this is
  // the founder journey that previously showed a demo Course instead.
  const adminSession = issueRotatingSession(ADMIN);
  const adminContext = await browser.newContext({ locale: "en-US" });
  await adminContext.addInitScript(() => {
    window.localStorage.setItem("gradex.locale", "en");
  });
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
  await adminPage.goto("/en/admin/catalog");
  await expect(adminPage.locator("h1")).toContainText("Course Review & Pricing Admin");

  const row = adminPage.getByTestId(`review-item-${courseID}`);
  await expect(row).toBeVisible();
  await expect(row).toContainText(courseTitleEn);
  await expect(adminPage.locator("body")).not.toContainText("Introduction to Programming");

  // 9. The Admin opens the exact submitted revision and reads the immutable
  // graph before making a review decision. These assertions use the Course
  // authored above, never a fixture or mutable Instructor draft.
  await adminPage.getByTestId(`inspect-review-item-${courseID}`).click();
  const inspector = adminPage.getByTestId("submitted-revision-inspector");
  await expect(inspector).toBeVisible();
  await expect(inspector.getByTestId("submitted-title-ar")).toContainText("دورة الفيديو الحقيقية");
  await expect(inspector.getByTestId("submitted-title-en")).toContainText(courseTitleEn);
  await expect(inspector.getByTestId("submitted-description-ar")).toContainText("وصف");
  await expect(inspector.getByTestId("submitted-description-en")).toContainText("Real media journey");
  await expect(inspector.getByTestId("submitted-study-year")).toContainText("YEAR_1");
  await expect(inspector.getByTestId("submitted-major")).toContainText("Engineering");
  await expect(inspector.getByTestId("submitted-subject")).toContainText("Programming");
  await expect(inspector.getByTestId("submitted-revision-state")).toContainText("PENDING_REVIEW");
  await expect(inspector.getByTestId(`submitted-section-${sectionID}`)).toContainText("Media Section");
  await expect(inspector.getByTestId(`submitted-lesson-${lessonID}`)).toContainText("Media Lesson");
  await expect(inspector.getByTestId(`submitted-lesson-media-state-${lessonID}`)).toContainText("READY");

  // Preview is issued through the reviewed Lesson endpoint and receives the
  // application-owned protected manifest route. The browser never receives a
  // private object-storage URL from this UI.
  const previewResponse = adminPage.waitForResponse((response) =>
    response.request().method() === "POST" &&
    new URL(response.url()).pathname === `/api/v1/admin/review/courses/${courseID}/revisions/${submittedRevisionID}/preview/${lessonID}`,
  );
  await inspector.getByTestId(`preview-submitted-lesson-${lessonID}`).click();
  expect((await previewResponse).status()).toBe(200);
  await expect(inspector.getByTestId("review-preview-player")).toBeVisible();
  await expect(inspector.getByTestId("review-protected-video")).toBeVisible();

  // Pricing shares this exact submitted-revision context. The Admin chooses
  // the human titles while the stable Section identity stays inside the UI.
  await inspector.getByTestId("pricing-amount").fill("25000");
  await inspector.getByTestId("pricing-reason").fill("Founder acceptance Course pricing");
  const coursePriceResponse = adminPage.waitForResponse((response) =>
    response.request().method() === "PUT" &&
    new URL(response.url()).pathname === `/api/v1/admin/courses/${courseID}/price`,
  );
  await inspector.getByTestId("pricing-submit").click();
  expect((await coursePriceResponse).status()).toBe(200);
  await expect(inspector.getByTestId("pricing-success")).toContainText("Successfully updated Course price");

  await inspector.getByTestId("pricing-scope-select").selectOption("SECTION");
  const sectionOption = inspector.getByTestId("pricing-section-select");
  await expect(sectionOption.getByRole("option", { name: /Media Section.*القسم/ })).toHaveCount(1);
  await sectionOption.selectOption(sectionID);
  await inspector.getByTestId("pricing-amount").fill("10000");
  await inspector.getByTestId("pricing-reason").fill("Founder acceptance Section pricing");
  const sectionPriceResponse = adminPage.waitForResponse((response) =>
    response.request().method() === "PUT" &&
    new URL(response.url()).pathname === `/api/v1/admin/courses/${courseID}/sections/${sectionID}/price`,
  );
  await inspector.getByTestId("pricing-submit").click();
  expect((await sectionPriceResponse).status()).toBe(200);
  await expect(inspector.getByTestId("pricing-success")).toContainText("Successfully updated Section price");

  // 10. Request changes through the inspector with an explicit Instructor
  // reason. The Instructor resubmits the exact revision, then the Admin
  // reopens it and approves from that same submitted-only surface.
  const requestChangesResponse = adminPage.waitForResponse((response) =>
    response.request().method() === "POST" &&
    new URL(response.url()).pathname === `/api/v1/admin/review/courses/${courseID}/revisions/${submittedRevisionID}/request-changes`,
  );
  await inspector.getByTestId("request-changes-inspected-revision").click();
  await inspector.getByTestId("request-changes-reason").fill("Please expand the Arabic lesson detail.");
  await inspector.getByTestId("submit-request-changes").click();
  expect((await requestChangesResponse).status()).toBe(200);
  await expect(adminPage.getByTestId("review-action-success")).toContainText("Change request sent to instructor");

  const resubmission = await instructorAPI.post(`/api/v1/courses/${courseID}/revisions/${submittedRevisionID}/submit`);
  expect(resubmission.status(), await resubmission.text()).toBe(200);
  await adminPage.reload();
  await expect(adminPage.getByTestId(`review-item-${courseID}`)).toBeVisible();
  await adminPage.getByTestId(`inspect-review-item-${courseID}`).click();
  const resubmittedInspector = adminPage.getByTestId("submitted-revision-inspector");
  await expect(resubmittedInspector.getByTestId("submitted-revision-state")).toContainText("PENDING_REVIEW");

  // 11. Approve through the inspector, against the exact revision that was
  // successfully rendered and previewed above.
  await resubmittedInspector.getByTestId("approve-inspected-revision").click();
  await expect(adminPage.getByTestId("review-action-success")).toContainText("Course published successfully");
  await expect(adminPage.getByTestId(`review-item-${courseID}`)).toHaveCount(0);

  const afterApproval = await admin.get("/api/v1/admin/review/queue");
  expect(afterApproval.status()).toBe(200);
  const remaining = (await afterApproval.json()) as Array<{ course_id?: string }>;
  expect(
    remaining.some((item) => item.course_id === courseID),
    "an approved Course must leave the server's review queue",
  ).toBe(false);

  const published = await admin.get(`/api/v1/admin/courses/${courseID}/price-history`);
  expect(published.status(), "the approved Course must still be addressable by the Admin routes").toBe(200);

  await adminContext.close();
  await instructorAPI.dispose();
  await admin.dispose();
  await context.close();
});
