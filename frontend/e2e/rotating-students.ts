import { execFileSync } from "child_process";
import fs from "fs";
import type { BrowserContext } from "@playwright/test";
import { e2eDatabaseEnvironment, SEED_BINARY_PATH, RUN_STATE_FILE_PATH } from "../src/lib/api/e2e-infrastructure";
import { frontendOrigin } from "../src/lib/api/e2e-ports";
import type { RotatingStudent } from "../src/lib/api/e2e-students";
export { queryEmailVerificationAction } from "../src/lib/api/e2e-actions";

/**
 * Rate-limit-safe Student allocation and production-valid session issuance for the E2E suite.
 *
 * The allocation arithmetic lives in `src/lib/api/e2e-students.ts` so its collision properties can
 * be proved by the unit suite; this module adds the Playwright and child-process concerns.
 */
export {
  expiredStudentFor,
  expiredTestSlot,
  genericTestSlot,
  lifecycleTestSlot,
  playerTestSlot,
  studentFor,
  viewportEvidenceTestSlot,
  LIFECYCLE_TEST_SLOT,
  PROGRESS_TEST_SLOT,
  ROTATING_EXPIRED_POOL_SIZE,
  ROTATING_EXPIRED_SLOTS,
  ROTATING_MAX_REPEATS,
  ROTATING_POOL_SIZE,
  ROTATING_TEST_SLOTS,
  type RotatingStudent,
} from "../src/lib/api/e2e-students";


type IssuedSession = {
  account_id: string;
  cookie_name: string;
  cookie_value: string;
  csrf_token: string;
};

/**
 * Issues a production-valid session for a seeded Student through the real session repository,
 * without spending the login endpoint's per-network budget. The credential is a real opaque
 * session credential in the real `__Host-` cookie; production middleware validates it exactly as
 * it validates a browser login. It is never written to the run-state file and never exposed to
 * browser JavaScript.
 */
export function issueRotatingSession(student: Pick<RotatingStudent, "accountID" | "email">): IssuedSession {
  if (!fs.existsSync(RUN_STATE_FILE_PATH)) {
    throw new Error(`E2E run state is missing at ${RUN_STATE_FILE_PATH}; cannot issue a session.`);
  }
  const state = JSON.parse(fs.readFileSync(RUN_STATE_FILE_PATH, "utf-8"));

  const output = execFileSync(SEED_BINARY_PATH, ["-issue-session", "-email", student.email], {
    env: {
      ...process.env,
      ...e2eDatabaseEnvironment(state.dbName),
    },
    encoding: "utf-8",
  });

  const session = JSON.parse(output.trim()) as IssuedSession;
  if (!session.cookie_value || !session.csrf_token) {
    throw new Error(`Session issuance returned an unusable session for ${student.email}.`);
  }
  if (session.account_id !== student.accountID) {
    throw new Error(
      `Session issuance resolved ${session.account_id} for ${student.email}, expected ${student.accountID}.`
    );
  }
  return session;
}

/**
 * Installs the issued session into a browser context. The page's session rehydrator resolves the
 * cookie against the real session route on load, so the browser holds its CSRF token exactly as
 * it would after logging in.
 */
export async function authenticateRotatingStudent(
  context: BrowserContext,
  student: RotatingStudent
): Promise<void> {
  const session = issueRotatingSession(student);
  const origin = new URL(frontendOrigin());
  await context.addCookies([
    {
      name: session.cookie_name,
      value: session.cookie_value,
      domain: origin.hostname,
      path: "/",
      httpOnly: true,
      // `127.0.0.1` is a trustworthy origin, so the production `__Host-` cookie attributes apply
      // unchanged over loopback HTTP.
      secure: true,
      sameSite: "Strict",
    },
  ]);
}

export function queryInvitationToken(invitationID: string): string {
  if (!fs.existsSync(RUN_STATE_FILE_PATH)) {
    throw new Error(`E2E run state is missing at ${RUN_STATE_FILE_PATH}; cannot query invitation token.`);
  }
  const state = JSON.parse(fs.readFileSync(RUN_STATE_FILE_PATH, "utf-8"));
  const output = execFileSync(SEED_BINARY_PATH, ["-query-invitation-token", "-invitation", invitationID], {
    env: {
      ...process.env,
      ...e2eDatabaseEnvironment(state.dbName),
    },
  });
  const parsed = JSON.parse(output.toString("utf-8"));
  return parsed.verification_token;
}
