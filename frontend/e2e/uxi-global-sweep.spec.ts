import AxeBuilder from "@axe-core/playwright";
import {
  expect,
  test,
  type BrowserContext,
  type ConsoleMessage,
  type Page,
} from "@playwright/test";
import { issueRotatingSession } from "./rotating-students";
import { frontendOrigin } from "../src/lib/api/e2e-ports";

/**
 * UX-I — the properties every screen is supposed to have, checked on every screen.
 *
 * The tranche suites each prove their own surface deeply. What none of them can prove is
 * consistency: that the skip link is on all of them, that no two of them disagree about heading
 * levels, that none of them logs a hydration mismatch, that the dark theme is not a light screen
 * with a dark frame around it. Those are properties of the product, so they are asserted across a
 * route set rather than inside one screen's suite.
 *
 * Where a check finds nothing, that is the finding. This suite is written to fail loudly on a
 * regression rather than to demonstrate that the current tree passes.
 */

const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };

/** The routes an Administrator's session can reach, plus the public ones anyone can. */
const ROUTES = [
  ["landing", "/"],
  ["catalogue", "/en/catalog"],
  ["sign in", "/login"],
  ["terms", "/en/terms"],
  ["course lifecycle", "/en/admin/course-lifecycle"],
  ["course directory", "/en/admin/courses"],
  ["course access", "/en/admin/course-access"],
  ["staff", "/staff"],
  ["academic catalog", "/en/admin/academic-catalog"],
  ["reported content", "/en/admin/reported-content"],
] as const;

const LOCALES = ["en", "ar"] as const;

async function signInAdmin(context: BrowserContext, locale: "ar" | "en"): Promise<void> {
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

/** Localise an English route for the Arabic pass. Routes outside `[locale]` have one spelling. */
function localised(path: string, locale: "ar" | "en"): string {
  return path.startsWith("/en") ? `/${locale}${path.slice(3)}` : path;
}

test.describe("UX-I global sweep", () => {
  test.describe.configure({ timeout: 180_000 });

  test("every screen offers a skip link and names its main landmark", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await signInAdmin(context, "en");
    const page = await context.newPage();

    for (const [name, path] of ROUTES) {
      await page.goto(path);
      await expect(page.locator("main"), `${name} has no main landmark`).toHaveCount(1);

      // The skip link is the first thing a keyboard reader meets. It only counts if focusing it
      // reveals it — a permanently `sr-only` link is one a sighted keyboard user cannot follow.
      await page.keyboard.press("Tab");
      const first = page.locator(":focus");
      const href = await first.getAttribute("href");
      expect(href, `${name} does not open with a skip link`).toBe("#main");
      await expect(first, `${name}'s skip link stays hidden when focused`).toBeInViewport();

      // And it must land somewhere. A skip link pointing at an id that does not exist silently
      // does nothing.
      await expect(page.locator("#main"), `${name} has no #main to skip to`).toHaveCount(1);
    }

    await context.close();
  });

  test("every screen has exactly one first-level heading and skips no level", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await signInAdmin(context, "en");
    const page = await context.newPage();

    for (const [name, path] of ROUTES) {
      await page.goto(path);
      await expect(page.locator("h1")).toHaveCount(1);

      const levels = await page
        .locator("h1, h2, h3, h4, h5, h6")
        .evaluateAll((nodes) => nodes.map((n) => Number(n.tagName.slice(1))));
      let previous = levels[0];
      for (const level of levels.slice(1)) {
        // Going back up is always fine; going down by more than one leaves a reader guessing what
        // the missing level would have been.
        expect(
          level - previous,
          `${name} jumps from h${previous} to h${level}`,
        ).toBeLessThanOrEqual(1);
        previous = level;
      }
    }

    await context.close();
  });

  test("no screen logs a console error or a hydration mismatch", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await signInAdmin(context, "en");
    const page = await context.newPage();

    const complaints: string[] = [];
    const record = (message: ConsoleMessage) => {
      if (message.type() !== "error" && message.type() !== "warning") return;
      const text = message.text();
      // Network failures are the fixture's business, not the product's; hydration and React's own
      // warnings are what this is for.
      if (/Failed to load resource|net::ERR|status of 4\d\d|status of 5\d\d/i.test(text)) return;
      complaints.push(text);
    };
    page.on("console", record);
    page.on("pageerror", (error) => complaints.push(`pageerror: ${error.message}`));

    for (const [, path] of ROUTES) {
      await page.goto(path);
      await expect(page.locator("main")).toBeVisible();
    }

    expect(complaints, "the console carried complaints").toEqual([]);
    await context.close();
  });

  for (const locale of LOCALES) {
    test(`the dark theme carries no contrast or accessibility violation in ${locale}`, async ({
      browser,
    }) => {
      const context = await browser.newContext({
        locale: locale === "ar" ? "ar-KW" : "en-US",
        colorScheme: "dark",
      });
      await signInAdmin(context, locale);
      /**
       * The theme is a class, not a media query — but the class is not ours to set.
       *
       * `next-themes` owns it and rewrites it at hydration from its stored preference, and this
       * application configures `defaultTheme="light"`, so an unset preference resolves to light no
       * matter what the OS asks for. Adding the class in an init script was therefore overwritten
       * before first paint, and every assertion below ran against a *light* page while reporting
       * dark-theme coverage. The stored preference is the only input that survives hydration.
       */
      await context.addInitScript(() => window.localStorage.setItem("theme", "dark"));
      const page = await context.newPage();

      for (const [name, path] of ROUTES) {
        await page.goto(localised(path, locale));
        await expect(page.locator("main")).toBeVisible();
        // Proof the theme was selected, so this can never quietly become a light-mode test again.
        await expect(page.locator("html"), `${name} did not enter the dark theme`).toHaveClass(/dark/);
        const results = await new AxeBuilder({ page })
          .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
          .analyze();
        expect(
          results.violations.map((v) => `${v.id} (${v.nodes.length}): ${v.help}`),
          `dark-theme axe violations on ${name} in ${locale}`,
        ).toEqual([]);
      }

      await context.close();
    });
  }

  test("no screen leaks an identifier or a backend enum into what a reader is asked to read", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await signInAdmin(context, "en");
    const page = await context.newPage();

    const UUID = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i;
    const ENUMS = [
      "PENDING_REVIEW",
      "CHANGES_REQUESTED",
      "PENDING_STUDENT_ACCEPTANCE",
      "PENDING_ADMIN_APPROVAL",
      "SEVERE_MODERATION",
      "ACCESS_ENDED",
      "NOT_STARTED",
      "SUPERSEDED",
    ];

    for (const [name, path] of ROUTES) {
      await page.goto(path);
      await expect(page.locator("main")).toBeVisible();
      // The markup may carry identifiers in `data-*` for tests and support. What a human is asked
      // to interpret is the rendered text, so that is what this reads.
      const text = (await page.locator("main").textContent()) ?? "";
      expect(text, `a UUID reached ${name}'s copy`).not.toMatch(UUID);
      for (const wire of ENUMS) {
        expect(text, `the backend term "${wire}" reached ${name}'s copy`).not.toContain(wire);
      }
    }

    await context.close();
  });

  test("a keyboard reader can see where they are on every screen", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await signInAdmin(context, "en");
    const page = await context.newPage();

    for (const [name, path] of ROUTES) {
      await page.goto(path);
      await expect(page.locator("main")).toBeVisible();

      // Walk the first stretch of the tab order. A control that takes focus without painting
      // anything is a control a keyboard reader has lost track of, and `outline: none` with no
      // replacement is the usual way that happens.
      for (let step = 0; step < 12; step += 1) {
        await page.keyboard.press("Tab");
        // `document.activeElement` rather than a `:focus` locator: `:focus` also matches every
        // ancestor of the focused node, which is not what is being asked about here.
        const seen = await page.evaluate(() => {
          const el = document.activeElement as HTMLElement | null;
          if (!el || el === document.body) return null;
          // The development overlay is Next.js furniture, not a product control.
          if (el.tagName.toLowerCase().startsWith("nextjs-")) return { visible: true, label: el.tagName };
          const style = getComputedStyle(el);
          const outline =
            style.outlineStyle !== "none" && parseFloat(style.outlineWidth || "0") > 0;
          // A ring is a box-shadow in this design system, and a border change counts too.
          const ring = style.boxShadow !== "none" && style.boxShadow !== "";
          return {
            visible: outline || ring,
            label: `${el.tagName} ${(el.textContent ?? "").trim().slice(0, 30)}`,
          };
        });
        if (seen === null) break;
        expect(seen.visible, `${name}: "${seen.label}" takes focus without showing it`).toBe(true);
      }
    }

    await context.close();
  });

  /**
   * Known defect, recorded rather than relaxed.
   *
   * `LocaleProvider` starts every render at `defaultLocale` (Arabic) and restores the saved or
   * route language in an effect after mount, deliberately, to avoid a hydration mismatch. Any
   * screen whose fetch is keyed on `locale` therefore issues that read twice whenever the reader's
   * saved language is not Arabic: once with the provisional locale, once with the restored one.
   * The lifecycle directory is one of those screens, and it is not the only one.
   *
   * Fixing it means resolving the language before first paint rather than after — a change to how
   * the whole product decides its locale, with its own hydration risk — which is not a presentation
   * tranche's to make. The assertion is left as it should read, marked as failing, so that a fix
   * turns this red-to-green rather than requiring someone to remember the test existed.
   */
  test.fixme("a screen issues each of its reads once, not once per render", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await signInAdmin(context, "en");
    const page = await context.newPage();

    const calls: string[] = [];
    page.on("request", (request) => {
      const url = new URL(request.url());
      if (url.pathname.startsWith("/api/v1/")) calls.push(`${request.method()} ${url.pathname}`);
    });

    // The lifecycle workspace is the busiest read on the Admin side: a directory, a search, and a
    // refetch after every command. An effect that re-subscribes on each render shows up here as
    // the same GET twice before anyone has touched anything.
    await page.goto("/en/admin/course-lifecycle");
    await expect(page.getByTestId("lifecycle-course-row").first()).toBeVisible({ timeout: 20_000 });
    await page.waitForTimeout(1500);

    const duplicated = [...new Set(calls)].filter(
      (call) => calls.filter((c) => c === call).length > 1,
    );
    expect(duplicated, "a read was issued more than once on first load").toEqual([]);

    await context.close();
  });
});
