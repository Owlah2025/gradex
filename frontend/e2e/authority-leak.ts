import { expect } from "@playwright/test";

/**
 * Shared authority-leak audit vocabulary for the S5 protected-learning suites.
 *
 * The S5 specs audit rendered output for authority internals: read-model field names, capability
 * flags, storage details, and signed-target parameters. Every entry in those lists is a *field or
 * flag identifier*, not an English word, and each is matched as a plain substring.
 *
 * `authorized` was the one entry that cannot be audited that way. As a bare substring it matches:
 *
 *   1. product prose — `siteConfig.description` says "authorized learning access", so the string is
 *      present in the `<meta name="description">` of every page and inside the RSC flight payload
 *      that serialises it; and
 *   2. `unauthorized` — Next.js App Router emits its own `"unauthorized":"$undefined"` router
 *      boundary key into the flight payload of every route segment.
 *
 * Both are unconditional, so the bare substring reported a leak on every page while proving
 * nothing about authority. `AUTHORIZATION_FLAG` audits the same concept at the shape the read-model
 * contract actually forbids — `authorized` used as a serialised field/flag key — and still fails on
 * a genuine leak in either raw or flight-escaped form:
 *
 *   `"authorized":true`   `\"authorized\":true`   `authorized: true`   `is_authorized":false`
 *
 * The negative lookbehind excludes only Next's `unauthorized` key. Nothing else in the audit
 * vocabulary is relaxed.
 */
export const AUTHORIZATION_FLAG = /(?<!un)authorized\\*["']?\s*:/i;

/**
 * Asserts one audit token is absent, accepting either a plain substring or a shaped pattern.
 *
 * Case handling differs by call site: some audits lowercase the haystack before checking, so the
 * patterns here are case-insensitive rather than relying on the caller.
 */
export function expectAbsent(
  haystack: string,
  token: string | RegExp,
  message?: string,
): void {
  if (typeof token === "string") {
    expect(haystack, message).not.toContain(token);
    return;
  }
  expect(haystack, message).not.toMatch(token);
}

/** Human-readable label for a token, for assertion messages. */
export function tokenLabel(token: string | RegExp): string {
  return typeof token === "string" ? token : token.source;
}
