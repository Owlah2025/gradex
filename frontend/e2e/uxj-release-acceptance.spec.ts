import fs from "fs";
import path from "path";
import {
  expect,
  test,
  request as playwrightRequest,
  type BrowserContext,
  type ConsoleMessage,
  type Page,
} from "@playwright/test";
import { issueRotatingSession } from "./rotating-students";
import { frontendOrigin } from "../src/lib/api/e2e-ports";

/**
 * UX-J — the release gate.
 *
 * Every tranche before this one proved a surface. None of them proved the *product*: that the five
 * kinds of principal each land somewhere that belongs to them, that a route survives being arrived
 * at cold rather than walked to, that the dark theme repaints the page rather than its frame, that
 * no screen anywhere shows a reader a UUID or a wire enum, and that Gradex never advertises a
 * commerce capability it does not have.
 *
 * Those are claims about the release, so they are asserted across the release-critical route set for
 * every role at once rather than inside any one screen's suite. Where a tranche suite already owns a
 * property deeply — Course Details' identifiers (UX-D), the Admin sweep (UX-I), the learning request
 * budget (UX-F) — this suite does not restate it. It covers what no single surface could.
 *
 * The last test writes the curated release evidence set. It is a capture, not an assertion, and it
 * exists so the screenshots a release decision rests on are produced by the same run that produced
 * the verdict.
 */

const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };
const INSTRUCTOR = { email: "instructor@example.test", accountID: "a0000000-0000-0000-0000-000000000003" };
const STUDENT_ACTIVE = "student-active@example.test";
const FIXTURE_PASSWORD = "StudentPassword123!";

/** The Student fixture the learning suites already use, so its progress state is the seeded one. */
const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const LESSON_ONE = "30000000-0000-0000-0000-000000000001";

const EVIDENCE_DIR =
  process.env.GRADEX_UXJ_EVIDENCE_DIR || path.join(__dirname, "..", "test-results", "uxj-release-evidence");

type Role = "admin" | "anonymous" | "instructor" | "student";

/* ------------------------------------------------------------------ sessions */

async function applySession(
  context: BrowserContext,
  principal: { accountID: string; email: string },
  locale: "ar" | "en",
): Promise<void> {
  const session = issueRotatingSession(principal);
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

/**
 * The Student fixture is a password account rather than a rotating one, so it signs in through the
 * real session endpoint. That is also the honest thing for a release gate: the Student's session is
 * the one the product issues, not one minted beside it.
 */
async function studentCookies(context: BrowserContext) {
  const page = await context.newPage();
  await page.goto("/en/catalog");
  const status = await page.evaluate(async ([email, password]) => {
    const bootstrap = await fetch("/api/v1/session/bootstrap", { method: "GET", credentials: "include" });
    const { csrf_token } = await bootstrap.json();
    const login = await fetch("/api/v1/sessions", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", Accept: "application/json", "X-CSRF-Token": csrf_token },
      body: JSON.stringify({ email, password }),
    });
    return login.status;
  }, [STUDENT_ACTIVE, FIXTURE_PASSWORD]);
  expect(status, "the Student fixture must sign in through the real session endpoint").toBe(201);
  const cookies = await context.cookies();
  await page.close();
  return cookies;
}

let studentSession: Awaited<ReturnType<typeof studentCookies>>;

test.beforeAll(async ({ browser }) => {
  const context = await browser.newContext();
  studentSession = await studentCookies(context);
  await context.close();
  fs.mkdirSync(EVIDENCE_DIR, { recursive: true });
});

async function contextFor(
  browser: import("@playwright/test").Browser,
  role: Role,
  locale: "ar" | "en",
  viewport?: { height: number; width: number },
): Promise<BrowserContext> {
  const context = await browser.newContext({
    locale: locale === "ar" ? "ar-KW" : "en-US",
    ...(viewport ? { viewport } : {}),
  });
  if (role === "admin") await applySession(context, ADMIN, locale);
  else if (role === "instructor") await applySession(context, INSTRUCTOR, locale);
  else if (role === "student") {
    await context.addInitScript((v) => window.localStorage.setItem("gradex.locale", v), locale);
    await context.addCookies(studentSession);
  } else {
    await context.addInitScript((v) => window.localStorage.setItem("gradex.locale", v), locale);
  }
  return context;
}

/* ------------------------------------------------------------------ the route set */

/**
 * The routes a release decision rests on, and the role each one belongs to. Public routes are listed
 * once under `anonymous`; a signed-in principal reaching them is covered by the sweep suites.
 */
const RELEASE_ROUTES: ReadonlyArray<{ path: string; role: Role; name: string }> = [
  { name: "landing", path: "/", role: "anonymous" },
  { name: "catalogue", path: "/en/catalog", role: "anonymous" },
  { name: "sign in", path: "/login", role: "anonymous" },
  { name: "register", path: "/register", role: "anonymous" },
  { name: "course access", path: "/en/access", role: "student" },
  { name: "student dashboard", path: "/en/learn/dashboard", role: "student" },
  { name: "student course", path: `/en/learn/courses/${COURSE_ID}`, role: "student" },
  { name: "student lesson", path: `/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ONE}`, role: "student" },
  { name: "academic profile", path: "/en/learn/academic-profile", role: "student" },
  { name: "student profile", path: "/en/learn/profile", role: "student" },
  { name: "instructor courses", path: "/en/instructor/courses", role: "instructor" },
  { name: "admin courses", path: "/en/admin/courses", role: "admin" },
  { name: "admin review queue", path: "/en/admin/catalog", role: "admin" },
  { name: "admin course access", path: "/en/admin/course-access", role: "admin" },
  { name: "admin lifecycle", path: "/en/admin/course-lifecycle", role: "admin" },
  { name: "admin academic catalogue", path: "/en/admin/academic-catalog", role: "admin" },
  { name: "staff", path: "/staff", role: "admin" },
];

/** The workspace roots. A principal must never be delivered into another role's root. */
const FOREIGN_ROOTS: Record<Role, readonly string[]> = {
  admin: ["/learn/", "/instructor/"],
  anonymous: ["/learn/", "/instructor/", "/admin/"],
  instructor: ["/learn/", "/admin/"],
  student: ["/instructor/", "/admin/"],
};

/** The text a reader is actually asked to read, with markup and scripts left out. */
async function readableText(page: Page): Promise<string> {
  return page.evaluate(() => document.body.innerText);
}

/**
 * Console errors that belong to the product rather than the harness.
 *
 * The browser writes its own line for any subresource that does not return 200 — that is Chrome
 * reporting a network status, not the application reporting a fault, and under a dense suite run the
 * API's rate limiter legitimately produces them (an observed 429 on the Lesson route is what forced
 * this to be stated exactly). UX-I already draws this line the same way; matching it keeps one
 * definition of "the console is clean" across the release rather than two.
 *
 * What is left is what this is for: React's own warnings, hydration mismatches, and uncaught errors.
 */
function isProductConsoleError(message: ConsoleMessage): boolean {
  if (message.type() !== "error") return false;
  const text = message.text();
  if (/Failed to load resource|net::ERR|status of 4\d\d|status of 5\d\d/i.test(text)) return false;
  if (/Download the React DevTools/i.test(text)) return false;
  return true;
}

/* ------------------------------------------------------------------ J1 — role landing */

test.describe("UX-J role and session acceptance", () => {
  test.describe.configure({ timeout: 180_000 });

  test("each principal class reaches its own workspace and is offered no other", async ({ browser }) => {
    const expected: ReadonlyArray<{ home: string; own: string; role: Role }> = [
      { home: "/en/learn/dashboard", own: "/en/learn/dashboard", role: "student" },
      { home: "/en/instructor/courses", own: "/en/instructor/courses", role: "instructor" },
      { home: "/en/admin/courses", own: "/en/admin/courses", role: "admin" },
    ];

    for (const { home, own, role } of expected) {
      const context = await contextFor(browser, role, "en");
      const page = await context.newPage();
      await page.goto(home);

      // The principal's own workspace renders rather than bouncing.
      await expect(page, `${role} was not delivered to ${own}`).toHaveURL(new RegExp(`${own}$`));
      await expect(page.locator("main"), `${role}'s workspace has no main landmark`).toHaveCount(1);

      // And the header offers no other role's workspace.
      const hrefs = await page
        .locator("header a")
        .evaluateAll((nodes) => nodes.map((n) => (n as HTMLAnchorElement).getAttribute("href") ?? ""));
      for (const foreign of FOREIGN_ROOTS[role]) {
        expect(
          hrefs.filter((href) => href.includes(foreign)),
          `${role} was offered a ${foreign} destination`,
        ).toEqual([]);
      }
      // An anchor with no href at all is the shape a guessed destination degrades into.
      expect(hrefs, `${role}'s header carries an anchor with no destination`).not.toContain("");
      expect(hrefs.join(" "), `${role}'s header carries an undefined destination`).not.toContain("undefined");

      await context.close();
    }
  });

  test("an anonymous visitor is offered public navigation and no workspace", async ({ browser }) => {
    const context = await contextFor(browser, "anonymous", "en");
    const page = await context.newPage();
    await page.goto("/");

    const hrefs = await page
      .locator("header a")
      .evaluateAll((nodes) => nodes.map((n) => (n as HTMLAnchorElement).getAttribute("href") ?? ""));
    for (const foreign of FOREIGN_ROOTS.anonymous) {
      expect(hrefs.filter((href) => href.includes(foreign)), `a visitor was offered ${foreign}`).toEqual([]);
    }
    expect(hrefs).not.toContain("");
    // The way in is offered, so the header did read the session rather than rendering a role view.
    // Matched by destination rather than by its words: this route is bilingual, and an English
    // locator would pass in one language and silently fail in the other.
    expect(
      hrefs.filter((href) => href === "/login"),
      "a visitor is offered no way to sign in",
    ).not.toEqual([]);
    await context.close();
  });

  test("a signed-out session leaves no authenticated workspace reachable", async ({ browser }) => {
    const context = await contextFor(browser, "student", "en");
    const page = await context.newPage();
    await page.goto("/en/learn/dashboard");
    await expect(page.locator("main")).toBeVisible();

    await context.clearCookies();
    await page.goto("/en/learn/dashboard");

    // Whatever the product chooses to render, it must not be the Student's own dashboard content
    // served from a session that no longer exists.
    const text = await readableText(page);
    expect(text, "a cleared session still rendered Student workspace content").not.toContain(
      "CS101: Introduction to Programming",
    );
    await context.close();
  });
});

/* ------------------------------------------------------------------ J2 — deep link and refresh */

test.describe("UX-J deep-link and refresh acceptance", () => {
  test.describe.configure({ timeout: 300_000 });

  test("every release-critical route survives a cold arrival and a reload", async ({ browser }) => {
    for (const { name, path: route, role } of RELEASE_ROUTES) {
      const context = await contextFor(browser, role, "en");
      const page = await context.newPage();
      const errors: string[] = [];
      page.on("console", (message) => {
        if (isProductConsoleError(message)) errors.push(`${name}: ${message.text()}`);
      });
      // An uncaught exception never reaches the console listener, and it is the strongest evidence
      // a cold arrival went wrong.
      page.on("pageerror", (error) => errors.push(`${name}: pageerror: ${error.message}`));

      // Cold arrival: typed, bookmarked, or followed from outside the app.
      await page.goto(route);
      await expect(page.locator("main"), `${name} has no main landmark on a cold arrival`).toHaveCount(1);
      await expect(
        page.getByRole("heading", { level: 1 }),
        `${name} does not name itself with a single first-level heading`,
      ).toHaveCount(1);
      const settled = page.url();

      // And the same route reloaded. A page that only works when walked to is a page a bookmark or
      // a browser restore breaks.
      await page.reload();
      await expect(page.locator("main"), `${name} has no main landmark after a reload`).toHaveCount(1);
      await expect(
        page.getByRole("heading", { level: 1 }),
        `${name} loses its first-level heading on a reload`,
      ).toHaveCount(1);
      expect(page.url(), `${name} moved somewhere else when it was reloaded`).toBe(settled);

      // A reload must never deliver a principal into a workspace that is not theirs.
      for (const foreign of FOREIGN_ROOTS[role]) {
        expect(
          new URL(page.url()).pathname.includes(foreign),
          `${name} put a ${role} into ${foreign} on a reload`,
        ).toBe(false);
      }

      // A hydration mismatch is a console error, and it is one this product must not log.
      expect(errors, `${name} logged a console error`).toEqual([]);
      await context.close();
    }
  });

  /**
   * `returnTo` is deliberately not re-asserted here.
   *
   * UX-H already owns it behaviourally — a hostile destination is dropped from the link the page
   * carries forward, and a safe internal one survives encoded. The obvious release-gate version of
   * this check, asserting the hostile string is absent from the rendered document, is unsound: the
   * query string is part of the URL, so Next embeds it in the router payload whether or not the
   * product would ever act on it. Such a test fails on a correct product and would be "fixed" by
   * weakening it, which is worse than not having it.
   */
});

/* ------------------------------------------------------------------ J3 — reading matter */

test.describe("UX-J reading matter across every role", () => {
  test.describe.configure({ timeout: 300_000 });

  /**
   * Identifier and enum shapes, not a fixture list. UX-I sweeps the Admin routes against the seeded
   * identifiers it knows; this asserts the *shape*, so a route rendering an identifier the fixture
   * list never anticipated still fails.
   */
  const UUID = /\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b/i;
  const WIRE_ENUM = /\b(PENDING_REVIEW|CHANGES_REQUESTED|PUBLISHED|UNPUBLISHED|ARCHIVED|RETIRED|SUSPENDED|ACTIVE|REVOKED|EXPIRED|CONSUMED|SCAN_PASSED|QUARANTINED|NO_VIDEO|AUTHENTICATED|ANONYMOUS)\b/;

  for (const locale of ["ar", "en"] as const) {
    test(`no release-critical screen shows a reader an identifier or a wire enum in ${locale}`, async ({
      browser,
    }) => {
      for (const { name, path: route, role } of RELEASE_ROUTES) {
        const context = await contextFor(browser, role, locale);
        const page = await context.newPage();
        await page.goto(route.startsWith("/en") ? `/${locale}${route.slice(3)}` : route);
        await expect(page.locator("main")).toBeVisible();

        const text = await readableText(page);
        const identifier = text.match(UUID);
        expect(identifier?.[0], `${name} shows a reader the identifier ${identifier?.[0]} in ${locale}`).toBeUndefined();
        const wireEnum = text.match(WIRE_ENUM);
        expect(wireEnum?.[0], `${name} shows a reader the wire value ${wireEnum?.[0]} in ${locale}`).toBeUndefined();

        await context.close();
      }
    });
  }
});

/* ------------------------------------------------------------------ J4 — business honesty */

test.describe("UX-J business honesty", () => {
  test.describe.configure({ timeout: 300_000 });

  /**
   * Gradex carries no payment, no cart, no coupon, no rating and no enrolment counter. Access is
   * arranged outside the platform and recorded by an Administrator. A screen that offers any of
   * these is claiming a capability the backend does not have, which is the one class of visual
   * defect that costs a real person real money.
   */
  const FALSE_CLAIMS: ReadonlyArray<{ pattern: RegExp; what: string }> = [
    { pattern: /add to cart|checkout|buy now|proceed to payment/i, what: "in-platform checkout" },
    { pattern: /السلة|الدفع الآن|ادفع الآن/, what: "in-platform checkout (Arabic)" },
    { pattern: /\bcoupon\b|\bpromo code\b|كوبون/i, what: "a coupon" },
    { pattern: /\bKNET\b|\bcard number\b|\bCVV\b|رقم البطاقة/i, what: "a card form" },
    { pattern: /\b\d+(\.\d+)? out of 5\b|\b\d+ reviews?\b|\bstar rating\b/i, what: "a rating" },
    { pattern: /\b\d[\d,]* students enrolled\b|\b\d[\d,]* enrolments?\b/i, what: "an enrolment count" },
    { pattern: /\brefund\b(?!.*polic)/i, what: "a refund action" },
  ];

  for (const locale of ["ar", "en"] as const) {
    test(`no release-critical screen claims a capability the product does not have in ${locale}`, async ({
      browser,
    }) => {
      for (const { name, path: route, role } of RELEASE_ROUTES) {
        const context = await contextFor(browser, role, locale);
        const page = await context.newPage();
        await page.goto(route.startsWith("/en") ? `/${locale}${route.slice(3)}` : route);
        await expect(page.locator("main")).toBeVisible();

        const text = await readableText(page);
        for (const { pattern, what } of FALSE_CLAIMS) {
          expect(pattern.test(text), `${name} offers ${what} in ${locale}`).toBe(false);
        }
        await context.close();
      }
    });
  }
});

/* ------------------------------------------------------------------ J5 — dark theme */

test.describe("UX-J dark theme acceptance", () => {
  test.describe.configure({ timeout: 300_000 });

  /** One representative route per product domain. Every domain must repaint, not just the frame. */
  const DOMAINS: ReadonlyArray<{ path: string; role: Role; name: string }> = [
    { name: "public", path: "/en/catalog", role: "anonymous" },
    { name: "auth", path: "/login", role: "anonymous" },
    { name: "student", path: "/en/learn/dashboard", role: "student" },
    { name: "instructor", path: "/en/instructor/courses", role: "instructor" },
    { name: "admin", path: "/en/admin/courses", role: "admin" },
    // The Course Access picker painted `bg-white` while its heading took the dark theme's near-white
    // foreground: 1.04:1, an invisible heading. Kept in the domain list so the surface that actually
    // regressed is the one measured.
    { name: "admin course access", path: "/en/admin/course-access", role: "admin" },
  ];

  test("no release-critical domain keeps a light-only surface in the dark theme", async ({ browser }) => {
    for (const { name, path: route, role } of DOMAINS) {
      const context = await contextFor(browser, role, "en");
      // `next-themes` owns the class on <html> and rewrites it at hydration from its stored
      // preference, whose default is light. Adding the class in an init script therefore does not
      // select the dark theme — it is overwritten before first paint, and the page under test is a
      // light one. The stored preference is the only input that survives, so that is what is set.
      await context.addInitScript(() => window.localStorage.setItem("theme", "dark"));
      const page = await context.newPage();
      await page.goto(route);
      await expect(page.locator("main")).toBeVisible();

      // Proof the theme was actually selected. Without this a regression in how the theme is stored
      // would turn every assertion below into a light-mode test that passes for the wrong reason.
      await expect(page.locator("html"), `${name} did not enter the dark theme`).toHaveClass(/dark/);

      // The page's own ground must be dark. A dark frame around a light document is the failure
      // this catches, and it is the one a screenshot review misses on a small surface.
      const bodyLuminance = await page.evaluate(() => {
        const parse = (value: string) => (value.match(/[\d.]+/g) ?? ["255", "255", "255"]).map(Number);
        const [r, g, b] = parse(getComputedStyle(document.body).backgroundColor);
        return (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255;
      });
      expect(bodyLuminance, `${name} paints a light body in the dark theme`).toBeLessThan(0.5);

      // And no painted surface inside `main` may stay pure white, which is what an untokenised
      // `bg-white` looks like once the rest of the page has turned over.
      const whiteSurfaces = await page.evaluate(() => {
        const offenders: string[] = [];
        const main = document.querySelector("main");
        if (!main) return offenders;
        for (const node of Array.from(main.querySelectorAll<HTMLElement>("*"))) {
          const background = getComputedStyle(node).backgroundColor;
          if (background === "rgb(255, 255, 255)") {
            offenders.push(`${node.tagName.toLowerCase()}.${node.className || "(no class)"}`);
          }
        }
        return offenders.slice(0, 8);
      });
      expect(whiteSurfaces, `${name} keeps a light-only surface in the dark theme`).toEqual([]);

      await context.close();
    }
  });

  /**
   * The destructive button, in the theme where it failed.
   *
   * A filled destructive control is the one place a colour token has to carry text, and in the dark
   * theme it did not: white on the lifted red is 3.72:1. This measures the pairing where it is
   * actually painted rather than trusting the token, so a future change to either half is caught.
   */
  test("a filled destructive control carries readable text in the dark theme", async ({ browser }) => {
    const context = await contextFor(browser, "admin", "en");
    await context.addInitScript(() => window.localStorage.setItem("theme", "dark"));
    const page = await context.newPage();
    await page.goto("/staff");
    await expect(page.locator("html")).toHaveClass(/dark/);

    const suspend = page.locator('[data-testid="staff-instructor-suspend"]');
    await expect(suspend, "the staff workspace offers no destructive control to measure").not.toHaveCount(0);

    const ratio = await suspend.first().evaluate((node) => {
      const channel = (value: number) => {
        const v = value / 255;
        return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
      };
      const luminance = (colour: string) => {
        const [r, g, b] = (colour.match(/[\d.]+/g) ?? ["0", "0", "0"]).map(Number);
        return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
      };
      const style = getComputedStyle(node);
      const a = luminance(style.color);
      const b = luminance(style.backgroundColor);
      return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
    });

    expect(ratio, "the destructive control's text does not clear AA where it is painted").toBeGreaterThanOrEqual(4.5);
    await context.close();
  });

  /**
   * The Course Access load failure, rendered.
   *
   * This paragraph carried `text-destructive` on `bg-destructive` — the same colour twice, so an
   * Administrator whose published-course read failed was shown an error they could not read. The
   * state only appears when the read fails, which is why no sweep over healthy pages found it.
   */
  test("a failed published-course read is readable, in both themes", async ({ browser }) => {
    for (const theme of ["light", "dark"] as const) {
      const context = await contextFor(browser, "admin", "en");
      await context.addInitScript((value) => window.localStorage.setItem("theme", value), theme);
      const page = await context.newPage();
      // The picker's options are the *public* catalogue, read once per language — that read is what
      // has to fail for the failure state to render.
      await page.route("**/api/v1/catalog/courses**", (route) =>
        route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ title: "unavailable" }) }),
      );
      await page.goto("/en/admin/course-access");

      const failure = page.getByTestId("course-access-courses-error");
      await expect(failure, `the ${theme} theme showed no failure state`).toBeVisible();

      const ratio = await failure.locator("p").evaluate((node) => {
        const channel = (value: number) => {
          const v = value / 255;
          return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
        };
        const luminance = (colour: string) => {
          const [r, g, b] = (colour.match(/[\d.]+/g) ?? ["0", "0", "0"]).map(Number);
          return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
        };
        // The paragraph's own background is translucent, so the painted ground is the card behind it.
        const style = getComputedStyle(node);
        const behind = getComputedStyle(node.closest("section") ?? document.body).backgroundColor;
        const a = luminance(style.color);
        const b = luminance(behind);
        return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
      });

      expect(ratio, `the ${theme} theme renders an unreadable failure message`).toBeGreaterThanOrEqual(4.5);
      await context.close();
    }
  });
});

/* ------------------------------------------------------------------ J6 — request budget */

test.describe("UX-J request acceptance", () => {
  test.describe.configure({ timeout: 180_000 });

  test("the established request wins are still held", async ({ browser }) => {
    // A confirmed anonymous visitor asks nothing about an account it does not have.
    const anonymous = await contextFor(browser, "anonymous", "en");
    const anonymousPage = await anonymous.newPage();
    const anonymousCalls: string[] = [];
    anonymousPage.on("request", (r) => {
      if (r.url().includes("/api/v1/")) anonymousCalls.push(r.url());
    });
    await anonymousPage.goto("/en/catalog");
    await expect(anonymousPage.locator("main")).toBeVisible();
    await anonymousPage.waitForTimeout(1500);
    expect(
      anonymousCalls.filter((url) => url.includes("/me/academic-profile")),
      "an anonymous visitor asked for an academic profile",
    ).toEqual([]);
    await anonymous.close();

    // The Instructor's course page reads its own courses once, not once per consumer.
    const instructor = await contextFor(browser, "instructor", "en");
    const instructorPage = await instructor.newPage();
    const instructorCalls: string[] = [];
    instructorPage.on("request", (r) => {
      if (r.url().includes("/api/v1/")) instructorCalls.push(`${r.method()} ${new URL(r.url()).pathname}`);
    });
    await instructorPage.goto("/en/instructor/courses");
    await expect(instructorPage.locator("main")).toBeVisible();
    await instructorPage.waitForTimeout(2000);
    const ownedReads = instructorCalls.filter((call) => call === "GET /api/v1/instructor/courses");
    expect(
      ownedReads.length,
      `the Instructor course directory read its own courses ${ownedReads.length} times:\n${instructorCalls.join("\n")}`,
    ).toBeLessThanOrEqual(1);
    await instructor.close();
  });

  /**
   * The locale-restoration duplicate read, bounded.
   *
   * `LocaleProvider` starts at the default language and restores the reader's saved one after mount,
   * so a screen whose read is keyed on locale issues it twice when the saved language is not the
   * default. UX-I records that defect and leaves its assertion red on purpose
   * (`uxi-global-sweep.spec.ts` — "a screen issues each of its reads once").
   *
   * This is the other half of that record, and the half a release needs: the defect is *bounded*.
   * One extra read per locale-keyed resource is a duplicate; an effect that re-subscribed per render
   * would be unbounded, and that is a different and far worse defect wearing the same symptom. This
   * pins the bound so the accepted debt cannot quietly grow into the unacceptable version while it
   * waits to be fixed.
   */
  test("the accepted locale-restoration duplicate stays bounded at one extra read", async ({ browser }) => {
    const context = await contextFor(browser, "admin", "en");
    const page = await context.newPage();
    const calls: string[] = [];
    page.on("request", (r) => {
      const url = new URL(r.url());
      if (url.pathname.startsWith("/api/v1/")) calls.push(`${r.method()} ${url.pathname}`);
    });

    await page.goto("/en/admin/course-lifecycle");
    await expect(page.locator("main")).toBeVisible();
    await page.waitForTimeout(2500);

    const worst = [...new Set(calls)]
      .map((call) => ({ call, times: calls.filter((c) => c === call).length }))
      .sort((a, b) => b.times - a.times);

    expect(
      worst.filter((entry) => entry.times > 2).map((entry) => `${entry.call} ×${entry.times}`),
      "a read repeated more than the one accepted locale-restoration duplicate",
    ).toEqual([]);

    await context.close();
  });
});

/* ------------------------------------------------------------------ J6b — phone task completion */

test.describe("UX-J phone task completion", () => {
  test.describe.configure({ timeout: 180_000 });

  /**
   * The access queue at 390px, judged by whether the work can be done.
   *
   * This surface is a hundred-row operational table and it is long — roughly thirteen thousand
   * pixels of scroll on a phone. Length alone is not a defect: the question a release has to answer
   * is whether an Administrator holding a phone can still act on a request. So this asserts the act,
   * not the aesthetics — the decision controls exist, are reachable, are not clipped by a sideways
   * scroll, and are large enough to hit.
   */
  test("an Administrator can act on an access request at 390px", async ({ browser }) => {
    const context = await contextFor(browser, "admin", "en", { height: 844, width: 390 });
    const page = await context.newPage();
    await page.goto("/en/admin/course-access");
    await expect(page.locator("main")).toBeVisible();

    // The control names the request it acts on ("Approve — someone@example.test"), which is what
    // makes a hundred identical rows distinguishable to a screen reader. Matched on that shape.
    const decide = page.getByRole("button", { name: /^Approve — / });
    await expect(decide, "no access request is actionable at 390px").not.toHaveCount(0);

    const control = decide.first();
    await control.scrollIntoViewIfNeeded();
    await expect(control).toBeVisible();
    await expect(control).toBeEnabled();

    // Reachable means inside the viewport once scrolled to, not merely present in the document.
    await expect(control).toBeInViewport();

    // And hittable. 24px is the WCAG 2.2 minimum; this records what the control actually is.
    const box = await control.boundingBox();
    expect(box, "the decision control has no box at 390px").not.toBeNull();
    expect(box!.height, `the decision control is ${box!.height}px tall at 390px`).toBeGreaterThanOrEqual(24);
    expect(box!.width, `the decision control is ${box!.width}px wide at 390px`).toBeGreaterThanOrEqual(24);

    // The page itself must not scroll sideways, which is what would put the action column off-screen.
    const sideways = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1);
    expect(sideways, "the access queue scrolls sideways at 390px").toBe(false);

    await context.close();
  });
});

/* ------------------------------------------------------------------ J7 — release evidence */

test.describe("UX-J release evidence", () => {
  test.describe.configure({ timeout: 600_000 });

  /**
   * The curated set a release decision is actually looked at through. Every surface in the release
   * report appears here once per viewport and language, plus a dark representative per domain, and
   * nothing else — a set nobody reviews is not evidence.
   */
  const EVIDENCE: ReadonlyArray<{ path: string; role: Role; name: string }> = [
    { name: "public-landing", path: "/", role: "anonymous" },
    { name: "public-catalogue", path: "/en/catalog", role: "anonymous" },
    { name: "auth-login", path: "/login", role: "anonymous" },
    { name: "auth-register", path: "/register", role: "anonymous" },
    { name: "student-dashboard", path: "/en/learn/dashboard", role: "student" },
    { name: "student-course", path: `/en/learn/courses/${COURSE_ID}`, role: "student" },
    { name: "student-lesson", path: `/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ONE}`, role: "student" },
    { name: "student-access", path: "/en/access", role: "student" },
    { name: "student-academic-profile", path: "/en/learn/academic-profile", role: "student" },
    { name: "instructor-courses", path: "/en/instructor/courses", role: "instructor" },
    { name: "admin-courses", path: "/en/admin/courses", role: "admin" },
    { name: "admin-review", path: "/en/admin/catalog", role: "admin" },
    { name: "admin-access", path: "/en/admin/course-access", role: "admin" },
    { name: "admin-staff", path: "/staff", role: "admin" },
    { name: "admin-academic-catalogue", path: "/en/admin/academic-catalog", role: "admin" },
  ];

  const VIEWPORTS = [
    { height: 844, label: "390", width: 390 },
    { height: 900, label: "1440", width: 1440 },
  ] as const;

  test("the curated release evidence set is captured", async ({ browser }) => {
    for (const viewport of VIEWPORTS) {
      for (const locale of ["en", "ar"] as const) {
        for (const { name, path: route, role } of EVIDENCE) {
          const context = await contextFor(browser, role, locale, {
            height: viewport.height,
            width: viewport.width,
          });
          const page = await context.newPage();
          await page.goto(route.startsWith("/en") ? `/${locale}${route.slice(3)}` : route);
          await expect(page.locator("main")).toBeVisible();
          await page.waitForTimeout(600);

          // A page-level sideways scroll is a mobile defect, so the capture records it rather than
          // leaving it for a reviewer to notice in an image.
          if (viewport.width === 390) {
            const overflows = await page.evaluate(
              () => document.documentElement.scrollWidth > window.innerWidth + 1,
            );
            expect(overflows, `${name} scrolls sideways at 390px in ${locale}`).toBe(false);
          }

          await page.screenshot({
            fullPage: true,
            path: path.join(EVIDENCE_DIR, `${viewport.label}-${locale}-${name}.png`),
          });
          await context.close();
        }
      }
    }

    // One dark representative per domain, at the width each domain is actually used at.
    const DARK: ReadonlyArray<{ path: string; role: Role; name: string }> = [
      { name: "public-catalogue", path: "/en/catalog", role: "anonymous" },
      { name: "auth-login", path: "/login", role: "anonymous" },
      { name: "student-dashboard", path: "/en/learn/dashboard", role: "student" },
      { name: "instructor-courses", path: "/en/instructor/courses", role: "instructor" },
      { name: "admin-courses", path: "/en/admin/courses", role: "admin" },
    ];
    for (const { name, path: route, role } of DARK) {
      const context = await contextFor(browser, role, "en", { height: 900, width: 1440 });
      // The stored preference, for the reason given in the dark-theme suite above.
      await context.addInitScript(() => window.localStorage.setItem("theme", "dark"));
      const page = await context.newPage();
      await page.goto(route);
      await expect(page.locator("main")).toBeVisible();
      await page.waitForTimeout(600);
      await page.screenshot({ fullPage: true, path: path.join(EVIDENCE_DIR, `dark-1440-en-${name}.png`) });
      await context.close();
    }

    const captured = fs.readdirSync(EVIDENCE_DIR).filter((file) => file.endsWith(".png"));
    expect(captured.length, "the release evidence set is incomplete").toBe(
      VIEWPORTS.length * 2 * EVIDENCE.length + DARK.length,
    );
  });
});

/* ------------------------------------------------------------------ J8 — anonymous discovery */

test.describe("UX-J anonymous discovery", () => {
  test.describe.configure({ timeout: 180_000 });

  test("a published course is reachable from the catalogue by its human identity alone", async ({
    browser,
  }) => {
    // The real public contract, not a fixture: whatever this run's catalogue actually publishes.
    const api = await playwrightRequest.newContext({ baseURL: frontendOrigin() });
    const response = await api.get("/api/v1/catalog/courses");
    expect(response.status(), "the public catalogue must answer an anonymous read").toBe(200);
    const body = (await response.json()) as { items?: Array<{ slug?: string; title?: string }> };
    const published = (body.items ?? []).filter((item) => typeof item.slug === "string");
    await api.dispose();

    expect(published.length, "this run publishes no course, so discovery cannot be accepted").toBeGreaterThan(0);

    const context = await contextFor(browser, "anonymous", "en");
    const page = await context.newPage();
    await page.goto("/en/catalog");

    /**
     * Matched by slug, not by title.
     *
     * The catalogue payload is localised, so a title read from an unlocalised API call is the
     * Arabic one while the page under test is English — a locator built from it fails on a correct
     * product. Slug is the identity the contract actually promises is stable across languages, and
     * asserting on it is what this journey is about.
     */
    for (const course of published.slice(0, 3)) {
      const card = page.locator(`a[href="/en/catalog/${course.slug}"]`);
      await expect(card, `the course at ${course.slug} is not reachable from the catalogue`).not.toHaveCount(0);

      // And what the visitor reads is a human name, never the identity string itself.
      const name = ((await card.first().innerText()) ?? "").trim();
      expect(name.length, `the catalogue entry for ${course.slug} carries no readable name`).toBeGreaterThan(0);
      expect(name, `the catalogue shows the visitor the raw slug ${course.slug}`).not.toContain(course.slug);
    }

    // And the detail it opens names itself, without an identifier anywhere in it.
    const first = published[0];
    await page.goto(`/en/catalog/${first.slug}`);
    await expect(page.getByRole("heading", { level: 1 })).not.toBeEmpty();
    expect(await readableText(page), "Course Details shows a visitor an identifier").not.toMatch(
      /\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b/i,
    );
    await context.close();
  });
});
