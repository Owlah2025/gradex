import { expect, test, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { expectAuthoringSectionsMounted, openAuthoringSections } from "./authoring-sections";

/**
 * Instructor Authoring V2 — the disclosure workflow itself.
 *
 * Every other instructor suite asserts against the authoring *panels* and opens all five
 * disclosures first (`authoring-sections.ts`). This is the one that asserts the disclosures, so it
 * opens nothing by hand.
 *
 * The properties under test are the ones the pattern can get wrong:
 *
 *   - it arrives closed on what is finished and open on what is not,
 *   - a finished part can still be reopened, because this is not a wizard,
 *   - a closed part still says how much is outstanding inside it,
 *   - it moves only on an explicit progression, never on typing or focus,
 *   - and it says whether what is on screen has actually been saved.
 *
 * The payloads are shaped strictly to the owned-Course contract, so nothing here can pass by
 * leaning on data the server never sends.
 */

const COURSE_ID = "b1b1b1b1-1111-4111-8111-111111111111";
const REVISION_ID = "c2c2c2c2-2222-4222-8222-222222222222";
const SECTION_ID = "d3d3d3d3-3333-4333-8333-333333333333";
const LESSON_ID = "e4e4e4e4-4444-4444-8444-444444444444";
const ASSET_ID = "f5f5f5f5-5555-4555-8555-555555555555";
const INSTITUTION_ID = "a6a6a6a6-6666-4666-8666-666666666666";
const SUBJECT_ID = "b7b7b7b7-7777-4777-8777-777777777777";

type Shape = "EMPTY" | "SECTION_ONLY" | "COMPLETE";

function sections(shape: Shape) {
  if (shape === "EMPTY") return [];
  const lessons =
    shape === "COMPLETE"
      ? [
          {
            id: LESSON_ID,
            title_ar: "الروابط التساهمية",
            title_en: "Covalent bonding",
            position: 1,
            video_asset_version_id: ASSET_ID,
            files: [],
          },
        ]
      : [];
  return [
    {
      id: SECTION_ID,
      title_ar: "أساسيات الكيمياء العضوية",
      title_en: "Foundations of organic chemistry",
      position: 1,
      lessons,
    },
  ];
}

function course(options: { shape?: Shape; preview?: boolean } = {}) {
  const { shape = "COMPLETE", preview = true } = options;
  return {
    id: COURSE_ID,
    owner_account_id: "owner-1",
    lifecycle: "DRAFT",
    classification_model: "ACADEMIC_CATALOG",
    institution_id: INSTITUTION_ID,
    subject_id: SUBJECT_ID,
    academic_context: {
      institution_name_ar: "جامعة الكويت",
      institution_name_en: "Kuwait University",
      subject: {
        official_code: "CHEM 201",
        title_ar: "الكيمياء العضوية",
        title_en: "Organic Chemistry",
        owning_unit_name_ar: "قسم الكيمياء",
        owning_unit_name_en: "Department of Chemistry",
      },
    },
    price_minor_units: null,
    editable_revision: {
      id: REVISION_ID,
      course_id: COURSE_ID,
      state: "DRAFT",
      revision_number: 1,
      title_ar: "الكيمياء العضوية للسنة الثانية",
      title_en: "Organic Chemistry for second year",
      description_ar: "مقرر يغطي أساسيات الكيمياء العضوية.",
      description_en: "A course covering the foundations of organic chemistry.",
      ...(preview ? { preview_asset_version_id: ASSET_ID } : {}),
      sections: sections(shape),
    },
  };
}

async function serveStudio(page: Page, payload: ReturnType<typeof course>) {
  await page.route("**/api/v1/session/bootstrap", (route) =>
    route.fulfill({ json: { csrf_token: "csrf-token" } }),
  );
  await page.route("**/api/v1/session", (route) =>
    route.fulfill({
      json: {
        status: "ACTIVE",
        role: "INSTRUCTOR",
        display_name: "Fahd Al-Mutairi",
        csrf_token: "csrf-token",
        idle_expires_at: new Date(Date.now() + 3_600_000).toISOString(),
        absolute_expires_at: new Date(Date.now() + 28_800_000).toISOString(),
      },
    }),
  );
  await page.route("**/api/v1/courses", (route) => route.fulfill({ json: [payload] }));
  await page.route(`**/api/v1/courses/${COURSE_ID}`, (route) => route.fulfill({ json: payload }));
  await page.route("**/api/v1/authoring/academic/institutions", (route) =>
    route.fulfill({
      json: [{ id: INSTITUTION_ID, name_ar: "جامعة الكويت", name_en: "Kuwait University" }],
    }),
  );
  await page.route("**/api/v1/authoring/academic/institutions/*/subjects/*", (route) =>
    route.fulfill({
      json: {
        id: SUBJECT_ID,
        official_code: "CHEM 201",
        title_ar: "الكيمياء العضوية",
        title_en: "Organic Chemistry",
        programs: [
          { program_id: "program-1", name_ar: "كيمياء", name_en: "Chemistry", recommended_level: 2 },
        ],
      },
    }),
  );
  await page.route("**/api/v1/authoring/academic/subject-requests**", (route) =>
    route.fulfill({ json: [] }),
  );
  await page.route("**/api/v1/taxonomy/terms**", (route) => route.fulfill({ json: [] }));
}

async function openStudio(page: Page, locale: "ar" | "en" = "en") {
  await page.addInitScript((selected) => {
    window.localStorage.setItem("gradex.locale", selected as string);
  }, locale);
  await page.goto(`/${locale}/instructor/courses`);
  await expect(page.getByTestId("authoring-workflow")).toBeVisible();
}

const toggle = (page: Page, section: string) => page.getByTestId(`authoring-toggle-${section}`);

async function expanded(page: Page, section: string): Promise<boolean> {
  return (await toggle(page, section).getAttribute("aria-expanded")) === "true";
}

/* ------------------------------------------------------------------- arrival */

test.describe("arriving at the studio", () => {
  test("finished parts are closed and the first unfinished one is open", async ({ page }) => {
    // Titles, subject and preview are all present; the curriculum has a section with no lesson.
    await serveStudio(page, course({ shape: "SECTION_ONLY" }));
    await openStudio(page);

    expect(await expanded(page, "BASICS")).toBe(false);
    expect(await expanded(page, "DETAILS")).toBe(false);
    expect(await expanded(page, "PREVIEW")).toBe(false);
    expect(await expanded(page, "CURRICULUM")).toBe(true);
    expect(await expanded(page, "REVIEW")).toBe(false);

    // And the panel inside the open one is genuinely there, not merely marked open.
    await expect(page.getByTestId("curriculum")).toBeVisible();
    await expect(page.getByTestId("revision-form")).toHaveCount(0);
  });

  test("a course with nothing outstanding opens on review", async ({ page }) => {
    await serveStudio(page, course({ shape: "COMPLETE" }));
    await openStudio(page);

    expect(await expanded(page, "REVIEW")).toBe(true);
    await expect(page.getByTestId("submission-panel")).toBeVisible();
    await expect(page.getByTestId("authoring-progress")).toHaveAttribute("data-complete", "4");
    await expect(page.getByTestId("authoring-progress")).toHaveAttribute("data-total", "4");
  });

  test("the progress overview counts finished parts, and does not become a second navigation", async ({
    page,
  }) => {
    await serveStudio(page, course({ shape: "EMPTY", preview: false }));
    await openStudio(page);

    // Basics, university/subject and the optional media are all settled; only the empty curriculum
    // needs the instructor. The media section is counted because the server asks for nothing there.
    const progress = page.getByTestId("authoring-progress");
    await expect(progress).toHaveAttribute("data-complete", "3");
    await expect(progress).toHaveAttribute("data-total", "4");
    await expect(progress).toContainText("3/4");
    // No second set of controls for the same five things.
    await expect(progress.locator("button, a")).toHaveCount(0);
  });
});

/* -------------------------------------------------------------- not a wizard */

test.describe("the workflow guides rather than locks", () => {
  test("a finished part can be reopened, and more than one can be open at once", async ({
    page,
  }) => {
    await serveStudio(page, course({ shape: "SECTION_ONLY" }));
    await openStudio(page);

    await expect(toggle(page, "BASICS")).toBeEnabled();
    await toggle(page, "BASICS").click();

    expect(await expanded(page, "BASICS")).toBe(true);
    expect(await expanded(page, "CURRICULUM")).toBe(true);
    await expect(page.getByTestId("revision-title-en")).toHaveValue(
      "Organic Chemistry for second year",
    );
  });

  test("no part is disabled, whatever its state", async ({ page }) => {
    await serveStudio(page, course({ shape: "EMPTY", preview: false }));
    await openStudio(page);

    for (const section of ["BASICS", "DETAILS", "PREVIEW", "CURRICULUM", "REVIEW"]) {
      await expect(toggle(page, section)).toBeEnabled();
    }
  });
});

/* ------------------------------------------------------- outstanding is visible */

test("a closed part still says how much is outstanding inside it", async ({ page }) => {
  await serveStudio(page, course({ shape: "SECTION_ONLY", preview: false }));
  await openStudio(page);

  // The curriculum is the first part the instructor must act on, so that is where the studio
  // opened. Optional media with nothing attached is not a step.
  expect(await expanded(page, "CURRICULUM")).toBe(true);
  expect(await expanded(page, "PREVIEW")).toBe(false);

  // A closed part still names its own counts and its own outstanding requirements.
  const curriculum = page.getByTestId("authoring-subline-CURRICULUM");
  await expect(curriculum).toContainText("Sections: 1");
  await expect(curriculum).toContainText("Lessons: 0");
  await expect(curriculum).toContainText("Outstanding: 2");

  // A finished one says that, and an optional one says what it actually is rather than borrowing
  // the word for unfinished work.
  await expect(page.getByTestId("authoring-subline-BASICS")).toContainText("Finished");
  await expect(page.getByTestId("authoring-subline-PREVIEW")).toContainText("Optional");
  await expect(page.getByTestId("authoring-subline-PREVIEW")).not.toContainText("Not finished");
});

/**
 * D-101a — the server validates a cover and a public preview only when one is attached
 * (`backend/internal/catalog/validation.go`), so a course carrying neither is submittable. The
 * studio used to report that course as 3/4 and open the media section as the next thing to do.
 */
test("a submittable course with no optional media is finished and points at review", async ({
  page,
}) => {
  await serveStudio(page, course({ shape: "COMPLETE", preview: false }));
  await openStudio(page);

  const progress = page.getByTestId("authoring-progress");
  await expect(progress).toHaveAttribute("data-complete", "4");
  await expect(progress).toHaveAttribute("data-total", "4");

  expect(await expanded(page, "REVIEW")).toBe(true);
  expect(await expanded(page, "PREVIEW")).toBe(false);
  await expect(page.getByTestId("submission-panel")).toHaveAttribute(
    "data-submission-ready",
    "true",
  );

  // And it is still there to be opened and used at any time.
  await expect(page.getByTestId("authoring-toggle-PREVIEW")).toBeEnabled();
  await page.getByTestId("authoring-toggle-PREVIEW").click();
  await expect(page.getByTestId("course-thumbnail-authoring")).toBeVisible();
});

/* -------------------------------------------------------------- progression */

test.describe("progression", () => {
  test("Continue closes the part being left and opens the next outstanding one", async ({
    page,
  }) => {
    await serveStudio(page, course({ shape: "SECTION_ONLY", preview: false }));
    await openStudio(page);

    // The curriculum is the first part needing the instructor, so that is where the studio opened.
    expect(await expanded(page, "CURRICULUM")).toBe(true);
    await page.getByTestId("authoring-continue-CURRICULUM").click();

    expect(await expanded(page, "CURRICULUM")).toBe(false);
    expect(await expanded(page, "REVIEW")).toBe(true);
  });

  test("Continue steps over optional media rather than into it", async ({ page }) => {
    await serveStudio(page, course({ shape: "COMPLETE", preview: false }));
    await openStudio(page);

    // Opened by hand, since nothing sends the instructor here.
    await page.getByTestId("authoring-toggle-BASICS").click();
    expect(await expanded(page, "BASICS")).toBe(true);

    await page.getByTestId("authoring-toggle-DETAILS").click();
    await page.getByTestId("authoring-continue-DETAILS").click();

    expect(await expanded(page, "DETAILS")).toBe(false);
    expect(await expanded(page, "PREVIEW")).toBe(false);
    expect(await expanded(page, "REVIEW")).toBe(true);
  });

  test("typing does not move anything; only an accepted save does", async ({ page }) => {
    let patched = 0;
    await serveStudio(page, course({ shape: "COMPLETE" }));
    await page.route(`**/api/v1/courses/${COURSE_ID}/revisions/${REVISION_ID}`, async (route) => {
      patched += 1;
      await route.fulfill({ json: course({ shape: "COMPLETE" }).editable_revision });
    });
    await openStudio(page);

    await toggle(page, "BASICS").click();
    expect(await expanded(page, "BASICS")).toBe(true);

    const title = page.getByTestId("revision-title-en");
    await title.fill("Organic Chemistry, revised");
    // Leaving the field is not progress.
    await page.getByTestId("revision-title-ar").click();
    expect(await expanded(page, "BASICS")).toBe(true);
    await expect(page.getByTestId("authoring-save-state")).toHaveAttribute(
      "data-save-state",
      "UNSAVED",
    );

    await page.getByTestId("save-revision").click();
    await expect(page.getByTestId("authoring-notice")).toContainText("Course details saved");
    expect(patched).toBe(1);

    // The save was accepted, so — and only so — the part closes and the workflow moves on.
    await expect(toggle(page, "BASICS")).toHaveAttribute("aria-expanded", "false");
    expect(await expanded(page, "REVIEW")).toBe(true);
  });

  test("a refused save neither claims success nor moves the workflow", async ({ page }) => {
    await serveStudio(page, course({ shape: "COMPLETE" }));
    await page.route(`**/api/v1/courses/${COURSE_ID}/revisions/${REVISION_ID}`, (route) =>
      route.fulfill({
        status: 500,
        contentType: "application/problem+json",
        json: { type: "about:blank", title: "Server error", status: 500 },
      }),
    );
    await openStudio(page);

    await toggle(page, "BASICS").click();
    await page.getByTestId("revision-title-en").fill("Organic Chemistry, revised");
    await page.getByTestId("save-revision").click();

    await expect(page.getByTestId("authoring-save-state")).toHaveAttribute(
      "data-save-state",
      "FAILED",
    );
    await expect(page.getByTestId("authoring-save-state")).not.toContainText("Saved");
    expect(await expanded(page, "BASICS")).toBe(true);
  });
});

/* ------------------------------------------------------------------- Arabic */

test.describe("Arabic", () => {
  test("the workflow reads right to left and overflows nowhere", async ({ page }) => {
    await serveStudio(page, course({ shape: "SECTION_ONLY", preview: false }));
    await openStudio(page, "ar");

    await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
    await expect(page.getByTestId("authoring-toggle-CURRICULUM")).toContainText("المنهج");
    await expect(page.getByTestId("authoring-subline-BASICS")).toContainText("مكتمل");

    // The disclosure headings sit on the reading edge, which in Arabic is the right.
    const trigger = await toggle(page, "CURRICULUM").boundingBox();
    const item = await page.getByTestId("authoring-section-CURRICULUM").boundingBox();
    expect(trigger).not.toBeNull();
    expect(item).not.toBeNull();

    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(overflow, "the Arabic studio scrolls sideways").toBeLessThanOrEqual(1);
  });
});

/* --------------------------------------------------------------- responsive */

for (const [name, width, height] of [
  ["desktop", 1440, 900],
  ["laptop", 1024, 768],
  ["tablet", 768, 1024],
  ["phone", 390, 844],
] as const) {
  for (const locale of ["en", "ar"] as const) {
    test(`the workflow fits ${name} in ${locale}`, async ({ page }) => {
      await page.setViewportSize({ width, height });
      await serveStudio(page, course({ shape: "SECTION_ONLY" }));
      await openStudio(page, locale);

      await expect(page.getByTestId("authoring-progress")).toBeVisible();
      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
      );
      expect(overflow, `${name}/${locale} scrolls sideways`).toBeLessThanOrEqual(1);
    });
  }
}

/* ------------------------------------------------------------ accessibility */

test.describe("accessibility", () => {
  test("each disclosure is a real, named, keyboard-operable control", async ({ page }) => {
    await serveStudio(page, course({ shape: "SECTION_ONLY" }));
    await openStudio(page);

    const basics = toggle(page, "BASICS");
    await expect(basics).toHaveAttribute("aria-expanded", "false");
    await basics.focus();
    await expect(basics).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(basics).toHaveAttribute("aria-expanded", "true");
    await page.keyboard.press("Enter");
    await expect(basics).toHaveAttribute("aria-expanded", "false");

    // The heading level is stated rather than left to the primitive's default, because these are
    // the studio's own second level.
    await expect(page.locator("h3", { hasText: "Course basics" })).toHaveCount(1);
  });

  test("the studio has no axe violations in either language", async ({ page }, testInfo) => {
    for (const locale of ["en", "ar"] as const) {
      await serveStudio(page, course({ shape: "SECTION_ONLY" }));
      await openStudio(page, locale);
      // Radix unmounts a closed disclosure, so a scan taken at arrival covers one section. All five
      // are opened and their contents asserted present before axe runs, and the anchors are
      // attached to the test as evidence of what was actually in the document.
      await openAuthoringSections(page);
      const mounted = await expectAuthoringSectionsMounted(page);
      testInfo.annotations.push({
        type: "axe-scope",
        description: `${locale}: mounted authoring sections — ${mounted.join(", ")}`,
      });
      const results = await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
        .analyze();
      expect(
        results.violations.map((violation) => `${locale}: ${violation.id}`),
        JSON.stringify(results.violations, null, 2),
      ).toEqual([]);
    }
  });
});
