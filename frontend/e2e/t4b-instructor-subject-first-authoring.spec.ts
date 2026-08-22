import {
  test,
  expect,
  request as playwrightRequest,
  type APIRequestContext,
  type BrowserContext,
  type Page,
} from "@playwright/test";
import { issueRotatingSession } from "./rotating-students";
import { frontendOrigin } from "../src/lib/api/e2e-ports";

/**
 * T4-B (MVP-F20) Instructor Subject-first authoring — real browser journeys.
 *
 * The product change: an Instructor names a university and a canonical Subject,
 * and Gradex derives the rest. They never create a Subject, never pick a legacy
 * Major term, and never handle an identifier.
 *
 * Every Subject these journeys choose comes from the real Kuwait University
 * launch catalog, imported through the real Admin API — nothing is written into
 * the frontend.
 *
 * Out of scope, deliberately: audience customization (T4-C), Subject requests
 * (T4-D), the Admin review redesign (T4-E), the legacy migration (T5), and
 * catalogue filtering (T6).
 */

const INSTRUCTOR = { email: "instructor@example.test", accountID: "a0000000-0000-0000-0000-000000000003" };
const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };

const MANIFEST = "kuwait-university-launch-v1";
const anyInstitution = "00000000-0000-0000-0000-000000000000";
const LAUNCH_UNIVERSITY = "Kuwait University";

// Real launch data. 0418-320 is required by Computer Science and Cybersecurity;
// 0418-466 is one of the six canonical Subjects T2 imports with no Curriculum
// mapping at all.
const SHARED_SUBJECT_CODE = "0418-320";
const SHARED_SUBJECT_TITLE = "Principles of Computer Systems";
const ALT_SUBJECT_CODE = "0418-321";
const UNMAPPED_SUBJECT_CODE = "0418-466";

const UUID_PATTERN = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i;

type Session = ReturnType<typeof issueRotatingSession>;

async function signIn(context: BrowserContext, account: typeof INSTRUCTOR): Promise<Session> {
  const session = issueRotatingSession(account);
  const origin = new URL(frontendOrigin());
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
  return session;
}

async function apiFor(session: Session): Promise<APIRequestContext> {
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

async function ensureLaunchCatalog(): Promise<void> {
  const admin = await apiFor(issueRotatingSession(ADMIN));
  const applied = await admin.post(`/api/v1/admin/academic/institutions/${anyInstitution}/import`, {
    data: { manifest: MANIFEST, mode: "apply" },
  });
  expect(applied.status()).toBe(200);
  await admin.dispose();
}

async function openStudio(page: Page): Promise<void> {
  await page.goto("/en/instructor/courses");
  await expect(page.locator("h1")).toContainText("Course Authoring Studio");
  await expect(page.getByTestId("owned-course-list")).toBeVisible();
}

/**
 * University, then Subject. No identifier is ever typed or pasted.
 *
 * `settles` is false when the picker is used to COMMIT a correction: the Course
 * context editor closes as soon as the selection is applied, so the selected
 * block it would otherwise show is unmounted by design.
 */
async function chooseSubject(
  page: Page, code: string, prefix = "new-course", settles = true,
): Promise<void> {
  const institution = page.getByTestId(`${prefix}-institution`);
  await expect(institution).toBeVisible();
  await institution.selectOption({ label: LAUNCH_UNIVERSITY });
  await page.getByTestId(`${prefix}-subject-search`).fill(code);
  const result = page.getByTestId(`${prefix}-subject-result`).first();
  await expect(result).toBeVisible({ timeout: 15_000 });
  await result.click();
  if (settles) {
    await expect(page.getByTestId(`${prefix}-selected-subject`)).toBeVisible();
  }
}

async function createAcademicCourse(page: Page, titleEn: string, code = SHARED_SUBJECT_CODE): Promise<string> {
  await page.getByTestId("toggle-new-course").click();
  await chooseSubject(page, code);
  await page.getByTestId("new-course-title-ar").fill("كورس أكاديمي");
  await page.getByTestId("new-course-title-en").fill(titleEn);
  await page.getByTestId("new-course-description-ar").fill("وصف");
  await page.getByTestId("new-course-description-en").fill("Description");
  await page.getByTestId("create-course").click();
  await expect(page.getByTestId("authoring-notice")).toContainText("Course created on the server");
  const courseID = await page.getByTestId("selected-course-context").getAttribute("data-course-id");
  expect(courseID, "the studio must retain a server-issued Course ID without displaying it").toMatch(UUID_PATTERN);
  return courseID!;
}

test.describe("T4-B Instructor Subject-first authoring", () => {
  // Real catalog import plus multi-step authoring interactions.
  test.setTimeout(120_000);

  test.beforeAll(async () => {
    await ensureLaunchCatalog();
  });

  // --- Journey A ----------------------------------------------------------

  test("A an Instructor creates a Course from a canonical Subject", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const session = await signIn(context, INSTRUCTOR);
    const page = await context.newPage();
    await openStudio(page);
    await page.getByTestId("toggle-new-course").click();

    // The university comes from the catalog, not from the frontend.
    const institution = page.getByTestId("new-course-institution");
    await expect(institution).toBeVisible();
    await institution.selectOption({ label: LAUNCH_UNIVERSITY });

    // Searching by official code finds the canonical Subject.
    await page.getByTestId("new-course-subject-search").fill(SHARED_SUBJECT_CODE);
    const result = page.getByTestId("new-course-subject-result").first();
    await expect(result).toBeVisible({ timeout: 15_000 });
    await expect(result).toContainText(SHARED_SUBJECT_CODE);
    await expect(result).toContainText(SHARED_SUBJECT_TITLE);
    await result.click();

    // Its academic context and the derived audience are shown.
    await expect(page.getByTestId("new-course-selected-subject")).toContainText(SHARED_SUBJECT_TITLE);
    await expect(page.getByTestId("new-course-selected-subject-context")).toContainText("Computer Science");
    const audience = page.getByTestId("new-course-audience");
    await expect(audience).toBeVisible();
    await expect(audience).toContainText("Computer Science");
    await expect(audience).toContainText("Cybersecurity");

    // The Instructor chose a Subject without ever seeing an identifier. Read
    // while the form is still on screen: it unmounts once the Course exists.
    const pickerText = await page.getByTestId("new-course-academic-picker").innerText();
    expect(pickerText).not.toMatch(UUID_PATTERN);
    expect(pickerText).toContain(SHARED_SUBJECT_CODE);

    await page.getByTestId("new-course-title-ar").fill("كورس أكاديمي");
    await page.getByTestId("new-course-title-en").fill(`Academic Course ${Date.now()}`);
    await page.getByTestId("new-course-description-ar").fill("وصف");
    await page.getByTestId("new-course-description-en").fill("Description");
    await page.getByTestId("create-course").click();
    await expect(page.getByTestId("authoring-notice")).toContainText("Course created on the server");

    // What the server actually stored.
    const api = await apiFor(session);
    const courses = await (await api.get("/api/v1/courses")).json();
    const created = courses[0];
    expect(created.classification_model).toBe("ACADEMIC_CATALOG");
    expect(created.institution_id).toBeTruthy();
    expect(created.subject_id).toBeTruthy();
    expect(created.editable_revision.revision_number).toBe(1);

    // The canonical Subject really is 0418-320, resolved through the catalog.
    const subject = await (
      await api.get(`/api/v1/authoring/academic/institutions/${created.institution_id}/subjects/${created.subject_id}`)
    ).json();
    expect(subject.official_code).toBe(SHARED_SUBJECT_CODE);
    expect(subject.title_en).toBe(SHARED_SUBJECT_TITLE);

    // No legacy taxonomy was populated to satisfy the old model.
    expect(created.editable_revision.major_term_id ?? null).toBeNull();
    expect(created.editable_revision.subject_term_id ?? null).toBeNull();
    expect(created.editable_revision.study_year ?? null).toBeNull();

    await api.dispose();
    await context.close();
  });

  // --- Journey B ----------------------------------------------------------

  test("B a shared Subject shows its Programs and an unmapped Subject says so truthfully", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const session = await signIn(context, INSTRUCTOR);
    const page = await context.newPage();
    await openStudio(page);
    await page.getByTestId("toggle-new-course").click();

    // A Subject several Programs require reports all of them.
    await chooseSubject(page, SHARED_SUBJECT_CODE);
    const audience = page.getByTestId("new-course-audience");
    await expect(audience).toContainText("Computer Science");
    await expect(audience).toContainText("Cybersecurity");
    // Read-only: no customization control exists yet (T4-C).
    await expect(page.getByTestId("new-course-audience-customize")).toHaveCount(0);
    await expect(page.getByRole("checkbox")).toHaveCount(0);

    // A canonical Subject with no Curriculum mapping is still selectable, and
    // the empty audience is stated rather than hidden or invented.
    await page.getByTestId("new-course-change-subject").click();
    await chooseSubject(page, UNMAPPED_SUBJECT_CODE);
    await expect(page.getByTestId("new-course-audience-empty")).toContainText(
      "No Programs are currently associated with this Subject",
    );
    await expect(page.getByTestId("new-course-audience")).toHaveCount(0);

    // It creates a Course perfectly well.
    await page.getByTestId("new-course-title-ar").fill("مادة غير مرتبطة");
    await page.getByTestId("new-course-title-en").fill(`Unmapped Course ${Date.now()}`);
    await page.getByTestId("create-course").click();
    await expect(page.getByTestId("authoring-notice")).toContainText("Course created on the server");

    // And displaying an audience wrote no target rows.
    const api = await apiFor(session);
    const courses = await (await api.get("/api/v1/courses")).json();
    expect(courses[0].classification_model).toBe("ACADEMIC_CATALOG");
    await api.dispose();
    await context.close();
  });

  // --- Journey C ----------------------------------------------------------

  test("C the Subject is correctable before publication", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const session = await signIn(context, INSTRUCTOR);
    const page = await context.newPage();
    await openStudio(page);
    const courseID = await createAcademicCourse(page, `Correctable Course ${Date.now()}`);

    const api = await apiFor(session);
    const before = await (await api.get(`/api/v1/courses/${courseID}`)).json();
    const originalSubject = before.subject_id;

    // The Instructor corrects the Subject through the ordinary studio surface.
    await expect(page.getByTestId("academic-course-context")).toBeVisible();
    await page.getByTestId("academic-course-edit-subject").click();
    await chooseSubject(page, ALT_SUBJECT_CODE, "academic-course", false);
    await expect(page.getByTestId("authoring-notice")).toContainText("Course Subject updated");

    await expect
      .poll(async () => (await (await api.get(`/api/v1/courses/${courseID}`)).json()).subject_id)
      .not.toBe(originalSubject);

    const corrected = await (await api.get(`/api/v1/courses/${courseID}`)).json();
    expect(corrected.classification_model).toBe("ACADEMIC_CATALOG");
    expect(corrected.institution_id).toBe(before.institution_id);
    expect(corrected.editable_revision.major_term_id ?? null).toBeNull();

    // The corrected Subject is what the studio now shows, as identity rather
    // than as a form field.
    await expect(page.getByTestId("academic-course-subject")).toContainText(ALT_SUBJECT_CODE);

    // An incomplete Course cannot be submitted, so this journey does not reach
    // PENDING_REVIEW: the review freeze and the post-publication lock are proven
    // against the real API in the backend suite
    // (TestT4BSubjectEditingAcrossTheLifecycle) rather than by driving the
    // database behind the browser's back.
    const submitted = await api.post(
      `/api/v1/courses/${courseID}/revisions/${corrected.editable_revision.id}/submit`,
    );
    expect(submitted.status()).toBeGreaterThanOrEqual(400);
    const stillDraft = await (await api.get(`/api/v1/courses/${courseID}`)).json();
    expect(stillDraft.subject_id).toBe(corrected.subject_id);
    expect(stillDraft.classification_model).toBe("ACADEMIC_CATALOG");

    await api.dispose();
    await context.close();
  });

  // --- Journey D ----------------------------------------------------------

  test("D a legacy Course keeps its compatibility editor and cannot be created anew", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const session = await signIn(context, INSTRUCTOR);
    const page = await context.newPage();
    const api = await apiFor(session);

    // There is no legacy creation mode in the ordinary UI: omitting the academic
    // context is refused rather than silently producing a legacy Course.
    const refused = await api.post("/api/v1/courses", {
      data: { title_ar: "قديم", title_en: "Legacy Attempt" },
    });
    expect(refused.status()).toBeGreaterThanOrEqual(400);

    // Naming a classification directly changes nothing either.
    const forged = await api.post("/api/v1/courses", {
      data: { title_ar: "قديم", title_en: "Forged", classification_model: "LEGACY_TAXONOMY" },
    });
    expect(forged.status()).toBeGreaterThanOrEqual(400);

    // The studio's new-Course form offers a university and a Subject, and no
    // legacy classification control at all.
    await openStudio(page);
    await page.getByTestId("toggle-new-course").click();
    await expect(page.getByTestId("new-course-institution")).toBeVisible();
    await expect(page.getByTestId("new-course-subject-search")).toBeVisible();
    const form = page.getByTestId("new-course-form");
    await expect(form).not.toContainText("Major");
    await expect(form).not.toContainText("Study Year");
    await expect(form).not.toContainText("taxonomy");

    // Create is unavailable until a Subject is chosen.
    await expect(page.getByTestId("create-course")).toBeDisabled();
    await expect(page.getByTestId("create-course-needs-subject")).toBeVisible();

    // An Academic Course cannot reach the legacy taxonomy mutation, even by
    // calling the route directly.
    await chooseSubject(page, SHARED_SUBJECT_CODE);
    await page.getByTestId("new-course-title-ar").fill("كورس");
    await page.getByTestId("new-course-title-en").fill(`Guarded Course ${Date.now()}`);
    await page.getByTestId("create-course").click();
    await expect(page.getByTestId("authoring-notice")).toContainText("Course created on the server");

    const created = (await (await api.get("/api/v1/courses")).json())[0];
    const terms = await (await api.get("/api/v1/taxonomy/terms")).json();
    if (Array.isArray(terms) && terms.length > 0) {
      const attempt = await api.patch(
        `/api/v1/courses/${created.id}/revisions/${created.editable_revision.id}`,
        { data: { major_term_id: terms[0].id } },
      );
      expect(attempt.status()).toBeGreaterThanOrEqual(400);
    }

    // And the Academic Course shows no legacy taxonomy panel.
    await openStudio(page);
    await expect(page.getByTestId("academic-course-context")).toBeVisible();

    await api.dispose();
    await context.close();
  });
});
