# Contract: Compromised-Password Screening Boundary

**Status**: Provider-neutral shared contract; HIBP production adapter approved by D-075
**Rule**: BR-002

## Ownership

`identity.prepareCredential` is the sole production boundary where password plaintext may be read.
It performs length/common-password checks, derives the compromised-password lookup representation,
compares returned candidates locally, and produces the Argon2id hash. No caller, adapter, logger,
database row, outbox event, error, or test diagnostic receives the plaintext.

## Required interface behavior

Illustrative Go shape:

```go
type CompromisedRangeSource interface {
    Lookup(ctx context.Context, request RangeLookup) (RangeResult, error)
}

type RangeLookup struct {
    SchemeVersion string
    Prefix        string
}

type RangeResult struct {
    CandidateSuffixes []string
}
```

The production adapter uses SHA-1 and a five-character uppercase hexadecimal prefix under D-075;
scheme and prefix size remain adapter-owned rather than caller-selected. Requirements:

- the lookup representation is derived inside `prepareCredential`;
- the adapter receives only a bounded prefix, never plaintext or the complete derived digest;
- the response is bounded in bytes/count and parsed strictly;
- the complete comparison happens locally in constant time where practical;
- nil/unconfigured source is an error, not “not compromised”;
- timeout, malformed response, source error, or uncertain result fails credential creation closed;
- errors contain only safe class/correlation data;
- no lookup input/result enters routine logs, Audit, Identity evidence, metrics labels, or traces;
- latency/error metrics identify adapter and outcome class without credential-derived values.

Every credential-creation/change command, including deployment bootstrap, registration, invitation
acceptance, recovery, and password change, must pass a required source. S1B1 wires bootstrap and
registration; later slices inherit the same boundary.

## Test adapter

The deterministic local source:

- implements the same prefix/candidate contract;
- uses fixed non-secret test vectors;
- can return clear, compromised, unavailable, malformed, and oversized results;
- performs no network access;
- is explicitly labeled development/test-only.

Production configuration must not silently substitute this fixture. Adapter mode uses the fixed HIBP
Range API source with verified HTTPS, `Add-Padding: true`, an accurate Gradex user agent, a
three-second default total request bound, and no added retry. Positive counts are returned for local
comparison; zero-count padding records are discarded. Provider failure remains a fail-closed
dependency result.

## Verification

Tests prove:

1. valid non-compromised passwords pass and are stored only as Argon2id;
2. compromised candidates fail with `ErrPasswordPolicy`;
3. unavailable, unconfigured, malformed, and oversized sources fail closed;
4. Unicode/spaces and 15–128 code-point bounds remain intact;
5. adapter spies never observe plaintext or the full derived digest;
6. password, representation, prefix, candidates, and hash do not appear in logs, errors, evidence,
   ordinary outbox JSON, or API output;
7. the repository plaintext-exposure guard still recognizes one reviewed exposure function.

The production provider decision is [D-075](../../../docs/DECISIONS.md#d-075--hibp-pwned-passwords-range-api-is-the-production-compromised-password-source).
