import { expect, test } from "@playwright/test";
import { queryLearningState } from "../src/lib/api/e2e-progress";
import { frontendOrigin } from "../src/lib/api/e2e-ports";
import {
  issueRotatingSession,
  queryEmailVerificationAction,
  queryInvitationToken,
} from "./rotating-students";
import {
  completePurchaseConfirmation,
  registerAndVerifyStudent,
  watchForPrematurePurchase,
} from "./student-journey";

const ADMIN = {
  email: "admin@example.test",
  accountID: "a0000000-0000-0000-0000-000000000000",
};
const EXISTING_STUDENT = {
	email: "student-purchase-existing@example.test",
	accountID: "a0000000-0000-0000-0000-000000000005",
};
const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const LESSON_ID = "30000000-0000-0000-0000-000000000001";
const NEW_STUDENT_EMAIL = "purchase-new-student@example.test";
// A purchase request now belongs to a verified Student, so each test either
// registers one or installs the seeded one's session. Distinct addresses keep
// the per-Student active-request uniqueness from making one test's outcome
// depend on another's.
const CANCELLED_PURCHASE_EMAIL = "purchase-cancelled@example.test";
const NEW_STUDENT_PASSWORD =
  process.env.GRADEX_E2E_REGISTRATION_PASSWORD || "KuwaitStudy!2026";

test.describe.configure({ timeout: 150_000 });

async function installAdminSession(
  context: import("@playwright/test").BrowserContext,
) {
  const session = issueRotatingSession(ADMIN);
  const origin = new URL(frontendOrigin());
  // The access workspace is bilingual now, so a spec that means English has to say so.
  await context.addInitScript(() => {
    window.localStorage.setItem("gradex.locale", "en");
  });
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

async function configurePurchaseableCourse(
  page: import("@playwright/test").Page,
) {
  await page.goto("/en/admin/course-access");
  await expect(
    page.getByRole("heading", { name: "Course access", exact: true }),
  ).toBeVisible();
  const course = page.getByTestId("course-access-course-select");
  await course.selectOption(COURSE_ID);
  await page.locator("#access-expiry-date").fill("2026-12-31");
  await page
    .locator("#access-expiry-reason")
    .first()
    .fill("Manual purchase E2E access term");
  await page.getByTestId("access-expiry-submit").click();
  await expect(page.getByTestId("course-access-notice")).toContainText(
    "The default access period was saved.",
  );
}

/**
 * The Student journey from Course Details to the WhatsApp handoff.
 *
 * Two things changed and both are asserted here rather than assumed. A purchase
 * request belongs to a verified Student, so the caller must already hold a
 * session; and pressing "Buy this course" opens a confirmation rather than
 * creating anything, so the run is watched from that press until the explicit
 * confirmation and must stay completely quiet in between.
 */
async function purchaseFromConfirmation(
  page: import("@playwright/test").Page,
  expected: { email: string },
) {
  await page.goto(`/en/catalog/${COURSE_ID}`);
  await expect(page.getByRole("heading", { name: /CS101/ })).toBeVisible();
  await expect(page.locator("main")).toContainText("25.000");

  const watch = watchForPrematurePurchase(page);
  await page.getByTestId("purchase-request-open").click();

  // What is being requested, stated before it is requested. Both values come
  // from the Course as the server describes it; the browser supplies neither.
  const confirmation = page.getByTestId("purchase-confirmation");
  await expect(confirmation).toBeVisible();
  await expect(page.getByTestId("purchase-course-title")).toContainText("CS101");
  await expect(page.getByTestId("purchase-price")).toContainText("25.000");
  // Nothing has been created and nothing has been opened.
  watch.assertQuiet();

  const handoff = await completePurchaseConfirmation(page);
  expect(handoff.response.reference).toMatch(/^GRX-[A-F0-9]{16}$/);
  expect(handoff.handoffURL).toBe(handoff.response.whatsapp_url);
  // The address on the request is the one the server read off the Account.
  const message = new URL(handoff.handoffURL).searchParams.get("text") || "";
  expect(message).toContain(expected.email);
  return handoff;
}

async function confirmPurchaseInAdminUI(
  page: import("@playwright/test").Page,
  email: string,
) {
  const search = page.locator("#purchase-request-search");
  await search.fill(email);
  await page.getByRole("button", { name: "Search" }).click();
  const row = page.getByRole("row").filter({ hasText: email });
  await expect(row).toContainText("CS101");
  await expect(row).toContainText("25.000");
  const confirmation = page.waitForResponse(
    (response) =>
      /\/api\/v1\/admin\/purchase-requests\/[^/]+\/confirm-payment$/.test(
        new URL(response.url()).pathname,
      ) && response.request().method() === "POST",
  );
  await row.getByRole("button", { name: /Confirm payment & send invitation/ }).click();
  // Gradex takes no money, so recording a payment as received is a deliberate confirmed step and
  // the confirmation says exactly that.
  const paymentDialog = page.getByTestId("purchase-request-confirm");
  await expect(paymentDialog).toBeVisible();
  await expect(paymentDialog).toContainText("Gradex does not take or verify money");
  await paymentDialog.getByTestId("confirm-accept").click();
  const confirmationResponse = await confirmation;
  expect(confirmationResponse.status()).toBe(200);
  const confirmationBody = (await confirmationResponse.json()) as {
    invitation: { id: string };
    purchase_request: { reference: string; state: string };
  };
  expect(confirmationBody.purchase_request.state).toBe("INVITATION_CREATED");
  await expect(page.getByText("The payment was recorded and the invitation sent.")).toBeVisible();
  return confirmationBody;
}

test.describe("Automated manual Course purchase flow", () => {
  test("new Student persists purchase intent before WhatsApp, registers, accepts once, and enters Course Home", async ({
    browser,
  }) => {
    const studentContext = await browser.newContext({ locale: "en-US" });
    const adminContext = await browser.newContext({ locale: "en-US" });
    try {
      // The external handoff is intercepted so CI sends no WhatsApp message.
      await studentContext.route("https://wa.me/**", (route) => route.abort());
      const studentPage = await studentContext.newPage();
      const adminPage = await adminContext.newPage();
      await installAdminSession(adminContext);
      await configurePurchaseableCourse(adminPage);

      // The Student exists before the purchase does. Registration mails a
      // six-digit code, the code is entered on the screen that opened by
      // itself, and proving it signs the Student in — no password re-entry,
      // because the code already proved control of the mailbox.
      await registerAndVerifyStudent(studentPage, {
        email: NEW_STUDENT_EMAIL,
        password: NEW_STUDENT_PASSWORD,
        displayName: "Purchase Student",
      });
      await studentPage.waitForURL((url) => /\/(en|ar)\/learn\/dashboard$/.test(url.pathname));

      const { response, handoffURL } = await purchaseFromConfirmation(studentPage, {
        email: NEW_STUDENT_EMAIL,
      });
      expect(response.reference).toMatch(/^GRX-[A-F0-9]{16}$/);
      expect(handoffURL).toBe(response.whatsapp_url);
      const message = new URL(handoffURL).searchParams.get("text") || "";
      for (const expected of [
        "CS101",
        "25.000 KWD",
        NEW_STUDENT_EMAIL,
        response.reference,
      ]) {
        expect(message).toContain(expected);
      }
      for (const forbidden of [COURSE_ID, "token=", "invitation_id"]) {
        expect(message).not.toContain(forbidden);
      }

      const confirmed = await confirmPurchaseInAdminUI(
        adminPage,
        NEW_STUDENT_EMAIL,
      );
      const invitationToken = queryInvitationToken(confirmed.invitation.id);
      expect(invitationToken).toBeTruthy();

      // The invitation context begins with its bearer in a fragment. The
      // Student is signed out first, so the journey back in is the real one:
      // the access route refuses an anonymous reader, carries only
      // invitation_id in returnTo, and never the bearer.
      //
      // Signing in with the password is also the assertion that the password
      // survived verification — proving the emailed code authenticated the
      // Student without replacing, consuming, or bypassing their credential.
      await studentPage.goto("/en/learn/dashboard");
      await studentPage
        .getByRole("button", { name: /sign out/i })
        .first()
        .click();
      await studentPage.waitForURL((url) => url.pathname === "/login");

      await studentPage.goto(
        `/en/access?invitation_id=${confirmed.invitation.id}#token=${encodeURIComponent(invitationToken)}`,
      );
      await studentPage.waitForURL((url) => url.pathname === "/login");
      expect(new URL(studentPage.url()).searchParams.get("returnTo")).toContain(
        `invitation_id=${confirmed.invitation.id}`,
      );
      expect(studentPage.url()).not.toContain("token=");
      await studentPage.locator("#email").fill(NEW_STUDENT_EMAIL);
      await studentPage.locator("#password").fill(NEW_STUDENT_PASSWORD);
      // Not `form button`: the password field carries a reveal control, so the
      // form holds more than one.
      await studentPage.locator('form button[type="submit"]').click();
      await studentPage.waitForURL(
        (url) =>
          /\/(en|ar)\/access$/.test(url.pathname) &&
          url.searchParams.get("invitation_id") === confirmed.invitation.id,
      );
      expect(studentPage.url()).not.toContain("token=");
      await expect(studentPage.getByTestId("accept-invitation")).toBeVisible();

      await studentPage.getByTestId("accept-invitation").click();
      await studentPage.waitForURL(
        (url) =>
          /\/(en|ar)\/learn\/courses\//.test(url.pathname) &&
          url.pathname.endsWith(`/courses/${COURSE_ID}`),
      );
      expect(studentPage.url()).not.toContain("token=");
      await expect(studentPage.locator("main")).toContainText("CS101");
      const state = queryLearningState(
        queryEmailVerificationAction(NEW_STUDENT_EMAIL).account_id,
        COURSE_ID,
      );
      expect(state.entitlement.count).toBe(1);
      expect(state.enrollment.count).toBe(1);

      // The Admin reads a factual terminal state rather than doing a second
      // approval. The semantic confirm action is gone once it has succeeded.
      await adminPage
        .locator("#purchase-request-search")
        .fill(response.reference);
      await adminPage.getByRole("button", { name: "Search" }).click();
      const finished = adminPage.getByTestId(
        `purchase-request-${response.reference}`,
      );
      await expect(finished).toContainText("Access granted");
      await expect(
        finished.getByRole("button", { name: /Confirm payment & send invitation/ }),
      ).toHaveCount(0);

      // Course Home is server-authorized; the protected Lesson is now reachable too.
      await studentPage.goto(
        `/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`,
      );
      await expect(studentPage.locator("video")).toBeVisible();
    } finally {
      await studentContext.close();
      await adminContext.close();
    }
  });

  test("existing Student purchase reaches automatic access, while legacy invitation behavior stays in S6", async ({
    browser,
  }) => {
    const studentContext = await browser.newContext({ locale: "en-US" });
    const adminContext = await browser.newContext({ locale: "en-US" });
    try {
      await studentContext.route("https://wa.me/**", (route) => route.abort());
      const studentPage = await studentContext.newPage();
      const adminPage = await adminContext.newPage();
      await installAdminSession(adminContext);
      await configurePurchaseableCourse(adminPage);

      // A purchase request belongs to a verified Student, so the session is
      // installed before the request exists rather than after it.
      const session = issueRotatingSession(EXISTING_STUDENT);
      const origin = new URL(frontendOrigin());
      await studentContext.addCookies([
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

      const { response } = await purchaseFromConfirmation(studentPage, {
        email: EXISTING_STUDENT.email,
      });
      const confirmed = await confirmPurchaseInAdminUI(
        adminPage,
        EXISTING_STUDENT.email,
      );
      await studentPage.goto(
        `/en/access?invitation_id=${confirmed.invitation.id}#token=${encodeURIComponent(queryInvitationToken(confirmed.invitation.id))}`,
      );
      await studentPage.getByTestId("accept-invitation").click();
      await studentPage.waitForURL(
        (url) =>
          /\/(en|ar)\/learn\/courses\//.test(url.pathname) &&
          url.pathname.endsWith(`/courses/${COURSE_ID}`),
      );
      const state = queryLearningState(EXISTING_STUDENT.accountID, COURSE_ID);
      expect(state.entitlement.count).toBe(1);
      expect(state.enrollment.count).toBe(1);
      await adminPage.locator("#purchase-request-search").fill(response.reference);
      await adminPage.getByRole("button", { name: "Search" }).click();
      await expect(
        adminPage.getByTestId(`purchase-request-${response.reference}`),
      ).toContainText("Access granted");
    } finally {
      await studentContext.close();
      await adminContext.close();
    }
  });

  test("Admin cancellation releases a paid invitation request for a fresh purchase intent", async ({
    browser,
  }) => {
    const studentContext = await browser.newContext({ locale: "en-US" });
    const adminContext = await browser.newContext({ locale: "en-US" });
    try {
      await studentContext.route("https://wa.me/**", (route) => route.abort());
      const studentPage = await studentContext.newPage();
      const adminPage = await adminContext.newPage();
      await installAdminSession(adminContext);
      await configurePurchaseableCourse(adminPage);

      await registerAndVerifyStudent(studentPage, {
        email: CANCELLED_PURCHASE_EMAIL,
        password: NEW_STUDENT_PASSWORD,
        displayName: "Cancelled Purchase Student",
      });
      await studentPage.waitForURL((url) => /\/(en|ar)\/learn\/dashboard$/.test(url.pathname));

      const first = await purchaseFromConfirmation(studentPage, {
        email: CANCELLED_PURCHASE_EMAIL,
      });
      await confirmPurchaseInAdminUI(adminPage, CANCELLED_PURCHASE_EMAIL);
      const original = adminPage.getByTestId(
        `purchase-request-${first.response.reference}`,
      );
      const cancellation = adminPage.waitForResponse(
        (response) =>
          /\/api\/v1\/admin\/purchase-requests\/[^/]+\/cancel$/.test(
            new URL(response.url()).pathname,
          ) && response.request().method() === "POST",
      );
      await original.getByRole("button", { name: /Cancel request/ }).click();
      await adminPage
        .getByTestId("purchase-request-confirm")
        .getByTestId("confirm-accept")
        .click();
      expect((await cancellation).status()).toBe(200);
      await expect(original).toContainText("Cancelled");

      // The cancelled request no longer occupies the per-Student active slot,
      // so the same Student may ask again and receives a new reference.
      const fresh = await purchaseFromConfirmation(studentPage, {
        email: CANCELLED_PURCHASE_EMAIL,
      });
      expect(fresh.response.reference).not.toBe(first.response.reference);
      await adminPage.locator("#purchase-request-search").fill(fresh.response.reference);
      await adminPage.getByRole("button", { name: "Search" }).click();
      await expect(
        adminPage.getByTestId(`purchase-request-${fresh.response.reference}`),
      ).toContainText("Waiting for payment");
    } finally {
      await studentContext.close();
      await adminContext.close();
    }
  });
});
