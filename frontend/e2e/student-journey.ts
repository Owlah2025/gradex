import { expect, type Page } from "@playwright/test";
import { waitForMessageMatching } from "./mailpit";

/**
 * The Student acquisition journey, as a Student performs it.
 *
 * Registration no longer mails a link. It mails a six-digit code, and the code
 * is never stored in plaintext — the database holds a keyed HMAC of it — so
 * there is nothing for a test to read out of PostgreSQL. That is the point of
 * the design, and it makes the mailbox the only honest fixture: this reads the
 * code out of the message the worker actually delivered, exactly as the Student
 * reads it out of theirs.
 *
 * The code is held in process memory. It is never logged, never written to a
 * file, and never placed in a test title or an assertion message.
 */

const CODE_PATTERN = /\b\d{6}\b/;

/**
 * The verification code from the message Gradex sent to `recipient`.
 *
 * It also asserts what the message must *not* contain. A verification message
 * carrying both a code and a link would put two live credentials for one
 * challenge in one mailbox, which is the property the link-to-code change
 * exists to establish.
 */
export async function readVerificationCodeFor(
  recipient: string,
  /**
   * When the challenge this code belongs to was requested.
   *
   * Mailpit is a shared development instance that is not reset between runs, so
   * an address can hold a superseded code from an earlier run alongside
   * today's. That one still looks like a verification message, and typing it is
   * correctly refused — which surfaces as "the product sent the wrong code"
   * rather than as the fixture reading the wrong message. Bounding the search
   * to messages sent after the request removes the ambiguity.
   */
  notBefore?: Date,
): Promise<string> {
  const message = await waitForMessageMatching(
    recipient,
    (candidate) => CODE_PATTERN.test(candidate.Text ?? ""),
    { notBefore },
  );
  const body = `${message.Text ?? ""}\n${message.HTML ?? ""}`;
  expect(
    /https?:\/\/[^\s"'<>]*verify-email/.test(body),
    "the verification message carried a link as well as a code",
  ).toBe(false);
  const found = CODE_PATTERN.exec(message.Text ?? "");
  if (!found) {
    throw new Error(`The message delivered to ${recipient} carries no verification code.`);
  }
  return found[0];
}

export type StudentRegistration = {
  email: string;
  password: string;
  displayName: string;
  /**
   * Where the journey should land once the Student is verified. Carried
   * through register → verify exactly as the product carries it, so the test
   * proves the destination survives rather than assuming it.
   */
  returnTo?: string;
};

/**
 * Registers a Student and completes the emailed-code verification.
 *
 * On return the browser holds an ordinary authenticated session. The password
 * is deliberately not re-entered: the code already proved control of the
 * mailbox, and a password prompt at that point establishes nothing further.
 */
export async function registerAndVerifyStudent(
  page: Page,
  registration: StudentRegistration,
): Promise<void> {
  const registerPath = registration.returnTo
    ? `/register?returnTo=${encodeURIComponent(registration.returnTo)}`
    : "/register";
  await page.goto(registerPath);

  await page.locator("#display-name").fill(registration.displayName);
  await page.locator("#email").fill(registration.email);
  await page.locator("#password").fill(registration.password);
  const policies = page.locator('fieldset input[type="checkbox"]');
  await expect(policies).toHaveCount(2);
  await policies.nth(0).check();
  await policies.nth(1).check();
  // Not `form button`: the password field carries a reveal control, so the form
  // holds more than one. And not the button's name either — these auth routes
  // carry no locale segment and render in the reader's stored language, so an
  // English name matches nothing on an Arabic page.
  // Captured before the request, so the code this journey reads back is the one
  // this journey caused to be sent.
  const requestedAt = new Date();
  await page.locator('form button[type="submit"]').click();

  // The verification screen opens on its own, already knowing which challenge
  // it is about. It must never ask for the address a second time.
  await page.waitForURL((url) => url.pathname === "/verify-email");
  const verificationURL = new URL(page.url());
  expect(
    verificationURL.searchParams.get("challenge"),
    "the verification screen was opened without a challenge",
  ).toBeTruthy();
  if (registration.returnTo) {
    expect(verificationURL.searchParams.get("returnTo")).toBe(registration.returnTo);
  }
  await expect(page.getByTestId("verification-masked-email")).toBeVisible();
  // The masked address must not be the address.
  await expect(page.getByTestId("verification-masked-email")).not.toContainText(
    registration.email,
  );
  await expect(page.locator('input[type="password"]')).toHaveCount(0);

  const code = await readVerificationCodeFor(registration.email, requestedAt);
  await page.getByTestId("verification-code-input").fill(code);
  await page.getByTestId("verification-code-submit").click();
}

/**
 * Signs an existing Student in through the real login form.
 */
export async function signIn(
  page: Page,
  credentials: { email: string; password: string },
  returnTo?: string,
): Promise<void> {
  await page.goto(returnTo ? `/login?returnTo=${encodeURIComponent(returnTo)}` : "/login");
  await page.locator("#email").fill(credentials.email);
  await page.locator("#password").fill(credentials.password);
  await page.locator('form button[type="submit"]').click();
}

export type PurchaseHandoff = {
  response: {
    reference: string;
    whatsapp_url: string;
    course_title: string;
    price_minor_units: number;
    currency: string;
    reused: boolean;
  };
  handoffURL: string;
};

/**
 * Completes a purchase from the confirmation card and captures the handoff.
 *
 * The page leaves the origin as soon as its fetch resolves, so the API response
 * is captured in a route proxy before the browser is fulfilled — Playwright
 * cannot read a response body after that navigation has started.
 */
export async function completePurchaseConfirmation(page: Page): Promise<PurchaseHandoff> {
  let persistedPayload: PurchaseHandoff["response"] | undefined;
  await page.route("**/api/v1/me/purchase-requests", async (route) => {
    if (route.request().method() !== "POST") {
      await route.continue();
      return;
    }
    const upstream = await route.fetch();
    const body = await upstream.body();
    persistedPayload = JSON.parse(body.toString("utf-8")) as PurchaseHandoff["response"];
    await route.fulfill({ response: upstream, body });
  });
  const persisted = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === "/api/v1/me/purchase-requests" &&
      response.request().method() === "POST",
  );
  const handoff = page.waitForRequest(
    (request) => request.isNavigationRequest() && request.url().startsWith("https://wa.me/"),
  );
  await page.getByTestId("purchase-request-submit").click({ noWaitAfter: true });
  const persistedResponse = await persisted;
  expect(persistedResponse.status()).toBe(201);
  const handoffRequest = await handoff;
  expect(persistedPayload).toBeDefined();
  return { response: persistedPayload!, handoffURL: handoffRequest.url() };
}

/**
 * Asserts that nothing has navigated to WhatsApp and that no purchase request
 * has been created.
 *
 * Installed before the CTA is pressed, so it observes the whole span between
 * "Buy this course" and the explicit confirmation. Both halves matter: an
 * operational task created before anyone confirmed is as wrong as a handoff
 * opened before anyone confirmed.
 */
export function watchForPrematurePurchase(page: Page): { assertQuiet(): void } {
  const violations: string[] = [];
  page.on("request", (request) => {
    const url = request.url();
    if (url.startsWith("https://wa.me/")) {
      violations.push("navigated to WhatsApp");
    }
    if (
      request.method() === "POST" &&
      new URL(url).pathname.endsWith("/purchase-requests")
    ) {
      violations.push(`created a purchase request via ${new URL(url).pathname}`);
    }
  });
  return {
    assertQuiet() {
      expect(violations, "the purchase journey acted before the Student confirmed").toEqual([]);
    },
  };
}
