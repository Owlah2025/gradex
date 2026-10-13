import { expect, test, type BrowserContext, type Page } from "@playwright/test";
import { frontendOrigin } from "../src/lib/api/e2e-ports";
import { issueRotatingSession } from "./rotating-students";
import {
  completePurchaseConfirmation,
  registerAndVerifyStudent,
  watchForPrematurePurchase,
  WHATSAPP_STUB,
} from "./student-journey";

/**
 * D-117 — WhatsApp contact and course-request reliability, against real layers.
 *
 * The production failure: a purchase request was persisted, the WhatsApp
 * handoff did not happen, and the Admin had no way to reach the Student because
 * Gradex never held a number. These journeys prove the repaired chain end to
 * end: the reminder, saving a number, a request that cannot be created without
 * one, a success state that does not depend on a popup, recovery of a pending
 * request, and the Admin reaching the Student from the stored number.
 */

const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };
const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const PASSWORD = process.env.GRADEX_E2E_REGISTRATION_PASSWORD || "KuwaitStudy!2026";

test.describe.configure({ timeout: 180_000 });

async function installAdminSession(context: BrowserContext, locale: "en" | "ar") {
  const session = issueRotatingSession(ADMIN);
  const origin = new URL(frontendOrigin());
  await context.addInitScript((value) => {
    window.localStorage.setItem("gradex.locale", value);
  }, locale);
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

async function stubWhatsApp(context: BrowserContext) {
  // CI never reaches WhatsApp: the external destination is stubbed.
  await context.route("https://wa.me/**", (route) =>
    route.fulfill({ status: 200, contentType: "text/html", body: WHATSAPP_STUB }),
  );
}

async function expectNoHorizontalScroll(page: Page) {
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1))
    .toBe(true);
}

test("English desktop: reminder, save, request with a popup blocker, recovery, and Admin contact", async ({ browser }) => {
  const email = "contact-reliability-en@example.test";
  const studentContext = await browser.newContext({ locale: "en-US", viewport: { width: 1280, height: 900 } });
  const adminContext = await browser.newContext({ locale: "en-US", viewport: { width: 1280, height: 900 } });
  try {
    await stubWhatsApp(studentContext);
    // Simulates a popup blocker: any scripted window.open is refused. The
    // repaired flow never calls it, so nothing here may depend on it.
    await studentContext.addInitScript(() => {
      window.open = () => null;
    });
    await studentContext.addInitScript(() => window.localStorage.setItem("gradex.locale", "en"));
    const page = await studentContext.newPage();

    await registerAndVerifyStudent(page, { email, password: PASSWORD, displayName: "Contact Reliability" });
    await page.waitForURL((url) => /\/(en|ar)\/learn\/dashboard$/.test(url.pathname));
    if (!page.url().includes("/en/")) await page.goto("/en/learn/dashboard");

    // 1. A Student with no number sees the reminder, and is not blocked by it.
    const prompt = page.getByTestId("contact-number-prompt");
    await expect(prompt).toBeVisible();
    await expect(prompt).toContainText("Complete Your Contact Information");
    await expect(prompt).toContainText("Add your WhatsApp number so the GradeX team can contact you");
    await expect(page.locator("html")).toHaveAttribute("dir", "ltr");

    // 2. The number is validated before it is saved, then saved inline.
    await page.getByTestId("contact-number-prompt-action").click();
    await page.getByTestId("prompt-contact-number").fill("2212 3456");
    await page.getByTestId("prompt-contact-save").click();
    await expect(page.getByTestId("prompt-contact-error")).toBeVisible();
    await page.getByTestId("prompt-contact-number").fill("5001 2345");
    const saved = page.waitForResponse(
      (response) => new URL(response.url()).pathname === "/api/v1/me/profile" && response.request().method() === "PUT",
    );
    await page.getByTestId("prompt-contact-save").click();
    expect((await saved).status()).toBe(200);
    // It disappears the moment the number exists, and stays gone.
    await expect(prompt).toHaveCount(0);
    await page.reload();
    await expect(page.getByTestId("continue-learning").or(page.locator("main"))).toBeVisible();
    await expect(page.getByTestId("contact-number-prompt")).toHaveCount(0);

    // 3. The profile says "Provided" — never "Verified" — and shows the number.
    await page.goto("/en/learn/profile");
    await expect(page.getByTestId("contact-number-status")).toHaveText("Provided");
    await expect(page.getByTestId("contact-number-current")).toHaveText("+965 500 12345");
    await expect(page.getByTestId("contact-number-card")).not.toContainText(/verified/i);

    // 4. The request uses the number on file, saves, and shows success without
    //    any automatic window. WhatsApp opens only from the explicit link.
    await page.goto(`/en/catalog/${COURSE_ID}`);
    const watch = watchForPrematurePurchase(page);
    await page.getByTestId("purchase-request-open").click();
    await expect(page.getByTestId("purchase-contact-current")).toContainText("+965 500 12345");
    watch.assertQuiet();
    const { response, handoffURL } = await completePurchaseConfirmation(page);
    expect(handoffURL).toBe(response.whatsapp_url);
    expect(response.reused).toBe(false);
    await expect(page.getByTestId("purchase-handoff")).toContainText("Your course request was submitted successfully.");
    await expect(page.getByTestId("purchase-handoff")).toContainText("Continue to WhatsApp to complete payment.");
    const message = new URL(handoffURL).searchParams.get("text") || "";
    expect(message).toContain(response.reference);
    expect(message).toContain("CS101");

    // 5. Returning to the Course shows the pending request and its link again;
    //    no second request is created.
    await page.goto(`/en/catalog/${COURSE_ID}`);
    const pending = page.getByTestId("purchase-pending");
    await expect(pending).toBeVisible();
    await expect(page.getByTestId("purchase-handoff-reference")).toHaveText(response.reference);
    await expect(page.getByTestId("purchase-handoff-link")).toHaveAttribute("href", response.whatsapp_url);
    await expect(page.getByTestId("purchase-request-open")).toHaveCount(0);

    // 6. The access page lists it with the same link.
    await page.goto("/en/access");
    const row = page.getByTestId("pending-payment-request");
    await expect(row).toHaveCount(1);
    await expect(row).toContainText(response.reference);
    await expect(page.getByTestId("pending-payment-request-link")).toHaveAttribute("href", response.whatsapp_url);

    // 7. The Admin finds the Student, sees the number, and can open WhatsApp.
    await installAdminSession(adminContext, "en");
    const admin = await adminContext.newPage();
    await admin.goto("/en/admin/users");
    await admin.locator("#admin-users-search").fill(email);
    await admin.getByTestId("admin-users-whatsapp-filter").selectOption("PROVIDED");
    await admin.getByTestId("admin-users-pending-payment-filter").check();
    await admin.getByRole("button", { name: "Apply filters" }).click();
    const directoryRow = admin.getByRole("row").filter({ hasText: email });
    await expect(directoryRow).toBeVisible();
    await expect(directoryRow.getByTestId("admin-request-contact").getByRole("link")).toHaveAttribute(
      "href",
      "https://wa.me/96550012345",
    );
    await expect(directoryRow.getByTestId("admin-directory-pending-payments")).toContainText("1 waiting for payment");

    // The same Student does not appear under "Missing WhatsApp".
    await admin.getByTestId("admin-users-whatsapp-filter").selectOption("MISSING");
    await admin.getByTestId("admin-users-pending-payment-filter").uncheck();
    await admin.getByRole("button", { name: "Apply filters" }).click();
    await expect(admin.getByTestId("admin-users-empty").or(admin.getByRole("row").first())).toBeVisible();
    await expect(admin.getByRole("row").filter({ hasText: email })).toHaveCount(0);

    // User 360: number, status, direct chat link, copy.
    await admin.getByTestId("admin-users-whatsapp-filter").selectOption("");
    await admin.getByRole("button", { name: "Apply filters" }).click();
    await admin.getByRole("row").filter({ hasText: email }).getByRole("link", { name: "Contact Reliability" }).click();
    const contact = admin.getByTestId("admin-student-contact");
    await expect(contact).toBeVisible();
    await expect(admin.getByTestId("admin-student-contact-status")).toHaveText("Provided");
    await expect(admin.getByTestId("admin-student-contact-number")).toHaveText("+965 500 12345");
    const chat = admin.getByTestId("admin-student-contact-chat");
    await expect(chat).toHaveAttribute("href", "https://wa.me/96550012345");
    await expect(chat).toHaveAttribute("target", "_blank");
    await expect(chat).toHaveAttribute("rel", "noopener noreferrer");
    await expect(admin.getByTestId("admin-contact-copy")).toBeVisible();

    // The Admin request queue shows the number captured with the request.
    await admin.goto("/en/admin/course-access");
    await admin.locator("#purchase-request-search").fill(response.reference);
    await admin.getByRole("button", { name: "Search" }).click();
    const requestRow = admin.getByTestId(`purchase-request-${response.reference}`);
    await expect(requestRow.getByTestId("admin-request-contact")).toContainText("+965 500 12345");
    await expect(requestRow.getByTestId("admin-request-contact")).toContainText("Given with this request");
  } finally {
    await studentContext.close();
    await adminContext.close();
  }
});

test("Arabic mobile: RTL reminder, a request refused until a number is given, and an international number", async ({ browser }) => {
  const email = "contact-reliability-ar@example.test";
  const context = await browser.newContext({ locale: "ar-KW", viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
  try {
    await stubWhatsApp(context);
    await context.addInitScript(() => window.localStorage.setItem("gradex.locale", "ar"));
    const page = await context.newPage();
    await registerAndVerifyStudent(page, { email, password: PASSWORD, displayName: "طالب التواصل" });
    await page.waitForURL((url) => /\/(en|ar)\/learn\/dashboard$/.test(url.pathname));
    if (!page.url().includes("/ar/")) await page.goto("/ar/learn/dashboard");

    await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
    const prompt = page.getByTestId("contact-number-prompt");
    await expect(prompt).toContainText("أكمل بيانات التواصل الخاصة بك");
    await expect(prompt).toContainText("يرجى إضافة رقم الواتساب الخاص بك");
    await expect(page.getByTestId("contact-number-prompt-action")).toHaveText("إضافة رقم الواتساب");
    await expectNoHorizontalScroll(page);

    // "Later" hides it for this session without saving anything.
    await page.getByTestId("contact-number-prompt-later").click();
    await expect(prompt).toHaveCount(0);

    // The purchase confirmation asks for the number and refuses to create a
    // request without one — no request is sent at all.
    await page.goto(`/ar/catalog/${COURSE_ID}`);
    await page.getByTestId("purchase-request-open").click();
    const numberField = page.getByTestId("purchase-contact-number");
    await expect(numberField).toBeVisible();
    await expect(numberField).toHaveAttribute("dir", "ltr");
    await expect(page.getByTestId("purchase-contact-region")).toHaveValue("KW");
    await expectNoHorizontalScroll(page);

    let posts = 0;
    page.on("request", (request) => {
      if (request.method() === "POST" && new URL(request.url()).pathname === "/api/v1/me/purchase-requests") posts += 1;
    });
    await expect(page.getByTestId("purchase-request-submit")).toBeEnabled();
    await page.getByTestId("purchase-request-submit").click();
    await expect(page.getByTestId("purchase-contact-error")).toHaveText("أضف رقم الواتساب للمتابعة.");
    expect(posts).toBe(0);

    // An Egyptian number, chosen from the country list.
    await page.getByTestId("purchase-contact-region").selectOption("EG");
    const { response } = await completePurchaseConfirmation(page, { whatsappNational: "100 123 4567" });
    expect(posts).toBe(1);
    await expect(page.getByTestId("purchase-handoff")).toContainText("تم إرسال طلب الاشتراك في الكورس بنجاح.");
    await expect(page.getByTestId("purchase-handoff-link")).toContainText("المتابعة إلى واتساب");
    await expectNoHorizontalScroll(page);

    // The number reached the Student's profile, normalized.
    const profile = await page.evaluate(async () => {
      const result = await fetch("/api/v1/me/profile", { credentials: "include", headers: { "Accept-Language": "ar" } });
      return (await result.json()) as { whatsapp_number: string | null };
    });
    expect(profile.whatsapp_number).toBe("+201001234567");

    // The reminder never returns once the number exists, even in a new session.
    await page.evaluate(() => window.sessionStorage.clear());
    await page.goto("/ar/learn/dashboard");
    await expect(page.locator("main")).toBeVisible();
    await expect(page.getByTestId("contact-number-prompt")).toHaveCount(0);

    // Recovery on mobile.
    await page.goto("/ar/access");
    await expect(page.getByTestId("pending-payment-request")).toContainText(response.reference);
    await expectNoHorizontalScroll(page);
  } finally {
    await context.close();
  }
});
