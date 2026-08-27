import AxeBuilder from "@axe-core/playwright";
import {
  expect,
  test,
  type BrowserContext,
  type Page,
} from "@playwright/test";
import { issueRotatingSession } from "./rotating-students";
import { frontendOrigin } from "../src/lib/api/e2e-ports";

/**
 * UX-I — the Admin course lifecycle workspace, in Arabic and on a phone.
 *
 * The Instructor studio already proves both of these about itself: UX-E walks it at five widths in
 * both languages and scans it with axe. The lifecycle workspace had neither. It is the screen that
 * carries the product's most consequential commands, and until now nothing asserted that an Arabic
 * reader sees them laid out correctly or that a reader on a phone can reach them at all.
 *
 * What is checked here is the page, not the transitions. No lifecycle route is called: selecting a
 * Course and opening a confirmation is enough to lay out every control the screen has, and the
 * transitions themselves belong to T8C.
 */

const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };

const VIEWPORTS = [
  ["phone", 390, 844],
  ["tablet", 768, 1024],
  ["wide", 1440, 900],
] as const;

const LOCALES = ["en", "ar"] as const;
type Locale = (typeof LOCALES)[number];

/** Every command the workspace offers, so "reachable" is measured against all of them. */
const COMMANDS = [
  "lifecycle-delist",
  "lifecycle-relist",
  "lifecycle-retire",
  "lifecycle-archive",
  "lifecycle-suspend",
  "lifecycle-restore",
] as const;

async function signInAdmin(context: BrowserContext, locale: Locale): Promise<void> {
  const session = issueRotatingSession(ADMIN);
  const origin = new URL(frontendOrigin());
  await context.addInitScript((v) => window.localStorage.setItem("gradex.locale", v), locale);
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
}

/** Open the workspace on whichever Course the directory lists first, and select it. */
async function openWorkspace(page: Page, locale: Locale): Promise<void> {
  await page.goto(`/${locale}/admin/course-lifecycle`);
  const row = page.getByTestId("lifecycle-course-row").first();
  await expect(row).toBeVisible({ timeout: 20_000 });
  // Addressed by role, not by label, so the same helper drives both languages.
  await row.getByRole("button").click();
  await expect(page.getByTestId("lifecycle-selected-title")).toBeVisible();
}

/** How far the document can be scrolled sideways. Anything above a rounding pixel is a defect. */
async function horizontalOverflow(page: Page): Promise<number> {
  return page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
}

test.describe("UX-I lifecycle workspace layout", () => {
  test.describe.configure({ timeout: 120_000 });

  for (const [name, width, height] of VIEWPORTS) {
    for (const locale of LOCALES) {
      test(`the workspace does not scroll sideways at ${name} (${width}px) in ${locale}`, async ({
        browser,
      }) => {
        const context = await browser.newContext({
          locale: locale === "ar" ? "ar-KW" : "en-US",
          viewport: { width, height },
        });
        await signInAdmin(context, locale);
        const page = await context.newPage();
        await openWorkspace(page, locale);

        // The directory is a three-column table with a Course title in it. A table wider than the
        // viewport scrolls inside its own container; the page itself never does.
        expect(
          await horizontalOverflow(page),
          "the workspace pushed the document sideways",
        ).toBeLessThanOrEqual(1);

        await context.close();
      });
    }
  }

  for (const locale of LOCALES) {
    test(`every lifecycle command is reachable on a phone in ${locale}`, async ({ browser }) => {
      const context = await browser.newContext({
        locale: locale === "ar" ? "ar-KW" : "en-US",
        viewport: { width: 390, height: 844 },
      });
      await signInAdmin(context, locale);
      const page = await context.newPage();
      await openWorkspace(page, locale);

      // Wrapping is fine; disappearing is not. Each command has to be on the page, hittable, and
      // inside the viewport once scrolled to — a control that wrapped off the edge is a command
      // an Administrator on a phone simply does not have.
      for (const command of COMMANDS) {
        const control = page.getByTestId(command);
        await expect(control, `${command} is missing`).toBeVisible();
        await control.scrollIntoViewIfNeeded();
        await expect(control, `${command} sits outside the viewport`).toBeInViewport();
        const box = await control.boundingBox();
        expect(box, `${command} has no layout`).not.toBeNull();
        expect(box!.x, `${command} starts off the near edge`).toBeGreaterThanOrEqual(0);
        expect(box!.x + box!.width, `${command} runs past the far edge`).toBeLessThanOrEqual(390);
        // A control smaller than the touch target guidance is not reachable on a phone in any
        // useful sense.
        expect(box!.height, `${command} is too short to hit`).toBeGreaterThanOrEqual(24);
      }

      await context.close();
    });
  }

  test("the workspace is Arabic and right to left, with its commands in Arabic", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "ar-KW" });
    await signInAdmin(context, "ar");
    const page = await context.newPage();
    await openWorkspace(page, "ar");

    await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
    await expect(page.locator("html")).toHaveAttribute("lang", "ar");

    // The commands are translated, not left in the language the screen was written in.
    for (const command of COMMANDS) {
      const label = (await page.getByTestId(command).textContent()) ?? "";
      expect(label.trim(), `${command} has no label`).not.toBe("");
      expect(
        label,
        `${command} is still in English on the Arabic screen`,
      ).toMatch(/[؀-ۿ]/);
    }

    // The workspace reads right to left where it matters: the table's own cells are start-aligned,
    // which in Arabic means the right edge.
    const alignment = await page
      .getByTestId("lifecycle-course-row")
      .first()
      .locator("th")
      .first()
      .evaluate((el) => getComputedStyle(el).textAlign);
    expect(["start", "right"], "the directory's rows are not start-aligned").toContain(alignment);

    await context.close();
  });

  test("a confirmation opened in Arabic fits, reads right to left, and returns focus", async ({
    browser,
  }) => {
    const context = await browser.newContext({
      locale: "ar-KW",
      viewport: { width: 390, height: 844 },
    });
    await signInAdmin(context, "ar");
    const page = await context.newPage();
    await openWorkspace(page, "ar");

    const trigger = page.getByTestId("lifecycle-archive");
    await trigger.click();
    const dialog = page.getByTestId("lifecycle-confirm-archive");
    await expect(dialog).toBeVisible();
    // Measured where it comes to rest, not part-way through arriving.
    await dialog.evaluate((el) => Promise.all(el.getAnimations().map((a) => a.finished)));

    const box = await dialog.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.x, "the confirmation starts off the near edge").toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width, "the confirmation runs past the far edge").toBeLessThanOrEqual(390);
    await expect(dialog).toContainText(/[؀-ۿ]/);
    await expect(dialog.getByTestId("confirm-cancel")).toBeInViewport();
    await expect(dialog.getByTestId("confirm-accept")).toBeInViewport();

    // Opening a dialog must not make the page behind it scrollable sideways either.
    expect(
      await horizontalOverflow(page),
      "the confirmation pushed the page sideways",
    ).toBeLessThanOrEqual(1);

    // Escape closes it, and the reader lands back on the control they opened it from.
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(trigger).toBeFocused();

    await context.close();
  });

  for (const locale of LOCALES) {
    test(`the workspace carries no accessibility violation in ${locale}`, async ({ browser }) => {
      const context = await browser.newContext({
        locale: locale === "ar" ? "ar-KW" : "en-US",
      });
      await signInAdmin(context, locale);
      const page = await context.newPage();
      await openWorkspace(page, locale);

      const scan = async (label: string) => {
        const results = await new AxeBuilder({ page })
          .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
          .analyze();
        expect(
          results.violations.map((v) => `${v.id} (${v.nodes.length}): ${v.help}`),
          `axe violations on ${label}`,
        ).toEqual([]);
      };

      await scan(`lifecycle workspace in ${locale}`);

      // And again with a confirmation open. A dialog is the part of a screen least likely to have
      // been scanned, and it is the part that traps focus and takes over the document.
      await page.getByTestId("lifecycle-archive").click();
      await expect(page.getByTestId("lifecycle-confirm-archive")).toBeVisible();
      await scan(`lifecycle confirmation in ${locale}`);

      await context.close();
    });
  }

  test("a long course title does not break the directory", async ({ browser }) => {
    const context = await browser.newContext({
      locale: "ar-KW",
      viewport: { width: 390, height: 844 },
    });
    await signInAdmin(context, "ar");
    const page = await context.newPage();

    // A title far longer than any seeded Course, served to the directory the screen reads. Long
    // Arabic titles are the realistic case the seeded fixtures do not cover.
    await page.route("**/api/v1/admin/courses*", async (route) => {
      const response = await route.fetch();
      const body = await response.json();
      const items = (body.items ?? []) as Record<string, unknown>[];
      if (items.length > 0) {
        items[0].title_ar =
          "مقرر تجريبي بعنوان طويل جدا لاختبار سلوك الجدول عندما يكون العنوان أطول بكثير من عرض الشاشة المتاحة على الهاتف المحمول";
        items[0].title_en =
          "A deliberately very long course title used to prove the directory survives a title far wider than a phone";
      }
      await route.fulfill({ response, json: { ...body, items } });
    });

    await openWorkspace(page, "ar");
    expect(
      await horizontalOverflow(page),
      "a long title pushed the directory sideways",
    ).toBeLessThanOrEqual(1);

    await context.close();
  });
});
