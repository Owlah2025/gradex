import { expect, test, type Page } from "@playwright/test";
import { frontendOrigin } from "../src/lib/api/e2e-ports";
import { issueRotatingSession } from "./rotating-students";

const bundle = {
  id: "30000000-0000-0000-0000-000000000001",
  slug: "bundle-30000000000000000000000000000001",
  title: "Engineering Starter Pack",
  description: "Digital foundations in one Bundle.",
  course_count: 3,
  price: { minor_units: 90000, regular_minor_units: 120000, offer_minor_units: 90000, currency: "KWD" },
  members: [0, 1, 2].map((position) => ({
    course_id: `20000000-0000-0000-0000-00000000000${position + 1}`,
    slug: `course-${position + 1}`,
    title: ["Digital Logic", "Programming", "Computer Architecture"][position],
    instructor_display_name: "Gradex Instructor",
    subject: { label: "Computer Engineering", code: `CE-${position + 1}` },
    position,
  })),
};

async function installSession(
  context: import("@playwright/test").BrowserContext,
  account: { accountID: string; email: string },
) {
  const session = issueRotatingSession(account);
  const origin = new URL(frontendOrigin());
  await context.addInitScript(() => window.localStorage.setItem("gradex.locale", "en"));
  await context.addCookies([{ name: session.cookie_name, value: session.cookie_value, domain: origin.hostname, path: "/", httpOnly: true, secure: true, sameSite: "Strict" }]);
}

async function mockSession(page: Page, role: "STUDENT" | "ADMIN" | "ANONYMOUS") {
  await page.route("**/api/v1/session/bootstrap", (route) => route.fulfill({ json: { csrf_token: "csrf-token" } }));
  await page.route("**/api/v1/session", (route) => role === "ANONYMOUS"
    ? route.fulfill({ status: 401, json: { type: "https://api.gradex.com/problems/authentication-required", status: 401 } })
    : route.fulfill({ json: { status: "ACTIVE", role, csrf_token: "csrf-token", display_name: "Test User", idle_expires_at: new Date(Date.now() + 3600000).toISOString(), absolute_expires_at: new Date(Date.now() + 7200000).toISOString() } }));
  await page.route("**/api/v1/me/academic-profile", (route) => route.fulfill({ status: 404, json: { status: 404 } }));
}

test("Bundle detail shows an accessible offer and sends only Bundle identity", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockSession(page, "STUDENT");
  await page.route("**/api/v1/catalog/bundles/**", (route) => route.fulfill({ json: bundle }));
  await page.route("**/api/v1/me/purchase-requests", async (route) => {
    if (route.request().method() === "GET") return route.fulfill({ json: { purchase_requests: [] } });
    expect(route.request().postDataJSON()).toEqual({ bundle_id: bundle.id });
    return route.fulfill({ json: { reference: "GRX-BUNDLE", whatsapp_url: "https://wa.me/96500000000", bundle_title: bundle.title, bundle_items: bundle.members.map((member) => ({ course_id: member.course_id, position: member.position, course_title: member.title })), price_minor_units: 90000, currency: "KWD", state: "WAITING_PAYMENT", reused: false } });
  });
  await page.route("https://wa.me/**", (route) => route.abort());
  await page.goto(`/en/catalog/bundles/${bundle.slug}`);
  await expect(page.getByRole("heading", { level: 1, name: bundle.title })).toBeVisible();
  await expect(page.locator("del")).toContainText("120.000 KWD");
  await expect(page.getByTestId("offer-price")).toContainText("90.000 KWD");
  await expect(page.getByRole("heading", { name: "Courses in this Bundle" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Request Bundle purchase" })).toBeEnabled();
  await expect(page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).resolves.toBe(true);
  await page.getByRole("button", { name: "Request Bundle purchase" }).click();
});

test("Arabic Bundle detail uses RTL and localized commerce copy", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockSession(page, "ANONYMOUS");
  await page.route("**/api/v1/catalog/bundles/**", (route) => route.fulfill({ json: { ...bundle, title: "باقة الهندسة", description: "أساسيات الهندسة في باقة واحدة." } }));
  await page.route("**/api/v1/me/purchase-requests", (route) => route.fulfill({ status: 401, contentType: "application/problem+json", json: { type: "https://api.gradex.com/problems/authentication-required", title: "Authentication required", status: 401, detail: "Sign in.", code: "AUTHENTICATION_REQUIRED" } }));
  await page.goto(`/ar/catalog/bundles/${bundle.slug}`);
  await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
  await expect(page.getByRole("heading", { level: 1, name: "باقة الهندسة" })).toBeVisible();
  await expect(page.getByText("الكورسات المشمولة في الباقة")).toBeVisible();
  await expect(page.getByRole("link", { name: "تسجيل الدخول" })).toBeVisible();
  await expect(page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).resolves.toBe(true);
});

test("Admin creates an ordered bilingual Bundle from published Courses", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockSession(page, "ADMIN");
  await page.route("**/api/v1/catalog/courses**", (route) => {
    const arabic = (route.request().headers()["accept-language"] ?? "").startsWith("ar");
    return route.fulfill({ json: { items: bundle.members.map((member) => ({ id: member.course_id, slug: member.slug, title: arabic ? `مقرر ${member.position + 1}` : member.title, instructor_display_name: member.instructor_display_name, price: { minor_units: 25000, regular_minor_units: 25000, currency: "KWD" }, has_preview: false })), page: 1, page_size: 24, total: 3 } });
  });
  await page.route("**/api/v1/admin/bundles", async (route) => {
    if (route.request().method() === "GET") return route.fulfill({ json: { items: [] } });
    const body = route.request().postDataJSON();
    expect(body.course_ids).toHaveLength(3);
    expect(body).not.toHaveProperty("currency");
    return route.fulfill({ status: 201, json: { id: bundle.id, slug: bundle.slug, lifecycle: "DRAFT", title_ar: body.title_ar, title_en: body.title_en, description_ar: body.description_ar, description_en: body.description_en, revision: 1, course_count: 3, eligible: true, price: { regular_minor_units: 120000, offer_minor_units: 90000, effective_minor_units: 90000, currency: "KWD" }, members: [], created_at: new Date().toISOString(), updated_at: new Date().toISOString() } });
  });
  await page.goto("/en/admin/bundles");
  await page.locator("#bundle-title-ar").fill("باقة الهندسة");
  await page.locator("#bundle-title-en").fill(bundle.title);
  await page.locator("#bundle-description-ar").fill("وصف الباقة");
  await page.locator("#bundle-description-en").fill(bundle.description);
  await page.locator("#bundle-regular").fill("120000");
  await page.locator("#bundle-offer").fill("90000");
  await page.locator("#bundle-price-reason").fill("Launch offer");
  for (const member of bundle.members) await page.getByRole("checkbox", { name: new RegExp(member.title) }).check();
  await expect(page.getByText("Selected Courses (3)")).toBeVisible();
  await page.getByRole("button", { name: "Save draft" }).click();
  await expect(page.getByText("Bundle saved.")).toBeVisible();
});

test("real Bundle request grants all snapshot Courses after one Admin confirmation", async ({ browser }) => {
  const studentContext = await browser.newContext({ locale: "en-US" });
  const adminContext = await browser.newContext({ locale: "en-US" });
  await installSession(studentContext, { accountID: "a0000000-0000-0000-0000-000000000104", email: "bundle-happy@example.test" });
  await installSession(adminContext, { accountID: "a0000000-0000-0000-0000-000000000000", email: "admin@example.test" });
  await studentContext.route("https://wa.me/**", (route) => route.abort());
  try {
    const studentPage = await studentContext.newPage();
    const adminPage = await adminContext.newPage();
    await studentPage.goto("/en/catalog/bundles/b0000000-0000-0000-0000-000000000001");
    await expect(studentPage.getByRole("heading", { level: 1, name: "Computer Engineering Starter Pack" })).toBeVisible();
    await expect(studentPage.locator("del")).toContainText("75.000 KWD");
    const created = studentPage.waitForResponse((response) => response.url().endsWith("/api/v1/me/purchase-requests") && response.request().method() === "POST");
    await studentPage.getByRole("button", { name: "Request Bundle purchase" }).click();
    expect((await created).status()).toBe(201);

    await adminPage.goto("/en/admin/course-access");
    await adminPage.locator("#purchase-request-search").fill("bundle-happy@example.test");
    await adminPage.getByRole("button", { name: "Search" }).click();
    const requestRow = adminPage.getByRole("row").filter({ hasText: "Computer Engineering Starter Pack" });
    await expect(requestRow).toContainText("CS101");
    await expect(requestRow).toContainText("Digital Logic");
    await expect(requestRow).toContainText("Computer Architecture");
    const confirmed = adminPage.waitForResponse((response) => /\/api\/v1\/admin\/purchase-requests\/[^/]+\/confirm-payment$/.test(new URL(response.url()).pathname) && response.request().method() === "POST");
    await requestRow.getByRole("button", { name: /Confirm payment & grant Bundle access/ }).click();
    const dialog = adminPage.getByTestId("purchase-request-confirm");
    await expect(dialog).toContainText("every Course in the saved Bundle snapshot");
    await dialog.getByTestId("confirm-accept").click();
    const confirmation = await confirmed;
    expect(confirmation.status()).toBe(200);
    const confirmationBody = await confirmation.json() as { invitation?: unknown; bundle_grants: unknown[]; purchase_request: { state: string } };
    expect(confirmationBody.invitation).toBeUndefined();
    expect(confirmationBody.bundle_grants).toHaveLength(3);
    expect(confirmationBody.purchase_request.state).toBe("ACCESS_GRANTED");

    await studentPage.goto("/en/learn/dashboard");
    await expect(studentPage.getByRole("heading", { name: "CS101: Introduction to Programming" })).toBeVisible();
    await expect(studentPage.getByRole("heading", { name: "Digital Logic" })).toBeVisible();
    await expect(studentPage.getByRole("heading", { name: "Computer Architecture" })).toBeVisible();
  } finally {
    await studentContext.close();
    await adminContext.close();
  }
});

test("real Course offer is quoted once while Course fulfillment stays invitation-based", async ({ browser }) => {
  const studentContext = await browser.newContext({ locale: "en-US" });
  const adminContext = await browser.newContext({ locale: "en-US" });
  await installSession(studentContext, { accountID: "a0000000-0000-0000-0000-000000000105", email: "bundle-offer@example.test" });
  await installSession(adminContext, { accountID: "a0000000-0000-0000-0000-000000000000", email: "admin@example.test" });
  await studentContext.route("https://wa.me/**", (route) => route.abort());
  try {
    const adminPage = await adminContext.newPage();
    await adminPage.goto("/en/admin/bundles?course_price=c0000000-0000-0000-0000-000000000002");
    await expect(adminPage.locator("#course-regular")).toHaveValue("30000");
    await adminPage.locator("#course-offer").fill("20000");
    await adminPage.locator("#course-price-reason").fill("E2E Course offer");
    const offerSaved = adminPage.waitForResponse((response) => response.url().endsWith("/api/v1/admin/courses/c0000000-0000-0000-0000-000000000002/price") && response.request().method() === "PUT");
    await adminPage.getByRole("button", { name: "Save Course pricing" }).click();
    expect((await offerSaved).status()).toBe(200);

    const studentPage = await studentContext.newPage();
    await studentPage.goto("/en/catalog/c0000000-0000-0000-0000-000000000002");
    await expect(studentPage.locator("del")).toContainText("30.000 KWD");
    await expect(studentPage.getByTestId("course-access-price")).toContainText("20.000 KWD");
    await studentPage.getByTestId("purchase-request-open").click();
    await expect(studentPage.getByTestId("purchase-price")).toContainText("30.000 KWD");
    await expect(studentPage.getByTestId("purchase-price")).toContainText("20.000 KWD");
    // The Course purchase handoff navigates the tab to WhatsApp as soon as the
    // POST resolves, which discards the response body before a waitForResponse
    // consumer can read it. Observing through a pass-through route keeps the
    // real backend response (route.fetch performs the actual request) while
    // capturing the server-authoritative quote before any navigation starts.
    let createdQuote: { status: number; price_minor_units: number } | null = null;
    await studentPage.route("**/api/v1/me/purchase-requests", async (route) => {
      const response = await route.fetch();
      const body = await response.json();
      if (route.request().method() === "POST") {
        createdQuote = { status: response.status(), price_minor_units: body.price_minor_units };
      }
      await route.fulfill({ response, json: body });
    });
    await studentPage.getByTestId("purchase-request-submit").click();
    await expect.poll(() => createdQuote).not.toBeNull();
    expect(createdQuote!.status).toBe(201);
    expect(createdQuote!.price_minor_units).toBe(20000);

    await adminPage.goto("/en/admin/bundles?course_price=c0000000-0000-0000-0000-000000000002");
    await expect(adminPage.locator("#course-offer")).toHaveValue("20000");
    await adminPage.locator("#course-price-reason").fill("Clear E2E Course offer");
    const offerCleared = adminPage.waitForResponse((response) => response.url().endsWith("/api/v1/admin/courses/c0000000-0000-0000-0000-000000000002/price") && response.request().method() === "PUT");
    await adminPage.getByRole("button", { name: "Clear offer" }).click();
    expect((await offerCleared).status()).toBe(200);

    await adminPage.goto("/en/admin/course-access");
    await adminPage.locator("#purchase-request-search").fill("bundle-offer@example.test");
    await adminPage.getByRole("button", { name: "Search" }).click();
    const requestRow = adminPage.getByRole("row").filter({ hasText: "bundle-offer@example.test" });
    await expect(requestRow).toContainText("20.000 KWD");

    await studentPage.goto("/en/catalog/c0000000-0000-0000-0000-000000000002");
    await expect(studentPage.locator("del")).toHaveCount(0);
    await expect(studentPage.getByTestId("course-access-price")).toContainText("30.000 KWD");
  } finally {
    await studentContext.close();
    await adminContext.close();
  }
});

/**
 * Real seeded Bundle used by every journey below. Each journey owns its own
 * Student (see `seedBundleJourneyStudents`) because a Bundle purchase request is
 * unique per (Bundle, requester) while it waits for payment, and confirmation
 * mutates that Student's entitlements.
 */
const realBundle = {
  path: "/en/catalog/bundles/b0000000-0000-0000-0000-000000000001",
  title: "Computer Engineering Starter Pack",
  primaryCourseID: "c0000000-0000-0000-0000-000000000001",
  courseHeadings: ["CS101: Introduction to Programming", "Digital Logic", "Computer Architecture"],
  regularMinorUnits: 75000,
  effectiveMinorUnits: 60000,
};

type ConfirmationBody = {
  invitation?: unknown;
  bundle_grants: { course_id: string; disposition: string; previous_access_ends_at?: string }[];
  purchase_request: { state: string; price_minor_units: number; regular_price_minor_units?: number };
};

async function requestRealBundle(page: Page) {
  await page.goto(realBundle.path);
  await expect(page.getByRole("heading", { level: 1, name: realBundle.title })).toBeVisible();
  const created = page.waitForResponse((response) => response.url().endsWith("/api/v1/me/purchase-requests") && response.request().method() === "POST");
  await page.getByRole("button", { name: "Request Bundle purchase" }).click();
  expect((await created).status()).toBe(201);
}

async function confirmRealBundle(adminPage: Page, studentEmail: string): Promise<ConfirmationBody> {
  await adminPage.goto("/en/admin/course-access");
  await adminPage.locator("#purchase-request-search").fill(studentEmail);
  await adminPage.getByRole("button", { name: "Search" }).click();
  const requestRow = adminPage.getByRole("row").filter({ hasText: realBundle.title });
  const confirmed = adminPage.waitForResponse((response) => /\/api\/v1\/admin\/purchase-requests\/[^/]+\/confirm-payment$/.test(new URL(response.url()).pathname) && response.request().method() === "POST");
  await requestRow.getByRole("button", { name: /Confirm payment & grant Bundle access/ }).click();
  await adminPage.getByTestId("purchase-request-confirm").getByTestId("confirm-accept").click();
  const response = await confirmed;
  expect(response.status()).toBe(200);
  return await response.json() as ConfirmationBody;
}

async function expectBundleCoursesOnDashboard(studentPage: Page) {
  await studentPage.goto("/en/learn/dashboard");
  for (const heading of realBundle.courseHeadings) {
    await expect(studentPage.getByRole("heading", { name: heading })).toBeVisible();
  }
}

test("real partially owned Bundle extends the held Course and grants the rest once", async ({ browser }) => {
  const studentContext = await browser.newContext({ locale: "en-US" });
  const adminContext = await browser.newContext({ locale: "en-US" });
  await installSession(studentContext, { accountID: "a0000000-0000-0000-0000-000000000102", email: "bundle-partial@example.test" });
  await installSession(adminContext, { accountID: "a0000000-0000-0000-0000-000000000000", email: "admin@example.test" });
  await studentContext.route("https://wa.me/**", (route) => route.abort());
  try {
    const studentPage = await studentContext.newPage();
    await requestRealBundle(studentPage);

    const body = await confirmRealBundle(await adminContext.newPage(), "bundle-partial@example.test");
    expect(body.invitation).toBeUndefined();
    expect(body.purchase_request.state).toBe("ACCESS_GRANTED");
    expect(body.bundle_grants).toHaveLength(3);

    const held = body.bundle_grants.find((grant) => grant.course_id === realBundle.primaryCourseID);
    expect(held?.disposition).toBe("EXTENDED");
    expect(held?.previous_access_ends_at).toBeTruthy();
    expect(body.bundle_grants.filter((grant) => grant.disposition === "GRANTED")).toHaveLength(2);

    await expectBundleCoursesOnDashboard(studentPage);
  } finally {
    await studentContext.close();
    await adminContext.close();
  }
});

test("real Bundle confirmation converges when two Admins confirm the same request", async ({ browser }) => {
  const studentContext = await browser.newContext({ locale: "en-US" });
  const firstAdminContext = await browser.newContext({ locale: "en-US" });
  const secondAdminContext = await browser.newContext({ locale: "en-US" });
  const admin = { accountID: "a0000000-0000-0000-0000-000000000000", email: "admin@example.test" };
  await installSession(studentContext, { accountID: "a0000000-0000-0000-0000-000000000103", email: "bundle-concurrent@example.test" });
  await installSession(firstAdminContext, admin);
  await installSession(secondAdminContext, admin);
  await studentContext.route("https://wa.me/**", (route) => route.abort());
  try {
    const studentPage = await studentContext.newPage();
    await requestRealBundle(studentPage);

    // Both Admin tabs are driven to the accept control before either is
    // released, so the two confirmations are issued without waiting for each
    // other. Strict simultaneity at the transaction boundary is proven by
    // TestConcurrentBundleConfirmationConverges; what this journey proves is
    // that the product converges on one fulfillment through the real UI.
    const [first, second] = await Promise.all([
      confirmRealBundle(await firstAdminContext.newPage(), "bundle-concurrent@example.test"),
      confirmRealBundle(await secondAdminContext.newPage(), "bundle-concurrent@example.test"),
    ]);
    for (const body of [first, second]) {
      expect(body.invitation).toBeUndefined();
      expect(body.purchase_request.state).toBe("ACCESS_GRANTED");
      expect(body.bundle_grants).toHaveLength(3);
    }
    expect(new Set(first.bundle_grants.map((grant) => grant.course_id)).size).toBe(3);

    await expectBundleCoursesOnDashboard(studentPage);
  } finally {
    await studentContext.close();
    await firstAdminContext.close();
    await secondAdminContext.close();
  }
});

// Declared last: it is the only journey that edits the shared seeded Bundle. It
// restores the Bundle's membership and price through the same Admin surface
// before finishing, so no later spec inherits an edited Bundle.
test("real Bundle request keeps its snapshot after the Bundle is edited and repriced", async ({ browser }) => {
  const studentContext = await browser.newContext({ locale: "en-US" });
  const adminContext = await browser.newContext({ locale: "en-US" });
  await installSession(studentContext, { accountID: "a0000000-0000-0000-0000-000000000101", email: "bundle-snapshot@example.test" });
  await installSession(adminContext, { accountID: "a0000000-0000-0000-0000-000000000000", email: "admin@example.test" });
  await studentContext.route("https://wa.me/**", (route) => route.abort());
  try {
    const studentPage = await studentContext.newPage();
    await studentPage.goto(realBundle.path);
    await expect(studentPage.locator("del")).toContainText("75.000 KWD");
    await expect(studentPage.getByTestId("offer-price")).toContainText("60.000 KWD");
    await requestRealBundle(studentPage);

    const adminPage = await adminContext.newPage();
    await adminPage.goto("/en/admin/bundles");
    const card = adminPage.getByRole("listitem").filter({ hasText: realBundle.title });
    await card.getByRole("button", { name: "Edit", exact: true }).click();
    const editor = adminPage.getByTestId("bundle-editor");
    await expect(editor.locator("#bundle-regular")).toHaveValue(String(realBundle.regularMinorUnits));
    await editor.getByRole("listitem").filter({ hasText: "Computer Architecture" }).getByRole("button", { name: "Remove" }).click();
    await expect(editor.getByText("Selected Courses (2)")).toBeVisible();
    await editor.locator("#bundle-regular").fill("90000");
    await editor.locator("#bundle-offer").fill("");
    await editor.locator("#bundle-price-reason").fill("Edited after the pending request");
    await editor.getByRole("button", { name: "Save changes" }).click();
    await expect(adminPage.getByText("Bundle saved.")).toBeVisible();

    const body = await confirmRealBundle(adminPage, "bundle-snapshot@example.test");
    expect(body.invitation).toBeUndefined();
    expect(body.purchase_request.state).toBe("ACCESS_GRANTED");
    expect(body.purchase_request.price_minor_units).toBe(realBundle.effectiveMinorUnits);
    expect(body.purchase_request.regular_price_minor_units).toBe(realBundle.regularMinorUnits);
    expect(body.bundle_grants).toHaveLength(3);
    await expectBundleCoursesOnDashboard(studentPage);

    await adminPage.goto("/en/admin/bundles");
    await adminPage.getByRole("listitem").filter({ hasText: realBundle.title }).getByRole("button", { name: "Edit", exact: true }).click();
    await editor.getByRole("checkbox", { name: /Computer Architecture/ }).check();
    await editor.locator("#bundle-regular").fill(String(realBundle.regularMinorUnits));
    await editor.locator("#bundle-offer").fill(String(realBundle.effectiveMinorUnits));
    await editor.locator("#bundle-price-reason").fill("Restore seeded Bundle");
    await editor.getByRole("button", { name: "Save changes" }).click();
    await expect(adminPage.getByText("Bundle saved.")).toBeVisible();
  } finally {
    await studentContext.close();
    await adminContext.close();
  }
});
