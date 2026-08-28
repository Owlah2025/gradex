import { expect, test } from "@playwright/test";
import { queryLearningState } from "../src/lib/api/e2e-progress";
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
const EXISTING_STUDENT = {
	email: "student-purchase-existing@example.test",
	accountID: "a0000000-0000-0000-0000-000000000005",
};
const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const LESSON_ID = "30000000-0000-0000-0000-000000000001";
const NEW_STUDENT_EMAIL = "purchase-new-student@example.test";
const EXISTING_STUDENT_PURCHASE_EMAIL = EXISTING_STUDENT.email;
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

async function createPurchaseIntentAndCaptureHandoff(
  page: import("@playwright/test").Page,
  email: string,
) {
  await page.goto(`/en/catalog/${COURSE_ID}`);
  await expect(page.getByRole("heading", { name: /CS101/ })).toBeVisible();
  await expect(page.locator("main")).toContainText("25.000");
  await page.getByTestId("purchase-request-open").click();
  await page.getByTestId("purchase-request-email").fill(email);

  // The page intentionally leaves the origin as soon as its fetch resolves.
  // Capture the API response in the route proxy before fulfilling the browser,
  // so Playwright never tries to read a response body after that navigation.
  let persistedPayload: { reference: string; whatsapp_url: string } | undefined;
  await page.route("**/api/v1/purchase-requests", async (route) => {
    if (route.request().method() !== "POST") {
      await route.continue();
      return;
    }
    const upstream = await route.fetch();
    const body = await upstream.body();
    persistedPayload = JSON.parse(body.toString("utf-8")) as {
      reference: string;
      whatsapp_url: string;
    };
    await route.fulfill({ response: upstream, body });
  });
  const persisted = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === "/api/v1/purchase-requests" &&
      response.request().method() === "POST",
  );
  const handoff = page.waitForRequest(
    (request) =>
      request.isNavigationRequest() &&
      request.url().startsWith("https://wa.me/"),
  );
  await page
    .getByTestId("purchase-request-submit")
    .click({ noWaitAfter: true });
  const persistedResponse = await persisted;
  expect(persistedResponse.status()).toBe(201);
  const handoffRequest = await handoff;
  expect(persistedPayload).toBeDefined();
  return { response: persistedPayload!, handoffURL: handoffRequest.url() };
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

      const { response, handoffURL } =
        await createPurchaseIntentAndCaptureHandoff(
          studentPage,
          NEW_STUDENT_EMAIL,
        );
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

      // The invitation context begins with its bearer in a fragment. An
      // unregistered recipient goes through real registration and verification
      // with only invitation_id in returnTo, never the bearer.
      await studentPage.goto(
        `/en/access?invitation_id=${confirmed.invitation.id}#token=${encodeURIComponent(invitationToken)}`,
      );
      await studentPage.waitForURL((url) => url.pathname === "/login");
      expect(new URL(studentPage.url()).searchParams.get("returnTo")).toContain(
        `invitation_id=${confirmed.invitation.id}`,
      );
      expect(studentPage.url()).not.toContain("token=");
      await studentPage.locator('a[href^="/register"]').click();
      await studentPage.waitForURL((url) => url.pathname === "/register");
      await studentPage.locator("#display-name").fill("Purchase Student");
      await studentPage.locator("#email").fill(NEW_STUDENT_EMAIL);
      await studentPage.locator("#password").fill(NEW_STUDENT_PASSWORD);
      const policies = studentPage.locator('fieldset input[type="checkbox"]');
      await expect(policies).toHaveCount(2);
      await policies.nth(0).check();
      await policies.nth(1).check();
      // Not `form button`: the password field now carries a reveal control, so
      // the form holds more than one. And not the button's name either — these
      // auth routes carry no locale segment and render in the reader's stored
      // language, so an English name matches nothing on an Arabic page.
      await studentPage.locator('form button[type="submit"]').click();
      await studentPage.waitForURL((url) => url.pathname === "/verify-email");

      const verification = queryEmailVerificationAction(NEW_STUDENT_EMAIL);
      const verificationReturnTo = new URL(studentPage.url()).searchParams.get(
        "returnTo",
      );
      expect(verificationReturnTo).toContain(
        `invitation_id=${confirmed.invitation.id}`,
      );
      await studentPage.goto(
        `/verify-email/result?returnTo=${encodeURIComponent(verificationReturnTo!)}#token=${encodeURIComponent(verification.verification_token)}`,
      );
      await expect(studentPage.getByRole("status")).toContainText(
        /Email confirmed|تم تأكيد البريد/,
      );
      expect(studentPage.url()).not.toContain("token=");
      await studentPage.locator('a[href^="/login"]').click();
      await studentPage.locator("#email").fill(NEW_STUDENT_EMAIL);
      await studentPage.locator("#password").fill(NEW_STUDENT_PASSWORD);
      // Same reason as the registration submit above.
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
      const state = queryLearningState(verification.account_id, COURSE_ID);
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

      const { response } = await createPurchaseIntentAndCaptureHandoff(
        studentPage,
        EXISTING_STUDENT_PURCHASE_EMAIL,
      );
      const confirmed = await confirmPurchaseInAdminUI(
        adminPage,
        EXISTING_STUDENT_PURCHASE_EMAIL,
      );
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

      const first = await createPurchaseIntentAndCaptureHandoff(
        studentPage,
        CANCELLED_PURCHASE_EMAIL,
      );
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

      const fresh = await createPurchaseIntentAndCaptureHandoff(
        studentPage,
        CANCELLED_PURCHASE_EMAIL,
      );
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
