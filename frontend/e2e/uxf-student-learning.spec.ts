import fs from "fs";
import path from "path";
import AxeBuilder from "@axe-core/playwright";
import { expect, test, type BrowserContext, type Page } from "@playwright/test";

/**
 * UX-F — the enrolled Student's journey, in a real browser against the real stack.
 *
 * Everything here is driven the way a Student drives it: controls are clicked rather than URLs
 * constructed, and every destination is whatever the product put in the link. Nothing is seeded for
 * the sake of the test — the Course, its ordering, its progress and its access window are the
 * harness's own fixtures, so an assertion that passes here is an assertion about the product.
 *
 * The one thing this suite cannot exercise against real data is a Course long enough to need the
 * curriculum's disclosure threshold: the seeded graph is three Lessons. That behaviour is pinned
 * down in `curriculum-model.test.ts` instead, and is recorded as remaining coverage debt.
 */

const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const LESSON_ONE = "30000000-0000-0000-0000-000000000001";
const LESSON_TWO = "30000000-0000-0000-0000-000000000002";
const LESSON_THREE = "30000000-0000-0000-0000-000000000003";

/** Every identifier the Student's own screens are built from, and none of which they may read. */
const IDENTIFIERS = [COURSE_ID, LESSON_ONE, LESSON_TWO, LESSON_THREE];

const TEXT = {
  en: {
    courseTitle: "CS101: Introduction to Programming",
    sectionOne: "Section 1: Basics",
    sectionTwo: "Section 2: Advanced Topics",
    lessonOne: "Lesson 1: Introduction",
    lessonTwo: "Lesson 2: Variables",
    lessonThree: "Lesson 3: Functions",
    myCourses: "My courses",
    openCourse: "Open course",
    activeDetail: "You can open every lesson.",
    previous: "Previous lesson",
    next: "Next lesson",
    firstLesson: "First lesson",
    lastLesson: "Last lesson",
    courseContents: "Course contents",
    completed: "Completed",
    percent: "33%",
    completedOverTotal: "1/3",
  },
  ar: {
    courseTitle: "مقدمة في البرمجة CS101",
    sectionOne: "القسم الأول: الأساسيات",
    sectionTwo: "القسم الثاني: البرمجة المتقدمة",
    lessonOne: "الدرس الأول: مرحباً بك",
    lessonTwo: "الدرس الثاني: المتغيرات",
    lessonThree: "الدرس الثالث: الدوال",
    myCourses: "مقرراتي",
    openCourse: "فتح المقرر",
    activeDetail: "يمكنك فتح كل الدروس.",
    previous: "الدرس السابق",
    next: "الدرس التالي",
    firstLesson: "هذا أول درس",
    lastLesson: "هذا آخر درس",
    courseContents: "محتويات المقرر",
    completed: "مكتمل",
    percent: "٣٣٪",
    completedOverTotal: "١/٣",
  },
} as const;

type Locale = keyof typeof TEXT;

test.describe.configure({ timeout: 120_000 });

async function authenticateStudent(context: BrowserContext, email: string) {
  const page = await context.newPage();
  await page.goto("/en/catalog");
  const result = await page.evaluate(async (studentEmail) => {
    const bootstrap = await fetch("/api/v1/session/bootstrap", { method: "GET", credentials: "include" });
    const { csrf_token } = await bootstrap.json();
    const login = await fetch("/api/v1/sessions", {
      method: "POST",
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
        Accept: "application/json",
        "X-CSRF-Token": csrf_token,
      },
      body: JSON.stringify({ email: studentEmail, password: "StudentPassword123!" }),
    });
    return login.status;
  }, email);
  expect(result).toBe(201);
  const cookies = await context.cookies();
  await page.close();
  return cookies;
}

/** The text a Student actually reads, with the markup and the scripts left out. */
function visibleText(page: Page): Promise<string> {
  return page.evaluate(() => document.body.innerText);
}

async function expectNoHorizontalOverflow(page: Page) {
  const overflowing = await page.evaluate(
    () => document.documentElement.scrollWidth > window.innerWidth,
  );
  expect(overflowing, "the document must not scroll sideways").toBe(false);
}

async function expectAxeClean(page: Page, label: string) {
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();
  expect(results.violations.map((v) => `${label}: ${v.id} (${v.nodes.length})`)).toEqual([]);
}

let activeCookies: Awaited<ReturnType<typeof authenticateStudent>>;

test.beforeAll(async ({ browser }) => {
  const context = await browser.newContext();
  activeCookies = await authenticateStudent(context, "student-active@example.test");
  await context.close();
});

test.beforeEach(async ({ context }) => {
  await context.addCookies(activeCookies);
});

/* ------------------------------------------------------------------ the journey */

for (const locale of ["en", "ar"] as const) {
  const t = TEXT[locale];

  test(`${locale}: a Student reaches their Course, moves through it, and comes back to where they were`, async ({
    page,
  }) => {
    // --- the Courses they actually have access to -------------------------
    await page.goto(`/${locale}/learn/dashboard`);
    await expect(page.locator("main")).toHaveAttribute("dir", locale === "ar" ? "rtl" : "ltr");
    const card = page.locator("main article").filter({ hasText: t.courseTitle });
    await expect(card).toHaveCount(1);

    // The access state is described, not merely tinted.
    await expect(card.getByText(t.activeDetail)).toBeVisible();

    // The progress figure is the server's, and the same one the Course itself will show.
    await expect(card.getByText(t.percent, { exact: false })).toBeVisible();
    await expect(card.getByText(t.completedOverTotal, { exact: false })).toBeVisible();
    const dashboardProgress = await card.getByRole("progressbar").getAttribute("aria-valuenow");

    // --- nothing technical is on the screen -------------------------------
    for (const identifier of IDENTIFIERS) {
      expect(await visibleText(page), "a Student must never have to read an identifier").not.toContain(
        identifier,
      );
    }

    // --- into the Course, through its own control -------------------------
    await card.getByRole("link", { name: t.openCourse }).click();
    await page.waitForURL(`**/${locale}/learn/courses/${COURSE_ID}`);
    await expect(page.getByRole("heading", { name: t.courseTitle, level: 1 })).toBeVisible();

    // The Course states the same progress as the Dashboard did.
    expect(await page.getByRole("progressbar").first().getAttribute("aria-valuenow")).toBe(
      dashboardProgress,
    );

    // Authored order, and a section that says how much of itself is done.
    const sections = page.locator("main h2");
    await expect(sections.nth(0)).toContainText(t.sectionOne);
    await expect(sections.nth(1)).toContainText(t.sectionTwo);
    for (const identifier of IDENTIFIERS) {
      expect(await visibleText(page)).not.toContain(identifier);
    }

    // --- into a Lesson ----------------------------------------------------
    await page.getByRole("link", { name: t.lessonOne }).first().click();
    await page.waitForURL(`**/lessons/${LESSON_ONE}`);
    await expect(page.getByRole("heading", { name: t.lessonOne, level: 1 })).toBeVisible();

    // The Lesson knows which Course it is in, and says so where the Student can act on it.
    await expect(page.getByRole("link", { name: t.courseTitle })).toBeVisible();

    // Where the Student is standing is marked in the contents, semantically.
    const current = page.locator('[aria-current="page"]');
    await expect(current).toHaveCount(1);
    await expect(current).toContainText(t.lessonOne);

    // The first Lesson offers no previous action — it says so instead.
    await expect(page.getByText(t.firstLesson)).toBeVisible();
    await expect(page.getByRole("link", { name: t.previous })).toHaveCount(0);

    // Next names its destination, so two navigation links are never announced alike.
    const next = page.getByRole("link", { name: `${t.next}: ${t.lessonTwo}` });
    await expect(next).toBeVisible();

    // --- forward, across a section boundary -------------------------------
    await next.click();
    await page.waitForURL(`**/lessons/${LESSON_TWO}`);
    await expect(page.getByRole("heading", { name: t.lessonTwo, level: 1 })).toBeVisible();
    // Lesson 2 closes Section 1; the server's next pointer must cross into Section 2.
    const acrossTheBoundary = page.getByRole("link", { name: `${t.next}: ${t.lessonThree}` });
    await expect(acrossTheBoundary).toBeVisible();
    await acrossTheBoundary.click();
    await page.waitForURL(`**/lessons/${LESSON_THREE}`);
    await expect(page.getByRole("heading", { name: t.lessonThree, level: 1 })).toBeVisible();
    // The section above the title is the *new* section, not the one navigated out of.
    await expect(page.locator("main").getByText(t.sectionTwo).first()).toBeVisible();

    // The last Lesson offers no next action.
    await expect(page.getByText(t.lastLesson)).toBeVisible();
    await expect(page.getByRole("link", { name: t.next })).toHaveCount(0);

    // --- backward, across the same boundary -------------------------------
    await page.getByRole("link", { name: `${t.previous}: ${t.lessonTwo}` }).click();
    await page.waitForURL(`**/lessons/${LESSON_TWO}`);
    await expect(page.getByRole("heading", { name: t.lessonTwo, level: 1 })).toBeVisible();

    // --- back out, and back in --------------------------------------------
    await page.getByRole("link", { name: t.myCourses }).first().click();
    await page.waitForURL(`**/${locale}/learn/dashboard`);
    // The figure survives leaving and returning because it was never the page's own.
    const returned = page.locator("main article").filter({ hasText: t.courseTitle });
    expect(await returned.getByRole("progressbar").getAttribute("aria-valuenow")).toBe(
      dashboardProgress,
    );
  });

  test(`${locale}: a completed Lesson is marked complete everywhere it appears`, async ({ page }) => {
    // Lesson 2 is the seeded completed Lesson. The Course contents and the Lesson's own header must
    // agree, because both read the same server flag rather than deciding for themselves.
    await page.goto(`/${locale}/learn/courses/${COURSE_ID}/lessons/${LESSON_TWO}`);
    await expect(page.getByTestId("lesson-state")).toHaveText(t.completed);

    await page.goto(`/${locale}/learn/courses/${COURSE_ID}`);
    const row = page.getByRole("link", { name: t.lessonTwo }).first();
    await expect(row).toContainText(t.completed);
  });

  test(`${locale}: a resource is offered by name and authorised only when it is asked for`, async ({
    page,
  }) => {
    const requests: string[] = [];
    page.on("request", (request) => requests.push(request.url()));

    await page.goto(`/${locale}/learn/courses/${COURSE_ID}/lessons/${LESSON_ONE}`);
    const download = page.getByRole("button", { name: /Lecture Notes PDF|ملاحظات المحاضرة/ });
    await expect(download.first()).toBeVisible();

    // Rendering a resource must never mint a download capability for it.
    expect(requests.filter((url) => url.includes("/materials/"))).toEqual([]);

    // The row describes the file, and names no storage object.
    const text = await visibleText(page);
    expect(text).not.toContain("storage_object_key");
    expect(text).not.toContain(".m3u8");
    for (const identifier of IDENTIFIERS) {
      expect(text).not.toContain(identifier);
    }
  });
}

/* ------------------------------------------------------------------ requests */

test("the learning routes issue one media authorization and no duplicate reads", async ({ page }) => {
  const calls: string[] = [];
  page.on("request", (request) => {
    const url = request.url();
    if (!url.includes("/api/v1/")) return;
    calls.push(`${request.method()} ${url.split("/api/v1")[1].split("?")[0]}`);
  });

  const count = (predicate: (call: string) => boolean) => calls.filter(predicate).length;

  const profileReads = () =>
    count((call) => call === "GET /me/academic-profile");

  /**
   * The Dashboard asks the account for its academic profile exactly once.
   *
   * Two things on this page want to know it — whether an account's profile outranks a browsing
   * preference, and whether to invite a Student who has never answered — and they are one question
   * about one principal-scoped resource. The application holds the answer above the page, so the
   * second consumer reads it rather than asking again.
   */
  await page.goto(`/en/learn/dashboard`);
  await expect(page.locator("main")).toBeVisible();
  await page.waitForTimeout(1500);
  expect(profileReads(), calls.join("\n")).toBe(1);

  calls.length = 0;
  // The Course page reads nothing about media at all.
  await page.goto(`/en/learn/courses/${COURSE_ID}`);
  await expect(page.locator("main")).toBeVisible();
  await page.waitForTimeout(1500);
  expect(count((call) => call.includes("/playback"))).toBe(0);
  expect(count((call) => call.includes("/materials/"))).toBe(0);

  calls.length = 0;
  await page.goto(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ONE}`);
  await expect(page.locator("main")).toBeVisible();
  await page.waitForTimeout(2500);

  // Exactly one authorization for exactly one Lesson's media.
  expect(count((call) => call.startsWith("POST /learn/lessons/") && call.endsWith("/playback"))).toBe(1);

  // The Course's contents are read on the server, beside the Lesson. Neither read may reach the
  // browser as a second round trip on top of the page it already delivered.
  expect(count((call) => call.startsWith("GET /learn/courses/"))).toBe(0);
  expect(count((call) => call.startsWith("GET /learn/dashboard"))).toBe(0);

  // A fresh page load re-reads it once, because the holder above the page is mounted again with
  // it. Once, though: not once per surface that wants it, and not again because the route's locale
  // replaced the provider's default during hydration.
  expect(profileReads()).toBeLessThanOrEqual(1);
});

/* ------------------------------------------------------------------ mobile */

test.describe("the learning experience at 390px", () => {
  test.use({ viewport: { width: 390, height: 844 } });

  for (const locale of ["en", "ar"] as const) {
    const t = TEXT[locale];

    test(`${locale}: the Course contents are one control away, and nothing scrolls sideways`, async ({
      page,
    }) => {
      await page.goto(`/${locale}/learn/dashboard`);
      await expectNoHorizontalOverflow(page);

      await page.goto(`/${locale}/learn/courses/${COURSE_ID}`);
      await expectNoHorizontalOverflow(page);

      await page.goto(`/${locale}/learn/courses/${COURSE_ID}/lessons/${LESSON_ONE}`);
      await expectNoHorizontalOverflow(page);

      // The standing column is not merely narrow at this width — it is out of the page entirely,
      // so a Student meets exactly one set of Lesson links.
      await expect(page.getByTestId("course-contents-sidebar")).toBeHidden();

      const trigger = page.getByTestId("open-course-contents");
      await expect(trigger).toBeVisible();
      await expect(trigger).toContainText(t.courseContents);
      await trigger.click();

      const drawer = page.getByRole("dialog");
      await expect(drawer).toBeVisible();
      await expect(drawer.getByText(t.lessonThree)).toBeVisible();
      await expectNoHorizontalOverflow(page);

      // Choosing a Lesson from the drawer navigates, exactly as choosing one from the column does.
      await drawer.getByRole("link", { name: t.lessonThree }).click();
      await page.waitForURL(`**/lessons/${LESSON_THREE}`);
      await expect(page.getByRole("heading", { name: t.lessonThree, level: 1 })).toBeVisible();
      await expectNoHorizontalOverflow(page);
    });
  }
});

/* ------------------------------------------------------------------ accessibility */

const AXE_SURFACES = [
  ["dashboard", `/learn/dashboard`],
  ["course", `/learn/courses/${COURSE_ID}`],
  ["lesson", `/learn/courses/${COURSE_ID}/lessons/${LESSON_ONE}`],
] as const;

for (const [name, route] of AXE_SURFACES) {
  for (const locale of ["en", "ar"] as const) {
    test(`axe: ${name} in ${locale}`, async ({ page }) => {
      await page.goto(`/${locale}${route}`);
      await expect(page.locator("main")).toBeVisible();
      await expectAxeClean(page, `${name}/${locale}`);
    });
  }
}

test("the learning statuses a Student reads clear AA against what they are drawn on", async ({
  page,
}) => {
  await page.goto(`/en/learn/courses/${COURSE_ID}`);
  await expect(page.locator("main")).toBeVisible();

  const ratios = await page.evaluate(() => {
    const channel = (value: number) => {
      const c = value / 255;
      return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
    };
    const luminance = (colour: string) => {
      const [r, g, b] = colour.match(/\d+(\.\d+)?/g)!.map(Number);
      return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
    };
    const backgroundOf = (element: Element): string => {
      let node: Element | null = element;
      while (node) {
        const colour = getComputedStyle(node).backgroundColor;
        if (colour && !colour.startsWith("rgba(0, 0, 0, 0)")) return colour;
        node = node.parentElement;
      }
      return "rgb(255, 255, 255)";
    };
    const ratio = (element: Element) => {
      const a = luminance(getComputedStyle(element).color);
      const b = luminance(backgroundOf(element));
      const [light, dark] = a > b ? [a, b] : [b, a];
      return (light + 0.05) / (dark + 0.05);
    };
    // The state attribute is on the pill itself, which is also the element whose colour pairing
    // this measures.
    const statusPill = document.querySelector("[data-learning-status]");
    const lessonState = document.querySelector('[href*="/lessons/"] span span');
    return {
      status: statusPill ? ratio(statusPill) : null,
      lesson: lessonState ? ratio(lessonState) : null,
    };
  });

  expect(ratios.status, "the access-state pill must be measurable").not.toBeNull();
  expect(ratios.status!).toBeGreaterThanOrEqual(4.5);
  expect(ratios.lesson!).toBeGreaterThanOrEqual(4.5);
});

/* ------------------------------------------------------------------ evidence */

const SHOTS = [
  ["dashboard", `/learn/dashboard`, 390, 844],
  ["dashboard", `/learn/dashboard`, 1440, 1000],
  ["course", `/learn/courses/${COURSE_ID}`, 390, 844],
  ["course", `/learn/courses/${COURSE_ID}`, 1440, 1000],
  ["lesson", `/learn/courses/${COURSE_ID}/lessons/${LESSON_ONE}`, 390, 844],
  ["lesson", `/learn/courses/${COURSE_ID}/lessons/${LESSON_ONE}`, 768, 1024],
  ["lesson", `/learn/courses/${COURSE_ID}/lessons/${LESSON_ONE}`, 1024, 900],
  ["lesson", `/learn/courses/${COURSE_ID}/lessons/${LESSON_ONE}`, 1440, 1000],
] as const;

for (const [name, route, width, height] of SHOTS) {
  for (const locale of ["en", "ar"] as const) {
    test(`evidence: ${name} at ${width}px in ${locale}`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width, height });
      await page.goto(`/${locale}${route}`);
      await expect(page.locator("main")).toBeVisible();
      // Give the player its first frame or its placeholder, so the shot is a real state.
      await page.waitForTimeout(1500);

      const file = path.join(
        process.env.GRADEX_UXF_EVIDENCE_DIR || testInfo.outputDir,
        `uxf-${name}-${locale}-${width}.png`,
      );
      fs.mkdirSync(path.dirname(file), { recursive: true });
      const shot = await page.screenshot({ fullPage: true, path: file });
      await testInfo.attach(`uxf-${name}-${locale}-${width}`, { body: shot, contentType: "image/png" });
    });
  }
}
