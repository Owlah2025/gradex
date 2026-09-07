import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import { en } from "../../lib/i18n/dictionaries/en";
import { ar } from "../../lib/i18n/dictionaries/ar";
import {
  passwordMaximum,
  passwordMinimum,
  validPassword,
} from "../../lib/identity/validation";

/**
 * D-100: the password floor is eight characters, everywhere.
 *
 * The failure this guards against is not "the number is wrong" — it is the
 * number being right in one layer and stale in another, which is invisible
 * until a reader is refused by a rule the screen in front of them never
 * mentioned. Registration accepting eight while password reset still demanded
 * fifteen would be the same defect wearing a different hat.
 *
 * So these assert agreement rather than value: the Go constant against the
 * TypeScript constant, the translated sentences against both, and every screen
 * that asks for a password against the shared helper instead of a literal of
 * its own.
 */

function repoRoot(): string {
  const frontend = process.cwd().endsWith("/frontend")
    ? process.cwd()
    : path.join(process.cwd(), "frontend");
  return path.dirname(frontend);
}

function readFrontend(relative: string): string {
  return fs.readFileSync(path.join(repoRoot(), "frontend", "src", relative), "utf8");
}

function backendPasswordPolicy(): { min: number; max: number } {
  const source = fs.readFileSync(
    path.join(repoRoot(), "backend", "internal", "identity", "password.go"),
    "utf8",
  );
  const min = /MinPasswordRunes\s*=\s*(\d+)/.exec(source);
  const max = /MaxPasswordRunes\s*=\s*(\d+)/.exec(source);
  assert.ok(min && max, "backend/internal/identity/password.go no longer declares the bounds");
  return { min: Number(min![1]), max: Number(max![1]) };
}

const PASSWORD_SURFACES = [
  "components/auth/registration-form.tsx",
  "components/auth/password-change-form.tsx",
  "components/auth/recovery-reset-form.tsx",
  "components/staff/staff-invitation-acceptance.tsx",
];

test("the shipped minimum is eight characters in both layers", () => {
  const backend = backendPasswordPolicy();
  assert.equal(passwordMinimum, 8, "the client minimum is not the D-100 value");
  assert.equal(
    backend.min,
    passwordMinimum,
    `the Go minimum is ${backend.min} while the client enforces ${passwordMinimum}; ` +
      "one layer would accept a password the other refuses",
  );
  assert.equal(backend.max, passwordMaximum, "the two layers disagree about the maximum");
});

test("the boundary is exactly where the policy says it is", () => {
  assert.equal(validPassword("a".repeat(passwordMinimum - 1)), false, "seven characters were accepted");
  assert.equal(validPassword("a".repeat(passwordMinimum)), true, "eight characters were refused");
  assert.equal(validPassword("a".repeat(passwordMaximum)), true, "the maximum length was refused");
  assert.equal(validPassword("a".repeat(passwordMaximum + 1)), false, "over the maximum was accepted");
  assert.equal(validPassword(""), false, "an empty password was accepted");
});

// The bound is in Unicode characters, not bytes or UTF-16 units. An eight
// character Arabic passphrase is sixteen bytes and must still clear the floor,
// and an eight character emoji string is sixteen UTF-16 units and must too.
test("the boundary counts characters, not bytes or UTF-16 units", () => {
  assert.equal(validPassword("كلمةسرجد"), true, "an eight-character Arabic passphrase was refused");
  assert.equal(validPassword("😀".repeat(passwordMinimum)), true, "eight code points were refused");
  assert.equal(
    validPassword("😀".repeat(passwordMinimum - 1)),
    false,
    "seven code points passed because they were counted as fourteen units",
  );
});

test("both languages state the minimum the product actually enforces", () => {
  for (const [language, dictionary] of [
    ["English", en],
    ["Arabic", ar],
  ] as const) {
    const stated = [
      dictionary.auth.common.passwordRule,
      dictionary.auth.register.invalidPassword,
      dictionary.auth.resetPassword.weak,
      dictionary.auth.passwordChange.weak,
      dictionary.auth.passwordChange.rejected,
    ];
    for (const sentence of stated) {
      assert.ok(
        sentence.includes(String(passwordMinimum)),
        `a ${language} password sentence does not state the enforced minimum: ${sentence}`,
      );
      assert.ok(
        !sentence.includes("15"),
        `a ${language} password sentence still states the superseded minimum: ${sentence}`,
      );
    }
  }
});

test("no password screen restates the minimum as a literal of its own", () => {
  for (const surface of PASSWORD_SURFACES) {
    const source = readFrontend(surface);
    assert.ok(
      /minLength=\{passwordMinimum\}/.test(source),
      `${surface} does not take its minimum from the shared constant`,
    );
    assert.ok(
      !/minLength=\{\s*\d+\s*\}/.test(source),
      `${surface} hard-codes a password length beside the shared constant`,
    );
  }
});

/**
 * D-100: the reveal control sits on the reader's trailing edge in both
 * languages without landing on top of the value.
 *
 * The field is pinned to `dir="ltr"` because a password is an opaque sequence,
 * so its own inline-end is always the right — while the button follows the page
 * and moves to the left in Arabic. The reserved space therefore has to be
 * chosen against the page direction. This asserts that it is, and that the fix
 * was not written as physical left/right offsets, which is the shape that would
 * quietly break the next time a surface changes direction.
 */
test("the shared password field reserves space on the side the reveal control is on", () => {
  const source = readFrontend("components/ui/password-input.tsx");

  assert.ok(
    /dir === "rtl" \? "ps-12" : "pe-12"/.test(source),
    "the reveal control's padding is not chosen from the page direction",
  );
  assert.ok(
    /inset-y-0 end-0/.test(source),
    "the reveal control is no longer pinned to the logical inline end",
  );
  assert.ok(
    !/\b(left-\d|right-\d|pl-\d|pr-\d)\b/.test(source),
    "the password field positions the reveal control with physical offsets",
  );
  // The state has to reach a reader who is not looking at the icon.
  assert.ok(source.includes("aria-pressed={revealed}"), "the reveal state is icon-only");
  assert.ok(source.includes("aria-label={label}"), "the reveal control has no accessible name");
  // Masked by default: the initial state is the one that hides the value.
  assert.ok(
    /useState\(false\)/.test(source),
    "the password field does not start masked",
  );
});

/**
 * D-100: an accepted reset request replaces the form.
 *
 * The old screen left the address and a live "Send reset link" on display under
 * a success banner, so a completed step and an outstanding one were on screen
 * together. These assert the two properties that matter and cannot be read off
 * a screenshot: that the accepted branch renders no field and no submit, and
 * that nothing outside the resolved success path can put the screen into it.
 */
test("an accepted reset request replaces the form rather than annotating it", () => {
  const source = readFrontend("components/auth/recovery-request-form.tsx");

  const accepted = source.slice(source.indexOf("if (acceptedEmail) {"));
  const acceptedBranch = accepted.slice(0, accepted.indexOf("\n  return (\n    <form"));
  assert.ok(acceptedBranch.length > 0, "the accepted branch is no longer a separate return");
  assert.ok(
    !/<Input\b/.test(acceptedBranch),
    "the accepted screen still renders the email field",
  );
  assert.ok(
    !/type="submit"/.test(acceptedBranch),
    "the accepted screen still renders the original submit control",
  );
  assert.ok(
    acceptedBranch.includes("recovery-resend"),
    "the accepted screen offers no way to ask again",
  );
  assert.ok(
    acceptedBranch.includes("backToSignIn"),
    "the accepted screen offers no way back to sign in",
  );
});

test("only a resolved server acceptance can show the accepted screen", () => {
  const source = readFrontend("components/auth/recovery-request-form.tsx");

  // Every assignment of the accepted address, and where it sits.
  const assignments = [...source.matchAll(/setAcceptedEmail\(/g)].map((match) => match.index ?? 0);
  assert.equal(assignments.length, 1, "the accepted state is set from more than one place");

  const submitBody = source.slice(
    source.indexOf("async function submit"),
    source.indexOf("async function resend"),
  );
  const awaitIndex = submitBody.indexOf("await requestPasswordReset");
  const setIndex = submitBody.indexOf("setAcceptedEmail(requested)");
  assert.ok(awaitIndex >= 0 && setIndex > awaitIndex, "the screen accepts before the server does");

  const catchBody = submitBody.slice(submitBody.indexOf("} catch (caught) {"));
  assert.ok(
    !catchBody.includes("setAcceptedEmail"),
    "a failed request can still show the accepted screen",
  );

  // Anti-enumeration: the accepted copy has to be true for an address with no
  // account behind it, so it may not claim an account or a delivery.
  for (const [language, dictionary] of [
    ["English", en],
    ["Arabic", ar],
  ] as const) {
    const copy = `${dictionary.auth.recover.acceptedTitle} ${dictionary.auth.recover.acceptedBody} ${dictionary.auth.recover.acceptedNext}`;
    assert.ok(
      !/\byour account\b|\bwe (?:have )?sent\b|حسابك/i.test(copy),
      `the ${language} accepted copy asserts something only an existing account makes true`,
    );
  }
});
