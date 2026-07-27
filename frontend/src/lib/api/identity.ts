import { authenticatedRequest, getJSON, postJSON } from "./http";
import type { AuthenticatedSession } from "@/lib/identity/session";

export type Policy = {
  kind: "PRIVACY_NOTICE" | "TERMS_OF_SERVICE";
  version: string;
  label: string;
  url: string;
};

export type RegistrationPolicySet = {
  id: string;
  policies: Policy[];
};

export type StudentRegistrationInput = {
  display_name: string;
  email: string;
  password: string;
  locale: "ar" | "en";
  policy_set_id: string;
};

export function getRegistrationPolicySet(locale: "ar" | "en") {
  return getJSON<RegistrationPolicySet>("/registration-policy-set", locale);
}

export function registerStudent(input: StudentRegistrationInput) {
  return postJSON<{ code: "REGISTRATION_REQUEST_ACCEPTED" }>(
    "/student-registrations",
    input,
    input.locale,
  );
}

export function requestEmailVerification(
  email: string,
  locale: "ar" | "en",
) {
  return postJSON<{ code: "VERIFICATION_REQUEST_ACCEPTED" }>(
    "/email-verification-requests",
    { email },
    locale,
  );
}

export function consumeEmailVerification(
  token: string,
  locale: "ar" | "en",
) {
  return postJSON<{ status: "VERIFIED" }>(
    "/email-verifications",
    { token },
    locale,
  );
}

/**
 * Signs in and creates one server-managed session family.
 *
 * Login sits behind the same anonymous origin/CSRF admission boundary as
 * registration, so it reuses `postJSON`. The response carries the session
 * representation; the credential itself arrives only as a `__Host-` cookie.
 */
export function createSession(
  email: string,
  password: string,
  locale: "ar" | "en",
) {
  return postJSON<AuthenticatedSession>(
    "/sessions",
    { email, password },
    locale,
  );
}

/**
 * Resolves the current cookie and rehydrates the in-memory CSRF token after a
 * reload. This read does not rotate credentials or extend idle expiry.
 */
export function getSession(locale: "ar" | "en") {
  return authenticatedRequest<AuthenticatedSession>(
    "/session",
    "GET",
    locale,
  ) as Promise<AuthenticatedSession>;
}

/** Deliberately rotates the credential and CSRF token together. */
export function renewSession(csrf: string, locale: "ar" | "en") {
  return authenticatedRequest<AuthenticatedSession>(
    "/session-renewals",
    "POST",
    locale,
    csrf,
  ) as Promise<AuthenticatedSession>;
}

/** Revokes the current family server-side, then clears the cookie. */
export function deleteSession(csrf: string, locale: "ar" | "en") {
  return authenticatedRequest<null>("/session", "DELETE", locale, csrf);
}

/**
 * Requests a password reset link.
 *
 * The response is deliberately uninformative: the server answers identically
 * for unknown, unverified, suspended, and eligible addresses, so callers must
 * not branch on it to say whether an account exists.
 */
export function requestPasswordReset(email: string, locale: "ar" | "en") {
  return postJSON<{ code: "PASSWORD_RESET_REQUEST_ACCEPTED" }>(
    "/password-reset-requests",
    { email },
    locale,
  );
}

/**
 * Consumes a reset link and replaces the password.
 *
 * No session is returned. Every family is invalidated server-side, so the
 * caller must sign in normally afterwards.
 */
export function completePasswordReset(
  token: string,
  password: string,
  locale: "ar" | "en",
) {
  return postJSON<{ status: "PASSWORD_RESET" }>(
    "/password-resets",
    { token, password },
    locale,
  );
}

export function createStaffInvitation(
  email: string,
  role: "INSTRUCTOR" | "ADMIN",
  locale: "ar" | "en",
  csrf?: string,
) {
  return authenticatedRequest<{
    id: string;
    email: string;
    invited_role: string;
    bearer: string;
    created_at: string;
  }>("/staff/invitations", "POST", locale, csrf, { email, role });
}

export function suspendStaffAccount(
  accountID: string,
  reason: string,
  locale: "ar" | "en",
  csrf?: string,
) {
  return authenticatedRequest<{
    already_suspended: boolean;
    revision: number;
    epoch: number;
  }>(`/staff/${encodeURIComponent(accountID)}/suspend`, "POST", locale, csrf, { reason });
}

export function reinstateStaffAccount(
  accountID: string,
  reason: string,
  locale: "ar" | "en",
  csrf?: string,
) {
  return authenticatedRequest<{
    already_active: boolean;
  }>(`/staff/${encodeURIComponent(accountID)}/reinstate`, "POST", locale, csrf, { reason });
}
