import {
  expect,
  test,
  type BrowserContext,
  type Page,
} from "@playwright/test";
import { issueRotatingSession } from "./rotating-students";
import { frontendOrigin } from "../src/lib/api/e2e-ports";

/**
 * UX-I — the consequential Admin lifecycle commands, and the answer the product now waits for.
 *
 * Retiring, archiving and suspending a Course used to happen on the click that named them. None of
 * the three is undone by the button beside it: retirement closes the Course to everyone new,
 * archival is terminal, and suspension stops every read for Students who are part-way through the
 * Course at that moment. T8C proves those transitions still happen. What this suite proves is the
 * part T8C takes for granted — that the naming click alone sends nothing, that the confirmation
 * says what will actually happen rather than repeating the button, that backing out leaves both the
 * Course and the reader's focus where they were, and that agreeing sends exactly one request.
 *
 * Every lifecycle route is intercepted. The subject here is the confirmation, not the transition,
 * and a suite that archived a real Course to prove a dialog would be destroying fixtures to test a
 * button.
 */

const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };

type Command = "retire" | "archive" | "suspend";

/** The route each command calls, so "nothing was sent" is measured rather than assumed. */
const ROUTE: Record<Command, string> = {
  retire: "**/api/v1/admin/courses/*/retire",
  archive: "**/api/v1/admin/courses/*/archive",
  suspend: "**/api/v1/admin/courses/*/access-suspension",
};

/** The sentence in each confirmation that could only belong to that command. */
const CONSEQUENCE: Record<Command, { en: string; ar: string }> = {
  retire: { en: "No one new can be given access", ar: "لن يتمكن أحد جديد" },
  archive: { en: "Archiving is terminal", ar: "الأرشفة نهائية" },
  suspend: { en: "studying it right now", ar: "يدرسونه الآن" },
};

async function signInAdmin(context: BrowserContext, locale: "ar" | "en"): Promise<void> {
  const session = issueRotatingSession(ADMIN);
  const origin = new URL(frontendOrigin());
  await context.addInitScript(
    (value) => window.localStorage.setItem("gradex.locale", value),
    locale,
  );
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

/**
 * Count every call to one command's route and refuse it.
 *
 * A refusal rather than a success keeps the fixture Course untouched while still driving the screen
 * through the whole path a confirmed command takes.
 */
async function countAndRefuse(page: Page, command: Command): Promise<string[]> {
  const calls: string[] = [];
  await page.route(ROUTE[command], async (route) => {
    calls.push(route.request().method());
    await route.fulfill({
      status: 503,
      contentType: "application/problem+json",
      body: JSON.stringify({
        type: "about:blank",
        title: "The course could not be changed.",
        detail: "The service is temporarily unavailable.",
        status: 503,
      }),
    });
  });
  return calls;
}

/** Open the lifecycle workspace on whichever Course the directory lists first. */
async function openFirstCourse(page: Page, locale: "ar" | "en"): Promise<void> {
  await page.goto(`/${locale}/admin/course-lifecycle`);
  const row = page.getByTestId("lifecycle-course-row").first();
  await expect(row).toBeVisible({ timeout: 20_000 });
  // The row's only control is its Manage button, addressed by role rather than by its label so the
  // same helper works in both languages.
  await row.getByRole("button").click();
  await expect(page.getByTestId("lifecycle-selected-title")).toBeVisible();
}

/** Suspension needs its recorded reason before the consequence can be stated. */
async function fillSuspensionReason(page: Page): Promise<void> {
  await page.getByTestId("lifecycle-suspension-cause").selectOption("SECURITY");
  await page.getByTestId("lifecycle-suspension-reason").fill("UX-I confirmation coverage");
}

test.describe("UX-I consequential lifecycle commands", () => {
  test.describe.configure({ timeout: 90_000 });

  for (const command of ["retire", "archive", "suspend"] as const) {
    test(`${command} sends nothing until it is confirmed, and then sends exactly one request`, async ({
      browser,
    }) => {
      const context = await browser.newContext({ locale: "en-US" });
      await signInAdmin(context, "en");
      const page = await context.newPage();
      const calls = await countAndRefuse(page, command);

      await openFirstCourse(page, "en");
      if (command === "suspend") await fillSuspensionReason(page);

      const trigger = page.getByTestId(`lifecycle-${command}`);
      await trigger.click();

      // The click that names the command is not the click that carries it out.
      const dialog = page.getByTestId(`lifecycle-confirm-${command}`);
      await expect(dialog).toBeVisible();
      expect(calls, "the naming click sent a request before anyone confirmed it").toEqual([]);

      // The confirmation states this command's own consequence, not a restatement of its label.
      await expect(dialog).toContainText(CONSEQUENCE[command].en);

      await dialog.getByTestId("confirm-cancel").click();
      await expect(dialog).toHaveCount(0);
      expect(calls, "backing out of the confirmation issued the command anyway").toEqual([]);

      // Backing out returns the reader to the control they opened the dialog from, rather than
      // dropping them on the document body at the top of a long workspace.
      await expect(trigger).toBeFocused();

      await trigger.click();
      await expect(dialog).toBeVisible();
      await dialog.getByTestId("confirm-accept").click();

      // The refusal reads as a refusal, and the command was issued once — not once per click.
      await expect(page.getByTestId("lifecycle-error")).toBeVisible({ timeout: 20_000 });
      expect(calls.length, "the confirmed command did not call the API exactly once").toBe(1);
      await expect(dialog).toHaveCount(0);

      await context.close();
    });
  }

  test("the reason suspension requires is asked for before the consequence is stated", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await signInAdmin(context, "en");
    const page = await context.newPage();
    const calls = await countAndRefuse(page, "suspend");

    await openFirstCourse(page, "en");
    // No reason given. The screen refuses while the Admin is still filling the form, rather than
    // after they have agreed to a consequence the product then declines to carry out.
    await page.getByTestId("lifecycle-suspend").click();
    await expect(page.getByTestId("lifecycle-confirm-suspend")).toHaveCount(0);
    await expect(page.getByTestId("lifecycle-error")).toBeVisible();
    expect(calls).toEqual([]);

    await fillSuspensionReason(page);
    await page.getByTestId("lifecycle-suspend").click();
    await expect(page.getByTestId("lifecycle-confirm-suspend")).toBeVisible();

    await context.close();
  });

  test("the confirmations are in Arabic, and laid out right to left, when the locale is Arabic", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "ar-KW" });
    await signInAdmin(context, "ar");
    const page = await context.newPage();

    await openFirstCourse(page, "ar");
    await expect(page.locator("html")).toHaveAttribute("dir", "rtl");

    for (const command of ["retire", "archive"] as const) {
      await page.getByTestId(`lifecycle-${command}`).click();
      const dialog = page.getByTestId(`lifecycle-confirm-${command}`);
      await expect(dialog).toBeVisible();
      await expect(dialog).toContainText(CONSEQUENCE[command].ar);
      // The consequence is the half a reader cannot get from the button, so it must be translated
      // rather than left in the language the screen was written in.
      await expect(dialog).not.toContainText(CONSEQUENCE[command].en);
      await dialog.getByTestId("confirm-cancel").click();
      await expect(dialog).toHaveCount(0);
    }

    await context.close();
  });

  test("a confirmation is usable on a phone", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await signInAdmin(context, "en");
    const page = await context.newPage();
    await page.setViewportSize({ width: 390, height: 844 });

    await openFirstCourse(page, "en");
    await page.getByTestId("lifecycle-archive").click();
    const dialog = page.getByTestId("lifecycle-confirm-archive");
    await expect(dialog).toBeVisible();

    // Measure where the dialog comes to rest, not where it is part-way through arriving. The
    // confirmation animates in, and its entry keyframes drive the same transform that centres it,
    // so a box read on the first frame is a box that has not finished moving — 133px in, on the way
    // to 16px. This is the difference between measuring the layout and measuring the animation.
    await dialog.evaluate((el) => Promise.all(el.getAnimations().map((a) => a.finished)));

    // Both answers are on screen and hittable. A confirmation whose cancel button has been pushed
    // off a 390px viewport is a confirmation with only one answer.
    const box = await dialog.boundingBox();
    expect(box, "the confirmation has no layout").not.toBeNull();
    expect(box!.x, "the confirmation starts off the left edge").toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width, "the confirmation runs past the right edge").toBeLessThanOrEqual(390);
    await expect(dialog.getByTestId("confirm-cancel")).toBeInViewport();
    await expect(dialog.getByTestId("confirm-accept")).toBeInViewport();

    // And the page itself never scrolls sideways to accommodate it.
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(overflow, "the confirmation pushed the page sideways").toBeLessThanOrEqual(1);

    await context.close();
  });
});
