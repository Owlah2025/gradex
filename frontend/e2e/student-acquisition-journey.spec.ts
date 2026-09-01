import { expect, test } from "@playwright/test";
import { apiOrigin } from "../src/lib/api/e2e-ports";
import { ar } from "../src/lib/i18n/dictionaries/ar";
import { en } from "../src/lib/i18n/dictionaries/en";
import {
  completePurchaseConfirmation,
  readVerificationCodeFor,
  registerAndVerifyStudent,
  watchForPrematurePurchase,
} from "./student-journey";

/**
 * The Student acquisition journey, end to end, in one language at a time.
 *
 * Course Details → Buy → sign in required → create account → registration →
 * emailed code → verification → back to the same Course, still buying →
 * confirmation → explicit completion → WhatsApp.
 *
 * Every step of that used to be wrong in some way: an anonymous visitor could
 * create a purchase request by typing any address, WhatsApp opened on the first
 * button press, verification arrived as a link and then asked for the password
 * again, and the language flipped as soon as the journey crossed an unprefixed
 * auth route. This is the one spec that drives the whole thing.
 */

const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const PASSWORD = process.env.GRADEX_E2E_REGISTRATION_PASSWORD || "KuwaitStudy!2026";

test.describe.configure({ timeout: 150_000 });

const journeys = [
  {
    locale: "ar" as const,
    email: "acquisition-ar@example.test",
    displayName: "طالب الشراء",
    dictionary: ar,
  },
  {
    locale: "en" as const,
    email: "acquisition-en@example.test",
    displayName: "Acquisition Student",
    dictionary: en,
  },
];

for (const journey of journeys) {
  test(`the whole acquisition journey stays in ${journey.locale}`, async ({ browser }) => {
    const context = await browser.newContext();
    // The external handoff is intercepted so CI sends no WhatsApp message.
    await context.route("https://wa.me/**", (route) => route.abort());
    const page = await context.newPage();
    const { locale, dictionary } = journey;

    try {
      // 1. A locale-addressed Course. Visiting it is a language choice, and the
      //    rest of the journey has to honour it across routes that carry no
      //    locale segment of their own.
      await page.goto(`/${locale}/catalog/${COURSE_ID}`);
      await expect(page.locator("html")).toHaveAttribute("lang", locale);

      const watch = watchForPrematurePurchase(page);

      // 2. Buy. An anonymous visitor gets the way in, not a request.
      await page.getByTestId("purchase-request-open").click();
      await expect(page.getByTestId("purchase-sign-in-required")).toBeVisible();
      await expect(page.getByTestId("purchase-sign-in-required")).toContainText(
        dictionary.access.purchase.signInRequiredTitle,
      );
      watch.assertQuiet();

      // 3. Create an account. The destination and the intent travel with it.
      await page.getByTestId("purchase-create-account").click();
      await page.waitForURL((url) => url.pathname === "/register");
      // An unprefixed route, rendered in the language the Course was read in.
      await expect(page.locator("html")).toHaveAttribute("lang", locale);
      await expect(page.locator("html")).toHaveAttribute(
        "dir",
        locale === "ar" ? "rtl" : "ltr",
      );
      const returnTo = new URL(page.url()).searchParams.get("returnTo");
      expect(returnTo).toBeTruthy();
      const intended = new URL(returnTo!, "https://gradex.test");
      expect(intended.pathname).toBe(`/${locale}/catalog/${COURSE_ID}`);
      expect(intended.searchParams.get("purchase")).toBe("1");

      // 4. Register and prove the emailed code. No link is followed, and the
      //    password is never asked for a second time.
      await registerAndVerifyStudent(page, {
        email: journey.email,
        password: PASSWORD,
        displayName: journey.displayName,
        returnTo: returnTo!,
      });

      // 5. Back on the same Course, in the same language, still buying.
      await page.waitForURL(
        (url) =>
          url.pathname === `/${locale}/catalog/${COURSE_ID}` &&
          url.searchParams.get("purchase") === "1",
      );
      await expect(page.locator("html")).toHaveAttribute("lang", locale);
      const confirmation = page.getByTestId("purchase-confirmation");
      await expect(confirmation).toBeVisible();
      await expect(confirmation).toContainText(dictionary.access.purchase.heading);
      await expect(page.getByTestId("purchase-price")).toContainText("25.000");
      // Nothing has been created, and nothing has opened, across the whole
      // journey from the first press to here.
      watch.assertQuiet();

      // 6. Only the explicit confirmation creates the request and opens the
      //    handoff.
      const handoff = await completePurchaseConfirmation(page);
      expect(handoff.response.reference).toMatch(/^GRX-[A-F0-9]{16}$/);
      expect(handoff.handoffURL).toBe(handoff.response.whatsapp_url);
      const message = new URL(handoff.handoffURL).searchParams.get("text") || "";
      // The address is the one the server read off the Account, never one the
      // browser supplied.
      expect(message).toContain(journey.email);
      expect(message).toContain("25.000 KWD");
      // And the handoff speaks the language the journey was conducted in.
      if (locale === "ar") {
        expect(message).toContain("السعر");
      } else {
        expect(message).toContain("Price");
      }
    } finally {
      await context.close();
    }
  });
}

/**
 * The verification screen's own states.
 *
 * A six-digit code is only safe because guessing it is metered, so the states
 * a Student meets when they get it wrong are part of the security design rather
 * than incidental copy.
 */
test("the verification screen meters guesses and says so", async ({ browser }) => {
  const context = await browser.newContext();
  const page = await context.newPage();
  const email = "acquisition-attempts@example.test";

  try {
    // English, chosen the way the product lets a visitor choose it: a
    // locale-addressed route records the preference, and the unprefixed
    // admission screens inherit it. Asserting against a dictionary without
    // establishing the language would be asserting against the default.
    await page.goto(`/en/catalog/${COURSE_ID}`);
    await expect(page.locator("html")).toHaveAttribute("lang", "en");

    await page.goto("/register");
    await expect(page.locator("html")).toHaveAttribute("lang", "en");
    await page.locator("#display-name").fill("Attempt Student");
    await page.locator("#email").fill(email);
    await page.locator("#password").fill(PASSWORD);
    const policies = page.locator('fieldset input[type="checkbox"]');
    await expect(policies).toHaveCount(2);
    await policies.nth(0).check();
    await policies.nth(1).check();
    const requestedAt = new Date();
    await page.locator('form button[type="submit"]').click();
    await page.waitForURL((url) => url.pathname === "/verify-email");

    // The screen knows which challenge it is about and shows the masked
    // address, so it never asks for the address a second time.
    await expect(page.getByTestId("verification-masked-email")).toBeVisible();
    await expect(page.getByTestId("verification-masked-email")).not.toContainText(email);
    await expect(page.locator('input[type="password"]')).toHaveCount(0);
    await expect(page.locator('input[type="email"]')).toHaveCount(0);

    // The resend is metered, and the countdown is the server's own timestamp
    // rather than a local guess: the control is not offered while it runs.
    await expect(page.getByTestId("verification-resend-countdown")).toBeVisible();
    await expect(page.getByTestId("verification-resend-countdown")).toContainText(/\d+/);
    await expect(page.getByTestId("verification-resend")).toHaveCount(0);

    const code = await readVerificationCodeFor(email, requestedAt);
    const wrong = code === "000000" ? "111111" : "000000";
    const input = page.getByTestId("verification-code-input");

    // Wrong, unknown, expired and superseded all arrive as one refusal on
    // purpose — telling them apart would be an oracle — so the message is the
    // same one every time until the budget runs out.
    for (let attempt = 1; attempt <= 4; attempt += 1) {
      await input.fill(wrong);
      await page.getByTestId("verification-code-submit").click();
      await expect(page.getByRole("alert").first()).toContainText(en.auth.code.invalid);
      // The field is cleared for the next attempt rather than leaving a value
      // the Student has already been told is wrong.
      await expect(input).toHaveValue("");
    }

    // The fifth spends the budget, and the message changes to the one that
    // names the recovery: a new code.
    await input.fill(wrong);
    await page.getByTestId("verification-code-submit").click();
    await expect(page.getByRole("alert").first()).toContainText(en.auth.code.exhausted);

    // Even the correct code cannot revive an exhausted challenge, and no
    // session is created.
    await input.fill(code);
    await page.getByTestId("verification-code-submit").click();
    await expect(page.getByRole("alert").first()).toBeVisible();
    await expect(page).toHaveURL(/\/verify-email\?/);
  } finally {
    await context.close();
  }
});

/**
 * The route that used to let any browser put any mailbox into the sales queue.
 */
test("the anonymous purchase route is gone, not merely refused", async ({ request }) => {
  // 404, not 403. A 403 would mean the route is still mounted and is only
  // refusing this caller; the route itself was withdrawn.
  const response = await request.post(`${apiOrigin()}/api/v1/purchase-requests`, {
    data: { course_id: COURSE_ID, email: "anyone@example.test" },
    headers: { "Content-Type": "application/json" },
    failOnStatusCode: false,
  });
  expect(response.status()).toBe(404);
});
