import { execFileSync } from "child_process";
import fs from "fs";
import { test, expect, type BrowserContext, type Page } from "@playwright/test";
import {
  e2eDatabaseEnvironment,
  SEED_BINARY_PATH,
  RUN_STATE_FILE_PATH,
} from "../src/lib/api/e2e-infrastructure";

/**
 * Mandatory password change — launch-blocking remediation acceptance.
 *
 * The founder's manual test found that the bootstrap Administrator was stuck.
 * `cmd/bootstrap-admin` creates it with a CHANGE_REQUIRED credential and prints
 * "the first sign-in must change it", but there was no mounted route and no
 * screen to do that with. Login succeeded, the session resolved, and then every
 * privileged request answered 403 PASSWORD_CHANGE_REQUIRED — permanently.
 *
 * These cases drive the whole recovery through the real browser: sign in with
 * the temporary password, land on the mandatory change screen, confirm the
 * account is still refused privileged work before the change, change the
 * password, and confirm the credential reached ACTIVE and the account can now
 * do privileged work.
 *
 * Nothing here writes SQL. The credential state is read back through the
 * seeder's read-only query verb, so reaching ACTIVE is something the product
 * did, not something the test arranged.
 */

const RESTRICTED_ADMIN = {
  email: "bootstrap-admin@example.test",
  accountID: "a0000000-0000-0000-0000-000000000010",
};
const RESTRICTED_INSTRUCTOR = {
  email: "instructor-restricted@example.test",
  accountID: "a0000000-0000-0000-0000-000000000011",
};

/** The throwaway fixture password every seeded Account is created with. */
const TEMPORARY_PASSWORD = "StudentPassword123!";

/** Reads the Account's credential state without mutating anything. */
function credentialState(email: string): string {
  if (!fs.existsSync(RUN_STATE_FILE_PATH)) {
    throw new Error(`E2E run state is missing at ${RUN_STATE_FILE_PATH}.`);
  }
  const state = JSON.parse(fs.readFileSync(RUN_STATE_FILE_PATH, "utf-8"));
  const output = execFileSync(
    SEED_BINARY_PATH,
    ["-query-credential-state", "-email", email],
    {
      env: { ...process.env, ...e2eDatabaseEnvironment(state.dbName) },
      encoding: "utf-8",
    },
  );
  return JSON.parse(output.trim()).credential_state;
}

/**
 * The identity screens are not language-addressable: they render the visitor's
 * saved language, which defaults to Arabic. These assertions read English copy,
 * so the run states the preference a founder would set with the toggle.
 */
async function preferEnglish(context: BrowserContext): Promise<void> {
  await context.addInitScript(() => {
    window.localStorage.setItem("gradex.locale", "en");
  });
}

/** Signs in through the real login form, exactly as a person would. */
async function signIn(page: Page, email: string, password: string) {
  await page.goto("/login");
  await page.locator("#email").fill(email);
  await page.locator("#password").fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
}

/**
 * Asks the API for something only an unrestricted Administrator may have,
 * through the browser's own session, and reports the status.
 */
async function privilegedRequestStatus(page: Page, path: string) {
  return page.evaluate(async (target) => {
    const response = await fetch(target, {
      credentials: "same-origin",
      cache: "no-store",
      headers: { Accept: "application/json, application/problem+json" },
    });
    return response.status;
  }, path);
}

test.describe("S13 mandatory password change", () => {
  test("A the bootstrap Administrator escapes CHANGE_REQUIRED entirely in the browser", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await preferEnglish(context);
    const page = await context.newPage();

    // Precondition: the Account really is in the state bootstrap-admin leaves.
    expect(credentialState(RESTRICTED_ADMIN.email)).toBe("CHANGE_REQUIRED");

    await signIn(page, RESTRICTED_ADMIN.email, TEMPORARY_PASSWORD);

    // The defect, inverted. Signing in no longer drops the Administrator into
    // an application surface that only 403s; it lands on the one screen that
    // resolves the state.
    await expect(page).toHaveURL(/\/password-change/);
    await expect(page.getByTestId("password-change-current")).toBeVisible();
    await expect(page.getByTestId("password-change-new")).toBeVisible();
    await expect(page.getByTestId("password-change-confirm")).toBeVisible();

    // Before the change the Administrator still holds no Admin authority. The
    // screen is a route out of the restriction, not a relaxation of it.
    expect(await privilegedRequestStatus(page, "/api/v1/taxonomy/terms")).toBe(403);
    expect(await privilegedRequestStatus(page, "/api/v1/staff-invitations")).toBe(403);
    expect(credentialState(RESTRICTED_ADMIN.email)).toBe("CHANGE_REQUIRED");

    // A privileged surface reached directly is bounced back here rather than
    // rendering a screen full of refusals.
    await page.goto("/staff");
    await expect(page).toHaveURL(/\/password-change/);

    // The wrong current password is refused, and changes nothing.
    await page.getByTestId("password-change-current").fill("not the current password");
    await page.getByTestId("password-change-new").fill("a brand new launch passphrase 9");
    await page.getByTestId("password-change-confirm").fill("a brand new launch passphrase 9");
    await page.getByTestId("password-change-submit").click();
    await expect(page.getByTestId("password-change-error")).toBeVisible();
    await expect(page).toHaveURL(/\/password-change/);
    expect(credentialState(RESTRICTED_ADMIN.email)).toBe("CHANGE_REQUIRED");

    // The real change.
    await page.getByTestId("password-change-current").fill(TEMPORARY_PASSWORD);
    await page.getByTestId("password-change-new").fill("a brand new launch passphrase 9");
    await page.getByTestId("password-change-confirm").fill("a brand new launch passphrase 9");
    await page.getByTestId("password-change-submit").click();

    // Redirected off the change screen to the Administrator's own surface.
    await expect(page).toHaveURL(/\/staff/);

    // The credential really moved, in the database, because the product moved
    // it — no SQL was written by this test.
    expect(credentialState(RESTRICTED_ADMIN.email)).toBe("ACTIVE");

    // And the authority is real: the same requests that were refused minutes
    // ago now succeed on the rotated session.
    expect(await privilegedRequestStatus(page, "/api/v1/taxonomy/terms")).toBe(200);
    expect(await privilegedRequestStatus(page, "/api/v1/staff-invitations")).toBe(200);

    // The Administrator can now do the thing the launch needs: invite staff.
    await expect(page.getByText("Invite Instructor")).toBeVisible();

    // Finally, the credential itself changed hands: the temporary password no
    // longer authenticates and the chosen one does, signing in unrestricted.
    // This runs in the same case rather than a later one so the journey never
    // depends on another test having gone first.
    const second = await browser.newContext({ locale: "en-US" });
    await preferEnglish(second);
    const secondPage = await second.newPage();

    await signIn(secondPage, RESTRICTED_ADMIN.email, TEMPORARY_PASSWORD);
    await expect(
      secondPage.getByText("The email or password is incorrect."),
    ).toBeVisible();
    await expect(secondPage).toHaveURL(/\/login/);

    await secondPage.locator("#password").fill("a brand new launch passphrase 9");
    await secondPage.getByRole("button", { name: "Sign in" }).click();

    // Asserted as a positive: leaving the sign-in screen is what proves the new
    // password authenticates. A "not on the change screen" check would have
    // passed just as happily on a failed login that never left /login.
    await expect(secondPage).not.toHaveURL(/\/login/);
    await expect(secondPage).not.toHaveURL(/\/password-change/);
    expect(await privilegedRequestStatus(secondPage, "/api/v1/taxonomy/terms")).toBe(200);

    await second.close();
    await context.close();
  });

  test("B a restricted Instructor follows the same lifecycle to its own studio", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await preferEnglish(context);
    const page = await context.newPage();

    expect(credentialState(RESTRICTED_INSTRUCTOR.email)).toBe("CHANGE_REQUIRED");

    await signIn(page, RESTRICTED_INSTRUCTOR.email, TEMPORARY_PASSWORD);
    await expect(page).toHaveURL(/\/password-change/);

    // The authoring studio is refused before the change.
    expect(await privilegedRequestStatus(page, "/api/v1/courses")).toBe(403);

    await page.getByTestId("password-change-current").fill(TEMPORARY_PASSWORD);
    await page.getByTestId("password-change-new").fill("an instructor launch passphrase 4");
    await page.getByTestId("password-change-confirm").fill("an instructor launch passphrase 4");
    await page.getByTestId("password-change-submit").click();

    await expect(page).toHaveURL(/\/en\/instructor\/courses/);
    expect(credentialState(RESTRICTED_INSTRUCTOR.email)).toBe("ACTIVE");

    await expect(page.locator("h1")).toContainText("Course Authoring Studio");
    expect(await privilegedRequestStatus(page, "/api/v1/courses")).toBe(200);

    await context.close();
  });
});
