import { approvedPolicyDocuments } from "./lg011-policy.generated";

export const approvedPolicyMetadata = {
  id: "gradex-legal-2026-08-09-v1",
  version: "2026-08-09-v1",
  privacyVersion: "2026-08-09-v1",
  termsVersion: "2026-08-09-v1",
  effectiveDate: "2026-08-09",
  minimumAge: 18,
  primaryLocale: "ar",
} as const;

export const stagingLegalRegistrationNumber = "STAGING-NOT-REGISTERED";
export const stagingLegalRegisteredAddress =
  "STAGING ONLY — LEGAL ENTITY DETAILS PENDING";
export const controlledStagingLocalOrigin = "https://gradex.localhost:18443";
export const controlledStagingLG019Origin = "https://staging.gradex.network";

export type LegalLocale = "ar" | "en";
export type LegalPolicyKind = "privacy" | "terms";

export type LegalIdentity = {
  operatorName: string;
  registrationNumber: string;
  registeredAddress: string;
  privacyEmail: string;
  supportEmail: string;
  securityEmail: string;
};

function isControlledStagingOrigin(publicOrigin: string): boolean {
  switch (publicOrigin) {
    case controlledStagingLocalOrigin:
    case controlledStagingLG019Origin:
      return true;
    default:
      return false;
  }
}

export function validateLegalIdentityMode(
  mode: string,
  publicOrigin: string,
  identity: LegalIdentity,
): boolean {
  const hasSentinel =
    identity.registrationNumber === stagingLegalRegistrationNumber ||
    identity.registeredAddress === stagingLegalRegisteredAddress;
  if (mode === "public") {
    if (hasSentinel) {
      throw new Error("public legal identity rejects controlled-staging sentinel values");
    }
    return false;
  }
  if (
    mode !== "controlled-staging" ||
    !isControlledStagingOrigin(publicOrigin) ||
    identity.registrationNumber !== stagingLegalRegistrationNumber ||
    identity.registeredAddress !== stagingLegalRegisteredAddress
  ) {
    throw new Error(
      "controlled-staging legal identity requires an exact approved disposable origin and sentinels",
    );
  }
  return true;
}

const approvedContactEmail = "ahmedhazemelmelegy11@gmail.com";

export function approvedPolicyDocument(locale: LegalLocale, kind: LegalPolicyKind): string {
  if (locale === "ar") {
    return kind === "privacy"
      ? approvedPolicyDocuments.arabicPrivacy
      : approvedPolicyDocuments.arabicTerms;
  }
  return kind === "privacy"
    ? approvedPolicyDocuments.englishPrivacy
    : approvedPolicyDocuments.englishTerms;
}

function configuredContactLine(
  line: string,
  identity: LegalIdentity,
  kind: LegalPolicyKind,
): string {
  const occurrences = line.split(approvedContactEmail).length - 1;
  if (occurrences === 0) return line;
  if (occurrences === 2) {
    return line
      .replace(approvedContactEmail, identity.privacyEmail)
      .replace(approvedContactEmail, identity.supportEmail);
  }
  if (/security vulnerability|account has been compromised|ثغرة أمنية|تعرض للاختراق/.test(line)) {
    return line.replace(approvedContactEmail, identity.securityEmail);
  }
  if (/^Support:|^الدعم:/.test(line) || (kind === "terms" && /account closure|إغلاق الحساب/.test(line))) {
    return line.replace(approvedContactEmail, identity.supportEmail);
  }
  return line.replace(approvedContactEmail, identity.privacyEmail);
}

export function interpolatePolicyDocument(
  source: string,
  identity: LegalIdentity,
  kind: LegalPolicyKind,
): string {
  return source
    .replaceAll("{{LEGAL_REGISTRATION_NUMBER}}", identity.registrationNumber)
    .replaceAll("{{LEGAL_REGISTERED_ADDRESS}}", identity.registeredAddress)
    .replaceAll("Gradex Courses", identity.operatorName)
    .split("\n")
    .map((line) => configuredContactLine(line, identity, kind))
    .join("\n");
}

export function canonicalPolicyPath(locale: LegalLocale, kind: LegalPolicyKind): string {
  return `/${locale}/${kind}`;
}

export function canonicalPolicyURL(
  publicOrigin: string,
  locale: LegalLocale,
  kind: LegalPolicyKind,
): string {
  return `${publicOrigin}${canonicalPolicyPath(locale, kind)}`;
}
