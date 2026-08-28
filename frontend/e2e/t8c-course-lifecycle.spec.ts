import { test, expect, type Browser, type BrowserContext, type Page } from "@playwright/test";
import { queryLearningState } from "../src/lib/api/e2e-progress";
import { frontendOrigin } from "../src/lib/api/e2e-ports";
import { issueRotatingSession } from "./rotating-students";

/**
 * T8C / MVP-F24C — AD-12, the Admin Course lifecycle, proved as a browser journey.
 *
 * # WHAT ONLY THE BROWSER CAN ANSWER
 *
 * The lifecycle commands themselves are already proved against real PostgreSQL by
 * `backend/internal/catalog/lifecycle_integration_test.go` (transition graph, retirement, access
 * preservation) and by `backend/internal/httpapi/privileged_audit_integration_test.go` (one audit
 * row per command). What no backend test can answer is whether a human Admin can reach those
 * commands through the product at all, whether the screen then shows the state the server holds,
 * and whether the public catalogue and an entitled Student change exactly as the contract says.
 *
 * # THE CANONICAL EFFECTS THIS FILE ASSERTS, AND WHERE THEY COME FROM
 *
 * Public visibility is one predicate, `catalogpublic.PublishedOnly`:
 *   `lifecycle = 'PUBLISHED' AND access_suspended_at IS NULL AND retired_at IS NULL`
 * so delist, archive, retirement and access suspension each remove a Course from discovery, and
 * only relist or restoration can put it back.
 *
 * Student access is a different authority — `internal/entitlement`. It reads
 * `access_suspended_at` and `retired_at`, and it reads no lifecycle value at all. So:
 *   - delist / relist / archive change discovery and never touch an existing Student's access;
 *   - access suspension denies the read with `COURSE_ACCESS_SUSPENDED` while leaving the
 *     Entitlement, the Enrollment and the Progress rows untouched;
 *   - retirement blocks only Entitlements whose `retirement_eligibility_at` is not before
 *     `retired_at` (BR-027), so a Student entitled before the retirement keeps learning.
 * None of that is invented here; each case asserts it through the real product.
 *
 * # ONE COURSE PER CASE
 *
 * Retirement and archival are terminal, so the cases cannot share a Course without becoming
 * order-dependent. `seedT8CLifecycleFixtures` seeds four published Courses, one per case, and
 * seeds none of the end states under test.
 */

const ADMIN_EMAIL = "admin@example.test";
const ADMIN_ID = "a0000000-0000-0000-0000-000000000000";

const DELIST_COURSE_TITLE = "T8C Delist Relist Course";
const SUSPENSION_COURSE_TITLE = "T8C Access Suspension Course";
const RETIREMENT_COURSE_TITLE = "T8C Retirement Course";
const ARCHIVE_COURSE_TITLE = "T8C Archive Course";

const SUSPENSION_COURSE_ID = "c8000000-0000-0000-0000-000000000002";
const SUSPENSION_STUDENT_ID = "a0000000-0000-0000-0000-0000000008c0";
const SUSPENSION_STUDENT_EMAIL = "t8c-suspension-student@example.test";

const ACTIVE_BADGE_SELECTOR = '[data-learning-status="active"]';
const UNAVAILABLE_HEADING = "Learning is unavailable";

// Four lifecycle journeys, each crossing the Admin surface, the public catalogue and — for the
// suspension case — protected learning, on a dev-mode first compilation of every route.
test.describe.configure({ timeout: 180_000 });

async function authenticateAs(
  context: BrowserContext,
  accountID: string,
  email: string,
): Promise<void> {
  const session = issueRotatingSession({ accountID, email });
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
 * Admin and Instructor routes are not language-addressable — `/en/admin/...` is an incidental path
 * segment, and `LocaleProvider` renders the visitor's saved language there. The saved choice is
 * therefore set the way the product stores it, exactly as T8B does, so these assertions can read
 * the English copy without asserting a language the product never promised from the URL.
 */
async function openAdminPage(browser: Browser): Promise<{ context: BrowserContext; page: Page }> {
  const context = await browser.newContext({ locale: "en-US", timezoneId: "Asia/Kuwait" });
  await context.addInitScript(() => {
    window.localStorage.setItem("gradex.locale", "en");
  });
  await authenticateAs(context, ADMIN_ID, ADMIN_EMAIL);
  return { context, page: await context.newPage() };
}

/**
 * Opens a Course on the Admin lifecycle workspace the way an Admin has it: by the words the
 * Course is known by. No identifier is typed anywhere in this journey.
 */
async function openLifecycleCourse(adminPage: Page, title: string): Promise<void> {
  await adminPage.goto("/en/admin/course-lifecycle");
  await adminPage.getByTestId("lifecycle-course-search").fill(title);
  await adminPage.getByTestId("lifecycle-course-search-submit").click();
  const row = adminPage.getByTestId("lifecycle-course-row").filter({ hasText: title });
  await expect(row, `${title} is not reachable on the Admin lifecycle workspace`).toHaveCount(1);
  await row.getByRole("button", { name: "Manage" }).click();
  await expect(adminPage.getByTestId("lifecycle-selected-title")).toHaveText(title);
}

/**
 * Issue one of the three consequential lifecycle commands.
 *
 * Retire, archive and suspend each state their consequence and wait for an answer before the
 * request is made. The transitions these cases assert are unchanged — the command still fires, and
 * still fires once — so the confirmation is a step on the way to them rather than a case of its
 * own. Tranche I's own spec is what proves the dialog itself.
 */
async function issueLifecycleCommand(
  adminPage: Page,
  command: "retire" | "archive" | "suspend",
): Promise<void> {
  await adminPage.getByTestId(`lifecycle-${command}`).click();
  const dialog = adminPage.getByTestId(`lifecycle-confirm-${command}`);
  await expect(dialog).toBeVisible();
  await dialog.getByTestId("confirm-accept").click();
  await expect(dialog).toHaveCount(0);
}

/** Reopens the Course after a mutation, so what is asserted is a later read and not an echo. */
async function refetchLifecycleCourse(adminPage: Page, title: string): Promise<void> {
  await adminPage.reload();
  await openLifecycleCourse(adminPage, title);
}

async function expectAdminLifecycleState(
  adminPage: Page,
  expected: { lifecycle: string; suspended?: boolean; retired?: boolean },
): Promise<void> {
  const selected = adminPage.getByTestId("lifecycle-selected-course");
  await expect(selected).toHaveAttribute("data-lifecycle-state", expected.lifecycle);
  await expect(selected).toHaveAttribute(
    "data-access-suspended",
    expected.suspended ? "true" : "false",
  );
  await expect(selected).toHaveAttribute("data-retired", expected.retired ? "true" : "false");
}

/** The public catalogue, read the way a visitor reads it: by title, with no session at all. */
async function expectPubliclyDiscoverable(page: Page, title: string, present: boolean): Promise<void> {
  await page.goto(`/en/catalog?q=${encodeURIComponent(title)}`);
  const card = page.getByRole("link", { name: new RegExp(title) });
  if (present) {
    await expect(card, `${title} must be publicly discoverable`).toHaveCount(1, { timeout: 15_000 });
  } else {
    await expect(card, `${title} must not be publicly discoverable`).toHaveCount(0, {
      timeout: 15_000,
    });
  }
}

test.describe("T8C / MVP-F24C — AD-12 Admin Course lifecycle", () => {
  /**
   * Case A — delist, then relist. Discovery is the whole claim: the Course leaves the public
   * catalogue and comes back as the same single listing.
   */
  test("A: an Admin delists a published Course and relists it", async ({ browser }) => {
    const admin = await openAdminPage(browser);
    const visitorContext = await browser.newContext({ locale: "en-US" });
    const visitor = await visitorContext.newPage();
    try {
      await expectPubliclyDiscoverable(visitor, DELIST_COURSE_TITLE, true);

      await openLifecycleCourse(admin.page, DELIST_COURSE_TITLE);
      await expectAdminLifecycleState(admin.page, { lifecycle: "PUBLISHED" });

      await admin.page.getByTestId("lifecycle-delist").click();
      await expect(admin.page.getByTestId("lifecycle-message")).toContainText("Delist completed");
      await expect(admin.page.getByTestId("lifecycle-error")).toHaveCount(0);

      await refetchLifecycleCourse(admin.page, DELIST_COURSE_TITLE);
      await expectAdminLifecycleState(admin.page, { lifecycle: "DELISTED" });

      await expectPubliclyDiscoverable(visitor, DELIST_COURSE_TITLE, false);

      await admin.page.getByTestId("lifecycle-relist").click();
      await expect(admin.page.getByTestId("lifecycle-message")).toContainText("Relist completed");
      await expect(admin.page.getByTestId("lifecycle-error")).toHaveCount(0);

      await refetchLifecycleCourse(admin.page, DELIST_COURSE_TITLE);
      await expectAdminLifecycleState(admin.page, { lifecycle: "PUBLISHED" });

      // Back as one Course, not as a second listing beside a stale one.
      await expectPubliclyDiscoverable(visitor, DELIST_COURSE_TITLE, true);
    } finally {
      await admin.context.close();
      await visitorContext.close();
    }
  });

  /**
   * Case B — Course access suspension and restoration. This is the only lifecycle action that
   * takes an existing Student's access away, and it must do so without touching the grant.
   */
  test("B: an Admin suspends Course access, the entitled Student is blocked, and restoration returns access", async ({
    browser,
  }) => {
    const before = queryLearningState(SUSPENSION_STUDENT_ID, SUSPENSION_COURSE_ID);
    expect(before.entitlement.found, "the T8C suspension Student has no seeded Entitlement").toBe(true);
    expect(before.entitlement.state).toBe("ACTIVE");
    expect(before.progress.length, "the suspension fixture must carry Progress history").toBeGreaterThan(0);

    const admin = await openAdminPage(browser);
    const studentContext = await browser.newContext({ locale: "en-US", timezoneId: "Asia/Kuwait" });
    await authenticateAs(studentContext, SUSPENSION_STUDENT_ID, SUSPENSION_STUDENT_EMAIL);
    const student = await studentContext.newPage();
    const visitorContext = await browser.newContext({ locale: "en-US" });
    const visitor = await visitorContext.newPage();
    try {
      // Learning before the change, so the "after" answer means something.
      await student.goto(`/en/learn/courses/${SUSPENSION_COURSE_ID}`);
      await expect(student.locator(ACTIVE_BADGE_SELECTOR)).toBeVisible({ timeout: 15_000 });
      await expectPubliclyDiscoverable(visitor, SUSPENSION_COURSE_TITLE, true);

      await openLifecycleCourse(admin.page, SUSPENSION_COURSE_TITLE);
      await expectAdminLifecycleState(admin.page, { lifecycle: "PUBLISHED" });

      await admin.page.getByTestId("lifecycle-suspension-cause").selectOption("SECURITY");
      await admin.page
        .getByTestId("lifecycle-suspension-reason")
        .fill("T8C E2E course access suspension");
      await issueLifecycleCommand(admin.page, "suspend");
      await expect(admin.page.getByTestId("lifecycle-message")).toContainText(
        "Access suspension completed",
      );
      await expect(admin.page.getByTestId("lifecycle-error")).toHaveCount(0);

      await refetchLifecycleCourse(admin.page, SUSPENSION_COURSE_TITLE);
      // Suspension is orthogonal to the presentation lifecycle: the Course is still PUBLISHED.
      await expectAdminLifecycleState(admin.page, { lifecycle: "PUBLISHED", suspended: true });

      await expectPubliclyDiscoverable(visitor, SUSPENSION_COURSE_TITLE, false);

      await student.goto(`/en/learn/courses/${SUSPENSION_COURSE_ID}`);
      await expect(student.getByRole("heading", { name: UNAVAILABLE_HEADING })).toBeVisible({
        timeout: 15_000,
      });
      await expect(student.locator(ACTIVE_BADGE_SELECTOR)).toHaveCount(0);

      // The grant, the Enrollment and the history are all still there: suspension denies a read,
      // it does not rewrite access.
      const suspended = queryLearningState(SUSPENSION_STUDENT_ID, SUSPENSION_COURSE_ID);
      expect(suspended.entitlement.id).toBe(before.entitlement.id);
      expect(suspended.entitlement.state).toBe("ACTIVE");
      expect(suspended.entitlement.revoked_at ?? null).toBeNull();
      expect(suspended.entitlement.access_ends_at).toBe(before.entitlement.access_ends_at);
      expect(suspended.enrollment.id).toBe(before.enrollment.id);
      expect(suspended.progress.length).toBe(before.progress.length);

      await admin.page
        .getByTestId("lifecycle-suspension-reason")
        .fill("T8C E2E course access restoration");
      await admin.page.getByTestId("lifecycle-restore").click();
      await expect(admin.page.getByTestId("lifecycle-message")).toContainText(
        "Access restoration completed",
      );
      await expect(admin.page.getByTestId("lifecycle-error")).toHaveCount(0);

      await refetchLifecycleCourse(admin.page, SUSPENSION_COURSE_TITLE);
      await expectAdminLifecycleState(admin.page, { lifecycle: "PUBLISHED", suspended: false });

      await expectPubliclyDiscoverable(visitor, SUSPENSION_COURSE_TITLE, true);

      await student.goto(`/en/learn/courses/${SUSPENSION_COURSE_ID}`);
      await expect(student.locator(ACTIVE_BADGE_SELECTOR)).toBeVisible({ timeout: 15_000 });

      // Restoration returned the same access; it did not issue a new one.
      const restored = queryLearningState(SUSPENSION_STUDENT_ID, SUSPENSION_COURSE_ID);
      expect(restored.entitlement.id).toBe(before.entitlement.id);
      expect(restored.entitlement.count).toBe(before.entitlement.count);
      expect(restored.enrollment.id).toBe(before.enrollment.id);
      expect(restored.progress.length).toBe(before.progress.length);
    } finally {
      await admin.context.close();
      await studentContext.close();
      await visitorContext.close();
    }
  });

  /**
   * Case C — retirement. It closes future acquisition rather than changing the presentation
   * lifecycle, and it is not repeatable: a second attempt is a refused business state, not a
   * second retirement.
   */
  test("C: an Admin retires a Course and a second retirement is refused", async ({ browser }) => {
    const admin = await openAdminPage(browser);
    const visitorContext = await browser.newContext({ locale: "en-US" });
    const visitor = await visitorContext.newPage();
    try {
      await expectPubliclyDiscoverable(visitor, RETIREMENT_COURSE_TITLE, true);

      await openLifecycleCourse(admin.page, RETIREMENT_COURSE_TITLE);
      await expectAdminLifecycleState(admin.page, { lifecycle: "PUBLISHED" });

      await issueLifecycleCommand(admin.page, "retire");
      await expect(admin.page.getByTestId("lifecycle-message")).toContainText("Retire completed");
      await expect(admin.page.getByTestId("lifecycle-error")).toHaveCount(0);

      await refetchLifecycleCourse(admin.page, RETIREMENT_COURSE_TITLE);
      // `retired_at` is the acquisition boundary; the presentation lifecycle is untouched.
      await expectAdminLifecycleState(admin.page, { lifecycle: "PUBLISHED", retired: true });

      await expectPubliclyDiscoverable(visitor, RETIREMENT_COURSE_TITLE, false);

      // The refusal is a coherent domain answer on a screen that still works, not a generic
      // failure and not a silent second retirement.
      await issueLifecycleCommand(admin.page, "retire");
      await expect(admin.page.getByTestId("lifecycle-error")).toBeVisible();
      await expect(admin.page.getByTestId("lifecycle-message")).toHaveCount(0);
      await expectAdminLifecycleState(admin.page, { lifecycle: "PUBLISHED", retired: true });
    } finally {
      await admin.context.close();
      await visitorContext.close();
    }
  });

  /**
   * Case D — archival, which is terminal. Nothing transitions out of ARCHIVED, and the screen has
   * to say so rather than appear to succeed.
   */
  test("D: an Admin archives a Course and the archived state is terminal", async ({ browser }) => {
    const admin = await openAdminPage(browser);
    const visitorContext = await browser.newContext({ locale: "en-US" });
    const visitor = await visitorContext.newPage();
    try {
      await expectPubliclyDiscoverable(visitor, ARCHIVE_COURSE_TITLE, true);

      await openLifecycleCourse(admin.page, ARCHIVE_COURSE_TITLE);
      await expectAdminLifecycleState(admin.page, { lifecycle: "PUBLISHED" });

      await issueLifecycleCommand(admin.page, "archive");
      await expect(admin.page.getByTestId("lifecycle-message")).toContainText("Archive completed");
      await expect(admin.page.getByTestId("lifecycle-error")).toHaveCount(0);

      await refetchLifecycleCourse(admin.page, ARCHIVE_COURSE_TITLE);
      await expectAdminLifecycleState(admin.page, { lifecycle: "ARCHIVED" });

      await expectPubliclyDiscoverable(visitor, ARCHIVE_COURSE_TITLE, false);

      // There is no canonical un-archive. Relisting an archived Course is refused, and the Course
      // is still archived afterwards.
      await admin.page.getByTestId("lifecycle-relist").click();
      await expect(admin.page.getByTestId("lifecycle-error")).toBeVisible();
      await expect(admin.page.getByTestId("lifecycle-message")).toHaveCount(0);
      await expectAdminLifecycleState(admin.page, { lifecycle: "ARCHIVED" });

      // The Course, its revision and its history survive archival: it is still reachable, by name,
      // on the Admin surface that hides nothing.
      await refetchLifecycleCourse(admin.page, ARCHIVE_COURSE_TITLE);
      await expectAdminLifecycleState(admin.page, { lifecycle: "ARCHIVED" });
    } finally {
      await admin.context.close();
      await visitorContext.close();
    }
  });
});
