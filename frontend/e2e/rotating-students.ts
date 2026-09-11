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
  ACCESS_A11Y_EXPIRED_AR_TEST_SLOT,
  ACCESS_A11Y_EXPIRED_EN_TEST_SLOT,
  ACCESS_A11Y_INVITED_AR_TEST_SLOT,
  ACCESS_A11Y_INVITED_EN_TEST_SLOT,
  ACADEMIC_ACCESS_PRESERVED_TEST_SLOT,
  ACADEMIC_DSAI_TEST_SLOT,
  ACADEMIC_INVITATION_TEST_SLOT,
  ACADEMIC_ONBOARDING_TEST_SLOT,
  ACADEMIC_SKIP_TEST_SLOT,
  ACADEMIC_UNDECLARED_TEST_SLOT,
  DASHBOARD_RESUME_AR_TEST_SLOT,
  DASHBOARD_RESUME_EN_TEST_SLOT,
  ENTITLEMENT_EXTEND_TEST_SLOT,
  ENTITLEMENT_PAST_DATE_TEST_SLOT,
  ENTITLEMENT_REVOKE_TEST_SLOT,
  ADMIN_REPORTED_CONTENT_TEST_SLOT,
  ENTITLEMENT_SHORTEN_TEST_SLOT,
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
  role: "STUDENT" | "INSTRUCTOR" | "ADMIN";
  cookie_name: string;
  cookie_value: string;
  csrf_token: string;
  /**
   * The trusted-device credential the session is bound to.
   *
   * Protected learning requires both cookies: the session authenticates, and
   * the device says which of the Student's browsers is asking. A context that
   * installed only the session would model a browser that has signed in but not
   * yet confirmed itself — a real state, and not the one these journeys are
   * about.
   */
  device_cookie_name: string;
  device_cookie_value: string;
  device_id: string;
};

/**
 * Issues a production-valid session for a seeded Student through the real session repository,
 * without spending the login endpoint's per-network budget. The credential is a real opaque
 * session credential in the real `__Host-` cookie; production middleware validates it exactly as
 * it validates a browser login. It is never written to the run-state file and never exposed to
 * browser JavaScript.
 */
export function issueRotatingSession(
  student: Pick<RotatingStudent, "accountID" | "email">,
  /**
   * Which of this Student's synthetic browsers to sign in.
   *
   * Slot 0 is "their browser" and is what every suite that simply needs a
   * signed-in Student wants: issuing again returns the same device, exactly as
   * a real Student signing in again on the same machine does. A second slot is
   * a genuinely different device, and only the device-security journey needs
   * one.
   */
  deviceSlot = 0,
): IssuedSession {
  if (!fs.existsSync(RUN_STATE_FILE_PATH)) {
    throw new Error(`E2E run state is missing at ${RUN_STATE_FILE_PATH}; cannot issue a session.`);
  }
  const state = JSON.parse(fs.readFileSync(RUN_STATE_FILE_PATH, "utf-8"));

  const output = execFileSync(
    SEED_BINARY_PATH,
    ["-issue-session", "-email", student.email, "-device-slot", String(deviceSlot)],
    {
      env: {
        ...process.env,
        ...e2eDatabaseEnvironment(state.dbName),
      },
      encoding: "utf-8",
    },
  );

  const session = JSON.parse(output.trim()) as IssuedSession;
  if (session.role === "STUDENT" && (!session.device_cookie_value || !session.device_id)) {
    throw new Error(`Session issuance returned no trusted device for ${student.email}.`);
  }
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
): Promise<IssuedSession> {
  const session = issueRotatingSession(student);
  await installIssuedSession(context, session);
  return session;
}

/**
 * Installs an already-issued session and its trusted device into a context.
 *
 * Separated from issuance so a journey that needs two independent devices on
 * one Account can issue twice and install each into its own browser context.
 */
export async function installIssuedSession(
  context: BrowserContext,
  session: IssuedSession
): Promise<void> {
  const origin = new URL(frontendOrigin());
  const attributes = {
    domain: origin.hostname,
    path: "/",
    httpOnly: true,
    // `127.0.0.1` is a trustworthy origin, so the production `__Host-` cookie attributes apply
    // unchanged over loopback HTTP.
    secure: true,
    sameSite: "Strict" as const,
  };
  const cookies = [{ name: session.cookie_name, value: session.cookie_value, ...attributes }];
  if (session.device_cookie_name && session.device_cookie_value) {
    cookies.push({ name: session.device_cookie_name, value: session.device_cookie_value, ...attributes });
  }
  await context.addCookies(cookies);
}

/** Cookie header for APIRequestContext calls made outside a browser context. */
export function issuedSessionCookieHeader(session: IssuedSession): string {
	const cookies = [`${session.cookie_name}=${session.cookie_value}`];
	if (session.device_cookie_name && session.device_cookie_value) {
		cookies.push(`${session.device_cookie_name}=${session.device_cookie_value}`);
	}
	return cookies.join("; ");
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
