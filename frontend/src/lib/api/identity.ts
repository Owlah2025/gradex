import { getJSON, postJSON } from "./http";

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
