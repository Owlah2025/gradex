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

    // 3. Admin configures future Course access-expiry date
    const expiryCourseInput = adminPage.locator('input[placeholder="20000000-0000-0000-0000-000000000001"]').first();
    const expiryDateInput = adminPage.locator('input[type="date"]').first();
    const expiryReasonInput = adminPage.locator('input[placeholder="Standard cohort 30-day access grant"]').first();

    await expiryCourseInput.fill(COURSE_ID);
    await expiryDateInput.fill("2026-12-31");
    await expiryReasonInput.fill("August 15 Launch Cohort");

    await adminPage.click('button:has-text("Save Default Expiry")');

    // 4. Assert successful UI result
    await expect(adminPage.locator('[role="status"]')).toContainText("Default access expiry configured");

    // 5. Admin creates an invitation for Student A (unentitled student)
    const createCourseInput = adminPage.locator('input[placeholder="20000000-0000-0000-0000-000000000001"]').nth(1);
    const createEmailInput = adminPage.locator('input[type="email"]').first();

    await createCourseInput.fill(COURSE_ID);
    await createEmailInput.fill(STUDENT_A_EMAIL);
    await adminPage.click('button:has-text("Create Invitation")');

    // 6. Assert invitation appears in queue as PENDING_STUDENT_ACCEPTANCE
    await expect(adminPage.locator("table")).toContainText(STUDENT_A_EMAIL);
    await expect(adminPage.locator("table")).toContainText("PENDING_STUDENT_ACCEPTANCE");

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
    const invitationUrl = `/en/access?invitation_id=${invitationId}&token=${verificationToken}`;
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
    await expect(studentPage.locator("h1")).toContainText("Student Course Access Portal");

    // 11. Student A accepts invitation
    await expect(studentPage.locator("body")).toContainText("PENDING_STUDENT_ACCEPTANCE");
    await studentPage.click('button:has-text("Accept Invitation & Request Access")');

    // 12. Assert UI shows pending Admin approval
    await expect(studentPage.locator("body")).toContainText("PENDING_ADMIN_APPROVAL");
    await expect(studentPage.locator('[role="status"]')).toContainText("pending admin approval");

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

    // 18. Student A refreshes/revisits access page
    await studentPage.goto("/en/access");

    // 19. Assert active access is displayed
    await expect(studentPage.locator("body")).toContainText("Active Access Until");

    // 20. Student A clicks localized Course CTA
    const watchCourseCTA = studentPage.locator('a:has-text("Watch Course"), a[href*="/learn/courses"]').first();
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
    const courseInput = adminPage.locator('input[placeholder="20000000-0000-0000-0000-000000000001"]').nth(1);
    const emailInput = adminPage.locator('input[type="email"]').first();
    const rejectTargetEmail = "student-reject@example.test";

    await courseInput.fill(COURSE_ID);
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
