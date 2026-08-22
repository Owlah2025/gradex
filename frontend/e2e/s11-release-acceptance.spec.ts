import { expect, test, type BrowserContext, type Page } from "@playwright/test";
import { queryLearningState, type LearningStateSnapshot } from "../src/lib/api/e2e-progress";
import { frontendOrigin } from "../src/lib/api/e2e-ports";
import {
  issueRotatingSession,
  queryEmailVerificationAction,
  queryInvitationToken,
} from "./rotating-students";

const ADMIN = {
  email: "admin@example.test",
  accountID: "a0000000-0000-0000-0000-000000000000",
};
const UNRELATED_STUDENT = {
  email: "student-unentitled@example.test",
  accountID: "a0000000-0000-0000-0000-000000000099",
};
const STUDENT_EMAIL = "s11-release-student@example.test";
const STUDENT_PASSWORD = process.env.GRADEX_E2E_REGISTRATION_PASSWORD || "KuwaitStudy!2026";
const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const LESSON_ID = "30000000-0000-0000-0000-000000000001";
const ASSET_VERSION_ID = "60000000-0000-0000-0000-000000000001";

async function installIssuedSession(
  context: BrowserContext,
  identity: { email: string; accountID: string },
) {
  const session = issueRotatingSession(identity);
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

async function expectZeroGrantState(studentID: string): Promise<LearningStateSnapshot> {
  const state = queryLearningState(studentID, COURSE_ID);
  expect(state.entitlement.found).toBe(false);
  expect(state.entitlement.count).toBe(0);
  expect(state.enrollment.found).toBe(false);
  expect(state.enrollment.count).toBe(0);
  expect(state.progress).toEqual([]);
  return state;
}

async function protectedMutationStatuses(page: Page, csrf: string) {
  return page.evaluate(
    async ({ lessonID, assetVersionID, csrfToken }) => {
      const playback = await fetch(`/api/v1/learn/lessons/${lessonID}/playback`, {
        method: "POST",
        credentials: "include",
        headers: { "X-CSRF-Token": csrfToken },
      });
      const progress = await fetch(`/api/v1/learn/lessons/${lessonID}/progress`, {
        method: "PUT",
        credentials: "include",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": csrfToken,
        },
        body: JSON.stringify({ position_seconds: 15, asset_version_id: assetVersionID }),
      });
      return { playback: playback.status, progress: progress.status };
    },
    { lessonID: LESSON_ID, assetVersionID: ASSET_VERSION_ID, csrfToken: csrf },
  );
}

test.describe("S11 release acceptance", () => {
  test("registration to protected learning is denial-safe and replay-safe", async ({ browser }) => {
    test.setTimeout(120_000);
    const studentContext = await browser.newContext({ locale: "en-US" });
    const adminContext = await browser.newContext({ locale: "en-US" });
    const unrelatedContext = await browser.newContext({ locale: "en-US" });

    try {
      await studentContext.addInitScript(() => window.localStorage.setItem("gradex.locale", "en"));
      let studentPage = await studentContext.newPage();

      // Student registration through the shipped form and policy contract.
      const policyResponsePromise = studentPage.waitForResponse(
        (response) => response.url().endsWith("/api/v1/registration-policy-set"),
      );
      await studentPage.goto("/register");
      const policyResponse = await policyResponsePromise;
      expect(policyResponse.status()).toBe(200);
      const policyBody = (await policyResponse.json()) as {
        id: string;
        version: string;
        effective_date: string;
        policies: Array<{ version: string; url: string }>;
      };
      if (process.env.GRADEX_E2E_EXTERNAL_ORIGIN) {
        expect(policyBody.id).toBe("gradex-legal-2026-08-09-v1");
        expect(policyBody.version).toBe("2026-08-09-v1");
        expect(policyBody.effective_date).toBe("2026-08-09");
        expect(policyBody.policies).toHaveLength(2);
        expect(policyBody.policies.every((policy) => policy.version === "2026-08-09-v1")).toBe(true);
        expect(policyBody.policies.every((policy) => policy.url.startsWith(frontendOrigin()))).toBe(true);
        await expect(studentPage.getByTestId("registration-policy-version")).toContainText("2026-08-09-v1");
      }
      await studentPage.locator("#display-name").fill("Release Student");
      await studentPage.locator("#email").fill(STUDENT_EMAIL);
      await studentPage.locator("#password").fill(STUDENT_PASSWORD);
      const policies = studentPage.locator('fieldset input[type="checkbox"]');
      await expect(policies).toHaveCount(2);
      for (let index = 0; index < 2; index += 1) {
        await policies.nth(index).check();
      }
      const registrationResponsePromise = studentPage.waitForResponse(
        (response) =>
          response.url().endsWith("/api/v1/student-registrations") &&
          response.request().method() === "POST",
      );
      await studentPage.getByRole("button", { name: "Create account" }).click();
      expect((await registrationResponsePromise).status()).toBe(202);
      await studentPage.waitForURL((url) => url.pathname === "/verify-email");

      const verification = queryEmailVerificationAction(STUDENT_EMAIL);
      expect(verification.account_id).toMatch(/^[0-9a-f-]{36}$/);

      // A bad bearer is recoverable: it is refused without consuming the valid delivery.
      await studentPage.goto("/verify-email/result#token=invalid-s11-verification-token");
      await expect(
        studentPage.getByRole("alert").filter({ hasText: "This link is unavailable" }),
      ).toBeVisible();
      await studentPage.close();
      studentPage = await studentContext.newPage();
      await studentPage.goto(
        `/verify-email/result#token=${encodeURIComponent(verification.verification_token)}`,
      );
      await expect(
        studentPage.getByRole("status").filter({ hasText: "Email confirmed" }),
      ).toBeVisible();
      await expect(studentPage).not.toHaveURL(/verification_token|token=/);

      // Real password login establishes the Student session used for the rest of the journey.
      await studentPage.getByRole("link", { name: "Go to login" }).click();
      await studentPage.locator("#email").fill(STUDENT_EMAIL);
      await studentPage.locator("#password").fill(STUDENT_PASSWORD);
      const loginResponsePromise = studentPage.waitForResponse(
        (response) => response.url().endsWith("/api/v1/sessions") && response.request().method() === "POST",
      );
      await studentPage.getByRole("button", { name: "Sign in" }).click();
      const loginResponse = await loginResponsePromise;
      expect(loginResponse.status()).toBe(201);
      const loginBody = (await loginResponse.json()) as { role: string; csrf_token: string };
      expect(loginBody.role).toBe("STUDENT");
      expect(loginBody.csrf_token).toBeTruthy();

      // Admin uses a production-valid session and configures the launch Course.
      const adminSession = await installIssuedSession(adminContext, ADMIN);
      const adminPage = await adminContext.newPage();
      await adminPage.goto("/en/admin/course-access");
      await expect(adminPage.locator("h1")).toContainText("Course Access Management");

      // The launch Course is chosen by title; no Course identifier is typed.
      const courseSelect = adminPage.getByTestId("course-access-course-select");
      await expect(courseSelect).toBeVisible();
      await expect(adminPage.locator("body")).not.toContainText("Course ID (UUID)");
      await courseSelect.selectOption(COURSE_ID);
      await expect(adminPage.getByTestId("course-access-selected-course")).toBeVisible();
      await adminPage.locator('input[type="date"]').first().fill("2026-12-31");
      await adminPage
        .locator('input[placeholder="Standard cohort 30-day access grant"]')
        .first()
        .fill("August 15 S11 release acceptance");
      await adminPage.getByRole("button", { name: "Save Default Expiry" }).click();
      await expect(adminPage.locator('[role="status"]')).toContainText("Default access expiry configured");

      await adminPage.locator('input[type="email"]').first().fill(STUDENT_EMAIL);
      await adminPage.getByRole("button", { name: "Create Invitation" }).click();
      await expect(adminPage.locator("table")).toContainText(STUDENT_EMAIL);

      const invitationCell = await adminPage.locator(`td:has-text("${STUDENT_EMAIL}")`).first().innerText();
      const invitationMatch = invitationCell.match(
        /([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})/i,
      );
      expect(invitationMatch).not.toBeNull();
      const invitationID = invitationMatch![1];
      const invitationToken = queryInvitationToken(invitationID);

      // Invalid acceptance is refused, then the valid link recovers without a premature grant.
      await studentPage.goto(
        `/en/access?invitation_id=${invitationID}#token=invalid-s11-invitation-token`,
      );
      // MVP-F12 renamed the control and stopped rendering wire enums to the Student, so the
      // selectors follow the current contract. What is audited is unchanged: a bad token is
      // refused with a visible reason, a good one records acceptance, and neither creates a grant —
      // `expectZeroGrantState` still proves the authority half against the database.
      await studentPage.getByTestId("accept-invitation").click();
      await expect(studentPage.getByTestId("access-invitation-error")).toBeVisible();
      await expect(studentPage.getByTestId("access-invitation-error")).toHaveText(
        /no longer usable|not available|could not be accepted|incomplete/i,
      );
      await expectZeroGrantState(verification.account_id);

      await studentPage.goto(
        `/en/access?invitation_id=${invitationID}#token=${encodeURIComponent(invitationToken)}`,
      );
      await studentPage.getByTestId("accept-invitation").click();
      // The Student reads plain language; the wire state is still asserted, on the data attribute.
      await expect(studentPage.getByTestId(`access-state-${COURSE_ID}`)).toHaveText("Waiting for approval");
      await expect(studentPage.getByTestId(`access-record-${COURSE_ID}`)).toHaveAttribute(
        "data-access-state",
        "AWAITING_APPROVAL",
      );
      await expectZeroGrantState(verification.account_id);

      // Course, Lesson, playback, and Progress all deny before Admin Approval.
      await studentPage.goto(`/en/learn/courses/${COURSE_ID}`);
      await expect(studentPage.locator("body")).toContainText(/unavailable/i);
      await studentPage.goto(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);
      await expect(studentPage.locator("body")).toContainText(/unavailable/i);
      await expect(studentPage.locator("video")).toHaveCount(0);
      expect(await protectedMutationStatuses(studentPage, loginBody.csrf_token)).toEqual({
        playback: 404,
        progress: 404,
      });
      await expectZeroGrantState(verification.account_id);

      // Admin Approval is the sole grant trigger.
      await adminPage.getByRole("button", { name: "Refresh Queue" }).click();
      const approveButton = adminPage.locator(
        `tr:has-text("${invitationID}") button:has-text("Approve")`,
      );
      const approvalResponsePromise = adminPage.waitForResponse(
        (response) =>
          response.url().endsWith(`/course-access-invitations/${invitationID}/approve`) &&
          response.request().method() === "POST",
      );
      await approveButton.click();
      const approvalResponse = await approvalResponsePromise;
      expect(approvalResponse.status()).toBe(200);
      const approvalBody = (await approvalResponse.json()) as {
        entitlement: { id: string; grant_source: string; source_invitation_id: string };
      };

      const granted = queryLearningState(verification.account_id, COURSE_ID);
      expect(granted.entitlement.found).toBe(true);
      expect(granted.entitlement.count).toBe(1);
      expect(granted.entitlement.state).toBe("ACTIVE");
      expect(granted.entitlement.id).toBe(approvalBody.entitlement.id);
      expect(granted.entitlement.grant_source).toBe("MANUAL_INVITATION");
      expect(granted.entitlement.source_invitation_id).toBe(invitationID);
      expect(granted.enrollment.found).toBe(true);
      expect(granted.enrollment.count).toBe(1);

      // The granted Student can reach Course, Lesson, playback issuance, and durable Progress.
      await studentPage.goto(`/en/learn/courses/${COURSE_ID}`);
      await expect(
        studentPage.getByRole("heading", { name: "CS101: Introduction to Programming", level: 1 }),
      ).toBeVisible();
      await studentPage.goto(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);
      await expect(studentPage.locator("video")).toBeVisible();
      await expect(studentPage.locator("video")).toHaveAttribute("src", /.+/);

      const progressStatus = await studentPage.evaluate(
        async ({ lessonID, assetVersionID, csrfToken }) => {
          const response = await fetch(`/api/v1/learn/lessons/${lessonID}/progress`, {
            method: "PUT",
            credentials: "include",
            headers: {
              "Content-Type": "application/json",
              "X-CSRF-Token": csrfToken,
            },
            body: JSON.stringify({ position_seconds: 15, asset_version_id: assetVersionID }),
          });
          return response.status;
        },
        {
          lessonID: LESSON_ID,
          assetVersionID: ASSET_VERSION_ID,
          csrfToken: loginBody.csrf_token,
        },
      );
      expect([200, 204]).toContain(progressStatus);
      await expect
        .poll(() => {
          const state = queryLearningState(verification.account_id, COURSE_ID);
          return state.progress.find((row) => row.lesson_identity_id === LESSON_ID)?.max_position_seconds ?? 0;
        })
        .toBeGreaterThanOrEqual(15);

      // An unrelated Student receives the same protected denials and creates no state.
      const unrelatedSession = await installIssuedSession(unrelatedContext, UNRELATED_STUDENT);
      const unrelatedPage = await unrelatedContext.newPage();
      await unrelatedPage.goto(`/en/learn/courses/${COURSE_ID}`);
      await expect(unrelatedPage.locator("body")).toContainText(/unavailable/i);
      await unrelatedPage.goto(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);
      await expect(unrelatedPage.locator("body")).toContainText(/unavailable/i);
      expect(await protectedMutationStatuses(unrelatedPage, unrelatedSession.csrf_token)).toEqual({
        playback: 404,
        progress: 404,
      });
      await expectZeroGrantState(UNRELATED_STUDENT.accountID);

      // Authorized replay reaches the grant path and returns the original Entitlement.
      const replay = await adminPage.evaluate(
        async ({ invitationID: id, csrfToken }) => {
          const response = await fetch(`/api/v1/admin/course-access-invitations/${id}/approve`, {
            method: "POST",
            credentials: "include",
            headers: { "X-CSRF-Token": csrfToken },
          });
          return { status: response.status, body: await response.json() };
        },
        { invitationID, csrfToken: adminSession.csrf_token },
      );
      expect(replay.status).toBe(200);
      expect(replay.body.entitlement.id).toBe(approvalBody.entitlement.id);

      const replayed = queryLearningState(verification.account_id, COURSE_ID);
      expect(replayed.entitlement.count).toBe(1);
      expect(replayed.entitlement.id).toBe(granted.entitlement.id);
      expect(replayed.entitlement.grant_source).toBe("MANUAL_INVITATION");
      expect(replayed.entitlement.source_invitation_id).toBe(invitationID);
      expect(replayed.enrollment.count).toBe(1);
      expect(replayed.enrollment.id).toBe(granted.enrollment.id);
    } finally {
      await studentContext.close();
      await adminContext.close();
      await unrelatedContext.close();
    }
  });
});
