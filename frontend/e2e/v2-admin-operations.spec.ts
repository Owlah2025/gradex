import {
  expect,
  request as playwrightRequest,
  test,
  type APIRequestContext,
  type Browser,
  type BrowserContext,
  type Page,
} from "@playwright/test";
import { frontendOrigin } from "../src/lib/api/e2e-ports";
import { installIssuedSession, issueRotatingSession } from "./rotating-students";
import { ADMIN, V2_ADMIN_TARGET, V2_GRANT_TARGET } from "./session/principals";

const V2_COURSE_ID = "c7000000-0000-0000-0000-000000000001";
const V2_ADMIN_TARGET_EMAIL = V2_ADMIN_TARGET.email;
const V2_GRANT_TARGET_EMAIL = V2_GRANT_TARGET.email;
const viewports = [375, 768, 1280] as const;
const locales = ["en", "ar"] as const;
type Session = ReturnType<typeof issueRotatingSession>;

async function apiFor(session: Session): Promise<APIRequestContext> {
  return playwrightRequest.newContext({
    baseURL: frontendOrigin(),
    extraHTTPHeaders: {
      Accept: "application/json, application/problem+json",
      Cookie: `${session.cookie_name}=${session.cookie_value}; ${session.device_cookie_name}=${session.device_cookie_value}`,
      Origin: frontendOrigin(),
      "X-CSRF-Token": session.csrf_token,
    },
  });
}

async function signedAdmin(browser: Browser, width = 1280): Promise<{ context: BrowserContext; page: Page; session: Session }> {
  const context = await browser.newContext({ locale: "en-US", viewport: { width, height: 900 } });
  const session = issueRotatingSession(ADMIN);
  await context.addInitScript(() => window.localStorage.setItem("gradex.locale", "en"));
  await installIssuedSession(context, session);
  return { context, page: await context.newPage(), session };
}

async function openUser(page: Page, email: string, accountID: string): Promise<void> {
  await page.goto("/en/admin/users");
  await expect(page.getByRole("heading", { name: "Users" })).toBeVisible();
  await page.getByPlaceholder("Name or email prefix").fill(email);
  await page.getByRole("button", { name: "Apply filters" }).click();
  const account = page.getByRole("link", { name: "Open account" });
  await expect(account).toHaveCount(1);
  await account.click();
  await expect(page).toHaveURL(new RegExp(`/en/admin/users/${accountID}$`));
}

test.describe("T8 V2 Admin operations", () => {
  test.describe.configure({ timeout: 150_000 });

  test("Admin diagnoses access, inspects progress/security, invalidates sessions, grants access, adjusts, revokes, and verifies audit", async ({ browser }) => {
    const admin = await signedAdmin(browser);
    const targetSession = issueRotatingSession(V2_ADMIN_TARGET);
    const targetAPI = await apiFor(targetSession);
    try {
      expect((await targetAPI.get("/api/v1/session")).status()).toBe(200);

      await admin.page.goto("/en/admin");
      await expect(admin.page.getByRole("heading", { name: "Operator home" })).toBeVisible();

      await openUser(admin.page, V2_ADMIN_TARGET_EMAIL, V2_ADMIN_TARGET.accountID);
      await admin.page.getByRole("tab", { name: "Courses & access" }).click();
      await expect(admin.page.getByRole("heading", { name: "V2 Completion Course" })).toBeVisible();

      await admin.page.getByRole("tab", { name: "Access diagnostic" }).click();
      const diagnosticCourse = admin.page.locator("#diagnostic-course");
      await expect(diagnosticCourse).toBeVisible();
      await diagnosticCourse.selectOption(V2_COURSE_ID);
      await admin.page.getByRole("button", { name: "Diagnose access" }).click();
      await expect(admin.page.getByText("Access is active", { exact: true }).first()).toBeVisible();

      await admin.page.getByRole("tab", { name: "Progress & activity" }).click();
      await expect(admin.page.getByText("V2 Completion Course")).toBeVisible();
      await expect(admin.page.getByText(/0\/2/)).toBeVisible();

      await admin.page.getByRole("tab", { name: "Devices & security" }).click();
      await expect(admin.page.getByText("Session created")).toBeVisible();

      await admin.page.getByRole("tab", { name: "Notes" }).click();
      await admin.page.locator("#admin-note-body").fill("V2 Admin operational note");
      await admin.page.getByRole("button", { name: "Add note" }).click();
      await expect(admin.page.getByText("V2 Admin operational note")).toBeVisible();

      await admin.page.getByRole("button", { name: "Sign out everywhere" }).click();
      const signoutDialog = admin.page.getByTestId("admin-user-action-dialog");
      await signoutDialog.getByLabel("Reason").fill("V2 session invalidation verification");
      await signoutDialog.getByTestId("confirm-accept").click();
      await expect(admin.page.getByText("All sessions signed out.")).toBeVisible();
      expect((await targetAPI.get("/api/v1/session")).status()).toBe(401);

      await admin.page.getByRole("button", { name: "Suspend" }).click();
      const suspendDialog = admin.page.getByTestId("admin-user-action-dialog");
      await suspendDialog.getByLabel("Reason").fill("V2 suspension verification");
      await suspendDialog.getByTestId("confirm-accept").click();
      await expect(admin.page.getByText("Account suspended.")).toBeVisible();

      await admin.page.getByRole("button", { name: "Restore" }).click();
      const restoreDialog = admin.page.getByTestId("admin-user-action-dialog");
      await restoreDialog.getByLabel("Reason").fill("V2 restoration verification");
      await restoreDialog.getByTestId("confirm-accept").click();
      await expect(admin.page.getByText("Account restored.")).toBeVisible();

      await admin.page.reload();
      await admin.page.getByRole("tab", { name: "Audit" }).click();
      const auditPanel = admin.page.getByRole("tabpanel", { name: "Audit" });
      await expect(auditPanel.getByText("V2 Admin operational note")).toBeVisible();
      await expect(auditPanel.getByText("V2 session invalidation verification")).toBeVisible();
      await expect(auditPanel.getByText("V2 suspension verification")).toBeVisible();
      await expect(auditPanel.getByText("V2 restoration verification")).toBeVisible();

      // Grant is a real pending-admin invitation followed by the queue's approval
      // command; the entitlement is then adjusted and revoked from the persisted record.
      await openUser(admin.page, V2_GRANT_TARGET_EMAIL, V2_GRANT_TARGET.accountID);
      await admin.page.getByRole("tab", { name: "Courses & access" }).click();
      await expect(admin.page.getByText("No entitlement records")).toBeVisible();

      await admin.page.goto("/en/admin/course-access");
      const pendingRow = admin.page.getByTestId("access-invitation-row").filter({ hasText: V2_GRANT_TARGET_EMAIL });
      await expect(pendingRow).toBeVisible();
      await pendingRow.getByRole("button", { name: `Approve — ${V2_GRANT_TARGET_EMAIL}` }).click();
      await admin.page.getByTestId("access-queue-confirm").getByTestId("confirm-accept").click();
      await expect(pendingRow.getByTestId("access-invitation-state")).toContainText("Access granted");

      const approvedRow = admin.page.getByTestId("access-invitation-row").filter({ hasText: V2_GRANT_TARGET_EMAIL });
      await approvedRow.getByRole("button", { name: `Manage access — ${V2_GRANT_TARGET_EMAIL}` }).click();
      const detail = admin.page.getByTestId("entitlement-detail");
      await expect(detail).toBeVisible();
      await detail.locator("#entitlement-expiry-date").fill("2030-01-01");
      await detail.locator("#entitlement-expiry-reason").fill("V2 expiry adjustment verification");
      await detail.locator("#entitlement-expiry-reference").fill("V2-ADJUST");
      await detail.getByTestId("save-entitlement-expiry").click();
      await expect(detail.getByTestId("entitlement-notice")).toHaveAttribute("data-tone", "success");
      await expect(detail.getByText("The access end date was changed.")).toBeVisible();

      await detail.locator("#entitlement-revoke-reason").fill("V2 revocation verification");
      await detail.locator("#entitlement-revoke-reference").fill("V2-REVOKE");
      await detail.getByTestId("revoke-entitlement").click();
      await admin.page.getByTestId("confirm-revoke-entitlement").getByTestId("confirm-accept").click();
      await expect(detail.getByTestId("entitlement-state")).toContainText("Access was ended");
      await expect(detail.getByTestId("entitlement-terminal")).toBeVisible();
    } finally {
      await targetAPI.dispose();
      await admin.context.close();
    }
  });

  for (const locale of locales) {
    for (const width of viewports) {
      test(`${locale} Admin root is usable at ${width}px`, async ({ browser }) => {
        const admin = await signedAdmin(browser, width);
        try {
          await admin.page.goto(`/${locale}/admin`);
          await expect(admin.page.locator("html")).toHaveAttribute("dir", locale === "ar" ? "rtl" : "ltr");
          await expect(admin.page.getByRole("heading", { name: locale === "ar" ? "الصفحة التشغيلية" : "Operator home" })).toBeVisible();
          await expect(admin.page.locator("main")).toBeVisible();
          await expect(admin.page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).resolves.toBe(true);
        } finally {
          await admin.context.close();
        }
      });
    }
  }
});
