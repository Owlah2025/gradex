import { execFileSync } from "child_process";
import fs from "fs";
import { e2eDatabaseEnvironment, RUN_STATE_FILE_PATH, SEED_BINARY_PATH } from "./e2e-infrastructure";

/**
 * The Account and the six-digit code Gradex emailed it.
 *
 * `verification_code`, not a token: registration no longer mails a link. The
 * code is unreadable from `identity_action_secrets` — that column holds a keyed
 * HMAC — so the seeder reads it out of the outbox's encrypted payload, which is
 * the same ciphertext the dispatcher opens to send the message.
 */
export type EmailVerificationAction = {
  account_id: string;
  verification_code: string;
};

export function parseEmailVerificationAction(raw: string): EmailVerificationAction {
  const trimmed = raw.trim();
  if (trimmed === "") {
    throw new Error("Email-verification query returned no output; the seeder helper did not run.");
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch {
    throw new Error(`Email-verification query returned non-JSON output: ${trimmed.slice(0, 200)}`);
  }
  if (
    typeof parsed !== "object" ||
    parsed === null ||
    typeof (parsed as EmailVerificationAction).account_id !== "string" ||
    !(parsed as EmailVerificationAction).account_id ||
    typeof (parsed as EmailVerificationAction).verification_code !== "string" ||
    !(parsed as EmailVerificationAction).verification_code
  ) {
    throw new Error("Email-verification query returned an unusable action.");
  }
  return parsed as EmailVerificationAction;
}

export function queryEmailVerificationAction(email: string): EmailVerificationAction {
  if (!fs.existsSync(RUN_STATE_FILE_PATH)) {
    throw new Error(`E2E run state is missing at ${RUN_STATE_FILE_PATH}; cannot query email verification.`);
  }
  const state = JSON.parse(fs.readFileSync(RUN_STATE_FILE_PATH, "utf-8"));
  const output = execFileSync(
    SEED_BINARY_PATH,
    ["-query-email-verification-token", "-email", email],
    {
      env: {
        ...process.env,
        ...e2eDatabaseEnvironment(state.dbName),
      },
      encoding: "utf-8",
    }
  );
  return parseEmailVerificationAction(output);
}
