import { test, expect, request as playwrightRequest, type APIRequestContext, type BrowserContext, type Page } from "@playwright/test";
import { issueRotatingSession } from "./rotating-students";
import { frontendOrigin } from "../src/lib/api/e2e-ports";

/**
 * Admin Catalog review surface — server-backed acceptance.
 *
 * Founder manual acceptance found `/en/admin/catalog` showing a Course that
 * does not exist ("Introduction to Programming" / `demo-course-1`) out of
 * component state, then required a pasted Course UUID to bridge review into
 * taxonomy and pricing. These cases prove the opposite properties: the queue
 * is the server's `PENDING_REVIEW` set and normal operation exposes no UUID
 * launcher or split Inspect/Administer workflow.
 *
 * The successful submission and its Admin approval need a READY Lesson video,
 * so that half of the journey is proved in `e2e/media-authoring`, which has
 * real object storage and a running worker.
 */

const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };
const INSTRUCTOR = { email: "instructor@example.test", accountID: "a0000000-0000-0000-0000-000000000003" };

const AUTHORIZATION_CANARY_ID = "c0000000-0000-0000-0000-000000000001";

// The founder's own vocabulary, entered exactly as the manual journey does.
const MAJOR_AR = "علوم الحاسب";
const MAJOR_EN = "Computer Science";
const SUBJECT_AR = "هندسة البرمجيات";
const SUBJECT_EN = "Software Engineering";
const SUBJECT_CODE = "SWE101";

type Session = ReturnType<typeof issueRotatingSession>;

async function signIn(context: BrowserContext, account: typeof ADMIN): Promise<Session> {
  const session = issueRotatingSession(account);
  const origin = new URL(frontendOrigin());
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
  return session;
}

async function apiContext(session: Session): Promise<APIRequestContext> {
  return playwrightRequest.newContext({
    baseURL: frontendOrigin(),
    extraHTTPHeaders: {
      Accept: "application/json, application/problem+json",
      Origin: frontendOrigin(),
      Cookie: `${session.cookie_name}=${session.cookie_value}`,
      "X-CSRF-Token": session.csrf_token,
    },
  });
}

async function openAdminCatalog(page: Page): Promise<void> {
  await page.goto("/en/admin/catalog");
  await expect(page.locator("h1")).toContainText("Course Review & Pricing Admin");
  // The queue resolves against the server before anything is asserted about it.
  await expect(page.getByTestId("review-queue-loading")).toHaveCount(0);
}

test.describe("S14 Admin Catalog review surface", () => {
  test("A the queue is the server's review queue, and carries no demo Course", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const session = await signIn(context, ADMIN);
    const admin = await apiContext(session);
    const page = await context.newPage();

    const response = await admin.get("/api/v1/admin/review/queue");
    expect(response.status()).toBe(200);
    const serverQueue = (await response.json()) as Array<{ course_id: string; revision_id: string }>;

    await openAdminCatalog(page);

    // What the page shows is what the server said — row for row.
    await expect(page.getByTestId("review-queue-count")).toContainText(String(serverQueue.length));
    if (serverQueue.length === 0) {
      // An empty real queue is stated honestly rather than filled in.
      await expect(page.getByTestId("review-queue-empty")).toBeVisible();
    }
    for (const item of serverQueue) {
      const row = page.getByTestId(`review-item-${item.course_id}`);
      await expect(row).toBeVisible();
      await expect(row).not.toContainText(item.course_id);
      await expect(row).not.toContainText(item.revision_id);
    }

    // The removed fixture must not reappear anywhere in production UI.
    await expect(page.locator("body")).not.toContainText("Introduction to Programming");
    await expect(page.locator("body")).not.toContainText("demo-course-1");

    await admin.dispose();
    await context.close();
  });

  test("B the UUID launcher and split administration workflow are absent", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await signIn(context, ADMIN);
    const page = await context.newPage();

    await openAdminCatalog(page);
    await expect(page.getByTestId("administer-course-id")).toHaveCount(0);
    await expect(page.locator('[data-testid^="administer-review-item-"]')).toHaveCount(0);
    await expect(page.getByRole("heading", { name: "Taxonomy Vocabulary Administration" })).toBeVisible();

    await context.close();
  });

  test("C the Admin creates taxonomy vocabulary that persists and reaches the Instructor", async ({ browser }) => {
    const adminContext = await browser.newContext({ locale: "en-US" });
    const adminSession = await signIn(adminContext, ADMIN);
    const page = await adminContext.newPage();

    await openAdminCatalog(page);

    await page.getByTestId("taxonomy-term-kind").selectOption("MAJOR");
    await page.getByTestId("taxonomy-term-label-ar").fill(MAJOR_AR);
    await page.getByTestId("taxonomy-term-label-en").fill(MAJOR_EN);
    await page.getByTestId("taxonomy-term-create").click();
    await expect(page.getByTestId("taxonomy-term-message")).toContainText("Term created and audited");

    await page.getByTestId("taxonomy-term-kind").selectOption("SUBJECT");
    await page.getByTestId("taxonomy-term-label-ar").fill(SUBJECT_AR);
    await page.getByTestId("taxonomy-term-label-en").fill(SUBJECT_EN);
    await page.getByTestId("taxonomy-term-academic-code").fill(SUBJECT_CODE);
    await page.getByTestId("taxonomy-term-create").click();
    await expect(page.getByTestId("taxonomy-term-message")).toContainText("Term created and audited");

    // Persisted, not merely rendered: a reload re-reads the vocabulary.
    await page.reload();
    await expect(page.getByTestId("review-queue-loading")).toHaveCount(0);
    await expect(page.getByRole("option", { name: `${MAJOR_EN} — MAJOR` })).toHaveCount(1);
    await expect(page.getByRole("option", { name: `${SUBJECT_EN} — SUBJECT` })).toHaveCount(1);

    // The Instructor sees the same vocabulary, through the Instructor route.
    const instructorContext = await browser.newContext({ locale: "en-US" });
    const instructorSession = await signIn(instructorContext, INSTRUCTOR);
    const instructorPage = await instructorContext.newPage();
    await instructorPage.goto("/en/instructor/courses");
    await expect(instructorPage.locator("h1")).toContainText("Course Authoring Studio");

    await expect(instructorPage.getByRole("option", { name: MAJOR_EN })).toHaveCount(1);
    await expect(
      instructorPage.getByRole("option", { name: `${SUBJECT_EN} (${SUBJECT_CODE})` }),
    ).toHaveCount(1);

    // Seeing the vocabulary is not owning it: the Admin taxonomy and review
    // routes stay refused to an Instructor.
    const instructorAPI = await apiContext(instructorSession);
    const termCreation = await instructorAPI.post("/api/v1/admin/taxonomy/terms", {
      data: { kind: "MAJOR", label_ar: "تخصص دخيل", label_en: "Intruding major" },
    });
    expect(termCreation.status(), "an Instructor must not create taxonomy vocabulary").toBeGreaterThanOrEqual(400);

    const override = await instructorAPI.put(`/api/v1/admin/courses/${AUTHORIZATION_CANARY_ID}/taxonomy`, {
      data: { revision_id: AUTHORIZATION_CANARY_ID, major_term_id: AUTHORIZATION_CANARY_ID, subject_term_id: AUTHORIZATION_CANARY_ID },
    });
    expect(override.status(), "an Instructor must not override Course taxonomy").toBeGreaterThanOrEqual(400);

    const queue = await instructorAPI.get("/api/v1/admin/review/queue");
    expect(queue.status(), "an Instructor must not read the Admin review queue").toBeGreaterThanOrEqual(400);

    // The refusals changed nothing the Admin can see.
    const adminAPI = await apiContext(adminSession);
    const terms = await adminAPI.get("/api/v1/taxonomy/terms");
    expect(terms.status()).toBe(200);
    const labels = ((await terms.json()) as Array<{ label_en: string }>).map((term) => term.label_en);
    expect(labels).toContain(MAJOR_EN);
    expect(labels).toContain(SUBJECT_EN);
    expect(labels).not.toContain("Intruding major");

    await instructorAPI.dispose();
    await adminAPI.dispose();
    await instructorContext.close();
    await adminContext.close();
  });

  test("D an incomplete submission reports the server's reason at the Submit control", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await signIn(context, INSTRUCTOR);
    const page = await context.newPage();

    await page.goto("/en/instructor/courses");
    await expect(page.locator("h1")).toContainText("Course Authoring Studio");

    await page.getByTestId("toggle-new-course").click();
    await page.getByTestId("new-course-title-ar").fill("دورة غير مكتملة");
    await page.getByTestId("new-course-title-en").fill(`Incomplete Course ${Date.now()}`);
    await page.getByTestId("new-course-description-ar").fill("وصف");
    await page.getByTestId("new-course-description-en").fill("Incomplete");
    await page.getByTestId("create-course").click();
    await expect(page.getByTestId("authoring-notice")).toContainText("Course created on the server");

    const submit = page.getByTestId("submit-for-review");
    await submit.click();

    // The founder clicked Submit and saw nothing happen, because the reason
    // rendered far above the viewport. It now renders at the control, and the
    // server's own violation codes are shown unchanged.
    const submitError = page.getByTestId("submit-error");
    await expect(submitError).toBeVisible();
    await expect(submitError).toContainText("TAXONOMY_DIMENSION_MISSING");
    await expect(submitError).toContainText("COURSE_EMPTY");

    const errorBox = await submitError.boundingBox();
    const buttonBox = await submit.boundingBox();
    expect(errorBox, "the failure must be laid out").not.toBeNull();
    expect(buttonBox, "the Submit control must be laid out").not.toBeNull();
    // Same screenful as the control that produced it.
    expect(Math.abs(errorBox!.y - buttonBox!.y)).toBeLessThan(400);

    await context.close();
  });
});
