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
const OTHER_INSTRUCTOR = { email: "instructor-other@example.test", accountID: "a0000000-0000-0000-0000-000000000004" };
const STUDENT = { email: "student-unentitled@example.test", accountID: "a0000000-0000-0000-0000-000000000099" };

/**
 * The Admin's exact words. Asserted verbatim on the Instructor's screen, so this proves the real
 * server-stored `review_reason` was rendered and not any hard-coded or inferred copy.
 */
const CHANGE_REQUEST_REASON = "Please update lesson 2 learning objectives";

const UUID_PATTERN = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i;

/** Course titles carry a run timestamp, so they are matched literally, never as a pattern. */
function escapeForRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

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

  // The media suite has its own isolated database, so it creates the minimum
  // canonical Academic Catalog context the normal Instructor flow requires.
  const institution = await admin.post("/api/v1/admin/academic/institutions", {
    data: {
      country_code: "KW", slug: "media-test-university",
      name_ar: "جامعة اختبار الوسائط", name_en: "Media Test University",
      max_academic_level: 4,
    },
  });
  expect(institution.status(), await institution.text()).toBe(201);
  const institutionID = (await institution.json() as { id: string }).id;
  const subject = await admin.post(`/api/v1/admin/academic/institutions/${institutionID}/subjects`, {
    data: { official_code: "CS101", title_ar: "برمجة", title_en: "Programming" },
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
  await page.getByTestId("new-course-institution").selectOption({ label: "Media Test University" });
  await page.getByTestId("new-course-subject-search").fill("CS101");
  await expect(page.getByTestId("new-course-subject-result")).toBeVisible();
  await page.getByTestId("new-course-subject-result").click();
  await page.getByTestId("new-course-title-ar").fill("دورة الفيديو الحقيقية");
  const courseTitleEn = `Real Video Course ${Date.now()}`;
  await page.getByTestId("new-course-title-en").fill(courseTitleEn);
  await page.getByTestId("new-course-description-ar").fill("وصف");
  await page.getByTestId("new-course-description-en").fill("Real media journey");
  await page.getByTestId("create-course").click();
  await expect(page.getByTestId("authoring-notice")).toContainText("Course created");
  const courseID = (await page.getByTestId("selected-course-context").getAttribute("data-course-id"))!;
  expect(courseID).toMatch(UUID_PATTERN);
  // The public preview is a second, separately uploaded PREVIEW Asset Version.
  // It is intentionally completed before any Lesson exists, which proves the
  // Instructor UI cannot be selecting or reusing protected Lesson media.
  const mp4Path = makeSampleMP4();
  const publicPreviewAuthoring = page.getByTestId("public-preview-authoring");
  await expect(publicPreviewAuthoring.getByTestId("public-preview-state")).toContainText("No public preview is selected");
  await publicPreviewAuthoring.locator('input[type="file"]').setInputFiles(mp4Path);
  await expect(publicPreviewAuthoring.getByTestId("public-preview-message")).toContainText("Public preview is ready for review", { timeout: 4 * 60 * 1000 });
  await expect(publicPreviewAuthoring.getByTestId("public-preview-state")).toContainText("A public preview is selected");

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
  await expect(attached).toHaveAttribute("data-video-attached", "true");

  // The asset version id comes from the API, which is the only place it was ever meaningful. The
  // studio used to print it beside the words "Video attached" and this spec scraped it back out of
  // the rendered text; an Instructor has no use for a media identifier, so it is no longer there.
  const instructorAPI = await apiContextFor(instructorSession);
  const authoredCourse = await instructorAPI.get(`/api/v1/courses/${courseID}`);
  expect(authoredCourse.status()).toBe(200);
  const authoredGraph = (await authoredCourse.json()) as {
    editable_revision?: { sections?: Array<{ lessons?: Array<{ id: string; video_asset_version_id?: string }> }> };
  };
  const authoredLesson = (authoredGraph.editable_revision?.sections ?? [])
    .flatMap((section) => section.lessons ?? [])
    .find((lesson) => lesson.id === lessonID);
  const assetVersionID = authoredLesson?.video_asset_version_id;
  expect(assetVersionID, "the submitted Lesson must carry its video Asset Version").toMatch(UUID_PATTERN);
  const assetStatus = await instructorAPI.get(`/api/v1/media/assets/${assetVersionID}`);
  expect(assetStatus.status()).toBe(200);
  expect((await assetStatus.json()).state).toBe("READY");

  // 6. Submission uses the Course-level canonical Subject; Academic Courses
  // never populate the legacy Major/Subject/Study-Year vocabulary.
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
  await expect(inspector.getByTestId("submitted-academic-university")).toContainText("Media Test University");
  await expect(inspector.getByTestId("submitted-academic-subject")).toContainText("CS101");
  await expect(inspector.getByTestId("submitted-academic-subject")).toContainText("Programming");
  await expect(inspector.getByTestId("submitted-academic-audience")).toContainText("Automatic");
  await expect(inspector.getByTestId("submitted-study-year")).toHaveCount(0);
  await expect(inspector.getByTestId("submitted-major")).toHaveCount(0);
  await expect(inspector.getByTestId("submitted-revision-state")).toContainText("PENDING_REVIEW");
  await expect(inspector.getByTestId("submitted-public-preview")).toContainText("A separate public preview is selected for this revision.");
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
  await inspector.getByTestId("request-changes-reason").fill(CHANGE_REQUEST_REASON);
  await inspector.getByTestId("submit-request-changes").click();
  expect((await requestChangesResponse).status()).toBe(200);
  await expect(adminPage.getByTestId("review-action-success")).toContainText("Change request sent to instructor");

  // ---------------------------------------------------------------------
  // MVP-F02 — the Instructor must be able to learn *why* the Course came
  // back, and resubmit, using nothing but the Instructor UI. Before this,
  // `review_reason` was written server-side and served to the owner, but no
  // Instructor surface read it: the only way to discover it was the database
  // or a hand-made API call, which is not a product.
  // ---------------------------------------------------------------------

  // 10a. The Instructor returns the ordinary way — reload the studio they
  // already had open. No deep link, no revision ID, no developer tools.
  await page.reload();
  await page.getByTestId(`owned-course-${courseID}`).click();

  // The standing notice is present, and carries the Admin's exact words.
  const changeRequest = page.getByTestId("change-request-notice");
  await expect(changeRequest).toBeVisible();
  await expect(page.getByTestId("change-request-reason")).toHaveText(CHANGE_REQUEST_REASON);

  // The state is explained in the Instructor's language; the wire enum is
  // still available for support, but it is not the explanation.
  await expect(page.getByTestId("revision-state")).toHaveText("Changes requested");
  await expect(page.getByTestId("revision-state")).toHaveAttribute("data-revision-state", "CHANGES_REQUESTED");

  // 10b. Another Instructor and a Student are refused the reason outright.
  // The reason travels with the owned-Course read, so refusing that read is
  // what protects it.
  const otherInstructorAPI = await apiContextFor(issueRotatingSession(OTHER_INSTRUCTOR));
  const otherRead = await otherInstructorAPI.get(`/api/v1/courses/${courseID}`);
  expect(
    otherRead.status(),
    "a non-owning Instructor must not read the change-request reason",
  ).toBeGreaterThanOrEqual(400);
  expect(await otherRead.text()).not.toContain(CHANGE_REQUEST_REASON);

  const studentAPI = await apiContextFor(issueRotatingSession(STUDENT));
  const studentRead = await studentAPI.get(`/api/v1/courses/${courseID}`);
  expect(studentRead.status(), "a Student must not read Instructor revision data").toBeGreaterThanOrEqual(400);
  expect(await studentRead.text()).not.toContain(CHANGE_REQUEST_REASON);

  // A Student must also not be able to perform the Admin review decision.
  const studentRequestChanges = await studentAPI.post(
    `/api/v1/admin/review/courses/${courseID}/revisions/${submittedRevisionID}/request-changes`,
    { data: { reason: "student attempt" } },
  );
  expect(
    studentRequestChanges.status(),
    "a Student must not perform the Admin request-changes mutation",
  ).toBeGreaterThanOrEqual(400);

  // The owning Instructor must not be able to request changes on their own
  // Course either — review is an Admin capability.
  const instructorRequestChanges = await instructorAPI.post(
    `/api/v1/admin/review/courses/${courseID}/revisions/${submittedRevisionID}/request-changes`,
    { data: { reason: "instructor attempt" } },
  );
  expect(
    instructorRequestChanges.status(),
    "an Instructor must not perform the Admin request-changes mutation",
  ).toBeGreaterThanOrEqual(400);

  // 10c. The Instructor edits the returned Course through the studio, then
  // resubmits through the studio. The raw submit API is deliberately not used
  // here: the point of this tranche is that the product can do it.
  const revisedTitleEn = `${courseTitleEn} (revised)`;
  await page.getByTestId("revision-title-en").fill(revisedTitleEn);
  await page.getByTestId("save-revision").click();
  await expect(page.getByTestId("authoring-notice")).toContainText("saved");

  await page.getByTestId("submit-for-review").click();
  await expect(page.getByTestId("authoring-notice")).toContainText("Course submitted for Admin review");

  // 10d. The resolved change request must not linger. The server keeps
  // `review_reason` on the revision row after a resubmission, so a surface
  // that rendered on the reason instead of the state would still be telling
  // the Instructor to fix something they already fixed.
  await expect(page.getByTestId("revision-state")).toHaveText("In review");
  await expect(page.getByTestId("revision-state")).toHaveAttribute("data-revision-state", "PENDING_REVIEW");
  await expect(page.getByTestId("change-request-notice")).toHaveCount(0);

  await otherInstructorAPI.dispose();
  await studentAPI.dispose();

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

  // ---------------------------------------------------------------------
  // MVP-F03 — the published Course must actually reach the public catalogue,
  // and nothing unpublished may reach it. Approval and publication are one
  // transaction (`swapLiveRevision` sets `lifecycle = 'PUBLISHED'` and moves
  // `live_revision_id`), so there is no asynchronous hop to wait for.
  // ---------------------------------------------------------------------

  // 12. A genuinely anonymous visitor. A fresh context with no cookies, so
  // this proves the *public* projection and not an Admin- or Instructor-only
  // view leaking through a warm session.
  const publicContext = await browser.newContext({ locale: "en-US" });
  const publicPage = await publicContext.newPage();

  await publicPage.goto("/en/catalog");
  await expect(publicPage.getByRole("heading", { name: "Catalogue", level: 1 })).toBeVisible();

  // Found the way a visitor finds it: by the human title, through the
  // catalogue's own search. No UUID, no slug typed by hand, no direct URL.
  await publicPage.getByRole("searchbox").fill(revisedTitleEn);
  await publicPage.getByRole("button", { name: "Search" }).click();

  const publicCard = publicPage.getByRole("link", { name: new RegExp(escapeForRegExp(revisedTitleEn)) });
  await expect(publicCard).toBeVisible();

  // The catalogue entry carries the Admin- and Instructor-configured data,
  // so publication did not lose Academic context, authorship, or price.
  const publicResults = publicPage.getByRole("region", { name: "Catalogue" });
  await expect(publicResults).toContainText("Media Test University");
  await expect(publicResults).toContainText("Programming");
  await expect(publicResults).toContainText("CS101");
  await expect(publicResults).toContainText("Dr. Instructor");
  // 25000 fils, rendered KWD with three decimals (D-045 keeps price informational).
  await expect(publicResults).toContainText("25.000");
  // Informational only: no in-platform commerce may appear (D-045).
  await expect(publicPage.locator("body")).not.toContainText(/add to cart|checkout|buy now/i);

  // 13. Opened through the catalogue's own affordance, not a constructed URL.
  await publicCard.click();
  await expect(publicPage.getByRole("heading", { level: 1 })).toContainText(revisedTitleEn);
  await expect(publicPage.locator("body")).toContainText("Media Section");
  const publicDetailURL = publicPage.url();

  // This is an anonymous course-scoped authorization: the browser supplies no
  // Asset Version identifier, and the public player receives the preview asset
  // only after the server resolves the approved live revision.
  const publicPreviewResponse = publicPage.waitForResponse((response) =>
    response.request().method() === "GET" &&
    new URL(response.url()).pathname === `/api/v1/media/courses/${courseID}/preview`,
  );
  await publicPage.getByRole("button", { name: "Watch preview" }).click();
  const issuedPreview = await publicPreviewResponse;
  expect(issuedPreview.status()).toBe(200);
  const previewAuthorization = await issuedPreview.json() as { url?: unknown };
  expect(typeof previewAuthorization.url).toBe("string");
  const publicPreviewPlayer = publicPage.getByTestId("public-preview-player");
  await expect(publicPreviewPlayer).toBeVisible();
  await expect(publicPreviewPlayer).toHaveAttribute("src", previewAuthorization.url as string);

  // The sibling Lesson Version is a protected asset, not the public preview.
  // No anonymous browser session can turn preview authorization into Lesson
  // authorization or an entitlement.
  const anonymousAPI = await playwrightRequest.newContext({ baseURL: frontendOrigin() });
  const protectedLessonAsPreview = await anonymousAPI.get(`/api/v1/media/previews/${assetVersionID}`);
  expect(protectedLessonAsPreview.status()).toBeGreaterThanOrEqual(400);
  await anonymousAPI.dispose();

  // 14. Negative visibility. A Course the Instructor never submitted must not
  // be public. Created through the real studio so its state is genuine.
  await page.getByTestId("toggle-new-course").click();
  await page.getByTestId("new-course-institution").selectOption({ label: "Media Test University" });
  await page.getByTestId("new-course-subject-search").fill("CS101");
  await expect(page.getByTestId("new-course-subject-result")).toBeVisible();
  await page.getByTestId("new-course-subject-result").click();
  const draftTitleEn = `Never Submitted Course ${Date.now()}`;
  await page.getByTestId("new-course-title-ar").fill("دورة لم تُرسل");
  await page.getByTestId("new-course-title-en").fill(draftTitleEn);
  await page.getByTestId("new-course-description-ar").fill("مسودة");
  await page.getByTestId("new-course-description-en").fill("Draft that must stay private");
  await page.getByTestId("create-course").click();
  await expect(page.getByTestId("authoring-notice")).toContainText("Course created");
  const draftCourseID = (await page.getByTestId("selected-course-context").getAttribute("data-course-id"))!;
  expect(draftCourseID).toMatch(UUID_PATTERN);
  await expect(page.getByTestId("revision-state")).toHaveAttribute("data-revision-state", "DRAFT");

  // The public API itself excludes it — this is the guarantee that matters.
  // `catalogpublic.PublishedOnly` is applied in SQL and the repository refuses
  // construction without a visibility predicate, so this is not frontend
  // filtering over a permissive response.
  // Anonymous, and explicitly English: the public API negotiates locale from
  // `Accept-Language` and defaults to Arabic, so an unlabelled request would
  // return Arabic titles and make English assertions vacuous.
  const anonymous = await playwrightRequest.newContext({
    baseURL: frontendOrigin(),
    extraHTTPHeaders: {
      Accept: "application/json, application/problem+json",
      "Accept-Language": "en",
    },
  });

  const draftSearch = await anonymous.get(`/api/v1/catalog/courses?q=${encodeURIComponent(draftTitleEn)}`);
  expect(draftSearch.status()).toBe(200);
  const draftSearchBody = await draftSearch.text();
  expect(draftSearchBody, "a DRAFT Course must not appear in public search").not.toContain(draftTitleEn);
  expect(draftSearchBody).not.toContain(draftCourseID);

  const draftDirect = await anonymous.get(`/api/v1/catalog/courses/${draftCourseID}`);
  expect(
    draftDirect.status(),
    "a direct public detail request for a DRAFT Course must not expose it",
  ).toBe(404);

  // The unpublished state is also absent from the rendered public catalogue.
  await publicPage.goto("/en/catalog");
  await publicPage.getByRole("searchbox").fill(draftTitleEn);
  await publicPage.getByRole("button", { name: "Search" }).click();
  await expect(publicPage.locator("body")).not.toContainText(draftTitleEn);

  // 15. Idempotency / invalid transition. The candidate is already APPROVED
  // and is now the live revision, so `lockPendingReviewCandidate` must refuse
  // a second approval rather than republishing or double-superseding.
  const secondApproval = await admin.post(
    `/api/v1/admin/review/courses/${courseID}/revisions/${submittedRevisionID}/approve`,
  );
  expect(
    [409, 422],
    `a second approval must be refused as an invalid transition, got ${secondApproval.status()}`,
  ).toContain(secondApproval.status());

  // The refusal changed nothing: the Course is still published and still the
  // same public entity.
  const stillPublic = await anonymous.get(new URL(publicDetailURL).pathname.replace("/en/catalog/", "/api/v1/catalog/courses/"));
  expect(stillPublic.status()).toBe(200);

  // 16. Public does not mean unprotected. Course Details being anonymous must
  // not carry protected learning access; entitlement enforcement itself is
  // proved in the S5/S6 suites, so this is a boundary assertion only.
  const anonymousLearn = await anonymous.get(`/api/v1/learn/courses/${courseID}`);
  expect(
    anonymousLearn.status(),
    "a public Course Details page must not grant anonymous protected-learning reads",
  ).toBeGreaterThanOrEqual(400);

  // 17. Revision isolation. A new candidate clones the live revision's whole
  // graph — sections, lessons, and `video_asset_version_id` — so revision B is
  // submittable without another upload. While B is PENDING_REVIEW, the public
  // catalogue must keep serving revision A: `PublishedOnly` joins on
  // `courses.live_revision_id = course_revisions.id`, and approval is the only
  // thing that moves that pointer.
  //
  // MVP-F11 — the candidate under test is created by *clicking the studio*, never by this spec
  // calling `PUT /api/v1/courses/:id/candidate`. The browser reaching that endpoint as a result of
  // the click is the point; substituting a raw request would prove nothing about the product.
  await page.reload();
  await page.getByTestId(`owned-course-${courseID}`).click();

  // A published Course is no longer a dead end: it offers the next step, and explains that the
  // published version keeps serving.
  await expect(page.getByTestId("no-editable-revision")).toHaveCount(0);
  const startPanel = page.getByTestId("start-revision-panel");
  await expect(startPanel).toBeVisible();
  await expect(startPanel).toContainText("This course is published");
  await expect(startPanel).toContainText("keeps serving until an administrator approves");

  await page.getByTestId("start-revision").click();

  // The studio moved into the new candidate, and says plainly that these edits are not live yet.
  await expect(page.getByTestId("revision-state")).toHaveAttribute("data-revision-state", "DRAFT");
  await expect(page.getByTestId("editing-published-notice")).toContainText(
    "Students still see the published version",
  );
  await expect(page.getByTestId("start-revision-panel")).toHaveCount(0);

  const candidateRevisionID = (await page.getByTestId("selected-course-context").getAttribute("data-revision-id"))!;
  expect(candidateRevisionID).toMatch(UUID_PATTERN);
  expect(candidateRevisionID, "the candidate must be a new revision, not the published one").not.toBe(
    submittedRevisionID,
  );

  // Clicking again must not fork the Course: the server returns the existing candidate.
  await page.reload();
  await page.getByTestId(`owned-course-${courseID}`).click();
  await expect(page.getByTestId("start-revision-panel")).toHaveCount(0);
  await expect(page.getByTestId("selected-course-context")).toHaveAttribute("data-revision-id", candidateRevisionID);

  // Edited and saved through the normal builder.
  const pendingRevisionTitleEn = `${revisedTitleEn} UNPUBLISHED REVISION`;
  await page.getByTestId("revision-title-en").fill(pendingRevisionTitleEn);
  await page.getByTestId("revision-title-ar").fill("مراجعة غير منشورة");
  await page.getByTestId("save-revision").click();
  await expect(page.getByTestId("authoring-notice")).toContainText("saved");

  // Isolation while B is still a DRAFT: the public Course is untouched by the edit.
  const draftStageDetail = await anonymous.get(`/api/v1/catalog/courses/${courseID}`);
  expect(draftStageDetail.status()).toBe(200);
  expect(
    await draftStageDetail.text(),
    "a saved DRAFT revision must not reach the public Course",
  ).not.toContain("UNPUBLISHED REVISION");

  // Submitted through the studio, not the API.
  await page.getByTestId("submit-for-review").click();
  await expect(page.getByTestId("authoring-notice")).toContainText("Course submitted for Admin review");
  await expect(page.getByTestId("revision-state")).toHaveAttribute("data-revision-state", "PENDING_REVIEW");

  // In review, the studio says so and offers no second revision.
  await page.reload();
  await page.getByTestId(`owned-course-${courseID}`).click();
  await expect(page.getByTestId("start-revision-panel")).toHaveCount(0);
  await expect(page.getByTestId("revision-state")).toHaveAttribute("data-revision-state", "PENDING_REVIEW");

  // Authorization: only the owning Instructor may begin a revision.
  const otherStart = await (await apiContextFor(issueRotatingSession(OTHER_INSTRUCTOR))).put(
    `/api/v1/courses/${courseID}/candidate`,
  );
  expect(
    otherStart.status(),
    "a non-owning Instructor must not create a candidate revision",
  ).toBeGreaterThanOrEqual(400);

  const studentStart = await (await apiContextFor(issueRotatingSession(STUDENT))).put(
    `/api/v1/courses/${courseID}/candidate`,
  );
  expect(studentStart.status(), "a Student must not create a candidate revision").toBeGreaterThanOrEqual(400);

  const anonymousStart = await anonymous.put(`/api/v1/courses/${courseID}/candidate`);
  expect(
    anonymousStart.status(),
    "an anonymous visitor must not create a candidate revision",
  ).toBeGreaterThanOrEqual(400);

  // A never-published Course has no live revision to clone, so the action must not be offered:
  // the studio already has an editable DRAFT there and `CreateCandidate` would refuse anyway.
  await page.getByTestId(`owned-course-${draftCourseID}`).click();
  await expect(page.getByTestId("start-revision-panel")).toHaveCount(0);
  await expect(page.getByTestId("editing-published-notice")).toHaveCount(0);
  await expect(page.getByTestId("revision-state")).toHaveAttribute("data-revision-state", "DRAFT");
  await page.getByTestId(`owned-course-${courseID}`).click();

  // Revision B is now PENDING_REVIEW while A stays live.
  const queueWithB = await admin.get("/api/v1/admin/review/queue");
  expect(queueWithB.status()).toBe(200);
  expect(
    ((await queueWithB.json()) as Array<{ course_id?: string }>).some((item) => item.course_id === courseID),
    "the pending revision must be back in the Admin review queue",
  ).toBe(true);

  // The pending revision's title must not be public anywhere.
  const pendingSearch = await anonymous.get(
    `/api/v1/catalog/courses?q=${encodeURIComponent(pendingRevisionTitleEn)}`,
  );
  expect(pendingSearch.status()).toBe(200);
  expect(
    await pendingSearch.text(),
    "a PENDING_REVIEW revision of a published Course must not leak publicly",
  ).not.toContain("UNPUBLISHED REVISION");

  // And the still-live revision A is what the public detail route serves.
  const liveDetail = await anonymous.get(`/api/v1/catalog/courses/${courseID}`);
  expect(liveDetail.status()).toBe(200);
  const liveDetailBody = await liveDetail.text();
  expect(liveDetailBody, "the public Course must still be revision A").toContain(revisedTitleEn);
  expect(liveDetailBody).not.toContain("UNPUBLISHED REVISION");

  // Same conclusion through the rendered public page, not only the API.
  await publicPage.goto(publicDetailURL);
  await expect(publicPage.getByRole("heading", { level: 1 })).toContainText(revisedTitleEn);
  await expect(publicPage.locator("body")).not.toContainText("UNPUBLISHED REVISION");

  await anonymous.dispose();
  await publicContext.close();

  await adminContext.close();
  await instructorAPI.dispose();
  await admin.dispose();
  await context.close();
});
