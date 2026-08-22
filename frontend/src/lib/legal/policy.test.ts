import assert from "node:assert/strict";
import test from "node:test";
import {
  approvedPolicyDocument,
  approvedPolicyMetadata,
  canonicalPolicyURL,
  interpolatePolicyDocument,
  validateLegalIdentityMode,
  type LegalIdentity,
} from "./policy";

const identity: LegalIdentity = {
  operatorName: "Gradex Courses",
  registrationNumber: "STAGING-NOT-REGISTERED",
  registeredAddress: "STAGING ONLY — LEGAL ENTITY DETAILS PENDING",
  privacyEmail: "privacy@gradex.example",
  supportEmail: "support@gradex.example",
  securityEmail: "security@gradex.example",
};

test("approved LG-011 metadata and canonical routes stay stable", () => {
  assert.deepEqual(approvedPolicyMetadata, {
    id: "gradex-legal-2026-08-09-v1",
    version: "2026-08-09-v1",
    privacyVersion: "2026-08-09-v1",
    termsVersion: "2026-08-09-v1",
    effectiveDate: "2026-08-09",
    minimumAge: 18,
    primaryLocale: "ar",
  });
  assert.equal(
    canonicalPolicyURL("https://gradex.example", "ar", "privacy"),
    "https://gradex.example/ar/privacy",
  );
  assert.equal(
    canonicalPolicyURL("https://gradex.example", "en", "terms"),
    "https://gradex.example/en/terms",
  );
});

test("all four approved documents interpolate the configuration-backed identity", () => {
  for (const locale of ["ar", "en"] as const) {
    for (const kind of ["privacy", "terms"] as const) {
      const rendered = interpolatePolicyDocument(approvedPolicyDocument(locale, kind), identity, kind);
      assert.match(rendered, /STAGING-NOT-REGISTERED/);
      assert.match(rendered, /STAGING ONLY — LEGAL ENTITY DETAILS PENDING/);
      assert.doesNotMatch(rendered, /\{\{LEGAL_/);
      assert.match(rendered, /2026-08-09-v1/);
      assert.match(rendered, /privacy@gradex\.example/);
      assert.match(rendered, /support@gradex\.example/);
      if (kind === "terms") assert.match(rendered, /security@gradex\.example/);
    }
  }
});

test("controlled-staging legal identity accepts only the two exact disposable origins", () => {
  for (const origin of [
    "https://gradex.localhost:18443",
    "https://staging.gradex.network",
  ]) {
    assert.equal(validateLegalIdentityMode("controlled-staging", origin, identity), true);
  }

  for (const origin of [
    "https://other.gradex.network",
    "http://staging.gradex.network",
  ]) {
    assert.throws(
      () => validateLegalIdentityMode("controlled-staging", origin, identity),
      /exact approved disposable origin and sentinels/,
    );
  }
});

test("controlled-staging legal identity requires both exact sentinel values", () => {
  for (const changedIdentity of [
    { ...identity, registrationNumber: "KWT-REAL-123" },
    { ...identity, registeredAddress: "Kuwait City, Kuwait" },
  ]) {
    assert.throws(
      () =>
        validateLegalIdentityMode(
          "controlled-staging",
          "https://staging.gradex.network",
          changedIdentity,
        ),
      /exact approved disposable origin and sentinels/,
    );
  }
});

test("public legal identity rejects either controlled-staging sentinel", () => {
  for (const publicIdentity of [
    { ...identity, registeredAddress: "Kuwait City, Kuwait" },
    { ...identity, registrationNumber: "KWT-REAL-123" },
  ]) {
    assert.throws(
      () => validateLegalIdentityMode("public", "https://gradex.example", publicIdentity),
      /public legal identity rejects controlled-staging sentinel values/,
    );
  }
});
