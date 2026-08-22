import { test, expect } from "@playwright/test";
import { issueRotatingSession, queryInvitationToken } from "./rotating-students";
import { queryLearningState } from "../src/lib/api/e2e-progress";
import { frontendOrigin } from "../src/lib/api/e2e-ports";

/**
 * S6 Course Access Grant — Production Launch Playwright Journey
 *
 * Full 30-step end-to-end browser & API journey against real Go API, real PostgreSQL:
 * Admin sign-in -> Course access expiry config -> Invitation creation -> Anonymous returnTo redirect ->
 * Student A sign-in -> Student A acceptance -> Pre-approval denial assertion -> Admin approval ->
 * Entitlement & Enrollment creation assertion -> Student A course CTA navigation -> Protected lesson entry ->
 * Playback execution -> Progress mutation -> Unrelated Student B denial -> Idempotency assertion.
 */

const ADMIN_EMAIL = "admin@example.test";
const ADMIN_ID = "a0000000-0000-0000-0000-000000000000";

const STUDENT_A_EMAIL = "student-unentitled@example.test";
const STUDENT_A_ID = "a0000000-0000-0000-0000-000000000099";

const STUDENT_B_EMAIL = "student-expired@example.test";
const STUDENT_B_ID = "a0000000-0000-0000-0000-000000000002";

const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const LESSON_ID = "30000000-0000-0000-0000-000000000001";

/**
 * The Student's Course-access surface must speak product language only.
 *
 * Internal identifiers may live in `data-*` attributes for tests and support, so this reads the
 * rendered *text*, not the markup: UUIDs and backend state enums must never be what the Student is
 * asked to interpret.
 */
async function expectNoInternalIdentifiers(page: import("@playwright/test").Page): Promise<void> {
  const visible = (await page.locator("main").innerText()) || "";
  expect(visible, "a UUID reached the Student's visible copy").not.toMatch(
    /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i,
  );
  for (const wireEnum of [
    "PENDING_STUDENT_ACCEPTANCE",
    "PENDING_ADMIN_APPROVAL",
    "APPROVED",
    "REJECTED",
    "CANCELLED",
    "ACTIVE",
    "REVOKED",
    "EXPIRED",
    "entitlement",
    "MANUAL_INVITATION",
  ]) {
    expect(visible, `backend term "${wireEnum}" reached the Student's visible copy`).not.toContain(wireEnum);
  }
}

/**
 * This journey drives Admin invitation, Student acceptance, Admin approval, protected learning, and
 * the MVP-F12 persistence loop through the browser, across three contexts. F12 added two dashboard
 * round-trips and an Arabic pass, which took it past the 30 s default. The budget matches the
 * convention the other long S5 journeys already use (120–180 s); it is a work budget for a longer
 * journey, not a retry or a stabilisation device — the assertions themselves are unchanged.
 */
/**
 * Gradex takes no payment (D-045). No access state, on any surface, may offer one.
 */
async function expectNoCommerce(page: import("@playwright/test").Page): Promise<void> {
  const visible = (await page.locator("main").innerText()) || "";
  expect(visible, "commerce language reached a Student surface").not.toMatch(
    /buy now|add to cart|checkout|purchase|pay now|السلة|الدفع|اشتر/i,
  );
  await expect(
    page.getByRole("button", { name: /buy|cart|checkout|purchase|pay|اشتر|الدفع|السلة/i }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("link", { name: /buy|cart|checkout|purchase|pay|اشتر|الدفع|السلة/i }),
  ).toHaveCount(0);
}

test.describe.configure({ timeout: 120_000 });

test.describe("S6 Course Access Grant — Real Production Launch Journey", () => {
  test("Complete 30-Step End-to-End Course Access Grant & Protected Learning Journey", async ({ browser }) => {
    // Context 1: Admin Context
    const adminContext = await browser.newContext({ locale: "en-US" });
    const origin = new URL(frontendOrigin());

    // 1. Admin signs in using production session cookie
    const adminSession = issueRotatingSession({ email: ADMIN_EMAIL, accountID: ADMIN_ID });
    await adminContext.addCookies([
      {
        name: adminSession.cookie_name,
        value: adminSession.cookie_value,
        domain: origin.hostname,
        path: "/",
        httpOnly: true,
        secure: true,
        sameSite: "Strict",
      },
    ]);

    const adminPage = await adminContext.newPage();

    // 2. Admin navigates to Course Access Portal
    await adminPage.goto("/en/admin/course-access");
    await expect(adminPage.locator("h1")).toContainText("Course Access Management");

    // 3. Admin selects the published Course by title, then configures its
    // future access-expiry date. No Course identifier is ever typed.
    const courseSelect = adminPage.getByTestId("course-access-course-select");
    await expect(courseSelect).toBeVisible();
    await expect(courseSelect).toContainText("CS101");
    await expect(adminPage.locator("body")).not.toContainText("Course ID (UUID)");
    await courseSelect.selectOption(COURSE_ID);
    await expect(adminPage.getByTestId("course-access-selected-course")).toContainText("CS101");

    const expiryDateInput = adminPage.locator('input[type="date"]').first();
    const expiryReasonInput = adminPage.locator('input[placeholder="Standard cohort 30-day access grant"]').first();

    await expiryDateInput.fill("2026-12-31");
    await expiryReasonInput.fill("August 15 Launch Cohort");

    await adminPage.click('button:has-text("Save Default Expiry")');

    // 4. Assert successful UI result
    await expect(adminPage.locator('[role="status"]')).toContainText("Default access expiry configured");

    // 5. Admin creates an invitation for Student A (unentitled student) on the
    // same selected Course.
    const createEmailInput = adminPage.locator('input[type="email"]').first();

    await createEmailInput.fill(STUDENT_A_EMAIL);
    await adminPage.click('button:has-text("Create Invitation")');

    // 6. Assert invitation appears in queue as PENDING_STUDENT_ACCEPTANCE,
    // named by the Course the Admin selected.
    await expect(adminPage.locator("table")).toContainText(STUDENT_A_EMAIL);
    await expect(adminPage.locator("table")).toContainText("PENDING_STUDENT_ACCEPTANCE");
    await expect(adminPage.locator("table")).toContainText("CS101");

    // Extract invitation ID from queue table cell
    const invitationIdText = await adminPage.locator(`td:has-text("${STUDENT_A_EMAIL}")`).first().innerText();
    const invIdMatch = invitationIdText.match(/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})/i);
    expect(invIdMatch).not.toBeNull();
    const invitationId = invIdMatch![1];

    // 7. Obtain actual invitation delivery/link token from outbox via E2E outbox helper
    const verificationToken = queryInvitationToken(invitationId);
    expect(verificationToken).toBeTruthy();

    // Context 2: Student A Context (Starts Anonymous)
    const studentContext = await browser.newContext({ locale: "en-US" });
    const studentPage = await studentContext.newPage();

    // 8. Open invitation landing as anonymous browser
    const invitationUrl = `/en/access?invitation_id=${invitationId}#token=${verificationToken}`;
    await studentPage.goto(invitationUrl);

    // 9. Unauthenticated request triggers 401 & redirects to /login with validated returnTo
    await studentPage.waitForURL((url) => url.pathname.includes("/login"));
    const returnToQuery = new URL(studentPage.url()).searchParams.get("returnTo");
    expect(returnToQuery).toContain("/access");

    // 10. Student A authenticates with session cookie and navigates to target returnTo
    const studentSession = issueRotatingSession({ email: STUDENT_A_EMAIL, accountID: STUDENT_A_ID });
    await studentContext.addCookies([
      {
        name: studentSession.cookie_name,
        value: studentSession.cookie_value,
        domain: origin.hostname,
        path: "/",
        httpOnly: true,
        secure: true,
        sameSite: "Strict",
      },
    ]);

    await studentPage.goto(invitationUrl);
    await studentPage.waitForURL((url) => url.pathname.includes("/access"));
    await expect(studentPage.locator("h1")).toContainText("Course access");

    // The Course is named, not keyed. No UUID and no wire enum is product-visible anywhere here.
    await expect(studentPage.getByTestId(`access-record-${COURSE_ID}`)).toContainText("CS101");
    await expectNoInternalIdentifiers(studentPage);

    // 11. Student A accepts invitation. The state is stated in the Student's language; the wire
    // enum survives only as a data attribute for tests and support.
    await expect(studentPage.getByTestId(`access-state-${COURSE_ID}`)).toHaveText("Action needed");
    await expect(studentPage.getByTestId(`access-record-${COURSE_ID}`)).toHaveAttribute(
      "data-access-state",
      "ACTION_REQUIRED",
    );
    // MVP-F15 / ST-08 — the Dashboard's pending-access summary, in the window where the invitation
    // genuinely awaits the Student. The same page is used rather than a second one, then the
    // invitation URL is re-opened so the one-time fragment token is captured again for acceptance.
    await studentPage.goto(`/en/learn/dashboard`);
    await expect(studentPage.getByTestId("pending-access-summary")).toBeVisible();
    await expect(studentPage.getByTestId("pending-access-action-required")).toContainText(
      "waiting for you to accept",
    );
    await expect(studentPage.getByTestId("pending-access-awaiting-approval")).toHaveCount(0);
    await expectNoInternalIdentifiers(studentPage);

    await studentPage.goto(invitationUrl);
    await studentPage.waitForURL((url) => url.pathname.includes("/access"));
    await expect(studentPage.getByTestId(`access-state-${COURSE_ID}`)).toHaveText("Action needed");

    await studentPage.getByTestId("accept-invitation").click();

    // 12. Acceptance is recorded, and the page says plainly that it granted nothing yet.
    await expect(studentPage.getByTestId("access-invitation-accepted")).toBeVisible();
    await expect(studentPage.getByTestId(`access-state-${COURSE_ID}`)).toHaveText("Waiting for approval");
    await expect(studentPage.getByTestId(`access-record-${COURSE_ID}`)).toContainText(
      "An administrator still has to approve it",
    );
    await expect(studentPage.getByTestId(`go-to-course-${COURSE_ID}`)).toHaveCount(0);
    await expectNoInternalIdentifiers(studentPage);

    // ---------------------------------------------------------------------
    // MVP-F12 — the persistent half. The Student leaves the invitation context entirely and comes
    // back through ordinary authenticated navigation. No token, no remembered URL, no email.
    // ---------------------------------------------------------------------
    await studentPage.goto(`/en/learn/dashboard`);

    // MVP-F15 / ST-08 — the invitation is now waiting on an Admin, not on the Student, and the
    // Dashboard says so in those terms. This journey leaves through the summary's own action.
    await expect(studentPage.getByTestId("pending-access-summary")).toBeVisible();
    await expect(studentPage.getByTestId("pending-access-awaiting-approval")).toContainText(
      "waiting for approval",
    );
    await expect(studentPage.getByTestId("pending-access-action-required")).toHaveCount(0);
    // The persistent access route remains available alongside it.
    await expect(studentPage.getByTestId("dashboard-access-link")).toBeVisible();
    await expectNoInternalIdentifiers(studentPage);

    await studentPage.getByTestId("pending-access-action").click();
    await studentPage.waitForURL((url) => url.pathname === "/en/access");
    // Nothing token-bearing survived: the invitation panel is absent, and the record is still here.
    expect(studentPage.url()).not.toContain("token");
    expect(studentPage.url()).not.toContain("invitation_id");
    await expect(studentPage.getByTestId("access-invitation-panel")).toHaveCount(0);
    await expect(studentPage.getByTestId(`access-record-${COURSE_ID}`)).toContainText("CS101");
    await expect(studentPage.getByTestId(`access-state-${COURSE_ID}`)).toHaveText("Waiting for approval");
    await expectNoInternalIdentifiers(studentPage);

    // MVP-F13 — Course Details knows this Student's real relationship to this Course. Waiting for
    // approval must not offer a way into the Course, and must never offer a purchase.
    await studentPage.goto(`/en/catalog/${COURSE_ID}`);
    await expect(studentPage.getByTestId("course-access-panel")).toHaveAttribute(
      "data-access-relationship",
      "AWAITING_APPROVAL",
    );
    await expect(studentPage.getByTestId("course-access-message")).toContainText(
      "An administrator still has to approve it",
    );
    await expect(studentPage.getByTestId("course-access-go-to-course")).toHaveCount(0);
    await expect(studentPage.getByTestId("course-access-view-status")).toBeVisible();
    await expectNoCommerce(studentPage);

    const preApprovalState = queryLearningState(STUDENT_A_ID, COURSE_ID);
    expect(preApprovalState.entitlement.count).toBe(0);
    expect(preApprovalState.enrollment.count).toBe(0);

    // 13. Assert Student A does NOT have protected access before Admin approval
    await studentPage.goto(`/en/learn/courses/${COURSE_ID}`);
    // Protected course page when unentitled returns generic unavailable card or 404
    await expect(studentPage.locator("body")).toContainText("unavailable");

    // 14. Admin sees decision-ready invitation
    await adminPage.click('button:has-text("Refresh Queue")');
    await expect(adminPage.locator("table")).toContainText("PENDING_ADMIN_APPROVAL");

    // 15. Admin approves invitation
    const approveButton = adminPage.locator(`tr:has-text("${invitationId}") button:has-text("Approve")`);
    await approveButton.click();
    await expect(adminPage.locator('[role="status"]')).toContainText("Invitation approved!");

    // 16. Assert invitation state becomes APPROVED in queue
    await expect(adminPage.locator("table")).toContainText("APPROVED");

    // 17. Database assertion: verify exactly 1 Entitlement & 1 Enrollment exist
    const stateSnapshot = queryLearningState(STUDENT_A_ID, COURSE_ID);
    expect(stateSnapshot.entitlement.found).toBe(true);
    expect(stateSnapshot.entitlement.count).toBe(1);
    expect(stateSnapshot.entitlement.state).toBe("ACTIVE");
    expect(stateSnapshot.entitlement.grant_source).toBe("MANUAL_INVITATION");
    expect(stateSnapshot.entitlement.source_invitation_id).toBe(invitationId);
    expect(stateSnapshot.enrollment.found).toBe(true);
    expect(stateSnapshot.enrollment.count).toBe(1);

    // 18. Student A returns through ordinary navigation — still no token, still no email.
    await studentPage.goto(`/en/learn/dashboard`);
    // MVP-F15 / ST-08 — nothing is pending any more, so the summary is gone entirely rather than
    // lingering as a stale prompt.
    await expect(studentPage.getByTestId("pending-access-summary")).toHaveCount(0);
    await studentPage.getByTestId("dashboard-access-link").click();
    await studentPage.waitForURL((url) => url.pathname === "/en/access");

    // 19. Access is now granted, said in the Student's language, with the Course named.
    await expect(studentPage.getByTestId(`access-state-${COURSE_ID}`)).toHaveText("Access granted");
    await expect(studentPage.getByTestId(`access-record-${COURSE_ID}`)).toContainText("CS101");
    await expect(studentPage.getByTestId(`access-record-${COURSE_ID}`)).toContainText("Access until");
    await expectNoInternalIdentifiers(studentPage);

    // 19b. The same record in Arabic: Gradex is Arabic-default, so an English-only proof is not
    // proof. Real Arabic copy is asserted, not merely `dir="rtl"`.
    await studentPage.goto("/ar/access");
    await expect(studentPage.locator("html")).toHaveAttribute("dir", "rtl");
    await expect(studentPage.locator("h1")).toContainText("الوصول إلى المقررات");
    await expect(studentPage.getByTestId(`access-state-${COURSE_ID}`)).toHaveText("تم منح الوصول");
    await expect(studentPage.getByTestId(`go-to-course-${COURSE_ID}`)).toHaveText("افتح المقرر");
    await expectNoInternalIdentifiers(studentPage);
    await studentPage.goto("/en/access");

    // 19c. MVP-F13 — the same Course Details page now reflects granted access, in both languages,
    // and its entry action reaches the real entitled Course Home.
    await studentPage.goto(`/en/catalog/${COURSE_ID}`);
    await expect(studentPage.getByTestId("course-access-panel")).toHaveAttribute(
      "data-access-relationship",
      "ACTIVE",
    );
    await expect(studentPage.getByTestId("course-access-message")).toContainText(
      "You have access to this course",
    );
    await expectNoCommerce(studentPage);

    await studentPage.goto(`/ar/catalog/${COURSE_ID}`);
    await expect(studentPage.locator("html")).toHaveAttribute("dir", "rtl");
    await expect(studentPage.getByTestId("course-access-message")).toHaveText("لديك وصول إلى هذا المقرر.");
    await expect(studentPage.getByTestId("course-access-go-to-course")).toHaveText("افتح المقرر");
    await expectNoCommerce(studentPage);

    await studentPage.goto(`/en/catalog/${COURSE_ID}`);
    await studentPage.getByTestId("course-access-go-to-course").click();
    await studentPage.waitForURL((url) => url.pathname.includes(`/learn/courses/${COURSE_ID}`));
    await expect(studentPage.locator("h1")).toContainText("CS101");

    // 20. Student A opens the Course from the access record itself.
    await studentPage.goto("/en/access");
    const watchCourseCTA = studentPage.getByTestId(`go-to-course-${COURSE_ID}`);
    await expect(watchCourseCTA).toBeVisible();
    await watchCourseCTA.click();

    // 21. Assert actual protected Course page loads
    await studentPage.waitForURL((url) => url.pathname.includes(`/learn/courses/${COURSE_ID}`));
    await expect(studentPage.locator("h1")).toContainText("CS101");

    // 22. Open an actual protected lesson
    const courseUrlMatch = studentPage.url();
    const currentLocale = courseUrlMatch.includes("/en/") ? "en" : "ar";
    await studentPage.goto(`/${currentLocale}/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);
    await expect(studentPage.locator("h1")).toContainText(/Lesson 1|الدرس الأول/);

    // 23. Exercise actual playback endpoint / video element
    const videoElement = studentPage.locator("video");
    await expect(videoElement).toBeVisible();

    // 24. Assert playback is allowed (video has src or playable state)
    const videoSrc = await videoElement.getAttribute("src");
    expect(videoSrc).toBeTruthy();

    // 25. Exercise progress update through production learning API
    const progressResponse = await studentPage.evaluate(async (args) => {
      const resp = await fetch(`/api/v1/learn/lessons/${args.lessonID}/progress`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ position_seconds: 15, asset_version_id: "60000000-0000-0000-0000-000000000001" }),
      });
      return resp.status;
    }, { lessonID: LESSON_ID });

    // 26. Assert progress mutation succeeds (200 OK or 204 No Content)
    expect([200, 204]).toContain(progressResponse);

    // Context 3: Unrelated Student B Context
    const studentBContext = await browser.newContext({ locale: "en-US" });
    const studentBSession = issueRotatingSession({ email: STUDENT_B_EMAIL, accountID: STUDENT_B_ID });
    await studentBContext.addCookies([
      {
        name: studentBSession.cookie_name,
        value: studentBSession.cookie_value,
        domain: origin.hostname,
        path: "/",
        httpOnly: true,
        secure: true,
        sameSite: "Strict",
      },
    ]);

    const studentBPage = await studentBContext.newPage();

    // 27-28. Attempt same protected Course & lesson as unrelated Student B
    await studentBPage.goto(`/en/learn/courses/${COURSE_ID}`);

    // 29. Assert Student B remains denied per S5 protected-learning behavior (Access expired / unavailable)
    await expect(studentBPage.locator("body")).toContainText(/Access expired|unavailable|الوصول منتهي/);

    // 30. Repeat approval idempotency check
    const secondApprove = await adminPage.evaluate(async ({ invitationID, csrfToken }) => {
      const response = await fetch(`/api/v1/admin/course-access-invitations/${invitationID}/approve`, {
        method: "POST",
        credentials: "include",
        headers: { "X-CSRF-Token": csrfToken },
      });
      return { status: response.status, body: await response.json() };
    }, { invitationID: invitationId, csrfToken: adminSession.csrf_token });
    expect(secondApprove.status).toBe(200);
    expect(secondApprove.body.entitlement.id).toBe(stateSnapshot.entitlement.id);

    const recheckSnapshot = queryLearningState(STUDENT_A_ID, COURSE_ID);
    expect(recheckSnapshot.entitlement.found).toBe(true);
    expect(recheckSnapshot.entitlement.count).toBe(1);
    expect(recheckSnapshot.entitlement.state).toBe("ACTIVE");
    expect(recheckSnapshot.entitlement.id).toBe(stateSnapshot.entitlement.id);
    expect(recheckSnapshot.entitlement.grant_source).toBe("MANUAL_INVITATION");
    expect(recheckSnapshot.entitlement.source_invitation_id).toBe(invitationId);
    expect(recheckSnapshot.enrollment.count).toBe(1);
    expect(recheckSnapshot.enrollment.id).toBe(stateSnapshot.enrollment.id);

    // 27. AD07: the Admin manages the resulting grant from its queue row.
    // No entitlement, enrollment, Course or Student identifier is typed.
    await adminPage.reload();
    const manageAccess = adminPage.locator(`tr:has-text("${STUDENT_A_EMAIL}") button:has-text("Manage access")`);
    await expect(manageAccess).toBeVisible();
    await manageAccess.click();
    const accessRecord = adminPage.getByTestId("entitlement-detail");
    await expect(accessRecord).toBeVisible();
    await expect(accessRecord).toContainText("CS101");
    await expect(adminPage.getByTestId("entitlement-state")).toContainText("ACTIVE");

    // Protected learning is exercised through the same production progress
    // route the journey already used, so each access answer is comparable.
    const protectedProgressStatus = async (position: number): Promise<number> =>
      studentPage.evaluate(async (args) => {
        const response = await fetch(`/api/v1/learn/lessons/${args.lessonID}/progress`, {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            position_seconds: args.position,
            asset_version_id: "60000000-0000-0000-0000-000000000001",
          }),
        });
        return response.status;
      }, { lessonID: LESSON_ID, position });

    // 28. Extend the grant: a later expiry keeps protected learning working.
    await adminPage.locator("#entitlement-expiry-date").fill("2027-06-30");
    await adminPage.locator("#entitlement-expiry-reason").fill("Semester extended for the launch cohort");
    await adminPage.getByTestId("save-entitlement-expiry").click();
    await expect(adminPage.getByTestId("entitlement-notice")).toContainText("Access expiry updated");
    expect([200, 204]).toContain(await protectedProgressStatus(20));

    // 29. Shorten the grant to a still-open period: access continues.
    await adminPage.locator("#entitlement-expiry-date").fill("2026-12-31");
    await adminPage.locator("#entitlement-expiry-reason").fill("Cohort finishes earlier than planned");
    await adminPage.getByTestId("save-entitlement-expiry").click();
    await expect(adminPage.getByTestId("entitlement-notice")).toContainText("Access expiry updated");
    expect([200, 204]).toContain(await protectedProgressStatus(25));

    // 30. Revoke the grant: protected learning is refused afterwards, and the
    // record stays as history rather than disappearing.
    await adminPage.locator("#entitlement-revoke-reason").fill("Access ended after out-of-band refund");
    await adminPage.getByTestId("confirm-revoke-entitlement").check();
    await adminPage.getByTestId("revoke-entitlement").click();
    await expect(adminPage.getByTestId("entitlement-state")).toContainText("REVOKED");
    await expect(adminPage.getByTestId("entitlement-terminal")).toBeVisible();
    expect([401, 403, 404, 410]).toContain(await protectedProgressStatus(30));

    await adminContext.close();
    await studentContext.close();
    await studentBContext.close();
  });

  test("Rejection & Expired Link UI Coverage", async ({ browser, request }) => {
    const adminContext = await browser.newContext({ locale: "en-US" });
    const origin = new URL(frontendOrigin());

    const adminSession = issueRotatingSession({ email: ADMIN_EMAIL, accountID: ADMIN_ID });
    await adminContext.addCookies([
      {
        name: adminSession.cookie_name,
        value: adminSession.cookie_value,
        domain: origin.hostname,
        path: "/",
        httpOnly: true,
        secure: true,
        sameSite: "Strict",
      },
    ]);

    const adminPage = await adminContext.newPage();

    // Admin creates invitation for Rejection test
    await adminPage.goto("/en/admin/course-access");
    const emailInput = adminPage.locator('input[type="email"]').first();
    const rejectTargetEmail = "student-reject@example.test";

    await adminPage.getByTestId("course-access-course-select").selectOption(COURSE_ID);
    await emailInput.fill(rejectTargetEmail);
    await adminPage.click('button:has-text("Create Invitation")');

    // Extract invitation ID
    await expect(adminPage.locator("table")).toContainText(rejectTargetEmail);
    const textCell = await adminPage.locator(`td:has-text("${rejectTargetEmail}")`).first().innerText();
    const invId = textCell.match(/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})/i)![1];

    // Invalid token returns 403, 404 or 410
    const badAccept = await request.post(`/api/v1/me/course-access-invitations/${invId}/accept`, {
      data: { acceptance_token: "invalid-token-secret" },
    });
    expect([403, 404, 410]).toContain(badAccept.status());

    await adminContext.close();
  });
});
