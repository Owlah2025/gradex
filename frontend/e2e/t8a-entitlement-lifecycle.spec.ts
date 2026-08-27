import { test, expect, type Browser, type BrowserContext, type Page } from "@playwright/test";
import { queryLearningState, type LearningStateSnapshot } from "../src/lib/api/e2e-progress";
import { frontendOrigin } from "../src/lib/api/e2e-ports";
import {
  authenticateRotatingStudent,
  issueRotatingSession,
  studentFor,
  ENTITLEMENT_EXTEND_TEST_SLOT,
  ENTITLEMENT_PAST_DATE_TEST_SLOT,
  ENTITLEMENT_REVOKE_TEST_SLOT,
  ENTITLEMENT_SHORTEN_TEST_SLOT,
  type RotatingStudent,
} from "./rotating-students";

/**
 * T8A / MVP-F24A — AD-09, the Admin Entitlement lifecycle, proved as a browser journey.
 *
 * # WHAT THIS FILE OWNS, AND WHAT IT DELIBERATELY DOES NOT
 *
 * The Entitlement operations themselves are already proved against real PostgreSQL by
 * `backend/internal/httpapi/entitlement_operations_integration_test.go`: the adjustment and
 * revocation audit rows, the outbox events `access.entitlement_adjusted` and
 * `access.entitlement_revoked`, the revision counter and its optimistic-concurrency refusal, the
 * invalid-transition refusals, and the production HTTP path. Repeating any of that here would move
 * a proof to a weaker layer, not add one.
 *
 * What only the real product can answer is the part below: that a human Admin can *reach* an
 * existing grant from the queue without handling an identifier, perform each BR-026 operation
 * through the actual form, see the result on a genuinely refetched screen, and that the Student's
 * access then changes — or does not — exactly as the operation says.
 *
 * # ONE STUDENT PER OPERATION
 *
 * Each case mutates the Entitlement it acts on, so each takes its own rotating slot (30-33). A
 * shared Student would mean the third case opening an Entitlement the second case had already
 * shortened, and the fourth opening one the third had already ended.
 *
 * # DATES ARE THE SERVER'S TO INTERPRET
 *
 * The form submits a calendar date; `access.ConvertKuwaitDateToUTCBoundary` turns date D into the
 * start of D+1 in Asia/Kuwait, expressed as UTC — so `2027-06-30` becomes `2027-06-30T21:00:00Z`.
 * That is the canonical instant, and the assertions below compare against it rather than against
 * a browser-local midnight. Every date is derived from the run instant so no case expires with
 * the calendar.
 */

const ADMIN_EMAIL = "admin@example.test";
const ADMIN_ID = "a0000000-0000-0000-0000-000000000000";

const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const COURSE_TITLE_EN = "CS101: Introduction to Programming";
const LESSON_ID = "30000000-0000-0000-0000-000000000001";

/** Known to the Node runner only, so the one Progress write can send the production payload. */
const RUNNER_ONLY_ASSET_VERSION_ID = "60000000-0000-0000-0000-000000000001";
const RETAINED_POSITION_SECONDS = 120;

/**
 * The access badge, matched on the state it renders as well as its copy. `LearningStatusBadge`
 * publishes `data-learning-status`, so the state and the words it chose are asserted together —
 * either alone would pass while the other regressed (GAP-04).
 */
const ACTIVE_BADGE = "Active access";
const EXPIRED_BADGE = "Access expired";
const ACTIVE_BADGE_SELECTOR = '[data-learning-status="active"]';
const EXPIRED_BADGE_SELECTOR = '[data-learning-status="expired"]';
/** Every ending other than an expired Entitlement renders the generic state, which names no cause. */
const UNAVAILABLE_HEADING = "Learning is unavailable";

/**
 * T7 / GAP-03 kept the whole learning dictionary, the report context and the oversized CourseHome
 * props out of the payload. Case C re-renders the expired surface, so it re-audits that boundary:
 * a regression there would land here first.
 */
const PROHIBITED_IN_EXPIRED_RENDER = [
  "report_context",
  "asset_version_id",
  "entitlement_id",
  "enrollment_id",
  "revision_id",
  "object_key",
  "storage_path",
  "playback_session",
  "can_play",
  "can_update_progress",
  RUNNER_ONLY_ASSET_VERSION_ID,
];

// Admin queue reads, four modal round trips, protected learning in two access states, and Next.js
// dev-mode first compilation of the Admin and learning routes.
test.describe.configure({ timeout: 180_000 });

function normalize(text: string): string {
  return text.toLowerCase().replace(/[^\p{L}\p{N}]/gu, "");
}

/** `YYYY-MM-DD`, the only shape the expiry form submits. */
function calendarDate(daysFromNow: number): string {
  const at = new Date(Date.now() + daysFromNow * 24 * 60 * 60 * 1000);
  return at.toISOString().slice(0, 10);
}

/**
 * The canonical instant a submitted date resolves to: midnight at the *end* of that Kuwait day,
 * which is 21:00 UTC on the date itself. This mirrors `ConvertKuwaitDateToUTCBoundary`; it does
 * not reimplement a policy, it states the one the server owns so the test can check it.
 */
function kuwaitBoundaryInstant(date: string): Date {
  const [year, month, day] = date.split("-").map(Number);
  return new Date(Date.UTC(year, month - 1, day, 21, 0, 0));
}

/**
 * The calendar day that instant falls on in Asia/Kuwait, formatted the way the product formats it.
 *
 * This used to hardcode `en-US` while calling itself "what the Admin screen renders", which was
 * true only for as long as the screen called `toLocaleString()` with whatever locale the browser
 * happened to carry. The product now has one date format, and Kuwait writes the day before the
 * month, so the assertion mirrors `src/lib/i18n/format.ts` rather than asserting a rendering no
 * screen produces.
 */
function displayedKuwaitDate(instant: Date): string {
  return new Intl.DateTimeFormat("en-GB", {
    dateStyle: "medium",
    numberingSystem: "latn",
    timeZone: "Asia/Kuwait",
  }).format(instant);
}

/**
 * Installs a production-valid session, exactly as `authenticateRotatingStudent` does for a
 * Student. The Admin is a fixed seeded Account rather than a pool slot, so it gets its own call
 * rather than being forced through the rotating-Student shape.
 */
async function authenticateAdmin(context: BrowserContext): Promise<void> {
  const session = issueRotatingSession({ accountID: ADMIN_ID, email: ADMIN_EMAIL });
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
}

/**
 * The AD-07 discovery path, driven the way an Admin has it: find the person on the Course Access
 * queue by their email, and press the control on their row. No Entitlement, Enrollment, Course or
 * Account identifier is typed anywhere in this journey.
 */
async function openManageAccess(adminPage: Page, student: RotatingStudent): Promise<void> {
  await adminPage.goto("/en/admin/course-access");
  const row = adminPage.locator(`tr:has-text("${student.email}")`);
  await expect(
    row,
    `${student.email} is not reachable on the Admin Course Access queue`,
  ).toHaveCount(1);
  await row.getByRole("button", { name: /Manage access/ }).click();
  const detail = adminPage.getByTestId("entitlement-detail");
  await expect(detail).toBeVisible();
  await expect(detail).toContainText("CS101");
}

/**
 * Reopens the record from the queue after a mutation. `setDetailModal(updated)` already shows the
 * mutation's own response; this proves the change is what a *later* read returns, which is the
 * only thing that distinguishes a persisted mutation from an optimistic one.
 */
async function refetchManageAccess(adminPage: Page, student: RotatingStudent): Promise<void> {
  await adminPage.reload();
  await openManageAccess(adminPage, student);
}

async function submitExpiry(
  adminPage: Page,
  date: string,
  reason: string,
  reference: string,
): Promise<void> {
  await adminPage.locator("#entitlement-expiry-date").fill(date);
  await adminPage.locator("#entitlement-expiry-reason").fill(reason);
  await adminPage.locator("#entitlement-expiry-reference").fill(reference);
  await adminPage.getByTestId("save-entitlement-expiry").click();
  await expect(adminPage.getByTestId("entitlement-notice")).toContainText("The access end date was changed.");
  await expect(adminPage.getByTestId("entitlement-error")).toHaveCount(0);
}

/** The Student's own browser, holding a real session in the real cookie. */
async function openStudentPage(browser: Browser, student: RotatingStudent): Promise<{ context: BrowserContext; page: Page }> {
  const context = await browser.newContext({
    locale: "en-US",
    // Pinned so the Admin screen's local rendering of the canonical instant is a fact about the
    // product, not about the machine the suite happens to run on.
    timezoneId: "Asia/Kuwait",
  });
  await authenticateRotatingStudent(context, student);
  return { context, page: await context.newPage() };
}

async function openAdminPage(browser: Browser): Promise<{ context: BrowserContext; page: Page }> {
  const context = await browser.newContext({ locale: "en-US", timezoneId: "Asia/Kuwait" });
  // The workspace is bilingual now, so a spec that means English has to say so. `LocaleProvider`
  // reads the saved language before the browser's, the same as every other Admin suite here.
  await context.addInitScript(() => {
    window.localStorage.setItem("gradex.locale", "en");
  });
  await authenticateAdmin(context);
  return { context, page: await context.newPage() };
}

function entitlementState(student: RotatingStudent): LearningStateSnapshot["entitlement"] {
  const snapshot = queryLearningState(student.accountID, COURSE_ID);
  expect(snapshot.entitlement.found, `${student.email} has no seeded Entitlement`).toBe(true);
  return snapshot.entitlement;
}

/** Protected learning, entered the way a Student enters it. */
async function expectActiveLearning(page: Page): Promise<void> {
  await page.goto(`/en/learn/courses/${COURSE_ID}`);
  await expect(page.getByRole("heading", { name: COURSE_TITLE_EN })).toBeVisible();
  const badge = page.locator(ACTIVE_BADGE_SELECTOR);
  await expect(badge).toBeVisible();
  await expect(badge).toHaveText(ACTIVE_BADGE);
}

test.describe("T8A / MVP-F24A — AD-09 Admin Entitlement lifecycle", () => {
  /**
   * Case A — extension. A later expiry keeps the Student learning, and the Admin sees the new
   * boundary on a refetched record.
   */
  test("A: an Admin extends an active grant and the Student keeps access", async ({ browser }, testInfo) => {
    const student = studentFor(testInfo, ENTITLEMENT_EXTEND_TEST_SLOT);
    const before = entitlementState(student);
    expect(before.state).toBe("ACTIVE");

    const admin = await openAdminPage(browser);
    const learner = await openStudentPage(browser, student);
    try {
      // The Student is learning before the change, so the "after" answer means something.
      await expectActiveLearning(learner.page);

      await openManageAccess(admin.page, student);
      await expect(admin.page.getByTestId("entitlement-state")).toContainText("Access is active");
      await expect(admin.page.getByTestId("entitlement-access-ends-at")).toContainText(
        displayedKuwaitDate(new Date(before.access_ends_at)),
      );

      const extendedTo = calendarDate(365);
      await submitExpiry(admin.page, extendedTo, "T8A E2E entitlement extension", "T8A-EXTEND");

      // The server's boundary, on a record read back from the API rather than echoed by the form.
      await refetchManageAccess(admin.page, student);
      await expect(admin.page.getByTestId("entitlement-state")).toContainText("Access is active");
      await expect(admin.page.getByTestId("entitlement-access-ends-at")).toContainText(
        displayedKuwaitDate(kuwaitBoundaryInstant(extendedTo)),
      );

      const after = entitlementState(student);
      expect(new Date(after.access_ends_at).toISOString()).toBe(
        kuwaitBoundaryInstant(extendedTo).toISOString(),
      );
      expect(new Date(after.access_ends_at).getTime()).toBeGreaterThan(
        new Date(before.access_ends_at).getTime(),
      );
      // The grant was adjusted, not re-issued: same Entitlement, original grant preserved.
      expect(after.id).toBe(before.id);
      expect(after.original_access_ends_at).toBe(before.original_access_ends_at);

      await expectActiveLearning(learner.page);
    } finally {
      await admin.context.close();
      await learner.context.close();
    }
  });

  /**
   * Case B — shortening while access stays open. The distinguishing claim: an earlier date is not
   * a revocation. The Student must still be learning afterwards.
   */
  test("B: an Admin shortens an active grant to a future date and access continues", async ({ browser }, testInfo) => {
    const student = studentFor(testInfo, ENTITLEMENT_SHORTEN_TEST_SLOT);
    const before = entitlementState(student);
    expect(before.state).toBe("ACTIVE");

    const admin = await openAdminPage(browser);
    const learner = await openStudentPage(browser, student);
    try {
      await expectActiveLearning(learner.page);

      await openManageAccess(admin.page, student);

      // Ten days out: earlier than the seeded thirty-day window, and far enough from today that a
      // slow run can never carry the boundary into the past mid-test.
      const shortenedTo = calendarDate(10);
      expect(
        kuwaitBoundaryInstant(shortenedTo).getTime(),
        "the shortened boundary must still be earlier than the seeded one",
      ).toBeLessThan(new Date(before.access_ends_at).getTime());

      await submitExpiry(admin.page, shortenedTo, "T8A E2E entitlement shortening", "T8A-SHORTEN");

      await refetchManageAccess(admin.page, student);
      await expect(admin.page.getByTestId("entitlement-state")).toContainText("Access is active");
      await expect(admin.page.getByTestId("entitlement-access-ends-at")).toContainText(
        displayedKuwaitDate(kuwaitBoundaryInstant(shortenedTo)),
      );
      await expect(admin.page.getByTestId("entitlement-revoked-at")).toHaveCount(0);

      const after = entitlementState(student);
      expect(new Date(after.access_ends_at).toISOString()).toBe(
        kuwaitBoundaryInstant(shortenedTo).toISOString(),
      );
      expect(new Date(after.access_ends_at).getTime()).toBeLessThan(
        new Date(before.access_ends_at).getTime(),
      );
      expect(after.state).toBe("ACTIVE");

      // The claim of this case: shortening to a still-open period is not an ending.
      await expectActiveLearning(learner.page);
    } finally {
      await admin.context.close();
      await learner.context.close();
    }
  });

  /**
   * Case C — a date already past ends access immediately (BR-026), while Enrollment and Progress
   * are kept as history. The Student's surface is the retained-expired presentation, and it must
   * still carry none of what T7 removed from it.
   */
  test("C: an Admin moves expiry into the past and the Student loses access at once", async ({ browser }, testInfo) => {
    const student = studentFor(testInfo, ENTITLEMENT_PAST_DATE_TEST_SLOT);
    const before = entitlementState(student);
    expect(before.state).toBe("ACTIVE");

    const admin = await openAdminPage(browser);
    const learner = await openStudentPage(browser, student);
    try {
      await expectActiveLearning(learner.page);

      // Real Progress, written through the production learning route by the authenticated Student,
      // so "Progress is preserved" is a claim about a row this journey actually created.
      const progressStatus = await learner.page.evaluate(async (args) => {
        const response = await fetch(`/api/v1/learn/lessons/${args.lessonID}/progress`, {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            position_seconds: args.position,
            asset_version_id: args.assetVersionID,
          }),
        });
        return response.status;
      }, {
        lessonID: LESSON_ID,
        position: RETAINED_POSITION_SECONDS,
        assetVersionID: RUNNER_ONLY_ASSET_VERSION_ID,
      });
      expect([200, 204]).toContain(progressStatus);
      expect(queryLearningState(student.accountID, COURSE_ID).progress.length).toBeGreaterThan(0);

      await openManageAccess(admin.page, student);
      const endedOn = calendarDate(-5);
      await submitExpiry(admin.page, endedOn, "T8A E2E past-date expiry", "T8A-PAST");

      await refetchManageAccess(admin.page, student);
      await expect(admin.page.getByTestId("entitlement-access-ends-at")).toContainText(
        displayedKuwaitDate(kuwaitBoundaryInstant(endedOn)),
      );

      const after = entitlementState(student);
      expect(new Date(after.access_ends_at).toISOString()).toBe(
        kuwaitBoundaryInstant(endedOn).toISOString(),
      );
      expect(new Date(after.access_ends_at).getTime()).toBeLessThan(Date.now());
      // Ending access is not deletion: the record, the Enrollment and the Progress all survive.
      expect(after.id).toBe(before.id);
      const retained = queryLearningState(student.accountID, COURSE_ID);
      expect(retained.enrollment.found).toBe(true);
      expect(retained.progress.length).toBeGreaterThan(0);

      // The Student, on the very next navigation.
      await learner.page.goto(`/en/learn/courses/${COURSE_ID}`);
      const expiredBadge = learner.page.locator(EXPIRED_BADGE_SELECTOR);
      await expect(expiredBadge).toBeVisible();
      await expect(expiredBadge).toHaveText(EXPIRED_BADGE);
      await expect(learner.page.locator(ACTIVE_BADGE_SELECTOR)).toHaveCount(0);

      const visible = (await learner.page.locator("main").innerText()) || "";
      expect(visible, "the expired surface rendered the active badge").not.toContain(ACTIVE_BADGE);

      // T7 / GAP-03 regression: the served payload, not only the rendered text.
      const html = normalize(await learner.page.content());
      for (const field of PROHIBITED_IN_EXPIRED_RENDER) {
        expect(html, `the expired render leaked ${field}`).not.toContain(normalize(field));
      }
      // The whole `dictionary.learning` reaching the payload is what T7 removed. `noExpiry` is
      // rendered by no access state this page can be in, so its presence means the catalogue —
      // not the narrow label set — was handed to a component again.
      expect(html, "the expired render carried the whole learning dictionary").not.toContain(
        normalize("No expiry date"),
      );
    } finally {
      await admin.context.close();
      await learner.context.close();
    }
  });

  /**
   * Case D — revocation. Access ends, the record stays as history, and the Student's surface is
   * the generic unavailable state that names no cause.
   */
  test("D: an Admin revokes a grant and the Student is refused", async ({ browser }, testInfo) => {
    const student = studentFor(testInfo, ENTITLEMENT_REVOKE_TEST_SLOT);
    const before = entitlementState(student);
    expect(before.state).toBe("ACTIVE");

    const admin = await openAdminPage(browser);
    const learner = await openStudentPage(browser, student);
    try {
      await expectActiveLearning(learner.page);

      await openManageAccess(admin.page, student);
      await admin.page.locator("#entitlement-revoke-reason").fill("T8A E2E entitlement revocation");
      await admin.page.locator("#entitlement-revoke-reference").fill("T8A-REVOKE");
      await admin.page.getByTestId("revoke-entitlement").click();
      await admin.page
        .getByTestId("confirm-revoke-entitlement")
        .getByTestId("confirm-accept")
        .click();
      await expect(admin.page.getByTestId("entitlement-notice")).toContainText("Access was ended");
      // A success notice and a failure notice are the same element with a different tone, so this
      // asserts the tone rather than the absence of a separate error element.
      await expect(admin.page.getByTestId("entitlement-notice")).toHaveAttribute(
        "data-tone",
        "success",
      );

      await refetchManageAccess(admin.page, student);
      await expect(admin.page.getByTestId("entitlement-state")).toContainText("Access was ended");
      await expect(admin.page.getByTestId("entitlement-revoked-at")).toBeVisible();
      // A terminal record offers no further operation.
      await expect(admin.page.getByTestId("entitlement-expiry-form")).toHaveCount(0);
      await expect(admin.page.getByTestId("entitlement-revoke-form")).toHaveCount(0);

      const after = entitlementState(student);
      expect(after.state).toBe("REVOKED");
      expect(after.revoked_at).toBeTruthy();
      expect(after.id).toBe(before.id);
      expect(queryLearningState(student.accountID, COURSE_ID).enrollment.found).toBe(true);

      await learner.page.goto(`/en/learn/courses/${COURSE_ID}`);
      await expect(learner.page.getByRole("heading", { name: UNAVAILABLE_HEADING })).toBeVisible();
      const visible = ((await learner.page.locator("main").innerText()) || "").toLowerCase();
      for (const cause of ["revoked", "suspended", "entitlement", "enrollment"]) {
        expect(visible, `the generic unavailable page named the cause "${cause}"`).not.toContain(cause);
      }

      // The Course itself is untouched: another Student's access to it is unaffected.
      const bystander = studentFor(testInfo, ENTITLEMENT_EXTEND_TEST_SLOT);
      expect(entitlementState(bystander).state).toBe("ACTIVE");
    } finally {
      await admin.context.close();
      await learner.context.close();
    }
  });
});
