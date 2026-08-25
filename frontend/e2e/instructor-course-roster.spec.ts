import { expect, request as playwrightRequest, test, type BrowserContext } from "@playwright/test";
import { frontendOrigin } from "../src/lib/api/e2e-ports";
import { issueRotatingSession } from "./rotating-students";

const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const INSTRUCTOR = {
  email: "instructor@example.test",
  accountID: "a0000000-0000-0000-0000-000000000003",
};
const OTHER_INSTRUCTOR = {
  email: "instructor-other@example.test",
  accountID: "a0000000-0000-0000-0000-000000000004",
};

async function installInstructorSession(
  context: BrowserContext,
  account: typeof INSTRUCTOR,
): Promise<void> {
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
}

test("an Instructor reaches the owned Course roster and another Instructor is denied", async ({ browser }) => {
  const context = await browser.newContext({ locale: "en-US" });
  await installInstructorSession(context, INSTRUCTOR);
  const page = await context.newPage();

  await page.goto("/en/instructor/courses");
  await expect(page.getByTestId(`owned-course-${COURSE_ID}`)).toBeVisible();
  await page.getByTestId(`owned-course-${COURSE_ID}`).click();
  await page.getByTestId("course-roster-toggle").click();

  await expect(page.getByTestId("course-roster")).toBeVisible();
  const activeRow = page.getByTestId("course-roster-row").filter({ hasText: "Active Student" });
  const expiredRow = page.getByTestId("course-roster-row").filter({ hasText: "Expired Student" });
  await expect(activeRow.locator("[data-roster-status]")).toHaveAttribute("data-roster-status", "ACTIVE");
  await expect(expiredRow.locator("[data-roster-status]")).toHaveAttribute("data-roster-status", "EXPIRED");
  await expect(activeRow.locator("time")).toHaveCount(3);
  await expect(page.locator("body")).not.toContainText("student-active@example.test");
  await expect(page.locator("body")).not.toContainText("payment");

  const otherSession = issueRotatingSession(OTHER_INSTRUCTOR);
  const otherAPI = await playwrightRequest.newContext({
    baseURL: frontendOrigin(),
    extraHTTPHeaders: {
      Accept: "application/json, application/problem+json",
      Origin: frontendOrigin(),
      Cookie: `${otherSession.cookie_name}=${otherSession.cookie_value}`,
    },
  });
  const denied = await otherAPI.get(`/api/v1/courses/${COURSE_ID}/students`);
  expect(denied.status()).toBe(403);

  await otherAPI.dispose();
  await context.close();
});
