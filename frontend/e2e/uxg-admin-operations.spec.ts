import fs from "fs";
import path from "path";
import AxeBuilder from "@axe-core/playwright";
import {
  expect,
  test,
  request as playwrightRequest,
  type APIRequestContext,
  type BrowserContext,
  type Page,
} from "@playwright/test";
import { issueRotatingSession } from "./rotating-students";
import { frontendOrigin } from "../src/lib/api/e2e-ports";

/**
 * UX-G — the staff-facing operational surfaces, driven the way an Administrator drives them.
 *
 * The lifecycle behaviours themselves are already proven elsewhere: T8B walks a real invitation
 * through SMTP and a real suspension through a refused sign-in. What this suite is for is the part
 * those tests take for granted — that an Administrator can *operate* the product without reading an
 * identifier, without meeting a backend enum, and without a consequential action happening on one
 * unconfirmed click.
 *
 * Nothing here seeds application state through SQL. Where a fixture is needed it is created through
 * the same API the product calls, so an assertion that passes is an assertion about the product.
 */

const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };

const RUN = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;

/** A UUID in any of the shapes a leak would take. */
const UUID = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i;

/** Backend vocabulary that must never reach ordinary staff-facing copy. */
const RAW_ENUMS = [
  "PENDING_REVIEW",
  "CHANGES_REQUESTED",
  "ACCESS_ENDED",
  "RETIRED",
  "DELISTED",
  "SUPERSEDED",
  "NOT_STARTED",
  "INSTRUCTOR",
];

type Session = ReturnType<typeof issueRotatingSession>;

async function signInAdmin(context: BrowserContext, locale: "ar" | "en"): Promise<Session> {
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
  return session;
}

async function apiFor(session: Session): Promise<APIRequestContext> {
  return playwrightRequest.newContext({
    baseURL: frontendOrigin(),
    extraHTTPHeaders: {
      Accept: "application/json, application/problem+json",
      Origin: frontendOrigin(),
      Cookie: `${session.cookie_name}=${session.cookie_value}`,
      "X-CSRF-Token": session.csrf_token,
    },
  });
}

/**
 * Everything the page says, as the markup says it.
 *
 * `textContent`, deliberately, not `innerText`. `innerText` returns text as CSS renders it, and the
 * operational tables style their column headers `uppercase` — so a perfectly good "Instructor"
 * heading comes back as "INSTRUCTOR" and reads like the backend enum it is not. Enum leakage is a
 * property of what was written into the DOM, so that is what this reads.
 */
async function readableText(page: Page): Promise<string> {
  return (await page.locator("main").textContent()) ?? "";
}

test.describe("UX-G Admin staff operations", () => {
  test.describe.configure({ timeout: 90_000 });

  test("A an invitation is visible after it is sent, and can be taken back", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const session = await signInAdmin(context, "en");
    const page = await context.newPage();
    const recipient = `instructor+uxg-a-${RUN}@gradex.local`;

    await page.goto("/staff");
    await expect(page.getByRole("heading", { name: "Staff" })).toBeVisible();

    // The invitation queue exists at all — before this tranche the two routes behind it were
    // implemented on the server and reachable from nowhere in the product.
    const queue = page.getByTestId("staff-invitations");
    await expect(queue).toBeVisible();

    await page.getByTestId("staff-invite-email").fill(recipient);
    await page.getByTestId("staff-invite-submit").click();
    await expect(page.getByTestId("staff-notice")).toHaveAttribute("data-tone", "success", {
      timeout: 20_000,
    });

    const row = page.getByTestId("staff-invitation-row").filter({ hasText: recipient });
    await expect(row).toBeVisible({ timeout: 20_000 });
    // Who, as what, and when — and no identifier.
    await expect(row).toContainText("Instructor");
    await expect(row.innerText()).resolves.not.toMatch(UUID);

    // Cancelling is consequential, so it is confirmed — and the confirmation says what will
    // actually happen rather than restating the button.
    await row.getByTestId("staff-invitation-cancel").click();
    const dialog = page.getByTestId("staff-confirm");
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("stops working immediately");
    await dialog.getByTestId("confirm-accept").click();

    await expect(page.getByText("The invitation was cancelled.")).toBeVisible({ timeout: 20_000 });
    await expect(page.getByTestId("staff-invitation-row").filter({ hasText: recipient })).toHaveCount(
      0,
    );

    // And the server agrees: the invitation is gone from the pending queue, not merely hidden.
    const api = await apiFor(session);
    const pending = await (await api.get("/api/v1/staff-invitations")).json();
    expect(
      (pending.invitations as { email: string }[]).some((item) => item.email === recipient),
      "a cancelled invitation is still pending on the server",
    ).toBe(false);
    await api.dispose();

    await context.close();
  });

  test("B cancelling the confirmation leaves the invitation alone and returns focus", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const session = await signInAdmin(context, "en");
    const page = await context.newPage();
    const recipient = `instructor+uxg-b-${RUN}@gradex.local`;

    await page.goto("/staff");
    await page.getByTestId("staff-invite-email").fill(recipient);
    await page.getByTestId("staff-invite-submit").click();
    const row = page.getByTestId("staff-invitation-row").filter({ hasText: recipient });
    await expect(row).toBeVisible({ timeout: 20_000 });

    const cancelControl = row.getByTestId("staff-invitation-cancel");
    await cancelControl.click();
    await expect(page.getByTestId("staff-confirm")).toBeVisible();
    await page.getByTestId("confirm-cancel").click();
    await expect(page.getByTestId("staff-confirm")).toHaveCount(0);

    // Backing out of a confirmation must put the reader back on the control they opened it from,
    // not on the document body at the top of the page.
    await expect(cancelControl).toBeFocused();

    // And nothing happened.
    await expect(row).toBeVisible();
    const api = await apiFor(session);
    const pending = await (await api.get("/api/v1/staff-invitations")).json();
    expect(
      (pending.invitations as { email: string }[]).some((item) => item.email === recipient),
      "backing out of the confirmation revoked the invitation anyway",
    ).toBe(true);

    // Clean up through the product's own route so the queue does not accumulate across runs.
    await row.getByTestId("staff-invitation-cancel").click();
    await page.getByTestId("staff-confirm").getByTestId("confirm-accept").click();
    await expect(page.getByText("The invitation was cancelled.")).toBeVisible({ timeout: 20_000 });
    await api.dispose();
    await context.close();
  });

  test("C the staff workspace carries no identifier and no backend enum, in either language", async ({
    browser,
  }) => {
    for (const locale of ["en", "ar"] as const) {
      const context = await browser.newContext({
        locale: locale === "ar" ? "ar-KW" : "en-US",
      });
      await signInAdmin(context, locale);
      const page = await context.newPage();
      await page.goto(`/staff`);
      await expect(page.getByTestId("staff-workspace")).toBeVisible();
      // Wait for both directories to settle so the assertion covers rendered rows, not a spinner.
      await expect(page.getByTestId("staff-instructors")).toBeVisible();
      await page.waitForTimeout(1500);

      const text = await readableText(page);
      expect(text, `${locale}: an identifier is on screen`).not.toMatch(UUID);
      for (const raw of RAW_ENUMS) {
        expect(text, `${locale}: the raw enum ${raw} is on screen`).not.toContain(raw);
      }
      await context.close();
    }
  });

  test("D the staff workspace is in Arabic when the locale is Arabic", async ({ browser }) => {
    const context = await browser.newContext({ locale: "ar-KW" });
    await signInAdmin(context, "ar");
    const page = await context.newPage();
    await page.goto("/staff");

    await expect(page.getByRole("heading", { name: "فريق العمل" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "دعوة مدرّس" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "دعوات منتظرة" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "حسابات المدرّسين" })).toBeVisible();
    await expect(page.getByTestId("staff-workspace")).toHaveAttribute("dir", "rtl");

    await context.close();
  });

  test("E a failed operation never reads as a success", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await signInAdmin(context, "en");
    const page = await context.newPage();
    const recipient = `instructor+uxg-e-${RUN}@gradex.local`;

    await page.goto("/staff");
    await page.getByTestId("staff-invite-email").fill(recipient);
    await page.getByTestId("staff-invite-submit").click();
    const row = page.getByTestId("staff-invitation-row").filter({ hasText: recipient });
    await expect(row).toBeVisible({ timeout: 20_000 });

    // The server refuses the revoke. The screen must say so — a success banner over a failed
    // mutation is the one outcome an operational surface must never produce.
    await page.route("**/api/v1/staff-invitations/*", async (route) => {
      if (route.request().method() !== "DELETE") {
        await route.continue();
        return;
      }
      await route.fulfill({
        status: 503,
        contentType: "application/problem+json",
        body: JSON.stringify({
          type: "about:blank",
          title: "The invitation could not be cancelled.",
          detail: "The service is temporarily unavailable.",
          status: 503,
        }),
      });
    });

    await row.getByTestId("staff-invitation-cancel").click();
    await page.getByTestId("staff-confirm").getByTestId("confirm-accept").click();

    await expect(page.getByTestId("staff-notice")).toHaveAttribute("data-tone", "error", {
      timeout: 20_000,
    });
    // And the row is still there, because nothing was cancelled.
    await expect(row).toBeVisible();

    await page.unroute("**/api/v1/staff-invitations/*");
    await row.getByTestId("staff-invitation-cancel").click();
    await page.getByTestId("staff-confirm").getByTestId("confirm-accept").click();
    await expect(page.getByText("The invitation was cancelled.")).toBeVisible({ timeout: 20_000 });
    await context.close();
  });

  for (const width of [390, 1024, 1440]) {
    test(`F the staff workspace is usable at ${width}px`, async ({ browser }) => {
      const context = await browser.newContext({
        locale: "en-US",
        viewport: { width, height: 900 },
      });
      await signInAdmin(context, "en");
      const page = await context.newPage();
      await page.goto("/staff");
      await expect(page.getByTestId("staff-workspace")).toBeVisible();
      await page.waitForTimeout(1000);

      // The page never scrolls sideways. A table wider than its column scrolls inside its own
      // container instead, which is what `TableContainer` is for.
      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
      );
      expect(overflow, `the page scrolls sideways at ${width}px`).toBeLessThanOrEqual(1);
      await context.close();
    });
  }

  test("G the staff workspace carries no detectable accessibility violation", async ({ browser }) => {
    for (const locale of ["en", "ar"] as const) {
      const context = await browser.newContext({
        locale: locale === "ar" ? "ar-KW" : "en-US",
      });
      await signInAdmin(context, locale);
      const page = await context.newPage();
      await page.goto("/staff");
      await expect(page.getByTestId("staff-workspace")).toBeVisible();
      await page.waitForTimeout(1500);
      const results = await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
        .analyze();
      expect(
        results.violations.map((violation) => `${violation.id}: ${violation.help}`),
        `${locale}: axe violations on the staff workspace`,
      ).toEqual([]);
      await context.close();
    }
  });
});

/* ------------------------------------------------------------------ evidence */

/**
 * Screenshots of the staff-facing surfaces, at the three widths and in both languages, written
 * where a reviewer can open them. `GRADEX_UXG_EVIDENCE_DIR` puts them somewhere durable; without it
 * they land in the run's own output directory and are attached to the report either way.
 */
const SHOTS = [["staff", "/staff", 390, 900], ["staff", "/staff", 1024, 900], ["staff", "/staff", 1440, 1000]] as const;

for (const [name, route, width, height] of SHOTS) {
  for (const locale of ["en", "ar"] as const) {
    for (const theme of ["light", "dark"] as const) {
      test(`evidence: ${name} at ${width}px in ${locale} ${theme}`, async ({ browser }, testInfo) => {
        const context = await browser.newContext({
          locale: locale === "ar" ? "ar-KW" : "en-US",
          viewport: { width, height },
          colorScheme: theme,
        });
        await signInAdmin(context, locale);
        await context.addInitScript(
          (value) => window.localStorage.setItem("theme", value),
          theme,
        );
        const page = await context.newPage();
        await page.goto(route);
        await expect(page.getByTestId("staff-workspace")).toBeVisible();
        await page.waitForTimeout(1500);

        const file = path.join(
          process.env.GRADEX_UXG_EVIDENCE_DIR || testInfo.outputDir,
          `uxg-${name}-${locale}-${theme}-${width}.png`,
        );
        fs.mkdirSync(path.dirname(file), { recursive: true });
        const shot = await page.screenshot({ fullPage: true, path: file });
        await testInfo.attach(`uxg-${name}-${locale}-${theme}-${width}`, {
          body: shot,
          contentType: "image/png",
        });
        await context.close();
      });
    }
  }
}

test.describe("UX-G consequential staff actions", () => {
  test("H suspension cannot be confirmed without the reason the server requires", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await signInAdmin(context, "en");
    const page = await context.newPage();

    // Every call the screen makes to the suspension route, so "exactly once" is measured rather
    // than assumed.
    const suspensions: string[] = [];
    await page.route("**/api/v1/accounts/*/suspension", async (route) => {
      suspensions.push(route.request().method());
      await route.fulfill({
        status: 503,
        contentType: "application/problem+json",
        body: JSON.stringify({
          type: "about:blank",
          title: "The account could not be changed.",
          detail: "The service is temporarily unavailable.",
          status: 503,
        }),
      });
    });

    await page.goto("/staff");
    const row = page.getByTestId("staff-instructor-row").first();
    await expect(row).toBeVisible({ timeout: 20_000 });
    await row.getByTestId("staff-instructor-suspend").click();

    const dialog = page.getByTestId("staff-confirm");
    await expect(dialog).toBeVisible();
    // The confirmation names the effect, not the button.
    await expect(dialog).toContainText("signed out everywhere immediately");
    // And it refuses to proceed until the recorded reason exists, rather than sending a request
    // the server can only refuse.
    await expect(dialog.getByTestId("confirm-accept")).toBeDisabled();
    expect(suspensions, "a request was sent before the reason was given").toEqual([]);

    await dialog.getByTestId("staff-instructor-reason").fill("UX-G reason");
    await expect(dialog.getByTestId("confirm-accept")).toBeEnabled();
    await dialog.getByTestId("confirm-accept").click();

    // One call, and a refusal that reads as a refusal.
    await expect(page.getByTestId("staff-notice")).toHaveAttribute("data-tone", "error", {
      timeout: 20_000,
    });
    expect(suspensions.length, "the confirmed action did not call the API exactly once").toBe(1);
    // Nothing changed, so the account is still active.
    await expect(page.getByTestId("staff-instructor-row").first()).toContainText("Active");

    await context.close();
  });
});

