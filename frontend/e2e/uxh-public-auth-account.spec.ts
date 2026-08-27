import fs from "fs";
import path from "path";
import AxeBuilder from "@axe-core/playwright";
import {
  expect,
  request as playwrightRequest,
  test,
  type BrowserContext,
  type Page,
} from "@playwright/test";
import {
  genericTestSlot,
  issueRotatingSession,
  studentFor,
} from "./rotating-students";
import { frontendOrigin } from "../src/lib/api/e2e-ports";

/**
 * UX-H — the way into the product, and the account once you are in.
 *
 * Everything before a Student has a workspace and everything they own once
 * they do: the landing page, sign-in, registration, the two invitation
 * entries, the password screens, and the academic profile.
 *
 * The behaviours themselves are proven elsewhere — S13 walks a mandatory
 * password change, T8B walks a staff invitation through real SMTP, T3 walks
 * the academic profile against the real catalog. What this suite is for is the
 * part those take for granted: that a visitor can cross from public browsing
 * into an account without losing what they were doing, that a refusal is a
 * sentence rather than a code, that no screen shows a token or an identifier,
 * and that a role nobody recognises is not quietly turned into a Student.
 */

const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };
const INSTRUCTOR = {
  email: "instructor@example.test",
  accountID: "a0000000-0000-0000-0000-000000000003",
};

/** The launch catalog, imported the way production imports it — and the way T3 does. */
const MANIFEST = "kuwait-university-launch-v1";
const anyInstitution = "00000000-0000-0000-0000-000000000000";

/** A UUID in any of the shapes a leak would take. */
const UUID = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i;

/** Backend vocabulary that must never reach an ordinary reader. */
const RAW_ENUMS = [
  "INVALID_CREDENTIALS",
  "AUTHENTICATION_FAILED",
  "AUTHENTICATION_REQUIRED",
  "PASSWORD_CHANGE_REQUIRED",
  "TOKEN_INVALID",
  "RATE_LIMITED",
  "VALIDATION_FAILED",
  "NOT_STARTED",
  "SKIPPED",
  "COMPLETED",
  "ENROLLED",
  "UNDECLARED",
  "NON_DEGREE",
  "FOUNDATION",
  "PENDING_STUDENT_ACCEPTANCE",
  "CONSUMED",
  "SUPERSEDED",
];

test.describe.configure({ timeout: 120_000 });

async function withLocale(context: BrowserContext, locale: "ar" | "en") {
  await context.addInitScript(
    (value) => window.localStorage.setItem("gradex.locale", value),
    locale,
  );
}

async function injectSession(
  context: BrowserContext,
  principal: { email: string; accountID: string },
) {
  const session = issueRotatingSession(principal);
  const origin = new URL(frontendOrigin());
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

/**
 * Imports the launch academic catalog.
 *
 * Without it there are no institutions, so the profile's choosers are empty and
 * a Student cannot say anything about their studies. It is Admin-imported data
 * rather than seed data, so any suite that renders the profile has to ask for
 * it first.
 */
async function ensureLaunchCatalog(): Promise<void> {
  const session = issueRotatingSession(ADMIN);
  const admin = await playwrightRequest.newContext({
    baseURL: frontendOrigin(),
    extraHTTPHeaders: {
      Accept: "application/json, application/problem+json",
      Origin: frontendOrigin(),
      Cookie: `${session.cookie_name}=${session.cookie_value}`,
      "X-CSRF-Token": session.csrf_token,
    },
  });
  const applied = await admin.post(
    `/api/v1/admin/academic/institutions/${anyInstitution}/import`,
    { data: { manifest: MANIFEST, mode: "apply" } },
  );
  expect(applied.status()).toBe(200);
  await admin.dispose();
}

/**
 * A seeded Student for one of this suite's journeys.
 *
 * The four general-purpose per-viewport slots, one per journey, so no case here
 * observes another's session or profile state. Nothing in this suite saves a
 * profile, so these Students are left exactly as they were found.
 */
function studentForJourney(
  testInfo: { repeatEachIndex: number },
  journey: 0 | 1 | 2 | 3,
) {
  return studentFor(testInfo, genericTestSlot(journey));
}

/** Signs in through the real form, the way a Student does. */
async function signInStudent(page: Page, email: string, locale: "ar" | "en" = "en") {
  await page.goto("/login");
  await page.locator("#email").fill(email);
  await page.locator("#password").fill("StudentPassword123!");
  await page
    .getByRole("button", { name: locale === "ar" ? /تسجيل الدخول/ : /sign in/i })
    .click();
  await page.waitForURL(/\/learn\/dashboard/, { timeout: 30_000 });
}

/** Everything a reader could actually see on this page. */
async function readableText(page: Page): Promise<string> {
  return (await page.locator("body").innerText()).replace(/\s+/g, " ");
}

async function axeClean(page: Page, label: string) {
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();
  expect(
    results.violations.map((violation) => `${violation.id}: ${violation.help}`),
    `axe violations on ${label}`,
  ).toEqual([]);
}

/** The page must never scroll sideways, at any width. */
async function noHorizontalOverflow(page: Page, label: string) {
  const overflow = await page.evaluate(() => ({
    scroll: document.documentElement.scrollWidth,
    client: document.documentElement.clientWidth,
  }));
  expect(
    overflow.scroll,
    `${label} overflows horizontally (${overflow.scroll} > ${overflow.client})`,
  ).toBeLessThanOrEqual(overflow.client + 1);
}

/* --------------------------------------------------------- anonymous entry */

test.describe("UX-H the way in", () => {
  for (const locale of ["en", "ar"] as const) {
    test(`${locale}: a visitor reaches sign-in from the landing page without a dead control`, async ({
      browser,
    }) => {
      const context = await browser.newContext({
        locale: locale === "ar" ? "ar-KW" : "en-US",
      });
      await withLocale(context, locale);
      const page = await context.newPage();
      await page.goto("/");
      await expect(page.locator("main")).toBeVisible();

      // Every link in the header resolves to something. The in-page anchors
      // belong to this page and only this page.
      const headerLinks = await page
        .locator("header a")
        .evaluateAll((nodes) =>
          nodes.map((node) => (node as HTMLAnchorElement).getAttribute("href")),
        );
      expect(headerLinks, "a header link carries no href").not.toContain(null);
      expect(headerLinks).not.toContain("");
      for (const href of headerLinks) {
        if (href?.startsWith("#")) {
          await expect(
            page.locator(href),
            `${href} is offered but the section does not exist`,
          ).toHaveCount(1);
        }
      }

      // No control that looks operable and is not.
      const deadButtons = await page.locator("header button").evaluateAll((nodes) =>
        nodes
          .filter(
            (node) =>
              !node.getAttribute("aria-haspopup") &&
              !node.getAttribute("aria-expanded") &&
              !node.closest("a") &&
              node.getAttribute("type") !== "submit",
          )
          .map((node) => node.textContent?.trim() || node.getAttribute("aria-label")),
      );
      // Language and theme toggles are real; a notifications bell was not.
      expect(deadButtons.join(" ")).not.toMatch(/notification|إشعار/i);

      await page.getByRole("link", { name: locale === "ar" ? /تسجيل الدخول/ : /Log in/i }).first().click();
      await expect(page).toHaveURL(/\/login/);
      await expect(page.locator("#email")).toBeVisible();
      await context.close();
    });
  }

  test("the catalogue's header offers a real route rather than another page's anchors", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await withLocale(context, "en");
    const page = await context.newPage();
    await page.goto("/en/catalog");
    await expect(page.locator("main")).toBeVisible();
    const hrefs = await page
      .locator("header a")
      .evaluateAll((nodes) =>
        nodes.map((node) => (node as HTMLAnchorElement).getAttribute("href") ?? ""),
      );
    // #courses, #why and #faq are landing-page sections. Off that page they are
    // three controls that move the reader nowhere.
    expect(hrefs.filter((href) => href.startsWith("#"))).toEqual([]);
    await context.close();
  });
});

/* ------------------------------------------------------------- sign-in UX */

test.describe("UX-H sign-in", () => {
  for (const locale of ["en", "ar"] as const) {
    test(`${locale}: a refused sign-in is a sentence, not a code`, async ({ browser }) => {
      const context = await browser.newContext({
        locale: locale === "ar" ? "ar-KW" : "en-US",
      });
      await withLocale(context, locale);
      const page = await context.newPage();
      await page.goto("/login");

      await page.locator("#email").fill("nobody@example.test");
      await page.locator("#password").fill("not-the-right-password");
      await page.getByRole("button", { name: /sign in|تسجيل الدخول/i }).click();

      const alert = page.getByRole("alert").first();
      await expect(alert).toBeVisible();
      const text = await readableText(page);
      for (const code of RAW_ENUMS) {
        expect(text, `${code} reached the reader`).not.toContain(code);
      }
      // And it must not reveal whether that address has an account.
      expect(text.toLowerCase()).not.toMatch(
        /no account with|not registered|unknown email|does not exist/,
      );
      await context.close();
    });
  }

  test("the password can be revealed, and the control says which state it is in", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await withLocale(context, "en");
    const page = await context.newPage();
    await page.goto("/login");

    const field = page.locator("#password");
    await field.fill("a-long-enough-passphrase");
    await expect(field).toHaveAttribute("type", "password");

    const reveal = page.getByRole("button", { name: /show password/i });
    await expect(reveal).toHaveAttribute("aria-pressed", "false");
    await reveal.click();
    await expect(field).toHaveAttribute("type", "text");
    await expect(page.getByRole("button", { name: /hide password/i })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    // Revealed or not, a password is an opaque sequence and is never reordered.
    await expect(field).toHaveAttribute("dir", "ltr");
    await context.close();
  });

  test("a second submit in the same instant reaches the server once", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await withLocale(context, "en");
    const page = await context.newPage();

    let attempts = 0;
    await page.route("**/api/v1/sessions", async (route) => {
      attempts += 1;
      await new Promise((resolve) => setTimeout(resolve, 400));
      await route.fulfill({
        status: 401,
        contentType: "application/problem+json",
        body: JSON.stringify({ code: "AUTHENTICATION_FAILED", status: 401 }),
      });
    });

    await page.goto("/login");
    await page.locator("#email").fill("someone@example.test");
    await page.locator("#password").fill("a-long-enough-passphrase");
    // Dispatched together, before any re-render could disable the control.
    await page.evaluate(() => {
      const form = document.querySelector("form");
      form?.requestSubmit();
      form?.requestSubmit();
    });
    await expect(page.getByRole("alert").first()).toBeVisible();
    expect(attempts, "one sign-in became two authentication attempts").toBe(1);
    await context.close();
  });

  test("a return-to that leaves this origin is refused, and a safe one is kept", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await withLocale(context, "en");
    const page = await context.newPage();

    // A hostile destination is dropped rather than carried across the hop.
    await page.goto("/login?returnTo=https://evil.example/steal");
    const forgot = page.getByRole("link", { name: /forgot your password/i });
    await expect(forgot).toHaveAttribute("href", "/recover");

    // A safe internal one survives, encoded.
    await page.goto("/login?returnTo=%2Fen%2Fcatalog");
    await expect(forgot).toHaveAttribute("href", /returnTo=%2Fen%2Fcatalog/);
    await context.close();
  });
});

/* ----------------------------------------------------- role destinations */

test.describe("UX-H where a session lands", () => {
  test("a Student who signs in reaches the Student surface", async ({
    browser,
  }, testInfo) => {
    const context = await browser.newContext({ locale: "en-US" });
    await withLocale(context, "en");
    const page = await context.newPage();
    await signInStudent(page, studentForJourney(testInfo, 0).email);
    await expect(page).toHaveURL(/\/en\/learn\/dashboard/);
    await context.close();
  });

  for (const [role, principal, destination] of [
    ["Instructor", INSTRUCTOR, /\/en\/instructor\/courses/],
    ["Admin", ADMIN, /\/en\/admin\/catalog/],
  ] as const) {
    test(`an ${role}'s header offers their own workspace and no other`, async ({ browser }) => {
      const context = await browser.newContext({ locale: "en-US" });
      await withLocale(context, "en");
      await injectSession(context, principal);
      const page = await context.newPage();
      await page.goto("/");
      await expect(page.locator("header")).toBeVisible();
      const workspace = page
        .locator("header a")
        .filter({ hasText: /workspace|studio|dashboard/i })
        .first();
      await expect(workspace).toHaveAttribute("href", destination);
      await expect(page.getByRole("button", { name: /sign out/i }).first()).toBeVisible();
      await context.close();
    });
  }

  test("a role this application does not know is offered no workspace at all", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await withLocale(context, "en");
    const page = await context.newPage();

    // The session route is where a role arrives from. A principal outside the
    // known set is still authenticated — it simply has no workspace here, and
    // the honest answer is to offer none rather than to guess Student.
    await page.route("**/api/v1/session", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          status: "AUTHENTICATED",
          role: "REGISTRAR",
          display_name: "Unclassified Principal",
          csrf_token: "csrf-for-an-unknown-role",
          idle_expires_at: new Date(Date.now() + 3_600_000).toISOString(),
          absolute_expires_at: new Date(Date.now() + 7_200_000).toISOString(),
        }),
      });
    });

    await page.goto("/");
    // Sign out is the one action that unambiguously applies, so it proves the
    // header did read the session rather than falling through to the guest view.
    await expect(page.getByRole("button", { name: /sign out/i }).first()).toBeVisible();

    const hrefs = await page
      .locator("header a")
      .evaluateAll((nodes) =>
        nodes.map((node) => (node as HTMLAnchorElement).getAttribute("href") ?? ""),
      );
    // No Student workspace, no Instructor workspace, no Admin workspace — and
    // no anchor carrying no href at all, which is what the guessed destination
    // used to degrade into.
    for (const invented of ["/en/learn/dashboard", "/en/instructor/courses", "/en/admin/catalog"]) {
      expect(hrefs, `an unknown role was offered ${invented}`).not.toContain(invented);
    }
    expect(hrefs).not.toContain("");
    expect(await readableText(page)).not.toContain("REGISTRAR");
    await context.close();
  });
});

/* -------------------------------------------------------------- invitation */

test.describe("UX-H invitation entry", () => {
  for (const locale of ["en", "ar"] as const) {
    test(`${locale}: an invitation link with no credential says so, and shows no token`, async ({
      browser,
    }) => {
      const context = await browser.newContext({
        locale: locale === "ar" ? "ar-KW" : "en-US",
      });
      await withLocale(context, locale);
      const page = await context.newPage();
      // Deliberately no fragment: the address typed, or a link that lost its
      // hash on the way. That is not an expired invitation.
      await page.goto("/staff/accept");
      await expect(page.locator("main")).toBeVisible();
      const text = await readableText(page);
      expect(text).not.toMatch(UUID);
      for (const code of RAW_ENUMS) {
        expect(text, `${code} reached the invitee`).not.toContain(code);
      }
      // And the form is not offered for an invitation nobody presented.
      await expect(page.locator("#staff-password")).toHaveCount(0);
      await context.close();
    });
  }

  test("a spent invitation is named as spent and points at signing in", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await withLocale(context, "en");
    const page = await context.newPage();

    // The preview route answers 200 and names the state. Each state has a
    // different next action; they used to collapse into one sentence.
    await page.route("**/api/v1/staff-invitations/preview", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ invited_role: "INSTRUCTOR", state: "CONSUMED" }),
      });
    });

    await page.goto("/staff/accept#token=a-spent-invitation-credential");
    await expect(page.getByTestId("staff-invitation-refused")).toBeVisible();
    await expect(page.getByRole("link", { name: /sign in/i })).toBeVisible();
    // The credential must never survive in the address bar.
    expect(page.url()).not.toContain("token=");
    const text = await readableText(page);
    expect(text).not.toContain("a-spent-invitation-credential");
    expect(text).not.toContain("CONSUMED");
    await context.close();
  });

  test("an expired invitation is not described as one already used", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await withLocale(context, "en");
    const page = await context.newPage();
    await page.route("**/api/v1/staff-invitations/preview", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ invited_role: "ADMIN", state: "EXPIRED" }),
      });
    });
    await page.goto("/staff/accept#token=an-expired-invitation-credential");
    await expect(page.getByTestId("staff-invitation-refused")).toBeVisible();
    const text = (await readableText(page)).toLowerCase();
    expect(text).toContain("expired");
    // Signing in is the answer to a consumed invitation, not to an expired one.
    expect(text).not.toContain("already been used");
    await context.close();
  });

  test("an open invitation states its role and its password rule before asking", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await withLocale(context, "en");
    const page = await context.newPage();
    await page.route("**/api/v1/staff-invitations/preview", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ invited_role: "INSTRUCTOR", state: "PENDING" }),
      });
    });
    await page.goto("/staff/accept#token=an-open-invitation-credential");
    await expect(page.getByTestId("staff-invitation-role")).toHaveText("Instructor");
    // The rule is stated, not discovered by being refused.
    await expect(page.locator("#staff-password-hint")).toContainText("15");
    expect(await readableText(page)).not.toContain("INSTRUCTOR");
    await context.close();
  });
});

/* ------------------------------------------------- academic context handoff */

test.describe("UX-H academic context across the join", () => {
  test.beforeAll(async () => {
    await ensureLaunchCatalog();
  });

  test("a browsing preference survives sign-in and is never claimed as account state", async ({
    browser,
  }, testInfo) => {
    const context = await browser.newContext({ locale: "en-US" });
    await withLocale(context, "en");
    const student = studentForJourney(testInfo, 1);
    const page = await context.newPage();

    // Set the preference the way the product sets it: on the public catalogue.
    await page.goto("/en/catalog");
    await expect(page.locator("main")).toBeVisible();
    const stored = await page.evaluate(() => {
      const key = Object.keys(window.localStorage).find((name) =>
        name.includes("academic"),
      );
      return key ? { key, value: window.localStorage.getItem(key) } : null;
    });

    await signInStudent(page, student.email);

    if (stored) {
      // Authenticating is not a reason to forget what the visitor was browsing.
      const after = await page.evaluate(
        (key) => window.localStorage.getItem(key),
        stored.key,
      );
      expect(after, "signing in discarded the browsing preference").toBe(stored.value);
    }

    // And the profile screen never says the browser's choice was saved to the
    // account, because no contract turns a public slug into a profile identifier.
    await page.goto("/en/learn/academic-profile");
    await expect(page.getByTestId("academic-profile-form")).toBeVisible();
    const text = (await readableText(page)).toLowerCase();
    expect(text).not.toMatch(/saved to your account already|we have set your (major|university)/);
    await context.close();
  });

  test("the academic profile shows real options and no identifiers", async ({
    browser,
  }, testInfo) => {
    const context = await browser.newContext({ locale: "en-US" });
    await withLocale(context, "en");
    const page = await context.newPage();
    await signInStudent(page, studentForJourney(testInfo, 2).email);

    await page.goto("/en/learn/profile");
    await expect(page.getByTestId("academic-profile-form")).toBeVisible();

    // The Student frame, so this screen is not a dead end.
    await expect(page.getByRole("link", { name: /my courses/i }).first()).toBeVisible();
    await expect(page.getByRole("button", { name: /sign out/i }).first()).toBeVisible();

    // Every option carries a real identifier as its value, and none of them is
    // shown to the Student.
    await expect
      .poll(() => page.locator("#profile-university option").count(), {
        timeout: 15_000,
      })
      .toBeGreaterThan(1);
    const values = await page
      .locator("#profile-university option")
      .evaluateAll((nodes) => nodes.map((node) => (node as HTMLOptionElement).value));
    expect(values.some((value) => UUID.test(value)), "no real institution options").toBe(true);
    const text = await readableText(page);
    expect(text).not.toMatch(UUID);
    for (const code of RAW_ENUMS) {
      expect(text, `${code} reached the Student`).not.toContain(code);
    }
    // Changing a password is reachable from the account surface.
    await expect(page.getByTestId("account-summary")).toBeVisible();
    await expect(page.getByRole("link", { name: /change password/i })).toBeVisible();
    await context.close();
  });

  test("signing out leaves no authenticated profile behind", async ({
    browser,
  }, testInfo) => {
    const context = await browser.newContext({ locale: "en-US" });
    await withLocale(context, "en");
    const page = await context.newPage();
    await signInStudent(page, studentForJourney(testInfo, 3).email);

    await page.getByRole("button", { name: /sign out/i }).first().click();
    await expect(page).toHaveURL(/\/login/, { timeout: 30_000 });
    await expect(page.getByRole("status").first()).toBeVisible();

    // Back on a public page the header is a guest header again: no workspace,
    // no sign out, nothing held over from the account that just left.
    await page.goto("/");
    await expect(page.locator("header")).toBeVisible();
    await expect(page.getByRole("button", { name: /sign out/i })).toHaveCount(0);
    await context.close();
  });
});

/* ------------------------------------------------------------ accessibility */

const AXE_SURFACES = [
  ["landing", "/"],
  ["login", "/login"],
  ["signup", "/register"],
  ["invitation", "/staff/accept"],
  ["password-change", "/password-change"],
  ["recover", "/recover"],
] as const;

for (const [name, route] of AXE_SURFACES) {
  for (const locale of ["en", "ar"] as const) {
    test(`axe: ${name} in ${locale}`, async ({ browser }) => {
      const context = await browser.newContext({
        locale: locale === "ar" ? "ar-KW" : "en-US",
        viewport: { width: 390, height: 900 },
      });
      await withLocale(context, locale);
      const page = await context.newPage();
      await page.goto(route);
      await expect(page.locator("main")).toBeVisible();
      await page.waitForTimeout(600);
      await axeClean(page, `${name} in ${locale}`);
      await noHorizontalOverflow(page, `${name} in ${locale} at 390px`);
      await context.close();
    });
  }
}

test("the academic profile is accessible in both languages", async ({
  browser,
}, testInfo) => {
  await ensureLaunchCatalog();
  for (const [index, locale] of (["en", "ar"] as const).entries()) {
    const context = await browser.newContext({
      locale: locale === "ar" ? "ar-KW" : "en-US",
      viewport: { width: 390, height: 900 },
    });
    await withLocale(context, locale);
    const page = await context.newPage();
    await signInStudent(
      page,
      studentForJourney(testInfo, index === 0 ? 2 : 3).email,
      locale,
    );
    await page.goto(`/${locale}/learn/profile`);
    await expect(page.getByTestId("academic-profile-form")).toBeVisible();
    await page.waitForTimeout(800);
    await axeClean(page, `academic profile in ${locale}`);
    await noHorizontalOverflow(page, `academic profile in ${locale} at 390px`);
    await context.close();
  }
});

/* ----------------------------------------------------------- responsiveness */

const WIDTHS = [390, 768, 1024, 1440] as const;

for (const width of WIDTHS) {
  test(`no horizontal overflow across the auth surfaces at ${width}px`, async ({ browser }) => {
    for (const locale of ["en", "ar"] as const) {
      const context = await browser.newContext({
        locale: locale === "ar" ? "ar-KW" : "en-US",
        viewport: { width, height: 900 },
      });
      await withLocale(context, locale);
      const page = await context.newPage();
      for (const [name, route] of AXE_SURFACES) {
        await page.goto(route);
        await expect(page.locator("main")).toBeVisible();
        await noHorizontalOverflow(page, `${name} in ${locale} at ${width}px`);
      }
      await context.close();
    }
  });
}

test("a long Arabic refusal does not push the sign-in form sideways", async ({ browser }) => {
  const context = await browser.newContext({
    locale: "ar-KW",
    viewport: { width: 390, height: 900 },
  });
  await withLocale(context, "ar");
  const page = await context.newPage();
  await page.goto("/login");
  await page.locator("#email").fill("nobody@example.test");
  await page.locator("#password").fill("not-the-right-password");
  await page.getByRole("button", { name: /تسجيل الدخول/ }).click();
  await expect(page.getByRole("alert").first()).toBeVisible();
  await noHorizontalOverflow(page, "the Arabic sign-in refusal at 390px");
  // The email field holds Latin text inside a right-to-left page and must not
  // be reordered by it.
  await expect(page.locator("#email")).toHaveAttribute("dir", "ltr");
  await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
  await context.close();
});

/* ------------------------------------------------------------------ evidence */

/**
 * Screenshots of the entry and account surfaces, at the two widths that matter
 * most and in both languages and themes. `GRADEX_UXH_EVIDENCE_DIR` puts them
 * somewhere durable; without it they land in the run's output directory and are
 * attached to the report either way.
 */
const SHOTS = [
  ["landing", "/"],
  ["login", "/login"],
  ["signup", "/register"],
  ["invitation", "/staff/accept"],
  ["password-change", "/password-change"],
] as const;

for (const [name, route] of SHOTS) {
  for (const width of [390, 1440] as const) {
    for (const locale of ["en", "ar"] as const) {
      for (const theme of ["light", "dark"] as const) {
        test(`evidence: ${name} at ${width}px in ${locale} ${theme}`, async ({
          browser,
        }, testInfo) => {
          const context = await browser.newContext({
            locale: locale === "ar" ? "ar-KW" : "en-US",
            viewport: { width, height: width === 390 ? 900 : 1000 },
            colorScheme: theme,
          });
          await withLocale(context, locale);
          await context.addInitScript(
            (value) => window.localStorage.setItem("theme", value),
            theme,
          );
          const page = await context.newPage();
          await page.goto(route);
          await expect(page.locator("main")).toBeVisible();
          await page.waitForTimeout(1500);

          const file = path.join(
            process.env.GRADEX_UXH_EVIDENCE_DIR || testInfo.outputDir,
            `uxh-${name}-${locale}-${theme}-${width}.png`,
          );
          fs.mkdirSync(path.dirname(file), { recursive: true });
          const shot = await page.screenshot({ fullPage: true, path: file });
          await testInfo.attach(`uxh-${name}-${locale}-${theme}-${width}`, {
            body: shot,
            contentType: "image/png",
          });
          await context.close();
        });
      }
    }
  }
}
