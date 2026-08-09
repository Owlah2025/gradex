# HIBP Production Compromised-Password Adapter Design

Date: 2026-08-09  
Launch gate: LG-021  
Status: Approved by Product Owner for implementation

## Scope

Integrate Have I Been Pwned (HIBP) Pwned Passwords Range API as the production
compromised-password source behind the existing `CompromisedRangeSource` and
`AdmissionService` boundaries. Registration, recovery, staff credential
admission, and bootstrap continue to share the existing credential-screening
policy instead of defining path-specific rules.

LG-011 is deliberately separate. This change does not invent Privacy Policy or
Terms content, versions, or URLs, and production registration must remain
fail-closed until an approved production `PolicySetResolver` can be composed.

## Existing Architecture

`AdmissionService` owns credential admission. It resolves the current policy
set and calls the shared password admission logic before beginning the account
creation transaction. The password logic hashes the exact submitted UTF-8
password, sends only a fixed-length digest prefix through
`CompromisedRangeSource`, validates returned suffix candidates, and compares
the remaining digest locally. Provider errors become the existing safe
dependency-unavailable result.

Development and tests use `DeterministicCompromisedSource`. Production
configuration already forbids that source and requires adapter approval, but
no concrete production source was wired.

## Adapter Design

Add one HIBP range-source implementation in the identity package:

- declare SHA-1 (`sha1-v1`) and a five-hex-character prefix;
- accept only uppercase hexadecimal five-character prefixes from the domain;
- issue one server-side HTTPS `GET` to
  `https://api.pwnedpasswords.com/range/{prefix}`;
- send `Add-Padding: true` and an accurate Gradex user agent;
- send no body, query parameters, account data, plaintext password, full
  digest, or digest suffix;
- require a successful bounded response and parse `SUFFIX:COUNT` records
  locally;
- discard padding records whose count is zero and return only positive-count
  suffixes to the existing local comparison logic;
- reject malformed, oversized, non-success, or unreadable responses;
- emit safe errors that contain no request prefix, response suffix, password,
  or provider payload.

The production constructor uses the fixed approved HTTPS origin and ordinary
verified TLS. Tests may inject an HTTPS test server and its trusted client;
there is no TLS skip-verification option in production configuration.

The existing timeout wrapper bounds the complete lookup. Its production
default changes from two seconds to the approved three seconds. The synchronous
path performs one request and adds no retry.

## Composition

Introduce one shared composition helper for the compromised-password source:

- development may select the existing deterministic source;
- production adapter mode selects HIBP and wraps it with the configured bounded
  timeout;
- unavailable or unsafe modes return an error;
- production cannot select the deterministic source.

The API admission, recovery, staff, and bootstrap composition roots use this
helper or the same HIBP constructor. No parallel registration use case is
created. Production admission composition will continue to stop at the
unresolved LG-011 policy-set boundary after the HIBP source is successfully
available.

## Failure and Privacy Behavior

Timeout, DNS or connection failure, TLS verification failure, non-200 status,
invalid content, and response-limit violations all fail closed through the
existing admission-unavailable contract. Account creation does not begin when
screening fails. A positive suffix with count greater than zero rejects the
credential; zero-count padding never does.

The adapter cannot access plaintext because its interface receives only the
prefix. Neither prefixes nor suffixes are placed in logs or errors. Returned
unrelated suffixes remain request-local and are not persisted.

## Verification

Deterministic TLS-server tests will prove request shape, headers, parsing,
padding behavior, response bounds, timeout behavior, and error privacy.
Composition tests will prove that production adapter mode selects HIBP and
that deterministic sources remain development-only. Registration integration
tests will cover a valid password, policy rejection, a compromised password,
and provider failure with zero created accounts on every rejection/failure.

A separate opt-in provider compatibility check may call the live range API
without deriving a real credential. It is not part of the deterministic test
suite. S11 regression and the disposable HTTPS stack will be rerun to the
extent possible; a production-mode registration journey remains blocked by
LG-011 until approved bilingual policy content exists.

## Out of Scope

- policy/legal copy or policy publication;
- an offline HIBP corpus or synchronization system;
- retries, caching, per-keystroke queries, or client-side lookup;
- payments, commerce, deployment-provider work, or unrelated refactoring.
