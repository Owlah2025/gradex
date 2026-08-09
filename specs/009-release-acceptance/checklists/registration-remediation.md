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

## LG-011 — production policy set

- [x] The Product Owner supplied authoritative Arabic/English Privacy and Terms bodies, exact versions, canonical routes, and identity configuration in `docs/legal/lg011-approved-policy-package.md`.
- [x] Terms §8 is the approved no-commerce payment/consumer-rights disclosure; the current MVP requires no separate Refund Policy. Privacy §4 and Terms §4 own course-access disclosure.
- [x] Production `ApprovedPolicySetResolver` composes the approved artifacts without development fixtures.
- [x] Registration persists the exact resolved policy/version/locale and later policy changes do not rewrite historical acceptance.
- [x] Missing/invalid public configuration and staging sentinels in public mode fail closed.
- [x] Controlled non-public staging accepts both exact sentinels only for the disposable HTTPS origin.
- [x] The full production-mode registration-to-learning journey passes with HIBP and the approved policy resolver composed together.

LG-011 software and the production-registration High are resolved. Actual legal registration number
and registered address remain external requirements before public T047. S11 remains open pending
independent closure review; this checklist is not approval or closure.
