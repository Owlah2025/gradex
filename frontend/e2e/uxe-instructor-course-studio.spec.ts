import { expect, test, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import fs from "node:fs";
import path from "node:path";

/**
 * UX-E — the Instructor Course Studio, in a real browser.
 *
 * The lifecycle itself is proved end to end against the real stack elsewhere: `s12-instructor-
 * video-upload` drives create → upload → submit → change request → resubmit → approve through the
 * product with real media, and `t4cde` proves the academic-identity and audience rules. This spec
 * does not repeat any of that.
 *
 * What it holds is the *presentation* contract at each lifecycle position, which those specs pass
 * through too quickly to examine: that a draft says what is stopping it, that a submitted course
 * says who has it and stops offering an editable form, that a returned course leads with the
 * reviewer's words, and that no identifier or wire enum reaches the reader in either language at
 * either end of the viewport range.
 *
 * The payloads are shaped strictly to the owned-Course contract — `GET /api/v1/courses` and
 * `GET /api/v1/courses/{id}` return exactly these fields — so nothing here can pass by leaning on
 * data the server never sends.
 */

const COURSE_ID = "b1b1b1b1-1111-4111-8111-111111111111";
const REVISION_ID = "c2c2c2c2-2222-4222-8222-222222222222";
const SECTION_ID = "d3d3d3d3-3333-4333-8333-333333333333";
const LESSON_ID = "e4e4e4e4-4444-4444-8444-444444444444";
const ASSET_ID = "f5f5f5f5-5555-4555-8555-555555555555";
const INSTITUTION_ID = "a6a6a6a6-6666-4666-8666-666666666666";
const SUBJECT_ID = "b7b7b7b7-7777-4777-8777-777777777777";

/** Every identifier the fixtures use, for the "none of these reach a reader" sweep. */
const ALL_IDENTIFIERS = [
  COURSE_ID,
  REVISION_ID,
  SECTION_ID,
  LESSON_ID,
  ASSET_ID,
  INSTITUTION_ID,
  SUBJECT_ID,
];

/** Wire vocabulary that must never be shown to an Instructor as its own explanation. */
const WIRE_ENUMS = [
  "PENDING_REVIEW",
  "CHANGES_REQUESTED",
  "ACADEMIC_CATALOG",
  "LEGACY_TAXONOMY",
  "LESSON_VIDEO_MISSING",
  "SECTION_EMPTY",
  "COURSE_EMPTY",
  "ACADEMIC_SUBJECT_MISSING",
  "LAB_MATERIAL",
];

const CHANGE_REASON =
  "Lesson 2 has no audio for the first ninety seconds. Please re-record and resubmit.";

const academicContext = {
  institution_name_ar: "جامعة الكويت",
  institution_name_en: "Kuwait University",
  subject: {
    official_code: "CHEM 201",
    title_ar: "الكيمياء العضوية",
    title_en: "Organic Chemistry",
    owning_unit_name_ar: "قسم الكيمياء",
    owning_unit_name_en: "Department of Chemistry",
    parent_unit_name_ar: "كلية العلوم",
    parent_unit_name_en: "Faculty of Science",
  },
};

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
      price_minor_units: 4500,
      lessons,
    },
  ];
}

function course(options: {
  state?: string;
  shape?: Shape;
  live?: boolean;
  reason?: string;
  subject?: boolean;
  candidate?: boolean;
}) {
  const {
    state = "DRAFT",
    shape = "COMPLETE",
    live = false,
    reason,
    subject = true,
    candidate = true,
  } = options;
  return {
    id: COURSE_ID,
    owner_account_id: "owner-1",
    lifecycle: live ? "PUBLISHED" : state,
    classification_model: "ACADEMIC_CATALOG",
    institution_id: INSTITUTION_ID,
    ...(subject ? { subject_id: SUBJECT_ID } : {}),
    academic_context: subject
      ? academicContext
      : { institution_name_ar: academicContext.institution_name_ar, institution_name_en: academicContext.institution_name_en },
    price_minor_units: live ? 12500 : null,
    ...(live ? { live_revision_id: "live-revision" } : {}),
    ...(candidate
      ? {
          editable_revision: {
            id: REVISION_ID,
            course_id: COURSE_ID,
            state,
            revision_number: 1,
            title_ar: "الكيمياء العضوية للسنة الثانية",
            title_en: "Organic Chemistry for second year",
            description_ar: "مقرر يغطي أساسيات الكيمياء العضوية.",
            description_en: "A course covering the foundations of organic chemistry.",
            preview_asset_version_id: shape === "COMPLETE" ? ASSET_ID : undefined,
            ...(reason ? { review_reason: reason } : {}),
            sections: sections(shape),
          },
        }
      : {}),
  };
}

async function serveStudio(page: Page, payload: ReturnType<typeof course>) {
  await page.route("**/api/v1/session/bootstrap", (route) =>
    route.fulfill({ json: { csrf_token: "csrf-token" } }),
  );
  // The resolve route is what rehydrates the in-memory session, CSRF token included. Without the
  // token every authoring command refuses locally before it reaches the network.
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
          {
            program_id: "program-1",
            name_ar: "كيمياء",
            name_en: "Chemistry",
            recommended_level: 2,
          },
        ],
      },
    }),
  );
  await page.route("**/api/v1/authoring/academic/subject-requests**", (route) =>
    route.fulfill({ json: [] }),
  );
  await page.route("**/api/v1/taxonomy/terms**", (route) => route.fulfill({ json: [] }));
}

async function openStudio(page: Page, locale: "ar" | "en") {
  await page.addInitScript((selected) => {
    window.localStorage.setItem("gradex.locale", selected as string);
  }, locale);
  await page.goto(`/${locale}/instructor/courses`);
  await expect(page.getByTestId(`owned-course-${COURSE_ID}`)).toBeVisible();
}

/** Everything a reader can actually see, with markup and attributes excluded. */
async function visibleText(page: Page): Promise<string> {
  return page.locator("#main").innerText();
}

/* ------------------------------------------------------------------ the directory */

test.describe("the course directory", () => {
  test("names each course, what it is taught for, and who acts next", async ({ page }) => {
    await serveStudio(page, course({ state: "DRAFT", shape: "COMPLETE" }));
    await openStudio(page, "en");

    const row = page.getByTestId(`owned-course-${COURSE_ID}`);
    await expect(row).toContainText("Organic Chemistry for second year");
    // Read from `academic_context`, which the payload has always carried.
    await expect(page.getByTestId(`owned-course-academic-${COURSE_ID}`)).toContainText(
      "Kuwait University",
    );
    await expect(page.getByTestId(`owned-course-academic-${COURSE_ID}`)).toContainText(
      "Organic Chemistry",
    );

    // The state, and beside it in words the thing a colour cannot say.
    await expect(page.getByTestId(`owned-course-standing-${COURSE_ID}`)).toHaveText("Draft");
    await expect(page.getByTestId(`owned-course-actor-${COURSE_ID}`)).toHaveText("Your turn");
  });

  test("a draft behind a published course is distinguished from a first draft", async ({ page }) => {
    await serveStudio(page, course({ state: "DRAFT", live: true }));
    await openStudio(page, "en");
    await expect(page.getByTestId(`owned-course-standing-${COURSE_ID}`)).toHaveText("Draft update");
    await expect(page.getByTestId("course-standing-meaning")).toContainText(
      "Students keep seeing the published course",
    );
  });

  test("an instructor with no courses is invited to create one", async ({ page }) => {
    await serveStudio(page, course({}));
    await page.route("**/api/v1/courses", (route) => route.fulfill({ json: [] }));
    await page.addInitScript(() => window.localStorage.setItem("gradex.locale", "en"));
    await page.goto("/en/instructor/courses");
    await expect(page.getByTestId("owned-course-list")).toContainText(
      "You have not created a course yet",
    );
    await expect(page.getByTestId("empty-create-course")).toBeVisible();
  });
});

/* ------------------------------------------------------------------ readiness */

test.describe("submission readiness", () => {
  test("an empty course names every requirement it has not met yet", async ({ page }) => {
    await serveStudio(page, course({ shape: "EMPTY" }));
    await openStudio(page, "en");

    await expect(page.getByTestId("submission-panel")).toHaveAttribute(
      "data-submission-ready",
      "false",
    );
    await expect(page.getByTestId("requirement-SECTIONS")).toHaveAttribute("data-met", "false");
    await expect(page.getByTestId("requirement-LESSON_VIDEOS")).toHaveAttribute("data-met", "false");
    // The academic identity is present on this fixture, so it is already satisfied.
    await expect(page.getByTestId("requirement-ACADEMIC_SUBJECT")).toHaveAttribute(
      "data-met",
      "true",
    );
    // Institution and subject are satisfied by the fixture; the three content rules are not.
    await expect(page.getByTestId("readiness-count")).toContainText("2/5");
  });

  test("a section with no lessons is named by its title, not its identifier", async ({ page }) => {
    await serveStudio(page, course({ shape: "SECTION_ONLY" }));
    await openStudio(page, "en");

    const requirement = page.getByTestId("requirement-SECTION_LESSONS");
    await expect(requirement).toHaveAttribute("data-met", "false");
    await expect(requirement).toContainText("Foundations of organic chemistry");
    await expect(requirement).not.toContainText(SECTION_ID);
  });

  test("a course missing its subject is told so, and told where", async ({ page }) => {
    await serveStudio(page, course({ subject: false, shape: "EMPTY" }));
    await openStudio(page, "en");
    await expect(page.getByTestId("requirement-ACADEMIC_SUBJECT")).toHaveAttribute(
      "data-met",
      "false",
    );
    await expect(page.getByTestId("requirement-ACADEMIC_SUBJECT")).toContainText("subject");
  });

  test("the launch price is presented as the administrator's, never as a prerequisite", async ({
    page,
  }) => {
    await serveStudio(page, course({ shape: "COMPLETE" }));
    await openStudio(page, "en");

    await expect(page.getByTestId("submission-panel")).toHaveAttribute(
      "data-submission-ready",
      "true",
    );
    // Ready with no price set at all.
    await expect(page.getByTestId("course-price-unset")).toBeVisible();
    await expect(page.getByTestId("submission-price-note")).toContainText(
      "You do not set the price",
    );
    await expect(page.getByTestId("course-pricing-summary")).toContainText(
      "Gradex sets the launch price",
    );

    const checklist = await page.getByTestId("readiness-checklist").innerText();
    expect(checklist.toLowerCase()).not.toContain("price");
  });

  test("submitting is confirmed, because it closes editing", async ({ page }) => {
    await serveStudio(page, course({ shape: "COMPLETE" }));
    await openStudio(page, "en");

    await page.getByTestId("submit-for-review").click();
    const dialog = page.getByTestId("submit-confirm");
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("cannot be edited until they decide");
    // Cancel is focused first, so a reflexive Enter submits nothing.
    await expect(dialog.getByTestId("confirm-cancel")).toBeFocused();
    await dialog.getByTestId("confirm-cancel").click();
    await expect(dialog).toBeHidden();
  });

  test("a server refusal is named in words, without its codes or its targets", async ({ page }) => {
    await serveStudio(page, course({ shape: "COMPLETE" }));
    await openStudio(page, "en");
    await page.route(`**/api/v1/courses/${COURSE_ID}/revisions/${REVISION_ID}/submit`, (route) =>
      route.fulfill({
        status: 422,
        json: {
          type: "https://api.gradex.com/problems/submission-incomplete",
          title: "Course submission incomplete",
          status: 422,
          code: "SUBMISSION_INCOMPLETE",
          violations: [
            { code: "LESSON_VIDEO_MISSING", target: `lesson:${LESSON_ID}` },
            { code: "LESSON_VIDEO_MISSING", target: `lesson:${SECTION_ID}` },
            { code: "SECTION_EMPTY", target: `section:${SECTION_ID}` },
          ],
        },
      }),
    );

    await page.getByTestId("submit-for-review").click();
    await page.getByTestId("submit-confirm").getByTestId("confirm-accept").click();

    const failure = page.getByTestId("submit-error");
    await expect(failure).toBeVisible();
    await expect(failure).toContainText("Every lesson needs a video.");
    await expect(failure).toContainText("Every section needs at least one lesson.");
    // Two lessons failed the same requirement; that is one thing to read.
    expect(
      (await failure.innerText()).split("Every lesson needs a video.").length - 1,
    ).toBe(1);
    for (const identifier of ALL_IDENTIFIERS) {
      await expect(failure).not.toContainText(identifier);
    }
    await expect(failure).not.toContainText("LESSON_VIDEO_MISSING");
    // The page-level region must not repeat the raw join beneath it.
    await expect(page.getByTestId("authoring-error")).toHaveCount(0);
  });
});

/* ------------------------------------------------------------------ lifecycle states */

test.describe("lifecycle states", () => {
  test("a submitted course says who has it and stops offering an editable form", async ({
    page,
  }) => {
    await serveStudio(page, course({ state: "PENDING_REVIEW" }));
    await openStudio(page, "en");

    await expect(page.getByTestId("revision-state")).toHaveText("In review");
    await expect(page.getByTestId("course-standing-actor")).toHaveText("With an administrator");
    await expect(page.getByTestId("course-standing")).toHaveAttribute("data-editable", "false");

    await expect(page.getByTestId("submitted-course-summary")).toBeVisible();
    await expect(page.getByTestId("submitted-sections")).toHaveText("1");
    await expect(page.getByTestId("submitted-lessons")).toHaveText("1");

    // The form the studio used to render over a revision the server refuses every write to.
    await expect(page.getByTestId("revision-form")).toHaveCount(0);
    await expect(page.getByTestId("add-section-form")).toHaveCount(0);
    await expect(page.getByTestId("submit-for-review")).toHaveCount(0);
  });

  test("a returned course leads with the reviewer's own words and can be resubmitted", async ({
    page,
  }) => {
    await serveStudio(page, course({ state: "CHANGES_REQUESTED", reason: CHANGE_REASON }));
    await openStudio(page, "en");

    await expect(page.getByTestId("change-request-notice")).toBeVisible();
    await expect(page.getByTestId("change-request-reason")).toHaveText(CHANGE_REASON);
    await expect(page.getByTestId("revision-state")).toHaveText("Changes requested");
    await expect(page.getByTestId("course-standing-actor")).toHaveText("Your turn");

    // Returned is an editable state, so the way back is the same way forward.
    await expect(page.getByTestId("course-standing")).toHaveAttribute("data-editable", "true");
    await expect(page.getByTestId("revision-form")).toBeVisible();
    await expect(page.getByTestId("submit-for-review")).toBeEnabled();
  });

  test("a published course with no open revision offers to start one", async ({ page }) => {
    await serveStudio(page, course({ live: true, candidate: false }));
    await openStudio(page, "en");
    await expect(page.getByTestId(`owned-course-standing-${COURSE_ID}`)).toHaveText("Published");
    await expect(page.getByTestId("start-revision-panel")).toBeVisible();
    await expect(page.getByTestId("start-revision")).toBeEnabled();
  });
});

/* ------------------------------------------------------------------ curriculum */

test.describe("the curriculum", () => {
  test("a lesson reports whether its video is attached, not which asset it is", async ({ page }) => {
    await serveStudio(page, course({ shape: "COMPLETE" }));
    await openStudio(page, "en");

    const attached = page.getByTestId(`lesson-video-ref-${LESSON_ID}`);
    await expect(attached).toHaveText("Video attached");
    await expect(attached).toHaveAttribute("data-video-attached", "true");
    await expect(attached).not.toContainText(ASSET_ID);
  });

  test("deleting a section asks first, and says what goes with it", async ({ page }) => {
    await serveStudio(page, course({ shape: "COMPLETE" }));
    await openStudio(page, "en");

    await page.getByTestId(`delete-section-${SECTION_ID}`).click();
    const dialog = page.getByTestId("curriculum-delete-confirm");
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("every lesson inside it");
    await expect(dialog).toContainText("cannot be undone");
    await expect(dialog.getByTestId("confirm-cancel")).toBeFocused();

    // Escape leaves the curriculum exactly as it was.
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
    await expect(page.getByTestId(`section-${SECTION_ID}`)).toBeVisible();
  });

  test("adding a section is not behind a confirmation", async ({ page }) => {
    await serveStudio(page, course({ shape: "COMPLETE" }));
    await openStudio(page, "en");
    await expect(page.getByTestId("add-section-form")).toBeVisible();
    // Both language fields are named, not merely placeheld.
    await expect(page.getByTestId("section-title-ar")).toHaveAccessibleName(/Arabic/);
    await expect(page.getByTestId("section-title-en")).toHaveAccessibleName(/English/);
  });
});

/* ------------------------------------------------------------------ nothing technical leaks */

for (const locale of ["en", "ar"] as const) {
  for (const state of ["DRAFT", "PENDING_REVIEW", "CHANGES_REQUESTED"] as const) {
    test(`no identifier or wire enum reaches the reader — ${locale} ${state}`, async ({ page }) => {
      await serveStudio(
        page,
        course({ state, reason: state === "CHANGES_REQUESTED" ? CHANGE_REASON : undefined }),
      );
      await openStudio(page, locale);

      const text = await visibleText(page);
      for (const identifier of ALL_IDENTIFIERS) {
        expect(text, `${identifier} is shown to the Instructor`).not.toContain(identifier);
      }
      for (const wire of WIRE_ENUMS) {
        expect(text, `the wire value ${wire} is shown to the Instructor`).not.toContain(wire);
      }
      // No bare UUID of any shape, from any field the fixtures do not name.
      expect(text).not.toMatch(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i);
    });
  }
}

/* ------------------------------------------------------------------ Arabic */

test.describe("Arabic", () => {
  test("the studio is directed, and its academic hierarchy reads in Arabic", async ({ page }) => {
    await serveStudio(page, course({ shape: "COMPLETE" }));
    await openStudio(page, "ar");

    await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
    await expect(page.getByTestId("academic-course-context")).toContainText("جامعة الكويت");
    await expect(page.getByTestId("academic-course-subject-name")).toContainText("الكيمياء العضوية");
    await expect(page.getByTestId("revision-state")).toHaveText("مسودة");
    await expect(page.getByTestId("course-standing-actor")).toHaveText("دورك الآن");
    await expect(page.getByTestId("submission-panel")).toContainText("أنت لا تحدد السعر");
  });

  test("the Latin subject code survives inside Arabic copy", async ({ page }) => {
    await serveStudio(page, course({ shape: "COMPLETE" }));
    await openStudio(page, "ar");
    // Isolated with <bdi>, so the code does not reorder against the Arabic around it.
    await expect(page.getByTestId("academic-course-context")).toContainText("CHEM 201");
    await expect(page.getByTestId("academic-course-context").locator("bdi").first()).toBeVisible();
  });

  test("the readiness checklist is Arabic, and names Arabic titles", async ({ page }) => {
    await serveStudio(page, course({ shape: "SECTION_ONLY" }));
    await openStudio(page, "ar");
    await expect(page.getByTestId("submission-panel")).toContainText("يلزم إكمال ما يلي");
    await expect(page.getByTestId("requirement-SECTION_LESSONS")).toContainText(
      "أساسيات الكيمياء العضوية",
    );
  });
});

/* ------------------------------------------------------------------ responsive */

const VIEWPORTS = [
  ["phone", 390, 844],
  ["tablet", 768, 1024],
  ["laptop", 1024, 768],
  ["desktop", 1280, 800],
  ["wide", 1440, 900],
] as const;

for (const [name, width, height] of VIEWPORTS) {
  for (const locale of ["en", "ar"] as const) {
    test(`the studio does not overflow at ${name} (${width}px) in ${locale}`, async ({ page }) => {
      await page.setViewportSize({ width, height });
      await serveStudio(page, course({ shape: "COMPLETE" }));
      await openStudio(page, locale);

      await expect(page.getByTestId("submission-panel")).toBeVisible();
      await expect(
        page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
      ).resolves.toBe(true);
    });
  }
}

test("a long resource name does not push the lesson row off the screen", async ({ page }) => {
  const long = "محاضرة الكيمياء العضوية الأسبوع الثاني عشر مراجعة شاملة ونماذج امتحانات سابقة.pdf";
  const withResource = course({ shape: "COMPLETE" });
  withResource.editable_revision!.sections[0].lessons[0].files = [
    {
      id: "file-1",
      kind: "RESOURCE",
      asset_version_id: ASSET_ID,
      display_name_ar: long,
      display_name_en: long,
      position: 1,
    },
  ] as never;

  await page.setViewportSize({ width: 390, height: 844 });
  await serveStudio(page, withResource);
  await openStudio(page, "ar");

  await expect(page.getByTestId("lesson-resource-file-1")).toBeVisible();
  await expect(
    page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
  ).resolves.toBe(true);
});

/* ------------------------------------------------------------------ accessibility */

for (const locale of ["en", "ar"] as const) {
  for (const [label, payload] of [
    ["a draft being built", course({ shape: "SECTION_ONLY" })],
    ["a course awaiting review", course({ state: "PENDING_REVIEW" })],
    ["a course returned for changes", course({ state: "CHANGES_REQUESTED", reason: CHANGE_REASON })],
  ] as const) {
    test(`${label} has no accessibility violations in ${locale}`, async ({ page }) => {
      await serveStudio(page, payload);
      await openStudio(page, locale);
      await expect(page.getByTestId("course-standing")).toBeVisible();

      const results = await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
        .analyze();
      const detail = results.violations
        .flatMap((violation) =>
          violation.nodes.map(
            (node) => `${violation.id}: ${node.html} — ${node.failureSummary ?? ""}`,
          ),
        )
        .join("\n");
      expect(results.violations.map((violation) => violation.id), detail).toEqual([]);
    });
  }
}

test("the delete dialog manages focus and is announced as a dialog", async ({ page }) => {
  await serveStudio(page, course({ shape: "COMPLETE" }));
  await openStudio(page, "en");

  const trigger = page.getByTestId(`delete-section-${SECTION_ID}`);
  await trigger.click();
  const dialog = page.getByTestId("curriculum-delete-confirm");
  await expect(dialog).toHaveAttribute("role", "dialog");
  await expect(dialog).toHaveAttribute("aria-labelledby", /.+/);
  await expect(dialog).toHaveAttribute("aria-describedby", /.+/);

  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();
  expect(
    results.violations.map((violation) => violation.id),
    results.violations.map((v) => `${v.id}: ${v.nodes[0]?.failureSummary ?? ""}`).join("\n"),
  ).toEqual([]);

  // Focus returns to the control that opened it.
  await page.keyboard.press("Escape");
  await expect(trigger).toBeFocused();
});

/* ------------------------------------------------------------------ contrast, measured */

test("the price and every secondary line clear AA where they are actually painted", async ({
  page,
}) => {
  await serveStudio(page, course({ live: true }));
  await openStudio(page, "en");

  const ratios = await page.evaluate(() => {
    const luminance = (rgb: string): number => {
      const [r, g, b] = rgb.match(/\d+(\.\d+)?/g)!.slice(0, 3).map(Number);
      const channel = (value: number) => {
        const c = value / 255;
        return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
      };
      return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
    };
    const groundOf = (element: Element): string => {
      let node: Element | null = element;
      while (node) {
        const background = getComputedStyle(node).backgroundColor;
        if (background && !/rgba\(0, 0, 0, 0\)|transparent/.test(background)) return background;
        node = node.parentElement;
      }
      return "rgb(255, 255, 255)";
    };
    const ratio = (element: Element) => {
      const foreground = luminance(getComputedStyle(element).color);
      const background = luminance(groundOf(element));
      const [high, low] = [foreground, background].sort((a, b) => b - a);
      return (high + 0.05) / (low + 0.05);
    };
    const measure = (testID: string) => {
      const element = document.querySelector(`[data-testid="${testID}"]`);
      return element ? ratio(element) : null;
    };
    return {
      price: measure("course-price-value"),
      standing: measure("course-standing-meaning"),
      readiness: measure("readiness-count"),
    };
  });

  // The figure this tranche was asked to fix: it measured 3.77:1 as `text-emerald-600`.
  expect(ratios.price, "the course price must clear AA").not.toBeNull();
  expect(ratios.price!).toBeGreaterThanOrEqual(4.5);
  expect(ratios.standing!).toBeGreaterThanOrEqual(4.5);
  expect(ratios.readiness!).toBeGreaterThanOrEqual(4.5);
});

/* ------------------------------------------------------------------ evidence */

const SHOTS = [
  ["directory-and-draft", course({ shape: "SECTION_ONLY" }), 1440, 900],
  ["ready-to-submit", course({ shape: "COMPLETE" }), 1440, 900],
  ["awaiting-review", course({ state: "PENDING_REVIEW" }), 1440, 900],
  ["changes-requested", course({ state: "CHANGES_REQUESTED", reason: CHANGE_REASON }), 1440, 900],
  ["draft-phone", course({ shape: "SECTION_ONLY" }), 390, 844],
] as const;

for (const [name, payload, width, height] of SHOTS) {
  for (const locale of ["en", "ar"] as const) {
    test(`evidence: ${name} at ${width}px in ${locale}`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width, height });
      await serveStudio(page, payload);
      await openStudio(page, locale);
      await expect(page.getByTestId("course-standing")).toBeVisible();

      /*
        Written to a file as well as attached. An attachment is only materialised by reporters that
        persist one, and this evidence has to be openable and looked at, not merely produced.
      */
      const file = path.join(
        process.env.GRADEX_UXE_EVIDENCE_DIR || testInfo.outputDir,
        `uxe-${name}-${locale}-${width}.png`,
      );
      fs.mkdirSync(path.dirname(file), { recursive: true });
      const shot = await page.screenshot({ fullPage: true, path: file });
      await testInfo.attach(`uxe-${name}-${locale}-${width}`, {
        body: shot,
        contentType: "image/png",
      });
    });
  }
}
