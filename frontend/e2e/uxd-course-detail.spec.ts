import { expect, test, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { ar } from "../src/lib/i18n/dictionaries/ar";
import { en } from "../src/lib/i18n/dictionaries/en";

/**
 * UX-D — the public Course Details experience, in a real browser.
 *
 * The catalogue is served from fixtures rather than the seeded database, because what is under test
 * is the *presentation* of one exact payload: an outline long enough to need disclosure, a title
 * long enough to wrap, a Subject code that mixes scripts, and both languages of every label. The
 * fixture is shaped strictly to the real public contract — `GET /api/v1/catalog/courses/{idOrSlug}`
 * returns exactly these fields and no others — so a test passing here cannot be relying on data the
 * server never sends.
 *
 * The end-to-end journey against real launch-manifest data is UX-C's; this spec does not repeat it.
 */

const COURSE_ID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa";
const SLUG = "data-structures-and-algorithms";

const UNAUTHORIZED = {
  type: "https://api.gradex.com/problems/unauthorized",
  title: "Unauthorized",
  status: 401,
  code: "UNAUTHORIZED",
};

const NOT_FOUND = {
  type: "https://api.gradex.com/problems/not-found",
  title: "Not found",
  status: 404,
  code: "NOT_FOUND",
};

const sectionTitles = {
  en: [
    "Getting started with algorithmic thinking",
    "Arrays, strings and the cost of copying",
    "Linked lists",
    "Stacks and queues, and where each one belongs",
    "Recursion and how to reason about it without losing the thread",
    "Trees",
    "Balanced search trees and their rebalancing rules",
    "Hashing",
    "Graphs and traversal",
    "Shortest paths",
    "Sorting, compared honestly",
  ],
  ar: [
    "مقدمة في التفكير الخوارزمي",
    "المصفوفات والسلاسل النصية وتكلفة النسخ",
    "القوائم المترابطة",
    "المكدسات والطوابير وأين يُستخدم كل منها",
    "الاستدعاء الذاتي وكيفية تتبعه دون فقدان التسلسل المنطقي",
    "الأشجار",
    "أشجار البحث المتوازنة وقواعد إعادة توازنها",
    "التجزئة",
    "الرسوم البيانية واجتيازها",
    "أقصر المسارات",
    "الترتيب، مقارنة منصفة",
  ],
} as const;

const LESSON_COUNTS = [4, 6, 3, 5, 7, 4, 6, 3, 8, 5, 6];
const TOTAL_LESSONS = LESSON_COUNTS.reduce((sum, count) => sum + count, 0);

/** Eight is the outline's disclosure threshold, so eleven sections exercise both halves of it. */
const COLLAPSED_SECTIONS = 8;

const localized = {
  en: {
    title: "Data Structures and Algorithms",
    instructor: "Dr. Fahd Al-Mutairi",
    university: "Kuwait University",
    major: "Computer Engineering",
    subject: "Data Structures",
    level: "Year 3",
    description: "This course covers the core data structures.",
    audience: ["Computer Engineering", "Computer Science"],
    price: "12.500 KWD",
    outline: "Course outline",
    academicHeading: "Where this course fits",
    universityLabel: "University",
    majorLabel: "Major",
    subjectLabel: "Subject",
    levelLabel: "Academic level",
    instructorHeading: "Your instructor",
    showAll: "Show every section",
    showFewer: "Show fewer sections",
    unavailable: "This course is not available",
    loadFailed: "This course could not be loaded",
    accessJump: "Access options",
    lessons: "lessons",
  },
  ar: {
    title: "هياكل البيانات والخوارزميات: بناء الحلول الفعّالة وتحليل تعقيدها الزمني",
    instructor: "د. فهد المطيري",
    university: "جامعة الكويت",
    major: "هندسة الحاسوب",
    subject: "هياكل البيانات",
    level: "السنة الثالثة",
    description: "يغطي هذا المقرر هياكل البيانات الأساسية.",
    audience: ["هندسة الحاسوب", "علوم الحاسوب"],
    price: "12.500 د.ك",
    outline: "محتوى المقرر",
    academicHeading: "موقع هذا المقرر في خطتك",
    universityLabel: "الجامعة",
    majorLabel: "التخصص",
    subjectLabel: "المقرر",
    levelLabel: "المستوى الدراسي",
    instructorHeading: "مدرّس المقرر",
    showAll: "اعرض كل الأقسام",
    showFewer: "اعرض أقساماً أقل",
    unavailable: "هذا المقرر غير متاح",
    loadFailed: "تعذّر تحميل هذا المقرر",
    accessJump: "خيارات الوصول",
    lessons: "دروس",
  },
} as const;

type Locale = keyof typeof localized;

/** Exactly the public projection: no rating, no enrolment count, no duration, no thumbnail. */
function courseFixture(locale: Locale, sectionCount = 11) {
  const copy = localized[locale];
  return {
    id: COURSE_ID,
    slug: SLUG,
    title: copy.title,
    instructor_display_name: copy.instructor,
    university: { label: copy.university },
    major: { label: copy.major },
    subject: { label: copy.subject, code: "CPE-232" },
    study_year: { label: copy.level },
    price: { minor_units: 12500, currency: "KWD" },
    has_preview: true,
    description: copy.description,
    sections: sectionTitles[locale].slice(0, sectionCount).map((title, index) => ({
      title,
      position: index + 1,
      lesson_count: LESSON_COUNTS[index],
    })),
    program_audience: [...copy.audience],
  };
}

type AccessBody = { status: number; json: unknown };

const ANONYMOUS: AccessBody = { status: 401, json: UNAUTHORIZED };

/** Signed in, with no invitation and no entitlement for this Course. */
const SIGNED_IN_NO_ACCESS: AccessBody = { status: 200, json: { items: [] } };

const ENTITLED: AccessBody = {
  status: 200,
  json: {
    items: [
      {
        course_id: COURSE_ID,
        course_title: "Data Structures and Algorithms",
        has_active_access: true,
        invitation: { state: "APPROVED" },
      },
    ],
  },
};

async function serveCourse(
  page: Page,
  locale: Locale,
  options: { access?: AccessBody; sectionCount?: number } = {},
) {
  const access = options.access ?? ANONYMOUS;
  await page.route("**/api/v1/catalog/courses/**", (route) =>
    route.fulfill({ json: courseFixture(locale, options.sectionCount) }),
  );
  await page.route("**/api/v1/me/course-access**", (route) =>
    route.fulfill({ status: access.status, json: access.json }),
  );
  await page.route("**/api/v1/me/academic-profile**", (route) =>
    route.fulfill({ status: 401, json: UNAUTHORIZED }),
  );
}

async function openCourse(page: Page, locale: Locale) {
  await page.goto(`/${locale}/catalog/${SLUG}`);
  await expect(page.getByTestId("course-detail-title")).toBeVisible();
}

for (const locale of ["en", "ar"] as const) {
  const copy = localized[locale];

  /**
   * Everything the public contract carries, rendered — and read back in the reader's own language.
   */
  test(`${locale} Course Details renders the real public course contract`, async ({ page }) => {
    await serveCourse(page, locale);
    await openCourse(page, locale);

    await expect(page.locator("html")).toHaveAttribute("dir", locale === "ar" ? "rtl" : "ltr");
    await expect(page.getByRole("heading", { level: 1, name: copy.title })).toBeVisible();
    await expect(page.getByTestId("course-detail-instructor-line")).toContainText(copy.instructor);
    await expect(page.getByTestId("course-detail-description")).toContainText(copy.description);
    await expect(page.getByTestId("course-detail-eyebrow")).toContainText(copy.university);

    // The one shared shell, not a bespoke catalogue header. Named from the
    // dictionary because the label used to be a hardcoded English "Primary" —
    // which this Arabic case was asserting, on an Arabic-default product.
    await expect(
      page.getByRole("navigation", {
        name: (locale === "ar" ? ar : en).nav.primaryNavigation,
      }),
    ).toBeAttached();
    await expect(page.locator("footer")).toBeAttached();
  });

  /**
   * The price is the canonical formatter's output, not this page's own arithmetic.
   *
   * The surface this replaces printed `(minor_units / 1000).toFixed(3)` beside a raw currency code,
   * so Arabic readers saw "12.500 KWD" where the rest of the product says "12.500 د.ك".
   */
  test(`${locale} Course Details prices the course through the shared formatter`, async ({ page }) => {
    await serveCourse(page, locale);
    await openCourse(page, locale);
    await expect(page.getByTestId("course-access-price")).toContainText(copy.price);
  });

  /**
   * The academic block names each field the way a student would, and never as a data structure.
   */
  test(`${locale} Course Details states academic relevance in student language`, async ({ page }) => {
    await serveCourse(page, locale);
    await openCourse(page, locale);

    const academic = page.getByTestId("course-academic-context");
    await expect(academic.getByRole("heading", { name: copy.academicHeading })).toBeVisible();

    for (const [testID, label, value] of [
      ["course-academic-university", copy.universityLabel, copy.university],
      ["course-academic-major", copy.majorLabel, copy.major],
      ["course-academic-subject", copy.subjectLabel, copy.subject],
      ["course-academic-level", copy.levelLabel, copy.level],
    ] as const) {
      await expect(academic.getByTestId(testID)).toContainText(label);
      await expect(academic.getByTestId(testID)).toContainText(value);
    }

    // The Subject's official code, beside the Subject and nowhere else.
    await expect(academic.getByTestId("course-academic-subject")).toContainText("CPE-232");
    for (const program of copy.audience) {
      await expect(academic.getByTestId("course-academic-audience")).toContainText(program);
    }

    // The word the previous surface shipped to Arabic screen readers in English.
    expect(await page.locator("main").innerText()).not.toContain("Taxonomy");
    expect((await page.locator("main").ariaSnapshot()).toLowerCase()).not.toContain("taxonomy");
  });

  /**
   * The outline, at the depth the contract supports: sections and their lesson counts.
   */
  test(`${locale} Course Details lists the real section hierarchy`, async ({ page }) => {
    await serveCourse(page, locale);
    await openCourse(page, locale);

    await expect(page.getByRole("heading", { level: 2, name: copy.outline })).toBeVisible();
    const totals = page.getByTestId("course-curriculum-totals");
    await expect(totals).toContainText("11");
    await expect(totals).toContainText(String(TOTAL_LESSONS));

    const rows = page.getByTestId("course-curriculum-section");
    await expect(rows).toHaveCount(COLLAPSED_SECTIONS);
    await expect(rows.first()).toContainText(sectionTitles[locale][0]);
    await expect(rows.first()).toContainText(`${LESSON_COUNTS[0]} ${copy.lessons}`);
  });

  /**
   * The disclosure is a real button with real state, operable without a mouse.
   */
  test(`${locale} the outline disclosure is keyboard operable and announces its state`, async ({ page }) => {
    await serveCourse(page, locale);
    await openCourse(page, locale);

    const disclosure = page.getByTestId("course-curriculum-disclosure");
    await expect(disclosure).toHaveRole("button");
    await expect(disclosure).toHaveAttribute("aria-expanded", "false");
    await expect(disclosure).toContainText(copy.showAll);

    await disclosure.focus();
    await expect(disclosure).toBeFocused();
    await page.keyboard.press("Enter");

    await expect(disclosure).toHaveAttribute("aria-expanded", "true");
    await expect(disclosure).toContainText(copy.showFewer);
    await expect(page.getByTestId("course-curriculum-section")).toHaveCount(11);

    // And it collapses again from the keyboard.
    await page.keyboard.press("Enter");
    await expect(disclosure).toHaveAttribute("aria-expanded", "false");
    await expect(page.getByTestId("course-curriculum-section")).toHaveCount(COLLAPSED_SECTIONS);
  });

  test(`${locale} Course Details credits the instructor without inventing credentials`, async ({ page }) => {
    await serveCourse(page, locale);
    await openCourse(page, locale);

    const instructor = page.getByTestId("course-instructor");
    await expect(instructor.getByRole("heading", { name: copy.instructorHeading })).toBeVisible();
    await expect(instructor).toContainText(copy.instructor);
  });

  /**
   * The page must not grow marketplace furniture the catalogue cannot support.
   */
  test(`${locale} Course Details shows no rating, review or enrolment statistic`, async ({ page }) => {
    await serveCourse(page, locale);
    await openCourse(page, locale);

    const rendered = await page.locator("main").innerText();
    // Whole words: "preview" legitimately contains "review", and the course preview is real.
    for (const invented of [
      /\bratings?\b/i,
      /\breviews?\b/i,
      /\bbestseller\b/i,
      /\benrol(?:led|ments?)\b/i,
      /\benrollments?\b/i,
      /\bstudents\b/i,
      /تقييم/,
      /مراجعات/,
      /الأكثر مبيعاً/,
      /عدد الطلاب/,
    ]) {
      expect(rendered, `unsupported marketplace data matched ${invented}`).not.toMatch(invented);
    }
    // And no gateway checkout, which does not exist in this product at all.
    await expect(
      page.getByRole("button", { name: /checkout|cart|coupon|buy now|payment|الدفع|السلة|قسيمة/i }),
    ).toHaveCount(0);
    await expect(page.locator("input[type='password'], input[autocomplete*='cc-']")).toHaveCount(0);
  });
}

/**
 * A 390px phone renders the whole page without a horizontal scrollbar, and keeps the access card
 * reachable once it has scrolled away.
 */
test("Course Details fits a 390px phone and keeps access reachable", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await serveCourse(page, "en");
  await openCourse(page, "en");

  await expect(
    page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
  ).resolves.toBe(true);

  // The access card is answered before the course body on a phone, not after it.
  const cardTop = await page.getByTestId("course-access-summary").evaluate((n) => n.getBoundingClientRect().top);
  const outlineTop = await page.getByTestId("course-curriculum").evaluate((n) => n.getBoundingClientRect().top);
  expect(cardTop).toBeLessThan(outlineTop);

  const bar = page.getByTestId("course-access-mobile-bar");
  await expect(bar).toHaveCount(0);

  await page.getByTestId("course-curriculum").scrollIntoViewIfNeeded();
  await expect(bar).toBeVisible();
  await expect(bar).toContainText("12.500 KWD");

  // It returns the reader to the card rather than duplicating the action.
  await page.getByTestId("course-access-mobile-jump").click();
  await expect(page.getByTestId("course-access-panel")).toBeInViewport();

  // And it never sits on top of the footer.
  await page.locator("footer").scrollIntoViewIfNeeded();
  await expect(bar).toHaveCount(0);
});

test("the desktop access card stays beside the course without covering the footer", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await serveCourse(page, "en");
  await openCourse(page, "en");

  const card = page.getByTestId("course-access-summary");
  // Beside the title, not stacked under the whole page.
  const cardBox = await card.boundingBox();
  const titleBox = await page.getByTestId("course-detail-title").boundingBox();
  expect(cardBox!.x).toBeGreaterThan(titleBox!.x + titleBox!.width - 1);

  await page.locator("footer").scrollIntoViewIfNeeded();
  const footerTop = (await page.locator("footer").boundingBox())!.y;
  const stuckBottom = (await card.boundingBox())!.y + (await card.boundingBox())!.height;
  expect(stuckBottom).toBeLessThanOrEqual(footerTop + 1);
});

/**
 * Preview stays exactly as protected as it was.
 *
 * The URL is minted by the media endpoint on demand and only for a Course whose published projection
 * says a public preview exists. Presentation changed; nothing here may reach a protected lesson.
 */
test("the public preview is requested on demand and only when one is published", async ({ page }) => {
  let previewRequests = 0;
  await serveCourse(page, "en");
  await page.route(`**/api/v1/media/courses/${COURSE_ID}/preview`, (route) => {
    previewRequests += 1;
    return route.fulfill({
      json: { url: "https://signed.example/approved-preview.mp4", expires_at: "2026-08-27T00:00:00Z" },
    });
  });

  await openCourse(page, "en");
  // Nothing is minted for a visitor who merely opened the page.
  expect(previewRequests).toBe(0);

  await page.getByTestId("watch-public-preview").click();
  await expect(page.getByTestId("public-preview-player")).toHaveAttribute(
    "src",
    "https://signed.example/approved-preview.mp4",
  );
  expect(previewRequests).toBe(1);
});

test("a course with no published preview offers no preview control", async ({ page }) => {
  await page.route("**/api/v1/catalog/courses/**", (route) =>
    route.fulfill({ json: { ...courseFixture("en"), has_preview: false } }),
  );
  await page.route("**/api/v1/me/**", (route) => route.fulfill({ status: 401, json: UNAUTHORIZED }));

  await openCourse(page, "en");
  await expect(page.getByTestId("course-preview")).toHaveCount(0);
  await expect(page.getByTestId("watch-public-preview")).toHaveCount(0);
});

test("a failed preview is recoverable and never exposes the media error", async ({ page }) => {
  await serveCourse(page, "en");
  await page.route(`**/api/v1/media/courses/${COURSE_ID}/preview`, (route) =>
    route.fulfill({ status: 403, json: { ...NOT_FOUND, status: 403, code: "FORBIDDEN" } }),
  );

  await openCourse(page, "en");
  await page.getByTestId("watch-public-preview").click();
  await expect(page.getByTestId("public-preview-error")).toContainText(
    "The preview could not be played. Try again.",
  );
  await expect(page.getByTestId("public-preview-player")).toHaveCount(0);
  // The course itself is unaffected, and the reader can try again.
  await expect(page.getByTestId("course-detail-title")).toBeVisible();
  await expect(page.getByTestId("public-preview-error").getByRole("button")).toBeVisible();
});

/**
 * An entitled Student is offered their Course, not an acquisition prompt.
 */
test("an entitled student is offered the course rather than a way to request it", async ({ page }) => {
  await serveCourse(page, "en", { access: ENTITLED });
  await openCourse(page, "en");

  const panel = page.getByTestId("course-access-panel");
  await expect(panel).toHaveAttribute("data-access-relationship", "ACTIVE");
  await expect(page.getByTestId("course-access-go-to-course")).toHaveAttribute(
    "href",
    `/en/learn/courses/${COURSE_ID}`,
  );
  await expect(page.getByTestId("purchase-request-open")).toHaveCount(0);
  await expect(page.getByTestId("course-access-sign-in")).toHaveCount(0);
});

test("an anonymous visitor is told how access works and offered the real request path", async ({ page }) => {
  await serveCourse(page, "en");
  await openCourse(page, "en");

  const panel = page.getByTestId("course-access-panel");
  await expect(panel).toHaveAttribute("data-access-relationship", "ANONYMOUS");
  await expect(page.getByTestId("course-access-how")).toContainText("An administrator invites you");
  await expect(page.getByTestId("course-access-sign-in")).toBeVisible();
  await expect(page.getByTestId("purchase-request-open")).toBeVisible();
  await expect(page.getByTestId("course-access-go-to-course")).toHaveCount(0);
});

/**
 * The two outcomes the contract permits, told apart.
 *
 * The public catalogue answers 404 identically for a course that is unpublished, suspended, retired
 * or absent, so the page states availability without claiming to know which — and never shows a
 * student the lifecycle vocabulary the Admin workspace uses.
 */
test("an unavailable course is stated plainly, with a way back to the catalogue", async ({ page }) => {
  await page.route("**/api/v1/catalog/courses/**", (route) =>
    route.fulfill({ status: 404, json: NOT_FOUND }),
  );
  await page.route("**/api/v1/me/**", (route) => route.fulfill({ status: 401, json: UNAUTHORIZED }));

  await page.goto("/en/catalog/course-unknown");
  const unavailable = page.getByTestId("course-detail-unavailable");
  await expect(unavailable).toContainText(localized.en.unavailable);
  await expect(unavailable.getByRole("link", { name: "Back to courses" })).toHaveAttribute(
    "href",
    "/en/catalog",
  );

  // No lifecycle terminology, and no raw problem document.
  const rendered = (await page.locator("main").innerText()).toLowerCase();
  for (const leaked of ["published", "retired", "suspended", "lifecycle", "404", "not_found"]) {
    expect(rendered).not.toContain(leaked);
  }
});

test("a transport failure is offered a retry and succeeds on the second attempt", async ({ page }) => {
  let attempts = 0;
  await page.route("**/api/v1/catalog/courses/**", (route) => {
    attempts += 1;
    return attempts === 1
      ? route.fulfill({ status: 500, json: { status: 500 } })
      : route.fulfill({ json: courseFixture("en") });
  });
  await page.route("**/api/v1/me/**", (route) => route.fulfill({ status: 401, json: UNAUTHORIZED }));

  await page.goto(`/en/catalog/${SLUG}`);
  const failed = page.getByTestId("course-detail-failed");
  await expect(failed).toContainText(localized.en.loadFailed);
  // A failure is not a missing course.
  await expect(page.getByTestId("course-detail-unavailable")).toHaveCount(0);

  await failed.getByRole("button", { name: "Retry" }).click();
  await expect(page.getByTestId("course-detail-title")).toBeVisible();
});

/**
 * Tranche C's personalisation survives the visit.
 *
 * A visitor who arrived from a catalogue narrowed to their university and program returns to that
 * same narrowed catalogue — never to a generic list they then have to re-narrow — and no identifier
 * is ever put in front of them.
 */
test("the academic context survives catalogue, course and the way back", async ({ page }) => {
  await page.route("**/api/v1/catalog/courses/**", (route) =>
    route.fulfill({ json: courseFixture("en") }),
  );
  await page.route("**/api/v1/catalog/courses*", (route) =>
    route.fulfill({ json: { items: [courseFixture("en")], page: 1, page_size: 20, total: 1 } }),
  );
  await page.route("**/api/v1/me/**", (route) => route.fulfill({ status: 401, json: UNAUTHORIZED }));
  await page.route("**/api/v1/catalog/academic-options/**", (route) =>
    route.fulfill({
      json: {
        items: [
          { slug: "kuwait-university", name_ar: "جامعة الكويت", name_en: "Kuwait University" },
          { slug: "computer-science", name_ar: "علوم الحاسوب", name_en: "Computer Science" },
        ],
      },
    }),
  );

  await page.goto("/en/catalog?institution=kuwait-university&program=computer-science");
  await expect(page.getByTestId("catalogue-academic-context")).toBeVisible();

  await page.getByRole("link", { name: localized.en.title }).first().click();
  await expect(page.getByTestId("course-detail-title")).toBeVisible();

  // The way back up is the breadcrumb now, and it carries the same academic
  // context the standalone back link did: stepping up returns to the filtered
  // catalogue the visitor was browsing, not to an unfiltered one.
  const back = page
    .getByTestId("breadcrumbs")
    .getByRole("link", { name: en.nav.courses, exact: true });
  await expect(back).toHaveAttribute(
    "href",
    "/en/catalog?institution=kuwait-university&program=computer-science",
  );
  await back.click();
  await expect(page.getByTestId("catalogue-academic-context")).toBeVisible();
});

test("no identifier is ever shown to a visitor on Course Details", async ({ page }) => {
  await serveCourse(page, "en");
  await openCourse(page, "en");

  const rendered = await page.locator("main").innerText();
  expect(rendered).not.toMatch(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i);
  expect(await page.locator("main").ariaSnapshot()).not.toMatch(
    /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i,
  );
});

/**
 * WCAG 2.1 AA over the page's real states, in both languages.
 */
for (const locale of ["en", "ar"] as const) {
  test(`${locale} Course Details has no accessibility violations`, async ({ page }) => {
    await serveCourse(page, locale);
    await openCourse(page, locale);
    // Expanded too: the disclosed rows are part of the page under audit.
    await page.getByTestId("course-curriculum-disclosure").click();
    await expect(page.getByTestId("course-curriculum-section")).toHaveCount(11);

    const results = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
      .analyze();
    const detail = results.violations
      .flatMap((violation) =>
        violation.nodes.map((node) => `${violation.id}: ${node.html} — ${node.failureSummary ?? ""}`),
      )
      .join("\n");
    expect(results.violations.map((violation) => violation.id), detail).toEqual([]);
  });
}

test("the unavailable state has no accessibility violations", async ({ page }) => {
  await page.route("**/api/v1/catalog/courses/**", (route) =>
    route.fulfill({ status: 404, json: NOT_FOUND }),
  );
  await page.route("**/api/v1/me/**", (route) => route.fulfill({ status: 401, json: UNAUTHORIZED }));
  await page.goto("/en/catalog/course-unknown");
  await expect(page.getByTestId("course-detail-unavailable")).toBeVisible();

  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();
  expect(
    results.violations.map((violation) => violation.id),
    results.violations.map((v) => `${v.id}: ${v.nodes[0]?.failureSummary ?? ""}`).join("\n"),
  ).toEqual([]);
});

/**
 * The purchase journey, from the CTA to the handoff.
 *
 * Three properties are under test and each was a defect before: an anonymous
 * visitor cannot create a purchase request at all; a signed-in Student is shown
 * what they are about to request before anything exists; and WhatsApp opens
 * only after the explicit confirmation, never as a side effect of pressing
 * "Buy this course".
 */

/** Fails the test if anything creates a request or leaves for WhatsApp. */
function watchForPrematurePurchase(page: Page): { assertQuiet(): void } {
  const violations: string[] = [];
  page.on("request", (request) => {
    const url = request.url();
    if (url.startsWith("https://wa.me/")) violations.push("navigated to WhatsApp");
    if (request.method() === "POST" && new URL(url).pathname.endsWith("/purchase-requests")) {
      violations.push(`created a purchase request via ${new URL(url).pathname}`);
    }
  });
  return {
    assertQuiet() {
      expect(violations, "the purchase journey acted before the Student confirmed").toEqual([]);
    },
  };
}

test("an anonymous visitor is sent to sign in, and creates nothing on the way", async ({ page }) => {
  await serveCourse(page, "en");
  await openCourse(page, "en");

  const watch = watchForPrematurePurchase(page);
  await page.getByTestId("purchase-request-open").click();

  // No email field, no request, no handoff — only the two ways to become the
  // Student a purchase request can belong to.
  await expect(page.getByTestId("purchase-sign-in-required")).toBeVisible();
  await expect(page.locator('input[type="email"]')).toHaveCount(0);
  const signIn = page.getByTestId("purchase-sign-in");
  const register = page.getByTestId("purchase-create-account");
  await expect(signIn).toBeVisible();
  await expect(register).toBeVisible();
  watch.assertQuiet();

  // Both carry this Course and the intent to buy it, so the journey can come
  // straight back to the confirmation rather than to a button already pressed.
  for (const control of [signIn, register]) {
    const href = await control.getAttribute("href");
    expect(href).toBeTruthy();
    const returnTo = new URL(href!, "https://gradex.test").searchParams.get("returnTo");
    expect(returnTo).toBeTruthy();
    const destination = new URL(returnTo!, "https://gradex.test");
    expect(destination.pathname).toBe(`/en/catalog/${SLUG}`);
    expect(destination.searchParams.get("purchase")).toBe("1");
  }
  expect((await signIn.getAttribute("href"))!.startsWith("/login")).toBe(true);
  expect((await register.getAttribute("href"))!.startsWith("/register")).toBe(true);
});

test("a signed-in Student sees what is being requested before anything is requested", async ({
  page,
}) => {
  await serveCourse(page, "en", { access: SIGNED_IN_NO_ACCESS });
  await openCourse(page, "en");

  const watch = watchForPrematurePurchase(page);
  await page.getByTestId("purchase-request-open").click();

  const confirmation = page.getByTestId("purchase-confirmation");
  await expect(confirmation).toBeVisible();
  // Both values come from the Course as the server described it. Neither is
  // typed by the browser, and neither is sent back on confirm.
  await expect(page.getByTestId("purchase-course-title")).toContainText(localized.en.title);
  await expect(page.getByTestId("purchase-price")).toContainText("12.500");
  await expect(confirmation).toContainText(en.access.purchase.intro);
  await expect(page.locator('input[type="email"]')).toHaveCount(0);
  watch.assertQuiet();

  // Cancelling leaves nothing behind and reopens nothing on reload.
  await page.getByTestId("purchase-request-cancel").click();
  await expect(page.getByTestId("purchase-confirmation")).toHaveCount(0);
  watch.assertQuiet();
});

test("WhatsApp opens only after the explicit confirmation, with a server-built handoff", async ({
  page,
}) => {
  await serveCourse(page, "en", { access: SIGNED_IN_NO_ACCESS });
  // The external handoff is intercepted so CI sends no WhatsApp message.
  await page.route("https://wa.me/**", (route) => route.abort());

  let submitted: unknown;
  await page.route("**/api/v1/me/purchase-requests", async (route) => {
    if (route.request().method() !== "POST") {
      await route.continue();
      return;
    }
    submitted = route.request().postDataJSON();
    await route.fulfill({
      status: 201,
      json: {
        reference: "GRX-ABCDEF0123456789",
        whatsapp_url: "https://wa.me/15550000000?text=Hello",
        course_title: localized.en.title,
        price_minor_units: 12500,
        currency: "KWD",
        state: "WAITING_PAYMENT",
        reused: false,
      },
    });
  });

  await openCourse(page, "en");
  const watch = watchForPrematurePurchase(page);
  await page.getByTestId("purchase-request-open").click();
  await expect(page.getByTestId("purchase-confirmation")).toBeVisible();
  watch.assertQuiet();

  const handoff = page.waitForRequest(
    (request) => request.isNavigationRequest() && request.url().startsWith("https://wa.me/"),
  );
  await page.getByTestId("purchase-request-submit").click({ noWaitAfter: true });
  const handoffRequest = await handoff;
  expect(handoffRequest.url()).toBe("https://wa.me/15550000000?text=Hello");

  // The body carries the Course and nothing else. An address here would decide
  // where Course access is eventually sent, and a price here would be a figure
  // the browser chose.
  expect(submitted).toEqual({ course_id: COURSE_ID });
});

test("the purchase intent survives the auth journey and reopens the confirmation", async ({
  page,
}) => {
  await serveCourse(page, "en", { access: SIGNED_IN_NO_ACCESS });
  // This is the state a Student is in on returning from sign-in or
  // verification: the destination they were sent back to carries the intent.
  await page.goto(`/en/catalog/${SLUG}?purchase=1`);
  await expect(page.getByTestId("purchase-confirmation")).toBeVisible();
  await expect(page.getByTestId("purchase-course-title")).toContainText(localized.en.title);
});

test("an entitled Student is never offered a purchase, intent flag or not", async ({ page }) => {
  await serveCourse(page, "en", { access: ENTITLED });
  await page.goto(`/en/catalog/${SLUG}?purchase=1`);
  await expect(page.getByTestId("course-access-go-to-course")).toBeVisible();
  await expect(page.getByTestId("purchase-confirmation")).toHaveCount(0);
  await expect(page.getByTestId("purchase-request-open")).toHaveCount(0);
  await expect(page.getByTestId("purchase-sign-in-required")).toHaveCount(0);
});

/**
 * Navigation: where this page sits, and the ways out of it.
 */
for (const locale of ["en", "ar"] as const) {
  test(`Course Details says where it sits and offers a route up (${locale})`, async ({ page }) => {
    await serveCourse(page, locale);
    await openCourse(page, locale);

    const dictionary = locale === "ar" ? ar : en;
    const crumbs = page.getByTestId("breadcrumbs");
    await expect(crumbs).toBeVisible();
    const links = crumbs.getByRole("link");
    await expect(links).toHaveCount(2);
    await expect(links.nth(0)).toHaveAttribute("href", "/");
    await expect(links.nth(0)).toHaveText(dictionary.nav.home);
    expect(await links.nth(1).getAttribute("href")).toContain(`/${locale}/catalog`);
    // The page you are on is announced, never a link to itself.
    const current = crumbs.locator('[aria-current="page"]');
    await expect(current).toHaveText(localized[locale].title);
    await expect(crumbs.getByRole("link", { name: localized[locale].title })).toHaveCount(0);

    // And the header offers the same two destinations without the URL bar.
    const header = page.getByRole("navigation", { name: dictionary.nav.primaryNavigation }).first();
    await expect(
      header.getByRole("link", { name: dictionary.nav.home, exact: true }),
    ).toHaveAttribute("href", "/");
    await expect(
      header.getByRole("link", { name: dictionary.nav.courses, exact: true }),
    ).toHaveAttribute("href", `/${locale}/catalog`);
    // My Learning belongs to a Student session, and this visitor has none.
    await expect(
      header.getByRole("link", { name: dictionary.nav.myLearning, exact: true }),
    ).toHaveCount(0);
  });
}

test("a Student session is offered My Learning in the header", async ({ page }) => {
  await serveCourse(page, "en", { access: SIGNED_IN_NO_ACCESS });
  await page.route("**/api/v1/session", (route) =>
    route.fulfill({
      json: {
        status: "AUTHENTICATED",
        role: "STUDENT",
        display_name: "Test Student",
        csrf_token: "test-csrf",
        idle_expires_at: "2027-01-01T00:00:00Z",
        absolute_expires_at: "2027-01-01T00:00:00Z",
      },
    }),
  );
  await openCourse(page, "en");
  const header = page.getByRole("navigation", { name: en.nav.primaryNavigation }).first();
  await expect(
    header.getByRole("link", { name: en.nav.myLearning, exact: true }),
  ).toHaveAttribute("href", "/en/learn/dashboard");
});

test("the mobile sheet offers the same destinations as the wide header", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await serveCourse(page, "en");
  await openCourse(page, "en");

  // Below `lg` the sheet is the only primary navigation there is.
  await page.getByRole("button", { name: en.meta.openMenu }).click();
  // `exact`, because the account controls in the same sheet carry a
  // "Browse courses" button whose accessible name contains "Courses".
  const sheet = page.getByRole("dialog");
  await expect(sheet.getByRole("link", { name: en.nav.home, exact: true })).toHaveAttribute(
    "href",
    "/",
  );
  await expect(sheet.getByRole("link", { name: en.nav.courses, exact: true })).toHaveAttribute(
    "href",
    "/en/catalog",
  );
});
