import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import { en } from "../../lib/i18n/dictionaries/en";
import { ar } from "../../lib/i18n/dictionaries/ar";

/**
 * The public, authentication and account surfaces.
 *
 * Most of what matters here cannot be proved by rendering: that a password is
 * never written to storage, that a server's refusal code never reaches a
 * reader, that two languages come from the dictionary rather than from a
 * conditional buried in a component. Those are properties of the shipped
 * source, so that is what these assert.
 */

function frontendRoot(): string {
  return process.cwd().endsWith("/frontend")
    ? process.cwd()
    : path.join(process.cwd(), "frontend");
}

function readSource(relative: string): string {
  return fs.readFileSync(path.join(frontendRoot(), "src", relative), "utf8");
}

const LOGIN = "components/auth/login-form.tsx";
const REGISTER = "components/auth/registration-form.tsx";
const PASSWORD_CHANGE = "components/auth/password-change-form.tsx";
const RECOVERY_REQUEST = "components/auth/recovery-request-form.tsx";
const RECOVERY_RESET = "components/auth/recovery-reset-form.tsx";
const SHELL = "components/auth/auth-shell.tsx";
const PASSWORD_INPUT = "components/ui/password-input.tsx";
const SIGN_OUT = "components/layout/sign-out-button.tsx";
const STAFF_INVITE = "components/staff/staff-invitation-acceptance.tsx";
const ACCESS_PAGE = "app/[locale]/access/page.tsx";
const PROFILE_FORM = "components/learning/academic-profile-form.tsx";

const CREDENTIAL_SURFACES = [
  LOGIN,
  REGISTER,
  PASSWORD_CHANGE,
  RECOVERY_RESET,
  STAFF_INVITE,
];

const TOUCHED_SURFACES = [
  ...CREDENTIAL_SURFACES,
  RECOVERY_REQUEST,
  SHELL,
  SIGN_OUT,
  ACCESS_PAGE,
  PROFILE_FORM,
];

/** Strips attribute bindings, leaving what a reader would actually see. */
function jsxTextOf(source: string): string {
  return source.replace(/[A-Za-z-]+=\{[^{}]*(\{[^{}]*\}[^{}]*)*\}/g, "");
}

test("every credential field is labelled, named to the browser, and typed", () => {
  for (const surface of CREDENTIAL_SURFACES) {
    const source = readSource(surface);
    // A password field the password manager cannot classify is one the reader
    // has to type by hand every time.
    assert.ok(
      /autoComplete="(current-password|new-password)"/.test(source),
      `${surface} has a password field with no autocomplete role`,
    );
    // Field renders the <label for>, so using it is what makes the control named.
    assert.ok(
      source.includes("<Field"),
      `${surface} builds a form control outside the shared Field`,
    );
  }
  const login = readSource(LOGIN);
  assert.ok(
    login.includes('autoComplete="email"') && login.includes('dir="ltr"'),
    "the sign-in email field is not isolated as LTR content",
  );
});

test("a second submit dispatched in the same render pass cannot reach the network", () => {
  // `disabled={submitting}` only takes effect on the next render. Two submits
  // fired before it both read the old state and both authenticate.
  for (const surface of [LOGIN, REGISTER]) {
    const source = readSource(surface);
    assert.ok(
      /inFlight\s*=\s*React\.useRef\(false\)/.test(source),
      `${surface} guards duplicate submission with state alone`,
    );
    assert.ok(
      /if\s*\(inFlight\.current\)\s*return;/.test(source),
      `${surface} never checks its in-flight guard`,
    );
    assert.ok(
      /inFlight\.current\s*=\s*false;/.test(source),
      `${surface} never releases its in-flight guard`,
    );
  }
});

test("no credential surface writes a password anywhere it could persist", () => {
  for (const surface of CREDENTIAL_SURFACES) {
    const source = readSource(surface);
    for (const sink of [
      "localStorage",
      "sessionStorage",
      "document.cookie",
      "console.log",
      "console.error",
      "console.warn",
    ]) {
      assert.ok(
        !source.includes(sink),
        `${surface} reaches ${sink} on a screen that handles a password`,
      );
    }
  }
});

test("a refusal reaches the reader as a sentence, never as a server code", () => {
  // These are real codes the identity API answers with. Each is mapped to
  // dictionary copy; none may be rendered.
  const codes = [
    "INVALID_CREDENTIALS",
    "AUTHENTICATION_FAILED",
    "AUTHENTICATION_REQUIRED",
    "AUTHENTICATION_UNAVAILABLE",
    "RATE_LIMITED",
    "VALIDATION_FAILED",
    "NOT_AUTHORIZED",
    "TOKEN_INVALID",
    "CSRF_FAILED",
    "SESSION_REPLACED",
    "SESSION_REUSE_DETECTED",
    "PASSWORD_CHANGE_REQUIRED",
    "PROFILE_INCOMPLETE",
    "INVITATION_EXPIRED",
    "NOT_STARTED",
    "COMPLETED",
    "SKIPPED",
    "PENDING_STUDENT_ACCEPTANCE",
    "ENROLLED",
    "UNDECLARED",
    "NON_DEGREE",
    "FOUNDATION",
  ];
  for (const surface of TOUCHED_SURFACES) {
    const text = jsxTextOf(readSource(surface));
    for (const code of codes) {
      assert.ok(
        !text.includes(`{${code}}`) && !text.includes(`>${code}<`),
        `${surface} renders the raw code ${code}`,
      );
    }
  }
  // And no dictionary entry is a code wearing a label's clothes.
  const leaves: string[] = [];
  const walk = (node: unknown) => {
    if (typeof node === "string") leaves.push(node);
    else if (node && typeof node === "object")
      Object.values(node as Record<string, unknown>).forEach(walk);
  };
  walk(en.auth);
  walk(ar.auth);
  for (const value of leaves) {
    assert.ok(
      !/^[A-Z][A-Z0-9]*(_[A-Z0-9]+)+$/.test(value.trim()),
      `an auth dictionary entry is the raw enum ${value}`,
    );
  }
});

test("no touched auth or account surface renders a technical identifier", () => {
  for (const surface of TOUCHED_SURFACES) {
    const source = readSource(surface);
    const text = jsxTextOf(source);
    assert.ok(
      !/\{\s*[A-Za-z.]*(invitationId|invitation_id|accountId|account_id|sessionId|session_id|csrf_token|csrfToken)\s*\}/.test(
        text,
      ),
      `${surface} renders a technical identifier as visible text`,
    );
    assert.ok(
      !/\{\s*(bearer|token)(\.current)?\s*\}/.test(text),
      `${surface} renders a bearer token`,
    );
  }
});

test("registration collects only what the registration contract takes", () => {
  const source = readSource(REGISTER);
  const body = source.slice(
    source.indexOf("await registerStudent({"),
    source.indexOf("});", source.indexOf("await registerStudent({")),
  );
  // Exactly the contract: a name, an email, a password, the reader's language,
  // and which policy set they accepted. Nothing collected "because other
  // products collect it".
  for (const field of [
    "display_name",
    "email",
    "password",
    "locale",
    "policy_set_id",
  ]) {
    assert.ok(body.includes(field), `registration stopped sending ${field}`);
  }
  for (const invented of [
    "phone",
    "date_of_birth",
    "gender",
    "civil_id",
    "university",
    "program_id",
    "institution_id",
  ]) {
    assert.ok(
      !body.includes(invented),
      `registration invented a ${invented} field the contract does not take`,
    );
  }
});

test("registration never claims a browsing preference was saved to the account", () => {
  const source = readSource(REGISTER);
  // The public catalogue names academic entities by slug and the profile route
  // takes internal identifiers. There is no contract joining them, so signup
  // must not pretend to have bound one.
  for (const forbidden of [
    "saveAcademicProfile",
    "adoptProfile",
    "institutionSlug",
    "programSlug",
  ]) {
    assert.ok(
      !source.includes(forbidden),
      `registration touches ${forbidden}, binding a browsing preference to a real account`,
    );
  }
});

test("signing in or registering never discards the browsing preference", () => {
  // The academic context a visitor set before they had an account is browser
  // state, and authenticating is not a reason to forget it. This is what keeps
  // the later profile confirmation continuous rather than a fresh start.
  for (const surface of [LOGIN, REGISTER, SIGN_OUT]) {
    const source = readSource(surface);
    for (const forbidden of ["clearAcademicContext", "clearAnonymous", "setAnonymous"]) {
      assert.ok(
        !source.includes(forbidden),
        `${surface} clears the visitor's academic browsing preference`,
      );
    }
  }
});

test("a mid-form link to the terms does not cost the reader the form", () => {
  const source = readSource(REGISTER);
  const anchor = source.slice(
    source.indexOf("href={policy.url}") - 240,
    source.indexOf("href={policy.url}") + 160,
  );
  assert.ok(anchor.includes('target="_blank"'), "the policy link navigates in place");
  assert.ok(
    anchor.includes('rel="noopener noreferrer"'),
    "the policy link opens a new tab without severing the opener",
  );
  assert.ok(
    source.includes("opensInNewTab"),
    "the new tab is not announced to a screen reader",
  );
});

test("the password reveal is a named, keyboard-operable control with state", () => {
  const source = readSource(PASSWORD_INPUT);
  assert.ok(source.includes('type="button"'), "the reveal submits the form");
  assert.ok(source.includes("aria-pressed"), "the reveal carries no state for a screen reader");
  assert.ok(source.includes("aria-label"), "the reveal is unnamed");
  assert.ok(
    source.includes('dir="ltr"'),
    "revealing a password reorders it on an Arabic page",
  );
  assert.ok(
    source.includes("end-0") && !source.includes("right-0"),
    "the reveal is pinned to a physical side rather than the logical end",
  );
  assert.ok(
    source.includes("showPassword") && source.includes("hidePassword"),
    "the reveal names itself from something other than the dictionary",
  );
});

test("no touched auth or account surface writes its copy inline in two languages", () => {
  /**
   * The thing being forbidden is a *sentence* chosen by a locale check —
   * prose that exists in the component instead of the dictionary, where
   * nothing can check that both languages are present, that they are not the
   * same string twice, or that they use the vocabulary the product settled on.
   *
   * Choosing `name_ar` over `name_en` on a record the server sent is a
   * different act and stays allowed: that copy is the catalogue's, not ours.
   * So the match is on a conditional whose branches are string literals.
   */
  const inlineCopy =
    /(?:locale\s*===\s*"ar"|isAr)\s*\?\s*["'`][^"'`]*["'`]\s*:\s*["'`][^"'`]*["'`]/g;
  for (const surface of TOUCHED_SURFACES) {
    const found = readSource(surface).match(inlineCopy) ?? [];
    assert.deepEqual(
      found,
      [],
      `${surface} writes reader-facing copy inline instead of in the dictionary`,
    );
  }
});

test("the auth shell speaks to the audience in front of it", () => {
  const source = readSource(SHELL);
  for (const audience of ["student", "staff", "session"]) {
    assert.ok(
      en.auth.shell[audience as "student"] !== undefined,
      `the dictionary has no ${audience} panel`,
    );
  }
  assert.ok(
    source.includes("t.auth.shell[audience]"),
    "the shell no longer selects its panel by audience",
  );
  // A lit step marker is a colour difference on a decorative list.
  assert.ok(
    source.includes("currentStep"),
    "the current step is conveyed by colour alone",
  );
  const staffInvite = readSource(STAFF_INVITE);
  assert.ok(
    staffInvite.includes('audience="staff"'),
    "a staff invitee is still told they are creating a Student account",
  );
});

test("the staff invitation names which of the four refusals it received", () => {
  const source = readSource(STAFF_INVITE);
  // The preview route answers with a state. Each one has a different next
  // action for the reader, and collapsing them made them guess.
  for (const state of ["CONSUMED", "EXPIRED", "REVOKED", "SUPERSEDED"]) {
    assert.ok(source.includes(state), `the invitation screen cannot report ${state}`);
    assert.ok(
      en.auth.staffInvitation[
        `${state.toLowerCase()}Title` as "consumedTitle"
      ] !== undefined,
      `no copy exists for a ${state} invitation`,
    );
  }
  // A page opened without a link at all is not a refused invitation.
  assert.ok(
    source.includes('kind: "missing"') && source.includes("missingTitle"),
    "an invitation link that was never presented is described as an invalid one",
  );
});

test("a required password change offers no way around itself", () => {
  const form = readSource(PASSWORD_CHANGE);
  // The escape hatch exists only for the visitor who came here deliberately.
  // For a restricted principal, leaving is the thing the server refuses.
  assert.ok(
    /!required && escape/.test(form),
    "the password-change screen offers a way out regardless of whether the change is required",
  );
  assert.ok(
    /const required = session\?\.password_change_required === true/.test(form),
    "the screen decides whether a change is required from something other than the session",
  );
  // And the screen is not a destination a crafted link may ask for.
  const returnTo = readSource("lib/identity/return-to.ts");
  assert.ok(
    returnTo.includes("passwordChangePath") &&
      /blockedRoots = \[[^\]]*passwordChangePath/s.test(returnTo),
    "password-change is no longer blocked as a returnTo destination",
  );
});

test("the required notice is shown only when a change is actually required", () => {
  const form = readSource(PASSWORD_CHANGE);
  const page = readSource("app/(auth)/password-change/page.tsx");
  assert.ok(
    /\{required \?\s*\(\s*<Alert tone="info" title=\{t\.auth\.passwordChange\.requiredTitle\}/s.test(
      form,
    ),
    "every visitor is told their account cannot continue without a password change",
  );
  assert.ok(
    page.includes("voluntaryIntro") && page.includes("voluntaryTitle"),
    "the screen opens with the same words whether or not the change is required",
  );
});

test("signing out drops local state even when there is no token to spend", () => {
  const source = readSource(SIGN_OUT);
  // Returning early on a missing CSRF token made the control do nothing at
  // all — no clear, no navigation, no feedback.
  assert.ok(
    !/if \(!csrf\) return;/.test(source),
    "sign out abandons the local teardown when it cannot call the server",
  );
  assert.ok(
    /if \(csrf\) await deleteSession/.test(source),
    "sign out calls the server without a token",
  );
  const finallyBlock = source.slice(source.indexOf("} finally {"));
  assert.ok(
    finallyBlock.includes("clearSession()"),
    "the in-memory session is not cleared on every path",
  );
  assert.ok(finallyBlock.includes("router.push"), "sign out lands nowhere");
});

test("signing out drops the account's academic profile with the account", () => {
  const provider = readSource("components/academic/academic-context-provider.tsx");
  // The profile is held above the page. If it survived sign-out it would go on
  // outranking the browsing preference for a visitor who no longer has it —
  // on a shared machine, for the next person.
  const gate = provider.slice(
    provider.indexOf('if (sessionResolution !== "AUTHENTICATED")'),
    provider.indexOf('if (sessionResolution !== "AUTHENTICATED")') + 500,
  );
  assert.ok(gate.includes("setProfile(null)"), "a stale profile survives sign-out");
  assert.ok(
    gate.includes("setProfileRead(false)"),
    "the profile is dropped but still reported as read",
  );
});

test("the account surface offers only what the product implements", () => {
  const source = readSource("components/learning/account-summary.tsx");
  // The session view carries a display name and a role. There is no email on
  // it, and a labelled box with nothing behind it is worse than no box.
  assert.ok(source.includes("display_name"), "the account surface names nobody");
  assert.ok(
    source.includes("/password-change"),
    "changing a password still has no route through the product",
  );
  for (const invented of ["email", "phone", "notifications", "delete", "export"]) {
    assert.ok(
      !new RegExp(`t\\.account\\.${invented}`).test(source),
      `the account surface offers ${invented}, which has no route behind it`,
    );
  }
  const text = jsxTextOf(source);
  assert.ok(!/\{\s*session\.role\s*\}/.test(text), "the raw role enum is rendered");
});

test("every screen that asks for a password states the rule before enforcing it", () => {
  for (const surface of [REGISTER, PASSWORD_CHANGE, RECOVERY_RESET, STAFF_INVITE]) {
    const source = readSource(surface);
    assert.ok(
      /hint=\{(t\.auth\.common\.passwordRule|text\.invalidPassword)\}/.test(source),
      `${surface} refuses a password by a rule it never showed`,
    );
  }
  // One statement of the rule, in one place, in both languages.
  assert.equal(
    en.auth.common.passwordRule.includes("15"),
    true,
    "the shared password rule does not state the minimum length",
  );
  assert.notEqual(
    ar.auth.common.passwordRule,
    en.auth.common.passwordRule,
    "the Arabic password rule is the English one",
  );
});

test("a password field can be read back on every screen that asks for one", () => {
  for (const surface of [LOGIN, REGISTER, PASSWORD_CHANGE, RECOVERY_RESET, STAFF_INVITE]) {
    const source = readSource(surface);
    assert.ok(
      source.includes("PasswordInput"),
      `${surface} asks for a 15-character password blind`,
    );
    assert.ok(
      !/type="password"/.test(source),
      `${surface} still hand-rolls a password field beside the shared one`,
    );
  }
});
