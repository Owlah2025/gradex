import {
  test,
  expect,
  request as playwrightRequest,
  type APIRequestContext,
  type BrowserContext,
  type Page,
} from "@playwright/test";
import { issueRotatingSession } from "./rotating-students";
import { actionLinkFor, messageCountFor, waitForMessageTo } from "./mailpit";
import { frontendOrigin } from "../src/lib/api/e2e-ports";

/**
 * T8B (MVP-F24B) AD-13 — Staff invitation lifecycle, in a browser, end to end.
 *
 * The lifecycle is already implemented and already proven at the integration layer: the invitation
 * secret is stored protected, the outbox payload is encrypted, replay is refused, and suspension and
 * reinstatement are audited. What was never observed is the part a person actually performs — an
 * Admin invites someone from a screen, a real message leaves the product over SMTP, and the invitee
 * turns that message into a working staff account.
 *
 * So nothing here reads the invitation secret from PostgreSQL or constructs a link. The Admin acts
 * through the Admin screen, the invitation is consumed from the message Mailpit received from the
 * worker, the new Instructor signs in through the ordinary login form, and the capability the server
 * grants is what the invitation said it would be — not what the browser asked for.
 *
 * Each case owns a unique recipient address, so no case depends on another's runtime state and no
 * search can adopt an unrelated historical message.
 */

const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };

const RUN = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;

// A staff invitation only needs an address nobody else in this run uses; it creates its own Account
// on completion, so it consumes no rotating Student slot.
function recipientFor(caseName: string): string {
  return `instructor+t8b-${caseName}-${RUN}@gradex.local`;
}

// Meets the 15–128 character staff policy. The policy is not weakened for the test.
const RECIPIENT_PASSWORD = "T8bStaffInvitation!2026";

type Session = ReturnType<typeof issueRotatingSession>;

async function signInAdmin(context: BrowserContext): Promise<Session> {
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
  return session;
}

async function apiContextFor(session: Session): Promise<APIRequestContext> {
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

/** Sends one invitation through the Admin screen and returns when the screen confirms it. */
async function inviteFromAdminScreen(page: Page, recipient: string): Promise<void> {
  await page.goto("/staff");
  await expect(page.getByRole("heading", { name: "Invite an instructor" })).toBeVisible();
  await page.locator("#staff-invite-email").fill(recipient);
  await page.getByTestId("staff-invite-submit").click();
  await expect(page.getByTestId("staff-notice")).toHaveAttribute("data-tone", "success", {
    timeout: 15_000,
  });
  await expect(page.getByText("The invitation was sent.")).toBeVisible();

  // The invitation the Admin just issued is visible as something still waiting, which is the whole
  // reason the pending list exists: before it, an invitation vanished the moment it was sent.
  const row = page.getByTestId("staff-invitation-row").filter({ hasText: recipient });
  await expect(row).toBeVisible({ timeout: 15_000 });
}

/**
 * Consumes the invitation from the delivered message and completes the account through the screen.
 * Returns the link so a caller can prove replay; the caller keeps it in memory only.
 */
async function completeInvitationFromEmail(
  context: BrowserContext,
  recipient: string,
  displayName: string,
): Promise<string> {
  const message = await waitForMessageTo(recipient);
  expect(message.To.map((to) => to.Address.toLowerCase())).toContain(recipient.toLowerCase());
  expect(message.Subject).toBe("You are invited to join Gradex staff");

  const invitationLink = actionLinkFor(message, "/staff/accept");

  const page = await context.newPage();
  await page.goto(invitationLink);
  await expect(page.getByRole("heading", { name: "Complete your staff invitation" })).toBeVisible();
  // The role is presented as fixed by the invitation. The form offers no way to change it.
  await expect(page.getByText("Instructor", { exact: true })).toBeVisible();
  await expect(
    page.getByText("The assigned role is fixed by the invitation and cannot be changed here."),
  ).toBeVisible();

  await page.locator("#staff-display-name").fill(displayName);
  await page.locator("#staff-password").fill(RECIPIENT_PASSWORD);
  await page.locator("#staff-password-confirm").fill(RECIPIENT_PASSWORD);
  await page.getByRole("button", { name: "Create staff account" }).click();
  await expect(
    page.getByText("Your staff account is ready. Sign in with the invited email address."),
  ).toBeVisible({ timeout: 15_000 });
  await page.close();

  return invitationLink;
}

/** Signs in through the ordinary login form and returns the page it landed on. */
async function signInThroughLoginForm(context: BrowserContext, email: string): Promise<Page> {
  const page = await context.newPage();
  await page.goto("/login");
  await page.locator("#email").fill(email);
  await page.locator("#password").fill(RECIPIENT_PASSWORD);
  await page.getByRole("button", { name: /sign in/i }).click();
  await page.waitForURL(/\/instructor\//, { timeout: 20_000 });
  return page;
}

test.describe("T8B AD-13 staff invitation lifecycle", () => {
  // Each case drives two or three real browser journeys and waits for a message to travel through
  // the outbox, the worker, the renderer, and SMTP. That is slower than a single-screen test and
  // the default per-test budget is not the right measure of it.
  test.describe.configure({ timeout: 180_000 });

  test("A an Admin invites an Instructor, the invitation arrives by email, and the invitee completes, signs in, and appears active", async ({
    browser,
  }) => {
    const recipient = recipientFor("a");
    const adminContext = await browser.newContext({ baseURL: frontendOrigin() });
    const adminSession = await signInAdmin(adminContext);
    const adminPage = await adminContext.newPage();

    await inviteFromAdminScreen(adminPage, recipient);

    // The Admin's own authority shows the invitation pending, addressed to the human-readable
    // recipient, carrying the role the server assigned rather than one the browser chose.
    const adminApi = await apiContextFor(adminSession);
    const pending = await adminApi.get("/api/v1/staff-invitations");
    expect(pending.status()).toBe(200);
    const pendingBody = await pending.json();
    const invitation = pendingBody.invitations.find(
      (entry: { email: string }) => entry.email.toLowerCase() === recipient.toLowerCase(),
    );
    expect(invitation, "the invitation the Admin just sent is listed as pending").toBeTruthy();
    expect(invitation.invited_role).toBe("INSTRUCTOR");
    expect(invitation.state).toBe("PENDING");
    // The list route never returns the completion secret.
    expect(JSON.stringify(invitation)).not.toMatch(/bearer|token|secret/i);

    const recipientContext = await browser.newContext({ baseURL: frontendOrigin() });
    await recipientContext.addInitScript(() => {
      window.localStorage.setItem("gradex.locale", "en");
    });
    await completeInvitationFromEmail(recipientContext, recipient, "Rana Instructor");

    // Completion issues no session: the new staff member signs in the ordinary way.
    const instructorPage = await signInThroughLoginForm(recipientContext, recipient);
    await expect(instructorPage).toHaveURL(/\/en\/instructor\//);

    // Server-authoritative capability, not a role field echoed back by the completion response.
    const instructorCookies = await recipientContext.cookies();
    const sessionCookie = instructorCookies.find((cookie) => cookie.name.startsWith("__Host-"));
    expect(sessionCookie, "the login established a real session cookie").toBeTruthy();
    const instructorApi = await playwrightRequest.newContext({
      baseURL: frontendOrigin(),
      extraHTTPHeaders: {
        Accept: "application/json, application/problem+json",
        Origin: frontendOrigin(),
        Cookie: `${sessionCookie!.name}=${sessionCookie!.value}`,
      },
    });
    const whoami = await instructorApi.get("/api/v1/session");
    expect(whoami.status()).toBe(200);
    expect((await whoami.json()).role).toBe("INSTRUCTOR");

    // The invited role is Instructor, so Admin-only staff authority stays refused.
    const refusedStaffRead = await instructorApi.get("/api/v1/staff-invitations");
    expect(refusedStaffRead.status()).toBeGreaterThanOrEqual(400);
    expect(refusedStaffRead.status()).toBeLessThan(500);

    // The Admin sees the resulting state without needing an id: the accepted invitee is now an
    // Instructor account, listed by address and shown Active.
    await adminPage.reload();
    const acceptedRow = adminPage.getByTestId("staff-instructor-row").filter({ hasText: recipient });
    await expect(acceptedRow).toBeVisible({ timeout: 15_000 });
    await expect(acceptedRow).toContainText("Active");

    const settledPending = await adminApi.get("/api/v1/staff-invitations");
    const stillPending = (await settledPending.json()).invitations.filter(
      (entry: { email: string }) => entry.email.toLowerCase() === recipient.toLowerCase(),
    );
    expect(stillPending, "the consumed invitation is no longer pending").toHaveLength(0);

    await instructorApi.dispose();
    await adminApi.dispose();
    await recipientContext.close();
    await adminContext.close();
  });

  test("B a consumed invitation link cannot be used a second time", async ({ browser }) => {
    const recipient = recipientFor("b");
    const adminContext = await browser.newContext({ baseURL: frontendOrigin() });
    const adminSession = await signInAdmin(adminContext);
    const adminPage = await adminContext.newPage();
    await inviteFromAdminScreen(adminPage, recipient);

    const recipientContext = await browser.newContext({ baseURL: frontendOrigin() });
    await recipientContext.addInitScript(() => {
      window.localStorage.setItem("gradex.locale", "en");
    });
    const invitationLink = await completeInvitationFromEmail(
      recipientContext,
      recipient,
      "Salem Replay",
    );

    // A clean context, as a forwarded link would be opened in.
    const replayContext = await browser.newContext({ baseURL: frontendOrigin() });
    await replayContext.addInitScript(() => {
      window.localStorage.setItem("gradex.locale", "en");
    });
    const replayPage = await replayContext.newPage();
    await replayPage.goto(invitationLink);
    await expect(
      replayPage.getByText("This invitation is invalid, expired, revoked, or already used."),
    ).toBeVisible({ timeout: 15_000 });
    // The completion form is not offered at all, so there is nothing to submit a second time.
    await expect(replayPage.locator("#staff-password")).toHaveCount(0);

    // No second account and no second grant: the address resolves to exactly one Instructor.
    const adminApi = await apiContextFor(adminSession);
    const instructors = await adminApi.get("/api/v1/staff-invitations/instructors");
    expect(instructors.status()).toBe(200);
    const matching = (await instructors.json()).instructors.filter(
      (account: { email: string }) => account.email.toLowerCase() === recipient.toLowerCase(),
    );
    expect(matching).toHaveLength(1);

    // Replay sent no further mail to the recipient.
    expect(await messageCountFor(recipient)).toBe(1);

    await adminApi.dispose();
    await replayContext.close();
    await recipientContext.close();
    await adminContext.close();
  });

  test("C an Admin suspends an Instructor account, capability is refused, and reinstatement restores it", async ({
    browser,
  }) => {
    const recipient = recipientFor("c");
    const adminContext = await browser.newContext({ baseURL: frontendOrigin() });
    await signInAdmin(adminContext);
    const adminPage = await adminContext.newPage();
    await inviteFromAdminScreen(adminPage, recipient);

    const recipientContext = await browser.newContext({ baseURL: frontendOrigin() });
    await recipientContext.addInitScript(() => {
      window.localStorage.setItem("gradex.locale", "en");
    });
    await completeInvitationFromEmail(recipientContext, recipient, "Noura Instructor");
    const instructorPage = await signInThroughLoginForm(recipientContext, recipient);
    await expect(instructorPage).toHaveURL(/\/en\/instructor\//);

    // Suspension is performed from the Admin screen, by address and with a stated reason.
    await adminPage.reload();
    const row = adminPage.getByTestId("staff-instructor-row").filter({ hasText: recipient });
    await expect(row).toBeVisible({ timeout: 15_000 });
    await expect(row).toContainText("Active");
    await row.getByTestId("staff-instructor-suspend").click();
    // Suspension signs a person out everywhere, so it is confirmed rather than performed on a
    // single click — and the confirmation states that effect rather than repeating the button. The
    // reason the server records is collected here, beside the consequence it describes.
    const suspendDialog = adminPage.getByTestId("staff-confirm");
    await expect(suspendDialog).toBeVisible();
    await suspendDialog.getByTestId("staff-instructor-reason").fill("T8B suspension acceptance");
    await expect(suspendDialog).toContainText("signed out everywhere immediately");
    await expect(suspendDialog).toContainText("students keep their access");
    await suspendDialog.getByTestId("confirm-accept").click();
    await expect(adminPage.getByText("The account was suspended.")).toBeVisible({ timeout: 15_000 });
    await expect(
      adminPage.getByTestId("staff-instructor-row").filter({ hasText: recipient }),
    ).toContainText("Suspended");

    // The suspended account keeps its identity — it is not deleted — and loses its capability.
    const suspendedCookies = await recipientContext.cookies();
    const suspendedCookie = suspendedCookies.find((cookie) => cookie.name.startsWith("__Host-"));
    const suspendedApi = await playwrightRequest.newContext({
      baseURL: frontendOrigin(),
      extraHTTPHeaders: {
        Accept: "application/json, application/problem+json",
        Origin: frontendOrigin(),
        Cookie: `${suspendedCookie!.name}=${suspendedCookie!.value}`,
      },
    });
    const refusedWhileSuspended = await suspendedApi.get("/api/v1/session");
    expect(refusedWhileSuspended.status()).toBeGreaterThanOrEqual(400);
    await suspendedApi.dispose();

    // A suspended account cannot sign in again either.
    const deniedContext = await browser.newContext({ baseURL: frontendOrigin() });
    await deniedContext.addInitScript(() => {
      window.localStorage.setItem("gradex.locale", "en");
    });
    const deniedPage = await deniedContext.newPage();
    await deniedPage.goto("/login");
    await deniedPage.locator("#email").fill(recipient);
    await deniedPage.locator("#password").fill(RECIPIENT_PASSWORD);
    await deniedPage.getByRole("button", { name: /sign in/i }).click();
    await expect(deniedPage).toHaveURL(/\/login/, { timeout: 15_000 });
    await deniedContext.close();

    // Reinstatement restores the same account, not a new one.
    const suspendedRow = adminPage
      .getByTestId("staff-instructor-row")
      .filter({ hasText: recipient });
    await suspendedRow.getByTestId("staff-instructor-reinstate").click();
    const reinstateDialog = adminPage.getByTestId("staff-confirm");
    await reinstateDialog
      .getByTestId("staff-instructor-reason")
      .fill("T8B reinstatement acceptance");
    await reinstateDialog.getByTestId("confirm-accept").click();
    await expect(adminPage.getByText("The account was reinstated.")).toBeVisible({
      timeout: 15_000,
    });
    await expect(
      adminPage.getByTestId("staff-instructor-row").filter({ hasText: recipient }),
    ).toContainText("Active");

    // Suspension ended the earlier session, so the contract is a fresh login — which now works.
    const restoredContext = await browser.newContext({ baseURL: frontendOrigin() });
    await restoredContext.addInitScript(() => {
      window.localStorage.setItem("gradex.locale", "en");
    });
    const restoredPage = await signInThroughLoginForm(restoredContext, recipient);
    await expect(restoredPage).toHaveURL(/\/en\/instructor\//);
    await restoredContext.close();

    await recipientContext.close();
    await adminContext.close();
  });
});
