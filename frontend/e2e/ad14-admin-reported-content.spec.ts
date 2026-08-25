import { expect, test, type BrowserContext } from "@playwright/test";
import {
  ADMIN_REPORTED_CONTENT_TEST_SLOT,
  authenticateRotatingStudent,
  issueRotatingSession,
  studentFor,
} from "./rotating-students";
import { frontendOrigin } from "../src/lib/api/e2e-ports";

const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };

async function authenticateAdmin(context: BrowserContext): Promise<void> {
  const session = issueRotatingSession(ADMIN);
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

test.describe.configure({ timeout: 120_000 });

test("AD-14 Student report reaches Admin queue and can be dismissed", async ({ browser }, testInfo) => {
  const student = studentFor(testInfo, ADMIN_REPORTED_CONTENT_TEST_SLOT);
  const studentContext = await browser.newContext({ locale: "en-US" });
  await authenticateRotatingStudent(studentContext, student);
  const studentPage = await studentContext.newPage();

  await studentPage.goto(`/en/learn/courses/${COURSE_ID}`);
  const reportButton = studentPage.getByRole("button", { name: /Report/ }).first();
  await expect(reportButton).toBeVisible();
  await reportButton.click();

  const reportDialog = studentPage.getByRole("dialog");
  await reportDialog.locator("select[name=reason]").selectOption("inaccurate");
  await reportDialog.getByRole("button", { name: "Send report" }).click();
  await expect(reportDialog.getByRole("status")).toContainText("Report received");
  await reportDialog.getByRole("button", { name: "Done" }).click();
  await studentContext.close();

  const adminContext = await browser.newContext({ locale: "en-US" });
  await authenticateAdmin(adminContext);
  const adminPage = await adminContext.newPage();
  await adminPage.goto("/en/admin/reported-content");
  await expect(adminPage.getByTestId("reported-content-loading")).toHaveCount(0);
  await expect(adminPage.getByTestId("reported-content-row").first()).toBeVisible();
  await adminPage.getByTestId("reported-content-row").first().click();
  await expect(adminPage.getByTestId("reported-content-detail")).toBeVisible();

  await adminPage.locator("#reported-content-resolution-reason").fill("Reviewed; no platform action required.");
  await adminPage.getByTestId("reported-content-dismiss").click();
  await expect(adminPage.getByTestId("reported-content-resolved")).toContainText("Resolution recorded");
  await expect(adminPage.getByTestId("reported-content-empty")).toBeVisible();

  const body = await adminPage.locator("body").textContent();
  expect(body).not.toContain(student.accountID);
  expect(body).not.toContain(ADMIN.accountID);
  await adminContext.close();
});
