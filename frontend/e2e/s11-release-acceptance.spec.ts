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

/**
 * Reads one invitation's identifier from the row's existing test hook.
 *
 * The Admin surface identifies an invitation by its invitee, and no longer renders the identifier
 * as readable text. The id is still needed here to look the delivery token up in the outbox, so it
 * is taken from the attribute the row already carries rather than from anything a human reads.
 */
async function invitationIdFromRow(page: Page, email: string): Promise<string> {
  const hook = page
    .locator(`tr:has(td:has-text("${email}")) [data-testid^="invitation-course-"]`)
    .first();
  await expect(hook).toBeVisible();
  const testid = await hook.getAttribute("data-testid");
  const id = (testid ?? "").replace("invitation-course-", "");
  expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i);
  return id;
}

test.describe("S11 release acceptance", () => {
  test("registration to protected learning is denial-safe and replay-safe", async ({ browser }) => {
    test.setTimeout(120_000);
    const studentContext = await browser.newContext({ locale: "en-US" });
    const adminContext = await browser.newContext({ locale: "en-US" });
    const unrelatedContext = await browser.newContext({ locale: "en-US" });

    try {
      await studentContext.addInitScript(() => window.localStorage.setItem("gradex.locale", "en"));
      // The access workspace is bilingual now, so the Admin context needs the same.
      await adminContext.addInitScript(() => window.localStorage.setItem("gradex.locale", "en"));
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

      // The screen opened by itself and already knows which challenge it is
      // about, so it asks for a code rather than for the address the Student
      // typed one screen ago.
      const challengeID = new URL(studentPage.url()).searchParams.get("challenge");
      expect(challengeID).toMatch(/^[0-9a-f-]{36}$/);
      await expect(studentPage.getByTestId("verification-masked-email")).toBeVisible();
      await expect(studentPage.getByTestId("verification-masked-email")).not.toContainText(
        STUDENT_EMAIL,
      );

      const verification = queryEmailVerificationAction(STUDENT_EMAIL);
      expect(verification.account_id).toMatch(/^[0-9a-f-]{36}$/);
      expect(verification.verification_code).toMatch(/^\d{6}$/);

      // A wrong code is recoverable: it is refused without consuming the live
      // challenge, and it costs one of the five attempts rather than the
      // challenge itself.
      const wrongCode = verification.verification_code === "000000" ? "111111" : "000000";
      await studentPage.getByTestId("verification-code-input").fill(wrongCode);
      await studentPage.getByTestId("verification-code-submit").click();
      await expect(studentPage.getByRole("alert").first()).toBeVisible();
      await expect(studentPage).toHaveURL(/\/verify-email\?/);

      // Proving the code activates the Account and signs the Student in, in one
      // step. No password is asked for: the code already proved control of the
      // mailbox, and this response is the session.
      const verificationResponsePromise = studentPage.waitForResponse(
        (response) =>
          response.url().endsWith("/api/v1/email-verification-codes") &&
          response.request().method() === "POST",
      );
      await studentPage.getByTestId("verification-code-input").fill(verification.verification_code);
      await studentPage.getByTestId("verification-code-submit").click();
      const verificationResponse = await verificationResponsePromise;
      expect(verificationResponse.status()).toBe(201);
      const sessionBody = (await verificationResponse.json()) as {
        role: string;
        csrf_token: string;
      };
      expect(sessionBody.role).toBe("STUDENT");
      expect(sessionBody.csrf_token).toBeTruthy();
      await studentPage.waitForURL((url) => /\/(en|ar)\/learn\/dashboard$/.test(url.pathname));
      // Nothing about the challenge or the code survives in the address bar.
      await expect(studentPage).not.toHaveURL(/challenge=|code=|token=/);

      // Admin uses a production-valid session and configures the launch Course.
      const adminSession = await installIssuedSession(adminContext, ADMIN);
      const adminPage = await adminContext.newPage();
      await adminPage.goto("/en/admin/course-access");
      await expect(adminPage.locator("h1")).toContainText("Course access");

      // The launch Course is chosen by title; no Course identifier is typed.
      const courseSelect = adminPage.getByTestId("course-access-course-select");
      await expect(courseSelect).toBeVisible();
      await expect(adminPage.locator("body")).not.toContainText("Course ID (UUID)");
      await courseSelect.selectOption(COURSE_ID);
      await expect(adminPage.getByTestId("course-access-selected-course")).toBeVisible();
      await adminPage.locator("#access-expiry-date").fill("2026-12-31");
      await adminPage
        .locator("#access-expiry-reason")
        .first()
        .fill("August 15 S11 release acceptance");
      await adminPage.getByTestId("access-expiry-submit").click();
      await expect(adminPage.getByTestId("course-access-notice")).toContainText("The default access period was saved.");

      await adminPage.locator("#access-invite-email").fill(STUDENT_EMAIL);
      await adminPage.getByTestId("access-invite-submit").click();
      await expect(adminPage.getByTestId("access-queue").locator("table")).toContainText(STUDENT_EMAIL);

      // Taken from the row's own test hook: the Admin queue identifies an invitation by its
      // invitee and no longer prints the identifier as readable text.
      const invitationID = await invitationIdFromRow(adminPage, STUDENT_EMAIL);
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
      expect(await protectedMutationStatuses(studentPage, sessionBody.csrf_token)).toEqual({
        playback: 404,
        progress: 404,
      });
      await expectZeroGrantState(verification.account_id);

      // Admin Approval is the sole grant trigger.
      await adminPage.getByRole("button", { name: "Refresh" }).click();
      const approveButton = adminPage.locator(
        // Addressed by the invitee, which is how the queue identifies an invitation to an Admin.
        `tr:has(td:has-text("${STUDENT_EMAIL}")) button:has-text("Approve")`,
      );
      const approvalResponsePromise = adminPage.waitForResponse(
        (response) =>
          response.url().endsWith(`/course-access-invitations/${invitationID}/approve`) &&
          response.request().method() === "POST",
      );
      await approveButton.click();
      // Granting access is a confirmed decision, not a single click.
      await adminPage.getByTestId("access-queue-confirm").getByTestId("confirm-accept").click();
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
          csrfToken: sessionBody.csrf_token,
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
