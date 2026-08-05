import { expect, test } from "@playwright/test";
import { frontendOrigin } from "../src/lib/api/e2e-ports";
import {
  authenticateRotatingStudent,
  issueRotatingSession,
  studentFor,
  LIFECYCLE_TEST_SLOT,
  PROGRESS_TEST_SLOT,
} from "./rotating-students";

test.describe("S5 Production-like Playwright Infrastructure Smoke Test", () => {
  test("authenticates real Student via Go API session and renders Course Home from real PostgreSQL", async ({ page }) => {
    // 1. Navigate to front-end page so browser origin and context are established
    await page.goto("/en/catalog");

    // 2. Perform authentic login inside browser using frontend API helpers
    const loginResult = await page.evaluate(async () => {
      // 1. Bootstrap
      const bootstrapRes = await fetch("/api/v1/session/bootstrap", {
        method: "GET",
        credentials: "include",
      });
      const { csrf_token } = await bootstrapRes.json();

      // 2. Login
      const loginRes = await fetch("/api/v1/sessions", {
        method: "POST",
        credentials: "include",
        headers: {
          "Content-Type": "application/json",
          Accept: "application/json",
          "X-CSRF-Token": csrf_token,
        },
        body: JSON.stringify({
          email: "student-active@example.test",
          password: "StudentPassword123!",
        }),
      });

      return { status: loginRes.status, body: await loginRes.json() };
    });

    expect(loginResult.status).toBe(201);
    expect(loginResult.body.role).toBe("STUDENT");
    expect(loginResult.body.display_name).toBe("Active Student");

    // 3. Navigate to Course Home for seeded course in browser
    const courseId = "c0000000-0000-0000-0000-000000000001";
    await page.goto(`/en/learn/courses/${courseId}`);

    // 4. Assert real backend Course Home data is rendered from PostgreSQL
    await expect(page.locator("main")).toBeVisible();
    await expect(page.getByRole("heading", { name: "CS101: Introduction to Programming", level: 1 })).toBeVisible();
    await expect(page.getByText("Section 1: Basics")).toBeVisible();
    await expect(page.getByText("Lesson 1: Introduction")).toBeVisible();

    // 5. Assert layout integrity
    await expect(page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).resolves.toBe(true);
  });

  // The run never depends on port 3000, so a developer server sitting on it is irrelevant. This
  // asserts the property directly at the level the suite actually uses.
  test("the run serves its own dynamically allocated frontend port rather than 3000", async ({ page, baseURL }) => {
    expect(baseURL).toBe(frontendOrigin());
    expect(baseURL).not.toContain(":3000");

    await page.goto("/en/catalog");
    const servedOrigin = await page.evaluate(() => window.location.origin);
    expect(servedOrigin).toBe(frontendOrigin());
    expect(servedOrigin).not.toContain(":3000");
  });

  test("a rotating Student's server-issued session is accepted by production middleware", async ({ browser }, testInfo) => {
    const student = studentFor(testInfo, PROGRESS_TEST_SLOT);
    const other = studentFor(testInfo, LIFECYCLE_TEST_SLOT);
    expect(other.accountID).not.toBe(student.accountID);

    // Issuance goes through the real session repository, so the credential is a real one — not an
    // unsigned cookie the middleware would have to be relaxed to accept.
    const issued = issueRotatingSession(student);
    expect(issued.cookie_name).toBe("__Host-gradex_session");
    expect(issued.cookie_value.length).toBeGreaterThan(20);
    expect(issued.csrf_token.length).toBeGreaterThan(20);

    const context = await browser.newContext();
    try {
      await authenticateRotatingStudent(context, student);
      const page = await context.newPage();

      // Production session resolution accepts the cookie and returns this Student's identity.
      await page.goto("/en/catalog");
      const resolved = await page.evaluate(async () => {
        const response = await fetch("/api/v1/session", { credentials: "include" });
        return { status: response.status, body: await response.json() };
      });
      expect(resolved.status).toBe(200);
      expect(resolved.body.role).toBe("STUDENT");
      expect(resolved.body.display_name).toBe(`Rotating Student ${String(student.index).padStart(3, "0")}`);

      // The Entitlement and Enrollment seeded for the rotating Student are real, so the protected
      // Course Home renders for them exactly as it does for the shared Active Student.
      await page.goto("/en/learn/courses/c0000000-0000-0000-0000-000000000001");
      await expect(
        page.getByRole("heading", { name: "CS101: Introduction to Programming", level: 1 })
      ).toBeVisible();

      // The opaque credential never reaches browser JavaScript.
      const visibleCookies = await page.evaluate(() => document.cookie);
      expect(visibleCookies).not.toContain(issued.cookie_value);
    } finally {
      await context.close();
    }
  });
});
