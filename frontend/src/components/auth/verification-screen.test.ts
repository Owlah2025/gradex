import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import { en } from "../../lib/i18n/dictionaries/en";
import { ar } from "../../lib/i18n/dictionaries/ar";
import { safeReturnTo } from "../../lib/identity/return-to";

function readSource(relative: string): string {
  const root = process.cwd().endsWith("/frontend")
    ? process.cwd()
    : path.join(process.cwd(), "frontend");
  return fs.readFileSync(path.join(root, "src", relative), "utf8");
}

const form = () => readSource("components/auth/verification-code-form.tsx");

/**
 * Source with its comments removed.
 *
 * The "the code never reaches a store or a log" assertions are about what the
 * code *does*. Prose explaining why it deliberately reads storage after mount
 * rather than during render is not a violation of that, and matching against
 * raw source made the two indistinguishable.
 */
function executableSource(relative: string): string {
  return readSource(relative)
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/(^|[^:])\/\/.*$/gm, "$1");
}

test("the verification screen never asks for the email address again", () => {
  const source = form();
  // Being asked for an address one screen after typing it is the defect the
  // whole challenge-carrying mechanism exists to remove.
  assert.ok(
    !/type="email"/.test(source),
    "the code screen carries an email field",
  );
  assert.ok(
    !/autoComplete="email"/.test(source),
    "the code screen asks the browser to autofill an address",
  );
  // What it shows instead is the masked address the server derived.
  assert.match(source, /data-testid="verification-masked-email"/);
  assert.match(source, /challenge\.masked_email/);
});

test("the code field is one accessible input, not six unlabelled boxes", () => {
  const source = form();
  // Six independent inputs need a great deal of care to stay usable with a
  // screen reader, a password manager, and platform code autofill. One input
  // with an accessible name gets all three.
  assert.match(source, /inputMode="numeric"/);
  assert.match(source, /autoComplete="one-time-code"/);
  assert.match(source, /maxLength=\{6\}/);
  assert.match(source, /<Field label=\{t\.auth\.code\.label\}/);
  assert.match(source, /aria-invalid=\{Boolean\(error\)\}/);
  // Pasting "482 913" or "code: 482913" out of a mail client is the common
  // case, so the digits are extracted rather than the paste refused.
  assert.match(source, /onPaste=/);
  assert.match(source, /clipboardData\.getData\("text"\)\.replace\(digitsOnly, ""\)/);
  // Enter submits, because the control is a real form.
  assert.match(source, /<form[^>]*onSubmit=\{submit\}/);
  assert.match(source, /type="submit"/);
});

test("the countdown is driven by the server's own timestamp", () => {
  const source = form();
  // Counting down from a local guess drifts away from the refusal the server
  // will actually apply, and shows an enabled control that then fails.
  assert.match(source, /secondsUntil\(challenge\.resend_available_at, Date\.now\(\)\)/);
  assert.match(source, /window\.setInterval\(tick, 1000\)/);
  assert.match(source, /window\.clearInterval\(timer\)/);
  assert.match(source, /data-testid="verification-resend-countdown"/);
  assert.match(source, /aria-live="polite"/);
  // The resend control exists only once the cooldown has elapsed.
  assert.match(source, /cooldown > 0 \?/);
  assert.match(source, /data-testid="verification-resend"/);
});

test("every code outcome the Student can act on has its own message", () => {
  const source = form();
  for (const code of [
    "VERIFICATION_CODE_INVALID",
    "VERIFICATION_CODE_EXHAUSTED",
    "VERIFICATION_CODE_RESEND_TOO_SOON",
    "RATE_LIMITED",
  ]) {
    assert.ok(source.includes(code), `no message for ${code}`);
  }
  // Wrong, unknown, expired, and superseded arrive as one problem code on
  // purpose. Inventing four messages for one response would claim a
  // distinction the server deliberately refuses to make.
  assert.match(source, /default:\s*\n\s*return t\.auth\.code\.unavailable/);
  for (const dictionary of [en, ar]) {
    for (const key of ["invalid", "exhausted", "resendTooSoon", "unavailable", "malformed"] as const) {
      assert.equal(typeof dictionary.auth.code[key], "string", `auth.code.${key}`);
    }
  }
  assert.notEqual(ar.auth.code.invalid, en.auth.code.invalid, "Arabic code copy is untranslated");
});

test("a proven code signs the Student in without asking for the password again", () => {
  const source = form();
  // The code already proved control of the mailbox. A password prompt here
  // establishes nothing further and is the step the journey exists to remove.
  assert.ok(!/PasswordInput/.test(source), "the code screen asks for a password");
  assert.ok(!/type="password"/.test(source), "the code screen asks for a password");
  assert.match(source, /const session = await verifyEmailCode\(/);
  assert.match(source, /setSession\(session\)/);
  assert.match(source, /postAuthenticationDestination\(/);
});

test("the spent challenge is dropped and the address bar follows a resend", () => {
  const source = form();
  assert.match(source, /forgetChallenge\(activeChallengeId\)/);
  // A reload after a resend must not ask about a challenge that is already
  // superseded, so the identifier in the URL is replaced with it.
  assert.match(source, /next\.set\(challengeParameter, replacement\.challenge_id\)/);
  // A resend carries no masked address; the screen keeps the one it has rather
  // than blanking the line the Student is reading.
  assert.match(source, /accepted\.verification\.masked_email \|\| \(challenge\?\.masked_email \?\? ""\)/);
});

test("a double submit cannot spend two of five attempts", () => {
  const source = form();
  // `submitting` is state and does not close the window between two submits
  // dispatched in one render pass. Every attempt costs one of five.
  assert.match(source, /const inFlight = React\.useRef\(false\)/);
  assert.match(source, /if \(inFlight\.current \|\| !activeChallengeId\) return/);
});

test("the requested destination survives the whole admission journey", () => {
  // register → verify → back where the visitor was going, revalidated at each
  // hop rather than trusted because an earlier screen saw it.
  const registration = readSource("components/auth/registration-form.tsx");
  assert.match(registration, /withReturnTo\("\/verify-email", searchParams\.get\("returnTo"\)\)/);
  assert.match(registration, /rememberChallenge\(accepted\.verification\)/);
  assert.match(registration, /challengeParameter/);

  const source = form();
  assert.match(source, /const returnTo = searchParams\.get\("returnTo"\)/);
  assert.match(source, /postAuthenticationDestination\(\s*\n?\s*session\.role,\s*\n?\s*returnTo,/);

  // And the destination itself is still validated by the shared rule.
  assert.equal(safeReturnTo("/ar/catalog/x?purchase=1"), "/ar/catalog/x?purchase=1");
  assert.equal(safeReturnTo("https://evil.example"), null);
  assert.equal(safeReturnTo("/verify-email"), null);
});

test("the code never reaches a URL, a store, or a log", () => {
  const source = executableSource("components/auth/verification-code-form.tsx");
  assert.ok(!/localStorage/.test(source), "the code screen writes localStorage");
  assert.ok(!/sessionStorage/.test(source), "the code screen writes sessionStorage directly");
  assert.ok(!/console\./.test(source), "the code screen logs");
  // The code is component state and goes into the request body. It must never
  // be put into the query string, where it would survive in history, referrers
  // and proxy logs.
  assert.ok(
    !/set\(\s*["'`]code/.test(source),
    "the code is written into the address bar",
  );
});
