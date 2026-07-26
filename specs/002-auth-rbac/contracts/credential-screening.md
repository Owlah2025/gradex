# Contract: Compromised-Password Screening Boundary

**Status**: Provider-neutral S1B1 contract; production adapter blocked by LG-021
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

The exact approved scheme name and prefix size are adapter configuration, not caller-selected
values. Requirements:

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

Production configuration must not silently substitute this fixture. Until LG-021 supplies an
approved source/license/privacy/failure/monitoring contract and staging vectors, production
credential creation remains unavailable.

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

The contract does not select or endorse a live provider.
