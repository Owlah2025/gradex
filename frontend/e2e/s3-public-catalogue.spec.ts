import { expect, test, type Page } from "@playwright/test";

const course = {
  id: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
  slug: "course-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  title: "Course title",
  instructor_display_name: "Instructor display name",
  major: { label: "Retired major", code: "RET-1" },
  subject: { label: "Subject label", code: "SUB-1" },
  study_year: { label: "Year 4" },
  price: { minor_units: 12500, currency: "KWD" },
  has_preview: true,
};

const detail = {
  ...course,
  description: "Instructor-authored course description.",
  sections: [
    { title: "First section", position: 1, lesson_count: 3 },
    { title: "Second section", position: 2, lesson_count: 2 },
  ],
};

const localized = {
  ar: {
    catalogue: "الكتالوج",
    title: "عنوان الدورة",
    description: "وصف الدورة الذي كتبه المدرس.",
    instructor: "اسم المدرس الظاهر",
    outline: "محتوى الدورة",
    loading: "جارٍ تحميل الدورات…",
    empty: "لا توجد دورات منشورة الآن.",
    unavailable: "هذه الدورة غير متاحة.",
    failed: "تعذر تحميل الكتالوج. حاول مرة أخرى.",
    language: "التبديل إلى الإنجليزية",
  },
  en: {
    catalogue: "Catalogue",
    title: "Course title",
    description: "Instructor-authored course description.",
    instructor: "Instructor display name",
    outline: "Course outline",
    loading: "Loading courses…",
    empty: "No published courses are available yet.",
    unavailable: "This course is unavailable.",
    failed: "The catalogue could not be loaded. Try again.",
    language: "Switch to Arabic",
  },
} as const;

const viewports = [
  ["phone", { width: 390, height: 844 }],
  ["tablet", { width: 768, height: 1024 }],
  ["desktop", { width: 1440, height: 1000 }],
] as const;

const forbiddenCommerce = [
  "checkout",
  "cart",
  "coupon",
  "buy now",
  "payment",
  "purchase",
  "الدفع",
  "السلة",
  "قسيمة",
  "اشتر",
  "شراء",
];

function localizedCourse(locale: "ar" | "en") {
  return {
    ...course,
    title: localized[locale].title,
    instructor_display_name: localized[locale].instructor,
    major: { label: locale === "ar" ? "تخصص متقاعد" : "Retired major", code: "RET-1" },
    subject: { label: locale === "ar" ? "مادة دراسية" : "Subject label", code: "SUB-1" },
    study_year: { label: locale === "ar" ? "السنة الرابعة" : "Year 4" },
  };
}

function localizedDetail(locale: "ar" | "en") {
  return {
    ...localizedCourse(locale),
    description: localized[locale].description,
    sections: locale === "ar"
      ? [
          { title: "القسم الأول", position: 1, lesson_count: 3 },
          { title: "القسم الثاني", position: 2, lesson_count: 2 },
        ]
      : detail.sections,
    // Deliberately unsupported server fields must never enter the rendered UI.
    section_price: 500,
    owner_email: "private@example.test",
  };
}

async function mockPublicCatalogue(page: Page, locale: "ar" | "en") {
  const list = { items: [localizedCourse(locale)], page: 1, page_size: 20, total: 1 };
  const courseDetail = localizedDetail(locale);
  await page.route("**/api/v1/catalog/courses", (route) => route.fulfill({ json: list }));
  await page.route("**/api/v1/catalog/courses/**", (route) => route.fulfill({ json: courseDetail }));
}

async function expectNoCommerceOrSectionPrice(page: Page) {
  const renderedText = await page.locator("main").innerText();
  const accessibilityTree = await page.locator("main").ariaSnapshot();

  for (const term of forbiddenCommerce) {
    expect(renderedText.toLowerCase()).not.toContain(term);
    expect(accessibilityTree.toLowerCase()).not.toContain(term);
  }
  expect(renderedText.toLowerCase()).not.toContain("section price");
  expect(renderedText).not.toContain("سعر القسم");
  await expect(page.getByRole("button", { name: /checkout|cart|coupon|buy now|payment|purchase|الدفع|السلة|قسيمة|اشتر|شراء/i })).toHaveCount(0);
  await expect(page.getByRole("link", { name: /checkout|cart|coupon|buy now|payment|purchase|الدفع|السلة|قسيمة|اشتر|شراء/i })).toHaveCount(0);
  await expect(page.locator("form")).toHaveCount(0);
}

async function expectVisibleKeyboardFocus(page: Page) {
  await page.keyboard.press("Tab");
  await expect(page.locator(":focus")).toBeVisible();
}

for (const locale of ["ar", "en"] as const) {
  for (const [viewportName, viewport] of viewports) {
    test(`${locale} ${viewportName} catalogue list is responsive, accessible, and commerce-free`, async ({ page }) => {
      await page.setViewportSize(viewport);
      await mockPublicCatalogue(page, locale);
      await page.goto(`/${locale}/catalog`);

      await expect(page.locator("html")).toHaveAttribute("dir", locale === "ar" ? "rtl" : "ltr");
      await expect(page.getByRole("main")).toBeVisible();
      await expect(page.getByRole("heading", { name: localized[locale].catalogue, level: 1 })).toBeVisible();
      await expect(page.getByRole("link", { name: localized[locale].title })).toBeVisible();
      await expect(page.getByRole("button", { name: localized[locale].language })).toBeVisible();
      await expect(page.getByText(locale === "ar" ? /تخصص متقاعد/ : /Retired major/)).toBeVisible();
      await expectNoCommerceOrSectionPrice(page);
      await expectVisibleKeyboardFocus(page);
      await expect(page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).resolves.toBe(true);
    });

    test(`${locale} ${viewportName} catalogue detail is responsive, accessible, and commerce-free`, async ({ page }) => {
      await page.setViewportSize(viewport);
      await mockPublicCatalogue(page, locale);
      await page.goto(`/${locale}/catalog/${course.slug}`);

      await expect(page.locator("html")).toHaveAttribute("dir", locale === "ar" ? "rtl" : "ltr");
      await expect(page.getByRole("main")).toBeVisible();
      await expect(page.getByRole("heading", { name: localized[locale].title, level: 1 })).toBeVisible();
      await expect(page.getByRole("heading", { name: localized[locale].outline, level: 2 })).toBeVisible();
      await expect(page.getByText(localized[locale].description, { exact: true })).toBeVisible();
      await expect(page.getByText("private@example.test", { exact: true })).toHaveCount(0);
      await expectNoCommerceOrSectionPrice(page);
      await expectVisibleKeyboardFocus(page);
      await expect(page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).resolves.toBe(true);
    });
  }
}

test("catalogue shows loading and follows a stable slug", async ({ page }) => {
  await page.route("**/api/v1/catalog/courses", async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 150));
    await route.fulfill({ json: { items: [localizedCourse("en")], page: 1, page_size: 20, total: 1 } });
  });
  await page.route("**/api/v1/catalog/courses/**", (route) => route.fulfill({ json: localizedDetail("en") }));
  await page.goto("/en/catalog");
  await expect(page.getByText(localized.en.loading, { exact: true })).toBeVisible();
  await page.getByRole("link", { name: localized.en.title }).click();
  await expect(page).toHaveURL(new RegExp(`/en/catalog/${course.slug}$`));
  await expect(page.getByRole("heading", { name: localized.en.title, level: 1 })).toBeVisible();
});

test("catalogue language choice persists on its language-addressed route", async ({ page }) => {
  await mockPublicCatalogue(page, "ar");
  await page.goto("/ar/catalog");
  await expect(page.getByRole("button", { name: localized.ar.language })).toBeVisible();
  await page.getByRole("button", { name: localized.ar.language }).click();
  await expect(page).toHaveURL(/\/en\/catalog$/);
  await expect(page.locator("html")).toHaveAttribute("dir", "ltr");
  await expect(page.evaluate(() => localStorage.getItem("gradex.locale"))).resolves.toBe("en");
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("dir", "ltr");
});

test("catalogue maps a public not-found problem to the anonymous state", async ({ page }) => {
  await page.route("**/api/v1/catalog/courses/**", (route) => route.fulfill({
    status: 404,
    json: {
      type: "https://api.gradex.com/problems/not-found",
      title: "Not found",
      status: 404,
      code: "NOT_FOUND",
    },
  }));
  await page.goto("/en/catalog/course-unknown");
  await expect(page.getByText(localized.en.unavailable, { exact: true })).toBeVisible();
});

test("catalogue renders an empty published collection", async ({ page }) => {
  await page.route("**/api/v1/catalog/courses", (route) => route.fulfill({ json: { items: [], page: 1, page_size: 20, total: 0 } }));
  await page.goto("/en/catalog");
  await expect(page.getByText(localized.en.empty, { exact: true })).toBeVisible();
});

test("catalogue renders a safe generic failure", async ({ page }) => {
  await page.route("**/api/v1/catalog/courses", (route) => route.fulfill({ status: 500, json: { status: 500 } }));
  await page.goto("/en/catalog");
  await expect(page.getByText(localized.en.failed, { exact: true })).toBeVisible();
});
