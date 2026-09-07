import { expect, test, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

/**
 * D-100 — the password floor, the reveal control, and the accepted reset request.
 *
 * Three reported defects, all of them things a unit test can only half prove:
 *
 *   1. the product asked for fifteen characters where eight is the policy,
 *   2. the reveal control sat on top of the value in Arabic, and
 *   3. an accepted reset request left the address and a live Send button on screen, so a finished
 *      step looked unfinished and was pressed again.
 *
 * The first is asserted here only at the boundary the reader meets — the refusal a form shows —
 * because the agreement between the two layers is proved in
 * `src/components/auth/password-policy-and-recovery.test.ts`, which reads the Go constant directly.
 */

const RESET_ENDPOINT = "**/api/v1/password-reset-requests";

async function withLocale(page: Page, locale: "ar" | "en") {
  await page.addInitScript((selected) => {
    window.localStorage.setItem("gradex.locale", selected as string);
  }, locale);
}

/* --------------------------------------------------------- the reveal control */

test.describe("the password reveal control", () => {
  for (const locale of ["en", "ar"] as const) {
    test(`does not sit on top of the value in ${locale}`, async ({ page }) => {
      await withLocale(page, locale);
      await page.goto("/register");

      const field = page.locator("#password");
      await expect(field).toBeVisible();
      // Long enough to reach the edge of the field in either direction.
      await field.fill("passphrase-that-runs-the-width-of-the-field");

      const control = page.getByRole("button", { name: /show password|إظهار كلمة المرور/i });
      await expect(control).toBeVisible();

      const box = (await field.boundingBox())!;
      const button = (await control.boundingBox())!;

      // The control is inside the field's box, at one of its inline edges.
      expect(button.x).toBeGreaterThanOrEqual(box.x - 1);
      expect(button.x + button.width).toBeLessThanOrEqual(box.x + box.width + 1);

      // And the field reserves that space, so the value cannot run underneath it. The reserved
      // side is read from the computed padding rather than assumed, which is the whole defect:
      // English reserves the right, Arabic reserves the left.
      const padding = await field.evaluate((element) => {
        const style = window.getComputedStyle(element);
        return {
          left: parseFloat(style.paddingLeft),
          right: parseFloat(style.paddingRight),
        };
      });
      const controlOnTheLeft = button.x < box.x + box.width / 2;
      const reserved = controlOnTheLeft ? padding.left : padding.right;
      expect(
        reserved,
        `the ${locale} field reserves no room on the side the reveal control is on`,
      ).toBeGreaterThanOrEqual(button.width - 1);

      if (locale === "ar") {
        // Arabic reads right to left, so the trailing edge — where the control belongs — is the
        // left. This is the reported defect, stated as an assertion.
        expect(controlOnTheLeft, "the Arabic reveal control is not on the trailing edge").toBe(
          true,
        );
      } else {
        expect(controlOnTheLeft, "the English reveal control is not on the trailing edge").toBe(
          false,
        );
      }
    });
  }

  test("is masked by default, reachable by keyboard, and says which state it is in", async ({
    page,
  }) => {
    await withLocale(page, "en");
    await page.goto("/register");

    const field = page.locator("#password");
    await field.fill("a-real-passphrase");
    await expect(field).toHaveAttribute("type", "password");

    const control = page.getByRole("button", { name: "Show password" });
    await expect(control).toHaveAttribute("aria-pressed", "false");

    const before = (await field.boundingBox())!;
    await control.focus();
    await expect(control).toBeFocused();
    await page.keyboard.press("Enter");

    await expect(field).toHaveAttribute("type", "text");
    const revealed = page.getByRole("button", { name: "Hide password" });
    await expect(revealed).toHaveAttribute("aria-pressed", "true");

    // Nothing moves when it is toggled.
    const after = (await field.boundingBox())!;
    expect(Math.abs(after.x - before.x)).toBeLessThanOrEqual(1);
    expect(Math.abs(after.width - before.width)).toBeLessThanOrEqual(1);

    await page.keyboard.press("Enter");
    await expect(field).toHaveAttribute("type", "password");
  });
});

/* ------------------------------------------------------------ the eight-character floor */

test.describe("the password policy the reader meets", () => {
  test("eight characters are accepted where seven are refused", async ({ page }) => {
    await withLocale(page, "en");
    await page.goto("/register");

    await expect(page.locator("#password-hint")).toContainText("8");
    await expect(page.locator("#password-hint")).not.toContainText("15");

    await page.locator("#display-name").fill("Fahd Al-Mutairi");
    await page.locator("#email").fill("reader@example.test");

    await page.locator("#password").fill("frt9xqz");
    await page.getByRole("button", { name: "Create account" }).click();
    await expect(page.locator("#password-error")).toContainText("8");

    await page.locator("#password").fill("frt9xqzm");
    // Eight characters clear the client rule: whatever the form complains about next, it is not
    // the password.
    await page.getByRole("button", { name: "Create account" }).click();
    await expect(page.locator("#password-error")).toHaveCount(0);
  });
});

/* ------------------------------------------------------- the accepted reset request */

test.describe("asking for a reset link", () => {
  for (const locale of ["en", "ar"] as const) {
    test(`an accepted request replaces the form in ${locale}`, async ({ page }) => {
      let requests = 0;
      await page.route(RESET_ENDPOINT, async (route) => {
        requests += 1;
        await route.fulfill({ json: { code: "PASSWORD_RESET_REQUEST_ACCEPTED" } });
      });
      await withLocale(page, locale);
      await page.goto("/recover");

      await expect(page.getByTestId("recovery-request")).toBeVisible();
      await page.locator("#recovery-email").fill("someone@example.test");
      await page.getByTestId("recovery-send").click();

      const accepted = page.getByTestId("recovery-accepted");
      await expect(accepted).toBeVisible();
      expect(requests).toBe(1);

      // The email field and the original send control are gone, not merely annotated.
      await expect(page.locator("#recovery-email")).toHaveCount(0);
      await expect(page.getByTestId("recovery-send")).toHaveCount(0);

      // What is left is one thing to read and two things to do.
      await expect(accepted.getByRole("status")).toBeVisible();
      await expect(page.getByTestId("recovery-resend")).toBeVisible();
      await expect(accepted.getByRole("link")).toHaveCount(1);

      // Nothing on it asserts that an account exists.
      const text = await page.locator("#main, body").first().innerText();
      expect(text).not.toContain("someone@example.test");
      if (locale === "en") {
        expect(text.toLowerCase()).not.toContain("your account");
      }
    });
  }

  test("the same words are shown whether or not the address has an account", async ({
    browser,
  }) => {
    const readings: string[] = [];
    for (const email of ["known@example.test", "unknown@example.test"]) {
      const context = await browser.newContext({ locale: "en-US" });
      const page = await context.newPage();
      await page.route(RESET_ENDPOINT, (route) =>
        route.fulfill({ json: { code: "PASSWORD_RESET_REQUEST_ACCEPTED" } }),
      );
      await withLocale(page, "en");
      await page.goto("/recover");
      await page.locator("#recovery-email").fill(email);
      await page.getByTestId("recovery-send").click();
      await expect(page.getByTestId("recovery-accepted")).toBeVisible();
      readings.push(await page.getByTestId("recovery-accepted").innerText());
      await context.close();
    }
    expect(readings[0]).toBe(readings[1]);
  });

  test("a server error keeps the form and does not claim the request was taken", async ({
    page,
  }) => {
    await page.route(RESET_ENDPOINT, (route) =>
      route.fulfill({
        status: 503,
        contentType: "application/problem+json",
        json: { type: "about:blank", title: "Unavailable", status: 503 },
      }),
    );
    await withLocale(page, "en");
    await page.goto("/recover");

    await page.locator("#recovery-email").fill("someone@example.test");
    await page.getByTestId("recovery-send").click();

    await expect(page.getByRole("alert").first()).toBeVisible();
    await expect(page.getByTestId("recovery-accepted")).toHaveCount(0);
    // The address the reader typed is still there to retry with.
    await expect(page.locator("#recovery-email")).toHaveValue("someone@example.test");
    await expect(page.getByTestId("recovery-send")).toBeVisible();
  });

  test("resending re-issues the same request and never withdraws the accepted state", async ({
    page,
  }) => {
    let requests = 0;
    await page.route(RESET_ENDPOINT, async (route) => {
      requests += 1;
      if (requests === 1) {
        await route.fulfill({ json: { code: "PASSWORD_RESET_REQUEST_ACCEPTED" } });
        return;
      }
      await route.fulfill({
        status: 429,
        contentType: "application/problem+json",
        json: { type: "about:blank", title: "Too many", status: 429, code: "RATE_LIMITED" },
      });
    });
    await withLocale(page, "en");
    await page.goto("/recover");

    await page.locator("#recovery-email").fill("someone@example.test");
    await page.getByTestId("recovery-send").click();
    await expect(page.getByTestId("recovery-accepted")).toBeVisible();

    await page.getByTestId("recovery-resend").click();
    await expect(page.getByRole("alert").first()).toContainText("Too many attempts");
    // The first request the server took is still live, so the accepted screen stands.
    await expect(page.getByTestId("recovery-accepted")).toBeVisible();
    await expect(page.locator("#recovery-email")).toHaveCount(0);
    expect(requests).toBe(2);
  });

  test("the accepted screen has no axe violations in either language", async ({ page }) => {
    for (const locale of ["en", "ar"] as const) {
      await page.route(RESET_ENDPOINT, (route) =>
        route.fulfill({ json: { code: "PASSWORD_RESET_REQUEST_ACCEPTED" } }),
      );
      await withLocale(page, locale);
      await page.goto("/recover");
      await page.locator("#recovery-email").fill("someone@example.test");
      await page.getByTestId("recovery-send").click();
      await expect(page.getByTestId("recovery-accepted")).toBeVisible();

      const results = await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
        .analyze();
      expect(
        results.violations.map((violation) => `${locale}: ${violation.id}`),
        JSON.stringify(results.violations, null, 2),
      ).toEqual([]);
    }
  });

  for (const width of [1440, 390]) {
    test(`the accepted screen fits ${width}px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 844 });
      await page.route(RESET_ENDPOINT, (route) =>
        route.fulfill({ json: { code: "PASSWORD_RESET_REQUEST_ACCEPTED" } }),
      );
      await withLocale(page, "ar");
      await page.goto("/recover");
      await page.locator("#recovery-email").fill("someone@example.test");
      await page.getByTestId("recovery-send").click();
      await expect(page.getByTestId("recovery-accepted")).toBeVisible();

      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
      );
      expect(overflow).toBeLessThanOrEqual(1);
    });
  }
});
