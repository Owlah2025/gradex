# S11 Production Registration Remediation Checklist

Date: 2026-08-09

## LG-021 — Compromised-password source

- [x] Product Owner approved HIBP Pwned Passwords Range API and its commercial use.
- [x] Production sends only the uppercase SHA-1 prefix of length five over verified HTTPS.
- [x] `Add-Padding: true`, Gradex user agent, local suffix comparison, and zero-count discard are proven.
- [x] One request is bounded by the approved three-second default with no added retry or redirect.
- [x] Timeout, TLS/network failure, non-success, malformed, and oversized responses fail closed.
- [x] Production and bootstrap composition select HIBP; deterministic sources remain development-only.
- [x] Valid, policy-invalid, compromised, and dependency-failure registration paths prove expected outcomes and zero facts on refusal.
- [x] Plaintext password, complete digest, suffix, identity, and provider response details are absent from requests, errors, and persistence evidence.
- [x] Deterministic tests, complete identity integration, race tests, live fixed-prefix compatibility, local Chromium, and disposable HTTPS regression pass.

## Remaining closure boundary

- [ ] LG-011 supplies published Arabic/English Privacy Notice, Terms, Refund Policy, and course-access disclosures with approved version identifiers and URLs.
- [ ] A production `PolicySetResolver` composes those approved artifacts without development fixtures.
- [ ] The full production-mode registration-to-learning journey passes after LG-011 closes.

S11 remains open with one High. This checklist records remediation evidence; it is not independent approval or closure.
