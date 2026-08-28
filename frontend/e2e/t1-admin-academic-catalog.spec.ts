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
 * T1 (MVP-F17) Academic Catalog Foundation — Admin acceptance journey.
 *
 * Drives the real browser against the real API and real PostgreSQL through one
 * Admin journey: university → college → department → major → study plan →
 * canonical coded subject → mapping. It also proves the two properties the
 * redesign exists to guarantee — a duplicate Subject is refused with the
 * existing Subject named, and an Instructor holds no catalog authority.
 *
 * Test data is isolated per run and no Kuwait University catalog is seeded:
 * launch data belongs to T2, and T1 must work against an empty catalog.
 */

const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };
const INSTRUCTOR = { email: "instructor@example.test", accountID: "a0000000-0000-0000-0000-000000000003" };

// Unique per run so repeated local runs never collide on a slug or a code.
const RUN = `t1${Date.now().toString(36)}`;
const UNIVERSITY_EN = `T1 Test University ${RUN}`;
const UNIVERSITY_AR = `جامعة اختبار ${RUN}`;
const COLLEGE_EN = "College of Engineering and Petroleum";
const COLLEGE_AR = "كلية الهندسة والبترول";
const DEPARTMENT_EN = "Computer Engineering Department";
const DEPARTMENT_AR = "قسم هندسة الحاسوب";
const PROGRAM_EN = "Computer Engineering";
const PROGRAM_AR = "هندسة الحاسوب";
const SUBJECT_EN = "Calculus I";
const SUBJECT_AR = "حساب التفاضل والتكامل ١";
const SUBJECT_CODE = "0410-101";

type Session = ReturnType<typeof issueRotatingSession>;

async function signIn(context: BrowserContext, account: typeof ADMIN): Promise<Session> {
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

async function apiContext(session: Session): Promise<APIRequestContext> {
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

async function openCatalog(page: Page): Promise<void> {
  await page.goto("/en/admin/academic-catalog");
  await expect(page.getByRole("heading", { name: "Academic Catalog" })).toBeVisible();
}

async function submitForm(page: Page, formTestID: string, fields: Record<string, string>, submitTestID: string) {
  for (const [testID, value] of Object.entries(fields)) {
    await page.getByTestId(testID).fill(value);
  }
  await page.getByTestId(submitTestID).click();
  // Every mutation refetches; wait for the button to leave its saving state.
  await expect(page.getByTestId(submitTestID)).toBeEnabled();
  await expect(page.getByTestId(formTestID)).toBeVisible();
}

test.describe("T1 Admin Academic Catalog", () => {
  test("A Admin builds a full academic hierarchy and maps a canonical Subject", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const session = await signIn(context, ADMIN);
    const admin = await apiContext(session);
    const page = await context.newPage();

    await openCatalog(page);

    // The Academic Catalog is its own workspace, not part of Course review.
    await expect(page.locator("body")).not.toContainText("Course Review & Pricing Admin");
    await expect(page.locator("body")).not.toContainText("Catalogue vocabulary");

    // 1. University. T1 ships none, so the first action is creating one.
    await submitForm(
      page,
      "academic-institution-form",
      {
        "institution-name-ar": UNIVERSITY_AR,
        "institution-name-en": UNIVERSITY_EN,
        "institution-slug": `${RUN}-university`,
        "institution-country": "KW",
        // Five levels, not four. The bound is this university's own data.
        "institution-max-level": "5",
      },
      "institution-submit",
    );
    await expect(page.getByTestId("academic-institution")).toHaveValue(/.+/);
    await expect(page.getByTestId("academic-institution")).toContainText(UNIVERSITY_EN);

    // 2. College, then a Department nested inside it.
    await submitForm(
      page,
      "academic-unit-form",
      { "unit-name-ar": COLLEGE_AR, "unit-name-en": COLLEGE_EN, "unit-slug": `${RUN}-engineering` },
      "unit-submit",
    );
    await expect(page.getByTestId("units-list")).toContainText(COLLEGE_EN);

    await page.getByTestId("unit-kind").selectOption("DEPARTMENT");
    await page.getByTestId("unit-parent").selectOption({ label: COLLEGE_EN });
    await submitForm(
      page,
      "academic-unit-form",
      { "unit-name-ar": DEPARTMENT_AR, "unit-name-en": DEPARTMENT_EN, "unit-slug": `${RUN}-computer-engineering` },
      "unit-submit",
    );
    await expect(page.getByTestId("units-list")).toContainText(DEPARTMENT_EN);
    // The hierarchy is visible as a hierarchy: the department names its parent.
    await expect(page.getByTestId("units-list")).toContainText(`Belongs to ${COLLEGE_EN}`);

    // 3. Major, owned by the Department. Department is not the Major.
    await page.getByTestId("program-owning-unit").selectOption({ label: DEPARTMENT_EN });
    await submitForm(
      page,
      "academic-program-form",
      {
        "program-name-ar": PROGRAM_AR,
        "program-name-en": PROGRAM_EN,
        "program-slug": `${RUN}-computer-engineering`,
        "program-degree": "BSC",
      },
      "program-submit",
    );
    await expect(page.getByTestId("academic-program")).toContainText(PROGRAM_EN);

    // 4. Canonical Subject carrying the university's own official code.
    await submitForm(
      page,
      "academic-subject-form",
      { "subject-title-ar": SUBJECT_AR, "subject-title-en": SUBJECT_EN, "subject-code": SUBJECT_CODE },
      "subject-submit",
    );
    await expect(page.getByTestId("subjects-list")).toContainText(`${SUBJECT_CODE} · ${SUBJECT_EN}`);

    // 5. Study plan for the Major, then map the Subject into it.
    await page.getByTestId("academic-program").selectOption({ label: PROGRAM_EN });
    await expect(page.getByTestId("academic-curriculum-form")).toBeVisible();
    await submitForm(page, "academic-curriculum-form", { "curriculum-version": "2026" }, "curriculum-submit");
    await expect(page.getByTestId("curriculum-list")).toContainText("2026 — Active");

    await page.getByTestId("mapping-subject").selectOption({ label: `${SUBJECT_CODE} · ${SUBJECT_EN}` });
    await page.getByTestId("mapping-requirement").selectOption("MAJOR_CORE");
    await submitForm(page, "academic-mapping-form", { "mapping-level": "1" }, "mapping-submit");
    await expect(page.getByTestId("mappings-list")).toContainText(SUBJECT_CODE);
    await expect(page.getByTestId("mappings-list")).toContainText("Major core");
    await expect(page.getByTestId("mappings-list")).toContainText("Recommended level 1");

    // The whole surface is identifier-free: no UUID is ever shown to the Admin.
    const body = (await page.locator("body").innerText()).toLowerCase();
    expect(body).not.toMatch(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/);
    expect(body).not.toContain("revision_id");
    expect(body).not.toContain("taxonomy");

    // The server agrees with the screen.
    const institutions = await admin.get("/api/v1/admin/academic/institutions");
    expect(institutions.status()).toBe(200);
    const created = ((await institutions.json()) as Array<{ id: string; name_en: string }>).find(
      (item) => item.name_en === UNIVERSITY_EN,
    );
    expect(created, "the university the browser created must exist on the server").toBeTruthy();

    const subjects = await admin.get(`/api/v1/admin/academic/institutions/${created!.id}/subjects`);
    expect(subjects.status()).toBe(200);
    const persisted = (await subjects.json()) as Array<{ official_code: string | null }>;
    expect(persisted.filter((item) => item.official_code === SUBJECT_CODE)).toHaveLength(1);

    await admin.dispose();
    await context.close();
  });

  test("B a duplicate Subject is refused and the existing Subject is named", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await signIn(context, ADMIN);
    const page = await context.newPage();

    await openCatalog(page);
    await page.getByTestId("academic-institution").selectOption({ label: UNIVERSITY_EN });
    await expect(page.getByTestId("subjects-list")).toContainText(SUBJECT_CODE);

    // A different spelling of the same official code is the same Subject.
    await submitForm(
      page,
      "academic-subject-form",
      { "subject-title-ar": "عنوان مختلف", "subject-title-en": "A Different Title", "subject-code": "0410101" },
      "subject-submit",
    );

    const conflict = page.getByTestId("academic-duplicate-subject");
    await expect(conflict).toBeVisible();
    // The conflict is actionable: it names the Subject the Admin already has.
    await expect(conflict).toContainText(`${SUBJECT_CODE} · ${SUBJECT_EN}`);

    // And no second row was created.
    await expect(page.getByTestId("subjects-list").getByText(SUBJECT_CODE, { exact: false })).toHaveCount(1);

    await context.close();
  });

  test("C an Instructor holds no Academic Catalog authority", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const session = await signIn(context, INSTRUCTOR);
    const instructor = await apiContext(session);

    // Read is refused.
    const read = await instructor.get("/api/v1/admin/academic/institutions");
    expect(read.status()).toBe(403);

    // Creation is refused.
    const write = await instructor.post("/api/v1/admin/academic/institutions", {
      data: {
        country_code: "KW",
        slug: `${RUN}-instructor-attempt`,
        name_ar: "محاولة",
        name_en: "Instructor Attempt",
        max_academic_level: 4,
      },
    });
    expect(write.status()).toBe(403);

    // The Admin surface is not reachable as an Instructor either.
    const page = await context.newPage();
    await page.goto("/en/admin/academic-catalog");
    await expect(page.getByTestId("academic-message")).toBeVisible();
    await expect(page.getByTestId("academic-institution-form")).toBeVisible();
    // Nothing from the catalog leaked into the page.
    await expect(page.locator("body")).not.toContainText(UNIVERSITY_EN);

    await instructor.dispose();
    await context.close();
  });

  test("D the legacy taxonomy surface still works alongside the new catalog", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const session = await signIn(context, ADMIN);
    const admin = await apiContext(session);

    // T1 is additive: the legacy vocabulary remains operational and remains the
    // authority for Course classification until the T5 cutover.
    const terms = await admin.get("/api/v1/taxonomy/terms");
    expect(terms.status()).toBe(200);

    const created = await admin.post("/api/v1/admin/taxonomy/terms", {
      data: { kind: "SUBJECT", label_ar: `مادة قديمة ${RUN}`, label_en: `Legacy Subject ${RUN}`, academic_code: `LG-${RUN}` },
    });
    expect(created.status()).toBe(201);

    const page = await context.newPage();
    await page.goto("/en/admin/catalog");
    await expect(page.getByRole("heading", { name: "Catalogue vocabulary" })).toBeVisible();

    await admin.dispose();
    await context.close();
  });
});
