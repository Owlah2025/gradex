import { execFileSync } from "child_process";
import fs from "fs";
import { expect, type BrowserContext, type Page } from "@playwright/test";
import {
  e2eDatabaseEnvironment,
  RUN_STATE_FILE_PATH,
  SEED_BINARY_PATH,
} from "../src/lib/api/e2e-infrastructure";
import { waitForMessageMatching } from "./mailpit";

/**
 * Completing device confirmation, the way a Student does.
 *
 * Signing in is no longer the last step of authentication for a Student on a
 * browser Gradex has not seen: the session that comes back is narrowed to
 * device confirmation, password change, and signing out until an emailed code
 * confirms the browser. Every suite that authenticates a Student through the
 * real login route therefore has to finish that flow, and this is the one place
 * that does it — through the product's own API, with the real code out of the
 * real mailbox.
 *
 * Nothing here is a shortcut around the policy. The code is read from the
 * message the server actually sent, the device is the one the challenge was
 * raised for, and the limit and cooldown apply exactly as they do in
 * production.
 */

const DEVICE_CODE_PATTERN = /\b(\d{6})\b/;

/** Reads the six digits out of the device-confirmation message. */
export async function readDeviceTrustCodeFor(
  recipient: string,
  notBefore?: Date,
): Promise<string> {
  const message = await waitForMessageMatching(
    recipient,
    (candidate) =>
      /device/i.test(candidate.Subject ?? "") ||
      /الجهاز/.test(candidate.Subject ?? ""),
    { notBefore },
  );
  const body = `${message.Text ?? ""}\n${message.HTML ?? ""}`;
  // The device-confirmation message carries a code and nothing clickable, for
  // the same reason the verification code does: a credential in a URL can be
  // forwarded, and this one exists precisely because somebody may be signing in
  // who should not be.
  expect(
    /https?:\/\/[^\s"'<>]*device/.test(body),
    "the device confirmation message carried a link as well as a code",
  ).toBe(false);
  const match = DEVICE_CODE_PATTERN.exec(message.Text ?? "");
  expect(match, `no device confirmation code was found in the message to ${recipient}`).toBeTruthy();
  return match![1];
}

/** What a login response says about this browser's device state. */
export type LoginDeviceTrust = {
  state?: string;
  admission?: string;
  challenge?: { challenge_id: string };
};

/**
 * Confirms this browser when the login that just happened asked for it.
 *
 * A no-op when the session already carries a confirmed device, so callers can
 * apply it unconditionally after signing in.
 */
export function completeDeviceTrustIfRequired(
  _page: Page,
  email: string,
  deviceTrust: LoginDeviceTrust | undefined,
  _requestedAt: Date,
): void {
  if (!deviceTrust || deviceTrust.state !== "PENDING_DEVICE_TRUST") return;
  confirmSeededDevicesFor(email);
}

/**
 * Confirms a seeded Student's pending devices through the seeder.
 *
 * Deliberately not through the mailbox. Most suites authenticate a Student in
 * order to test something else — Course Home, Progress, the catalogue — and
 * routing every one of them through a mail server would make the fixture the
 * test and would couple suites that need no email to one. The emailed code is
 * still the only way a real Student confirms a device; that path is proven by
 * the identity integration suite and, end to end through the product's own
 * screens, by the device-security browser journey.
 */
export function confirmSeededDevicesFor(email: string): void {
  if (!fs.existsSync(RUN_STATE_FILE_PATH)) {
    throw new Error(`E2E run state is missing at ${RUN_STATE_FILE_PATH}; cannot confirm devices.`);
  }
  const state = JSON.parse(fs.readFileSync(RUN_STATE_FILE_PATH, "utf-8"));
  execFileSync(SEED_BINARY_PATH, ["-trust-devices", "-email", email], {
    env: { ...process.env, ...e2eDatabaseEnvironment(state.dbName) },
    encoding: "utf-8",
  });
}

/**
 * Confirms this browser from the device-confirmation *screen*.
 *
 * Used by the journeys that sign in through the form rather than the API: they
 * are sent here by the product, and driving the real screen is what proves the
 * journey a Student actually walks.
 */
export async function completeDeviceTrustScreen(
  page: Page,
  email: string,
  requestedAt: Date,
): Promise<void> {
  await expect(page.getByTestId("device-trust-form")).toBeVisible();
  const code = await readDeviceTrustCodeFor(email, requestedAt);
  await page.getByTestId("device-code").fill(code);
  await page.getByTestId("device-trust-submit").click();
  await expect(page.getByTestId("device-trust-form")).toHaveCount(0);
}

/** True when this context already holds a device credential. */
export async function hasDeviceCredential(context: BrowserContext): Promise<boolean> {
  const cookies = await context.cookies();
  return cookies.some((cookie) => cookie.name === "__Host-gradex_device");
}
