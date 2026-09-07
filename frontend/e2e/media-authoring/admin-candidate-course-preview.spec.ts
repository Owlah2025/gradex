import { execFileSync } from "child_process";
import fs from "fs";
import os from "os";
import path from "path";
import { test, expect, request as playwrightRequest, type APIRequestContext, type Browser } from "@playwright/test";
import { issueRotatingSession } from "../rotating-students";
import { frontendOrigin } from "../../src/lib/api/e2e-ports";
import { RUN_STATE_FILE_PATH } from "../../src/lib/api/e2e-infrastructure";
import { captureFailureDiagnostic } from "./diagnostics";
import { openAuthoringSections } from "../authoring-sections";

/**
 * The Admin candidate course-preview journey, with nothing simulated.
 *
 * # WHY THIS SPEC EXISTS SEPARATELY
 *
 * The public preview route correctly refuses anything that is not the live, `APPROVED` revision, so
 * before publication the one asset every visitor meets first was the one asset the reviewing Admin
 * could not watch. The route that closes that gap is proved at unit and integration level against a
 * real database, but neither of those proves the thing that actually matters to an Admin: that
 * pressing the control yields *moving pictures*. A signed URL that resolves to nothing is a valid
 * URL and a useless review.
 *
 * So this journey runs on the media stack — real MinIO, a real worker, real ffmpeg — and asserts
 * playable bytes on both sides of the wire: the object itself is fetched and its MP4 container
 * inspected, and the browser's own `<video>` element is required to report a decoded frame size and
 * a duration.
 *
 * # WHAT IT ALSO REFUSES TO ASSUME
 *
 * Every boundary the route depends on is re-checked here against the same live Course: the public
 * route still cannot reach the candidate, neither the Student nor the owning Instructor can call
 * the Admin route, no entitlement appears, and the access is audited. Those are proved at
 * integration level too — but a capability that behaves correctly in a package test and incorrectly
 * through the real router, the real session middleware and the real browser is exactly the class of
 * defect an end-to-end journey exists to catch.
 */

const INSTRUCTOR = { email: "instructor@example.test", accountID: "a0000000-0000-0000-0000-000000000003" };
const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };
const STUDENT = { email: "student-unentitled@example.test", accountID: "a0000000-0000-0000-0000-000000000099" };

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

/** Reads the run's own database directly, for the facts no API is supposed to expose. */
function sql(statement: string): string {
  const state = JSON.parse(fs.readFileSync(RUN_STATE_FILE_PATH, "utf8")) as { dbName: string };
  return execFileSync(
    "psql",
    [`postgres://gradex:gradex@localhost:5432/${state.dbName}?sslmode=disable`, "-Atq", "-c", statement],
    { encoding: "utf8" },
  )
    .trim()
    .split("\n")[0]
    .trim();
}

/** A genuine H.264/AAC MP4 — real bytes, real container, real duration. */
function makeSampleMP4(seconds: number): string {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gradex-candidate-preview-"));
  const file = path.join(directory, "preview.mp4");
  execFileSync(
    "ffmpeg",
    [
      "-y",
      "-f", "lavfi", "-i", `testsrc=size=320x240:rate=15`,
      "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100",
      "-t", String(seconds),
      "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
      "-c:a", "aac", "-b:a", "64k",
      "-movflags", "+faststart",
      file,
    ],
    { stdio: "ignore" },
  );
  return file;
}

async function signedInPage(browser: Browser, account: typeof ADMIN, session: Session) {
  const origin = new URL(frontendOrigin());
  const context = await browser.newContext({ locale: "en-US" });
  await context.addInitScript(() => {
    window.localStorage.setItem("gradex.locale", "en");
  });
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
  return { context, page: await context.newPage() };
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

test("an Admin plays the real public preview belonging to the candidate revision under review", async ({
  browser,
}, testInfo) => {
  const adminSession = issueRotatingSession(ADMIN);
  const admin = await apiContextFor(adminSession);

  // The media suite owns an isolated database, so it builds the minimum canonical Academic Catalog
  // context an ordinary Instructor Course needs.
  const institution = await admin.post("/api/v1/admin/academic/institutions", {
    data: {
      country_code: "KW",
      slug: "candidate-preview-university",
      name_ar: "جامعة معاينة المرشح",
      name_en: "Candidate Preview University",
      max_academic_level: 4,
    },
  });
  expect(institution.status(), await institution.text()).toBe(201);
  const institutionID = (await institution.json() as { id: string }).id;
  const subject = await admin.post(`/api/v1/admin/academic/institutions/${institutionID}/subjects`, {
    data: { official_code: "CPV101", title_ar: "معاينة", title_en: "Preview Subject" },
  });
  expect(subject.status(), await subject.text()).toBe(201);

  /* ---------------------------------------------- 1. the Instructor uploads a real course preview */

  const instructorSession = issueRotatingSession(INSTRUCTOR);
  const { context: instructorContext, page } = await signedInPage(browser, INSTRUCTOR, instructorSession);

  await page.goto("/en/instructor/courses");
  await expect(page.locator("h1")).toContainText("Course Authoring Studio");

  await page.getByTestId("toggle-new-course").click();
  await page.getByTestId("new-course-institution").selectOption({ label: "Candidate Preview University" });
  await page.getByTestId("new-course-subject-search").fill("CPV101");
  await expect(page.getByTestId("new-course-subject-result")).toBeVisible();
  await page.getByTestId("new-course-subject-result").click();
  await page.getByTestId("new-course-title-ar").fill("دورة معاينة المرشح");
  const courseTitleEn = `Candidate Preview Course ${Date.now()}`;
  await page.getByTestId("new-course-title-en").fill(courseTitleEn);
  await page.getByTestId("new-course-description-ar").fill("وصف");
  await page.getByTestId("new-course-description-en").fill("Candidate preview journey");
  await page.getByTestId("create-course").click();
  await expect(page.getByTestId("authoring-notice")).toContainText("Course created");
  await openAuthoringSections(page);
  const courseID = (await page.getByTestId("selected-course-context").getAttribute("data-course-id"))!;
  expect(courseID).toMatch(UUID_PATTERN);

  // The public preview is its own PREVIEW Asset Version, uploaded before any Lesson exists — which
  // is also the proof that this control cannot be reusing protected Lesson media.
  const previewFile = makeSampleMP4(3);
  const previewAuthoring = page.getByTestId("public-preview-authoring");
  await expect(previewAuthoring.getByTestId("public-preview-state")).toContainText("No public preview is attached");
  await previewAuthoring.locator('input[type="file"]').setInputFiles(previewFile);

  /* ---------------------------------------------- 2. processing completes on the real worker */

  await expect(previewAuthoring.getByTestId("public-preview-message")).toContainText(
    "Public preview is ready for review",
    { timeout: 4 * 60 * 1000 },
  );
  await expect(previewAuthoring.getByTestId("public-preview-state")).toContainText("A public preview is attached");

  // A Lesson with a real video, because submission is refused without one.
  await page.getByTestId("section-title-ar").fill("القسم");
  await page.getByTestId("section-title-en").fill("Preview Section");
  await page.getByTestId("add-section").click();
  const sectionRow = page.locator('[data-testid^="section-"]').first();
  await expect(sectionRow).toContainText("Preview Section");
  const sectionID = (await sectionRow.getAttribute("data-testid"))!.replace("section-", "");
  await page.getByTestId(`lesson-title-ar-${sectionID}`).fill("الدرس");
  await page.getByTestId(`lesson-title-en-${sectionID}`).fill("Preview Lesson");
  await page.getByTestId(`add-lesson-${sectionID}`).click();
  const lessonID = (await page
    .locator('[data-testid^="lesson-video-upload-"]')
    .first()
    .getAttribute("data-testid"))!.replace("lesson-video-upload-", "");
  await page.getByTestId(`lesson-video-file-${lessonID}`).setInputFiles(makeSampleMP4(2));
  await expect(page.getByTestId(`lesson-video-phase-${lessonID}`)).toContainText("Ready", {
    timeout: 4 * 60 * 1000,
  });

  await page.getByTestId("submit-for-review").click();
  await page.getByTestId("submit-confirm").getByTestId("confirm-accept").click();
  await expect(page.getByTestId("authoring-notice")).toContainText("Submitted. An administrator will review it");

  /* ---------------------------------------------- 3. the preview belongs to the candidate revision */

  const queue = await admin.get("/api/v1/admin/review/queue");
  expect(queue.status()).toBe(200);
  const queued = (await queue.json()) as Array<{ course_id?: string; revision_id?: string }>;
  const queueItem = queued.find((item) => item.course_id === courseID);
  expect(queueItem, `submitted Course ${courseID} must be in the review queue`).toBeTruthy();
  const revisionID = queueItem!.revision_id!;
  expect(revisionID).toMatch(UUID_PATTERN);

  const reviewed = await admin.get(`/api/v1/admin/review/courses/${courseID}/revisions/${revisionID}`);
  expect(reviewed.status()).toBe(200);
  const reviewedGraph = (await reviewed.json()) as {
    editable_revision?: { id?: string; preview_asset_version_id?: string; preview_asset_state?: string };
    live_revision_id?: string | null;
  };
  expect(reviewedGraph.editable_revision?.id).toBe(revisionID);
  const previewAssetVersionID = reviewedGraph.editable_revision?.preview_asset_version_id;
  expect(previewAssetVersionID, "the candidate revision must own a public preview").toMatch(UUID_PATTERN);
  expect(reviewedGraph.editable_revision?.preview_asset_state).toBe("READY");
  /*
    READY is not self-evidence on its own, so the safety provenance is asserted directly — the same
    disjunction `ExactVersionProvenanceJoin` requires before any preview may be signed: either
    exact-version scan evidence, or D-088 trusted-validation evidence.

    The stronger "ffmpeg must have run" rule is deliberately *conditional*, because it is
    conditional in the product. Migration `0031_trusted_public_preview` says so in its own words:
    a PREVIEW holding scan provenance still becomes READY straight from SCAN_PASSED with no
    processing evidence, and only a *validated* preview owes a successful processing attempt and a
    trusted duration. This harness runs the scanner path (`MEDIA_SCANNER_MODE=DEVELOPMENT_NO_OP`),
    so demanding a processing attempt unconditionally would assert a rule the product does not have
    on the path under test. What is asserted instead is the rule that does bind, on whichever path
    the asset actually took — and the bytes themselves are proved below, which is the claim this
    journey exists to make.
  */
  const provenance = sql(
    `SELECT CASE
       WHEN successful_validation_attempt_id IS NOT NULL THEN 'validated'
       WHEN successful_scan_attempt_id IS NOT NULL THEN 'scanned'
       ELSE 'none' END
     FROM media_asset_versions WHERE id = '${previewAssetVersionID}'::uuid`,
  );
  expect(
    provenance,
    "a deliverable preview must carry one of the two legitimate safety provenances",
  ).not.toBe("none");
  if (provenance === "validated") {
    expect(
      sql(`SELECT (successful_processing_attempt_id IS NOT NULL)::text FROM media_asset_versions WHERE id = '${previewAssetVersionID}'::uuid`),
      "a trusted-validation preview owes a successful ffmpeg processing attempt",
    ).toBe("true");
    expect(
      sql(`SELECT trusted_duration_ms::text FROM media_asset_versions WHERE id = '${previewAssetVersionID}'::uuid`),
      "a trusted-validation preview owes a measured duration",
    ).toMatch(/^[1-9][0-9]*$/);
  }
  // This Course has never published, so there is no live revision the preview could be coming from.
  expect(reviewedGraph.live_revision_id ?? null).toBeNull();

  await instructorContext.close();

  /* ---------------------------------------------- 4. the Admin opens that exact revision */

  const { context: adminContext, page: adminPage } = await signedInPage(browser, ADMIN, adminSession);
  await adminPage.goto("/en/admin/catalog");
  await expect(adminPage.locator("h1")).toContainText("Course review & administration");
  const row = adminPage.getByTestId(`review-item-${courseID}`);
  await expect(row).toBeVisible();
  await expect(row).toContainText(courseTitleEn);
  await adminPage.getByTestId(`inspect-review-item-${courseID}`).click();

  const inspector = adminPage.getByTestId("submitted-revision-inspector");
  await expect(inspector).toBeVisible();
  await expect(inspector.getByTestId("submitted-title-en")).toContainText(courseTitleEn);
  await expect(inspector.getByTestId("submitted-public-preview")).toContainText(
    "A separate public preview is attached to this version.",
  );

  const previewSection = inspector.getByTestId("review-course-preview");
  await expect(previewSection).toBeVisible();
  await previewSection.scrollIntoViewIfNeeded();
  await previewSection.screenshot({ path: testInfo.outputPath("admin-candidate-preview-before.png") });

  /* ---------------------------------------------- 5. playback is requested through the new route */

  const issuance = adminPage.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname ===
        `/api/v1/admin/review/courses/${courseID}/revisions/${revisionID}/public-preview`,
  );
  await previewSection.getByTestId("watch-review-course-preview").click();
  const issued = await issuance;
  expect(issued.status()).toBe(200);
  const payload = (await issued.json()) as {
    course_id: string;
    revision_id: string;
    preview_asset_version_id: string;
    url: string;
    expires_at: string;
  };

  /* ---------------------------------------------- 8. the identifiers are the ones asked for */

  expect(payload.course_id).toBe(courseID);
  expect(payload.revision_id).toBe(revisionID);
  expect(payload.preview_asset_version_id).toBe(previewAssetVersionID);
  expect(new Date(payload.expires_at).getTime()).toBeGreaterThan(Date.now());

  /* ---------------------------------------------- 6. the signed URL returns real video bytes */

  const objectStore = await playwrightRequest.newContext();
  const object = await objectStore.get(payload.url);
  expect(object.status(), `the signed preview URL must resolve: ${payload.url.split("?")[0]}`).toBe(200);
  const bytes = await object.body();
  expect(bytes.byteLength, "the signed object must not be empty").toBeGreaterThan(1024);
  // An MP4 declares itself: bytes 4..8 of the first box are the `ftyp` type. This is the difference
  // between "a URL that resolved" and "a video".
  expect(bytes.subarray(4, 8).toString("latin1")).toBe("ftyp");
  expect(object.headers()["content-type"]).toContain("video/mp4");
  await objectStore.dispose();

  /* ---------------------------------------------- 7. and the browser genuinely decodes them */

  const player = inspector.getByTestId("review-course-preview-player");
  await expect(player).toBeVisible();
  const decoded = await player.evaluate(
    (element: HTMLVideoElement) =>
      new Promise<{ width: number; height: number; duration: number; readyState: number; error: number | null }>(
        (resolve) => {
          const report = () =>
            resolve({
              width: element.videoWidth,
              height: element.videoHeight,
              duration: element.duration,
              readyState: element.readyState,
              error: element.error ? element.error.code : null,
            });
          if (element.readyState >= 1) return report();
          element.addEventListener("loadedmetadata", report, { once: true });
          element.addEventListener("error", report, { once: true });
          setTimeout(report, 30_000);
        },
      ),
  );
  expect(decoded.error, "the media element must not report a decode or network error").toBeNull();
  expect(decoded.readyState, "the media element must have metadata").toBeGreaterThanOrEqual(1);
  expect(decoded.width, "the decoded picture must have a real width").toBeGreaterThan(0);
  expect(decoded.height).toBeGreaterThan(0);
  expect(decoded.duration).toBeGreaterThan(0);
  await previewSection.screenshot({ path: testInfo.outputPath("admin-candidate-preview-playing.png") });
  await adminPage.screenshot({ path: testInfo.outputPath("admin-candidate-preview-workspace.png"), fullPage: false });

  /* ---------------------------------------------- 9. the public route still cannot reach it */

  const anonymous = await playwrightRequest.newContext({ baseURL: frontendOrigin() });
  const publicPreview = await anonymous.get(`/api/v1/media/courses/${courseID}/preview`);
  expect(
    publicPreview.status(),
    "an unpublished candidate preview must stay unreachable anonymously",
  ).toBe(404);
  await anonymous.dispose();

  /* ---------------------------------------------- 10. and nobody but review staff may call it */

  const routePath = `/api/v1/admin/review/courses/${courseID}/revisions/${revisionID}/public-preview`;
  const student = await apiContextFor(issueRotatingSession(STUDENT));
  expect((await student.post(routePath)).status(), "a Student must be refused").toBe(403);
  await student.dispose();
  const instructor = await apiContextFor(instructorSession);
  expect(
    (await instructor.post(routePath)).status(),
    "the owning Instructor must be refused",
  ).toBe(403);
  await instructor.dispose();

  /* ---------------------------------------------- 11. nothing was granted */

  expect(
    sql(`SELECT count(*) FROM entitlements WHERE course_id = '${courseID}'::uuid`),
    "watching a candidate preview must create no entitlement",
  ).toBe("0");
  expect(
    sql(`SELECT count(*) FROM enrollments WHERE course_id = '${courseID}'::uuid`),
    "watching a candidate preview must create no enrollment",
  ).toBe("0");

  /* ---------------------------------------------- 12. and the access was audited */

  expect(
    sql(
      `SELECT count(*) FROM audit_events WHERE action = 'ADMIN_CONTENT_PREVIEWED'
        AND target_type = 'COURSE_REVISION_PREVIEW' AND target_id = '${revisionID}'
        AND actor_account_id = '${ADMIN.accountID}'::uuid`,
    ),
    "the Admin preview must be audited against the reviewed revision",
  ).toBe("1");

  await adminContext.close();
});
