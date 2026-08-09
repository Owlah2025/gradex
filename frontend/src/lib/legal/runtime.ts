import "server-only";

import {
  controlledStagingOrigin,
  stagingLegalRegisteredAddress,
  stagingLegalRegistrationNumber,
  type LegalIdentity,
} from "./policy";

export type LegalRuntime = {
  publicOrigin: string;
  identity: LegalIdentity;
  controlledStaging: boolean;
};

const developmentIdentity: LegalIdentity = {
  operatorName: "Gradex Courses",
  registrationNumber: stagingLegalRegistrationNumber,
  registeredAddress: stagingLegalRegisteredAddress,
  privacyEmail: "ahmedhazemelmelegy11@gmail.com",
  supportEmail: "ahmedhazemelmelegy11@gmail.com",
  securityEmail: "ahmedhazemelmelegy11@gmail.com",
};

function exactOrigin(raw: string, requireHTTPS: boolean): string {
  let parsed: URL;
  try {
    parsed = new URL(raw);
  } catch {
    throw new Error("PUBLIC_ORIGIN must be an exact absolute origin for legal policies");
  }
  const canonical = `${parsed.protocol}//${parsed.host}`;
  if (
    parsed.username ||
    parsed.password ||
    parsed.pathname !== "/" ||
    parsed.search ||
    parsed.hash ||
    (raw !== canonical && raw !== `${canonical}/`) ||
    (requireHTTPS && parsed.protocol !== "https:")
  ) {
    throw new Error("PUBLIC_ORIGIN must be an exact HTTPS origin for legal policies");
  }
  return canonical;
}

function required(name: string): string {
  const value = process.env[name]?.trim();
  if (!value) throw new Error(`${name} is required for legal policies`);
  return value;
}

function validEmail(name: string, value: string): string {
  if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(value)) {
    throw new Error(`${name} must be a valid email address`);
  }
  return value;
}

function configuredIdentity(): LegalIdentity {
  return {
    operatorName: required("LEGAL_OPERATOR_NAME"),
    registrationNumber: required("LEGAL_REGISTRATION_NUMBER"),
    registeredAddress: required("LEGAL_REGISTERED_ADDRESS"),
    privacyEmail: validEmail("PRIVACY_EMAIL", required("PRIVACY_EMAIL")),
    supportEmail: validEmail("SUPPORT_EMAIL", required("SUPPORT_EMAIL")),
    securityEmail: validEmail("SECURITY_EMAIL", required("SECURITY_EMAIL")),
  };
}

function controlledStagingMode(publicOrigin: string, identity: LegalIdentity): boolean {
  const mode = required("LEGAL_IDENTITY_MODE");
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
    publicOrigin !== controlledStagingOrigin ||
    identity.registrationNumber !== stagingLegalRegistrationNumber ||
    identity.registeredAddress !== stagingLegalRegisteredAddress
  ) {
    throw new Error(
      "controlled-staging legal identity requires the exact disposable S11 origin and sentinels",
    );
  }
  return true;
}

function developmentRuntime(): LegalRuntime {
  return {
    publicOrigin: exactOrigin(process.env.PUBLIC_ORIGIN || "http://localhost:3000", false),
    identity: developmentIdentity,
    controlledStaging: true,
  };
}

export function legalRuntime(): LegalRuntime {
  if (process.env.NODE_ENV !== "production") return developmentRuntime();
  const publicOrigin = exactOrigin(required("PUBLIC_ORIGIN"), true);
  const identity = configuredIdentity();
  return {
    publicOrigin,
    identity,
    controlledStaging: controlledStagingMode(publicOrigin, identity),
  };
}
