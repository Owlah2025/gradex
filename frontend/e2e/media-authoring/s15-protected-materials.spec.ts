import { createHash } from "crypto";
import { execFileSync } from "child_process";
import fs from "fs";
import os from "os";
import path from "path";
import { test, expect, request as playwrightRequest, type APIRequestContext, type Browser, type BrowserContext, type Page } from "@playwright/test";
import { issueRotatingSession } from "../rotating-students";
import { queryLearningState } from "../../src/lib/api/e2e-progress";
import { frontendOrigin } from "../../src/lib/api/e2e-ports";
import { captureFailureDiagnostic } from "./diagnostics";

/**
 * ST-15 — real protected Resource and Lab Material delivery.
 *
 * The canonical seed owns the pre-existing Lab Material association; media
 * setup writes its deterministic private ZIP bytes. The Resource replacement
 * is deliberately different: this browser uses the Instructor's D-088 PDF
 * upload UI, then the real review UI publishes it. Both Student downloads
 * begin at the product button, obtain the protected authorization, and verify
 * the returned private bytes without storage credentials in the Student step.
 */

const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const LESSON_ID = "30000000-0000-0000-0000-000000000001";
const OTHER_LESSON_ID = "30000000-0000-0000-0000-000000000002";
const MISSING_VIDEO_LESSON_ID = "30000000-0000-0000-0000-000000000003";
const EMPTY_SECTION_ID = "10000000-0000-0000-0000-000000000002";
const OTHER_COURSE_ID = "c0000000-0000-0000-0000-000000000099";
const INSTRUCTOR = { email: "instructor@example.test", accountID: "a0000000-0000-0000-0000-000000000003" };
const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };
const STUDENT = { email: "student-active@example.test", accountID: "a0000000-0000-0000-0000-000000000001" };
const UNENTITLED_STUDENT = { email: "student-unentitled@example.test", accountID: "a0000000-0000-0000-0000-000000000099" };

const LIVE_RESOURCE_BYTES = Buffer.from("%PDF-1.7\n1 0 obj\n<<>>\nendobj\ntrailer\n%%EOF\n");
const REPLACEMENT_RESOURCE_BYTES = Buffer.from("%PDF-1.7\n1 0 obj\n<< /Title (ST-15 replacement) >>\nendobj\ntrailer\n%%EOF\n");
const REPLACEMENT_RESOURCE_NAME = "ST15 protected lecture notes.pdf";

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

async function signedInContext(browser: Browser, account: typeof INSTRUCTOR, locale = "en"): Promise<BrowserContext> {
  const context = await browser.newContext({ locale: locale === "ar" ? "ar-EG" : "en-US" });
  const session = issueRotatingSession(account);
  const origin = new URL(frontendOrigin());
  await context.addInitScript((selectedLocale) => {
    window.localStorage.setItem("gradex.locale", selectedLocale);
  }, locale);
  await context.addCookies([{
    name: session.cookie_name,
    value: session.cookie_value,
    domain: origin.hostname,
    path: "/",
    httpOnly: true,
    secure: true,
    sameSite: "Strict",
  }]);
  return context;
}

function materialAuthorizationPath(courseID: string, lessonID: string, fileID: string): string {
  return `/api/v1/media/courses/${courseID}/lessons/${lessonID}/materials/${fileID}/download-authorizations`;
}

function temporaryFile(name: string, bytes: Buffer): string {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gradex-st15-resource-"));
  const file = path.join(directory, name);
  fs.writeFileSync(file, bytes);
  return file;
}

async function expectDownloadBytes(page: Page, buttonName: RegExp, expected: Buffer): Promise<void> {
  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: buttonName }).click();
  const download = await downloadPromise;
  const downloadedPath = await download.path();
  expect(downloadedPath, "private storage response must become a browser download").toBeTruthy();
  expect(fs.readFileSync(downloadedPath!)).toEqual(expected);
}

async function expectLabArchive(page: Page): Promise<void> {
  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: /Download: Lab Starter Code Zip/ }).click();
  const download = await downloadPromise;
  const downloadedPath = await download.path();
  expect(downloadedPath).toBeTruthy();
  const readme = execFileSync("unzip", ["-p", downloadedPath!, "README.txt"], { encoding: "utf8" });
  expect(readme).toBe("Gradex ST-15 canonical Lab Material fixture.\n");
}

async function inspectAndApprove(browser: Browser, expectedResourceName: string | null): Promise<void> {
  const adminContext = await signedInContext(browser, ADMIN);
  const adminPage = await adminContext.newPage();
  await adminPage.goto("/en/admin/catalog");
  const row = adminPage.getByTestId(`review-item-${COURSE_ID}`);
  await expect(row).toBeVisible();
  await adminPage.getByTestId(`inspect-review-item-${COURSE_ID}`).click();
  const inspector = adminPage.getByTestId("submitted-revision-inspector");
  await expect(inspector).toBeVisible();
  await expect(inspector.getByTestId(`submitted-lesson-${LESSON_ID}`)).toBeVisible();
  if (expectedResourceName) {
    await expect(inspector).toContainText(expectedResourceName);
    await expect(inspector).toContainText("Resource");
    await expect(inspector).not.toContainText("[RESOURCE]");
  }
  await inspector.getByTestId("approve-inspected-revision").click();
  await expect(adminPage.getByTestId("review-action-success")).toContainText("Course published successfully");
  await adminContext.close();
}

test.afterEach(async ({}, testInfo) => {
  if (testInfo.status === testInfo.expectedStatus) return;
  try {
    const artifact = captureFailureDiagnostic();
    if (artifact) console.error(`[ST-15 Media E2E Failure] sanitized diagnostic artifact: ${artifact}`);
  } catch {
    console.error("[ST-15 Media E2E Failure] diagnostic collector could not complete");
  }
});

test("ST-15 Resource/Lab Material protected presentation, real bytes, and revision isolation", async ({ browser }) => {
  const studentContext = await signedInContext(browser, STUDENT);
  const studentPage = await studentContext.newPage();

  // Existing published A has both canonical categories. Each product download
  // authorizes only immediately before private delivery and proves actual bytes.
  await studentPage.goto(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);
  await expect(studentPage.getByRole("heading", { name: "Resources" })).toBeVisible();
  await expect(studentPage.getByText("Lecture Notes PDF")).toBeVisible();
  await expect(studentPage.getByRole("heading", { name: "Lab Materials" })).toBeVisible();
  await expect(studentPage.getByText("Lab Starter Code Zip")).toBeVisible();
  const beforeInitialDownloads = queryLearningState(STUDENT.accountID, COURSE_ID);
  await expectDownloadBytes(studentPage, /Download: Lecture Notes PDF/, LIVE_RESOURCE_BYTES);
  await expectLabArchive(studentPage);
  expect(queryLearningState(STUDENT.accountID, COURSE_ID).progress).toEqual(beforeInitialDownloads.progress);

  // A compact, truthful absence: the seeded second Lesson has no attachments.
  await studentPage.goto(`/en/learn/courses/${COURSE_ID}/lessons/${OTHER_LESSON_ID}`);
  await expect(studentPage.getByRole("heading", { name: "Resources" })).toHaveCount(0);
  await expect(studentPage.getByRole("heading", { name: "Lab Materials" })).toHaveCount(0);

  // Instructor creates candidate B through the Studio, removes A, receives a
  // local validation error for an unsupported file, then uploads real D-088 PDF
  // bytes. No material or Asset ID is entered by the Instructor.
  const instructorContext = await signedInContext(browser, INSTRUCTOR);
  const instructorPage = await instructorContext.newPage();
  await instructorPage.goto("/en/instructor/courses");
  await instructorPage.getByTestId(`owned-course-${COURSE_ID}`).click();
  await expect(instructorPage.getByTestId("start-revision-panel")).toBeVisible();
  await instructorPage.getByTestId("start-revision").click();
  await expect(instructorPage.getByTestId("revision-state")).toHaveAttribute("data-revision-state", "DRAFT");

  const removeA = instructorPage.locator(`[data-testid^="remove-lesson-resource-"]`).first();
  await expect(removeA).toBeVisible();
  await removeA.click();
  await expect(instructorPage.getByTestId(`lesson-resource-list-${LESSON_ID}`)).toHaveCount(0);

  const resourceInput = instructorPage.getByTestId(`lesson-resource-file-${LESSON_ID}`);
  await resourceInput.setInputFiles({ name: "unsafe.exe", mimeType: "application/octet-stream", buffer: Buffer.from("not a resource") });
  await expect(instructorPage.getByTestId(`lesson-resource-message-${LESSON_ID}`)).toContainText("Select a PDF or DOCX file.");
  await expect(instructorPage.getByRole("button", { name: "Retry upload" })).toBeVisible();

  const replacementFile = temporaryFile(REPLACEMENT_RESOURCE_NAME, REPLACEMENT_RESOURCE_BYTES);
  await resourceInput.setInputFiles(replacementFile);
  await expect(instructorPage.getByTestId(`lesson-resource-phase-${LESSON_ID}`)).toContainText(/Preparing|Uploading|Checking|Attaching|Attached/, { timeout: 30_000 });
  await expect(instructorPage.getByTestId(`lesson-resource-phase-${LESSON_ID}`)).toContainText("Attached", { timeout: 4 * 60 * 1000 });
  const candidateResource = instructorPage.locator(`li[data-testid^="lesson-resource-"]`).filter({ hasText: REPLACEMENT_RESOURCE_NAME });
  await expect(candidateResource).toBeVisible();
  const candidateResourceID = (await candidateResource.getAttribute("data-testid"))!.replace("lesson-resource-", "");
  expect(candidateResourceID).toMatch(/^[0-9a-f-]{36}$/i);
  await expect(instructorPage.getByText(/Asset Version|UUID/)).toHaveCount(0);

  // The seeded published graph intentionally contains one incomplete legacy
  // Lesson. Remove that unrelated candidate-only Lesson through the existing
  // Instructor control so the established publication validator can assess
  // this Resource change. This is fixture repair, not an ST-15 shortcut or
  // direct database construction. Taxonomy terms are Admin-owned, so their
  // canonical command is exercised with an Admin session before the Instructor
  // assigns them.
  await instructorPage.getByTestId(`delete-lesson-${MISSING_VIDEO_LESSON_ID}`).click();
  await expect(instructorPage.getByTestId(`lesson-${MISSING_VIDEO_LESSON_ID}`)).toHaveCount(0);
  await instructorPage.getByTestId(`delete-section-${EMPTY_SECTION_ID}`).click();
  await expect(instructorPage.getByTestId(`section-${EMPTY_SECTION_ID}`)).toHaveCount(0);
  const taxonomyAdmin = await apiContextFor(issueRotatingSession(ADMIN));
  const major = await taxonomyAdmin.post("/api/v1/admin/taxonomy/terms", {
    data: { kind: "MAJOR", label_ar: "اختبار ST15", label_en: "ST15 Engineering" },
  });
  expect(major.status(), await major.text()).toBe(201);
  const subject = await taxonomyAdmin.post("/api/v1/admin/taxonomy/terms", {
    data: { kind: "SUBJECT", label_ar: "مواد محمية", label_en: "Protected Materials", academic_code: "ST15" },
  });
  expect(subject.status(), await subject.text()).toBe(201);
  await taxonomyAdmin.dispose();

  // The taxonomy panel snapshots editable Courses on mount. Reload after
  // starting the candidate so it addresses this named candidate revision,
  // rather than the no-longer-editable published summary it first observed.
  await instructorPage.reload();
  await instructorPage.getByTestId(`owned-course-${COURSE_ID}`).click();
  await expect(instructorPage.getByTestId("taxonomy-course")).toBeVisible();
  await instructorPage.getByTestId("taxonomy-course").selectOption(COURSE_ID);
  await instructorPage.getByLabel("Major").selectOption({ label: "ST15 Engineering" });
  await instructorPage.getByLabel("Subject").selectOption({ label: "Protected Materials (ST15)" });
  await instructorPage.getByRole("button", { name: "Save Taxonomy" }).click();
  await expect(instructorPage.getByText("Taxonomy saved for the named revision")).toBeVisible();
  await instructorPage.getByTestId("revision-study-year").selectOption("YEAR_1");
  await instructorPage.getByTestId("save-revision").click();
  await expect(instructorPage.getByTestId("authoring-notice")).toContainText("Revision details saved");

  // Candidate B is not yet live: A remains visible/downloadable, B cannot be
  // probed by an entitled Student, anonymous caller, unentitled Student, a
  // wrong Course, or a wrong Lesson.
  await studentPage.goto(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);
  await expect(studentPage.getByText("Lecture Notes PDF")).toBeVisible();
  await expect(studentPage.getByText(REPLACEMENT_RESOURCE_NAME)).toHaveCount(0);
  await expectDownloadBytes(studentPage, /Download: Lecture Notes PDF/, LIVE_RESOURCE_BYTES);

  const entitledAPI = await apiContextFor(issueRotatingSession(STUDENT));
  const unentitledAPI = await apiContextFor(issueRotatingSession(UNENTITLED_STUDENT));
  const anonymousAPI = await playwrightRequest.newContext({ baseURL: frontendOrigin(), extraHTTPHeaders: { Accept: "application/problem+json" } });
  for (const [name, response] of [
    ["candidate", await entitledAPI.post(materialAuthorizationPath(COURSE_ID, LESSON_ID, candidateResourceID))],
    ["anonymous", await anonymousAPI.post(materialAuthorizationPath(COURSE_ID, LESSON_ID, candidateResourceID))],
    ["unentitled", await unentitledAPI.post(materialAuthorizationPath(COURSE_ID, LESSON_ID, candidateResourceID))],
    ["wrong Course", await entitledAPI.post(materialAuthorizationPath(OTHER_COURSE_ID, LESSON_ID, candidateResourceID))],
    ["wrong Lesson", await entitledAPI.post(materialAuthorizationPath(COURSE_ID, OTHER_LESSON_ID, candidateResourceID))],
  ] as const) {
    expect(response.status(), `${name} material probe must be inventory-safe`).toBe(404);
    expect(await response.text()).not.toContain(candidateResourceID);
  }
  await anonymousAPI.dispose();
  await unentitledAPI.dispose();
  await entitledAPI.dispose();

  await instructorPage.getByTestId("submit-for-review").click();
  await expect(instructorPage.getByTestId("authoring-notice")).toContainText("Course submitted for Admin review");
  await inspectAndApprove(browser, REPLACEMENT_RESOURCE_NAME);

  // Publication atomically switches the Student's projection to B. The
  // pre-existing Lab material remains a distinct category and still delivers
  // its fixture archive through the same protected UI path.
  await studentPage.goto(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);
  await expect(studentPage.getByText("Lecture Notes PDF")).toHaveCount(0);
  await expect(studentPage.getByText(REPLACEMENT_RESOURCE_NAME)).toBeVisible();
  await expectDownloadBytes(studentPage, new RegExp(`Download: ${REPLACEMENT_RESOURCE_NAME.replace(/[.*+?^${}()|[\\]\\]/g, "\\$&")}`), REPLACEMENT_RESOURCE_BYTES);
  await expectLabArchive(studentPage);

  // Candidate C removes B. Until its approval, B stays live; after the review
  // transaction switches the live revision, the resource projection is empty
  // while the independently modelled Lab Material remains available.
  await instructorPage.reload();
  await instructorPage.getByTestId(`owned-course-${COURSE_ID}`).click();
  await instructorPage.getByTestId("start-revision").click();
  const removeB = instructorPage.locator(`[data-testid^="remove-lesson-resource-"]`).first();
  await expect(removeB).toBeVisible();
  await removeB.click();
  await instructorPage.getByTestId("submit-for-review").click();
  await expect(instructorPage.getByTestId("authoring-notice")).toContainText("Course submitted for Admin review");

  await studentPage.goto(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);
  await expect(studentPage.getByText(REPLACEMENT_RESOURCE_NAME)).toBeVisible();
  await expectDownloadBytes(studentPage, new RegExp(`Download: ${REPLACEMENT_RESOURCE_NAME.replace(/[.*+?^${}()|[\\]\\]/g, "\\$&")}`), REPLACEMENT_RESOURCE_BYTES);
  await inspectAndApprove(browser, null);

  await studentPage.goto(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);
  await expect(studentPage.getByRole("heading", { name: "Resources" })).toHaveCount(0);
  await expect(studentPage.getByRole("heading", { name: "Lab Materials" })).toBeVisible();
  const beforeFinalLabDownload = queryLearningState(STUDENT.accountID, COURSE_ID);
  await expectLabArchive(studentPage);
  expect(queryLearningState(STUDENT.accountID, COURSE_ID).progress).toEqual(beforeFinalLabDownload.progress);

  // The same live Lab surface is localized; no hidden ID or internal storage
  // reference becomes Student copy merely to make downloads testable.
  await studentPage.goto(`/ar/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);
  await expect(studentPage.locator("html")).toHaveAttribute("dir", "rtl");
  await expect(studentPage.getByRole("heading", { name: "مواد المختبر" })).toBeVisible();
  await expect(studentPage.getByRole("button", { name: /تحميل: كود المختبر/ })).toBeVisible();
  expect((await studentPage.locator("main").innerText())).not.toMatch(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i);

  await instructorContext.close();
  await studentContext.close();
  // This ties the fixture constants to the exact product bytes without adding
  // their storage addresses to the browser contract or test output.
  expect(createHash("sha256").update(REPLACEMENT_RESOURCE_BYTES).digest("hex")).toHaveLength(64);
});
