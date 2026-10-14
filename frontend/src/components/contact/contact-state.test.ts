import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import { ProblemError } from "../../lib/api/problem";
import { ar } from "../../lib/i18n/dictionaries/ar";
import { en } from "../../lib/i18n/dictionaries/en";
import {
  contactProblem,
  pendingHandoffURL,
  planPurchaseContact,
  readContactInput,
} from "./contact-state";

function readSource(relative: string): string {
  const root = process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend");
  return fs.readFileSync(path.join(root, "src", relative), "utf8");
}

function problem(code: string): ProblemError {
  return new ProblemError({
    type: "https://api.gradex.com/problems/validation-failed",
    title: "Request validation failed",
    status: 422,
    code: "VALIDATION_FAILED",
    errors: [{ code, detail: "", location: "body", parameter: "whatsapp_number" }],
  } as never);
}

test("typed input is read as empty, invalid, or a normalized number", () => {
  assert.deepEqual(readContactInput("", "KW"), { kind: "empty" });
  assert.deepEqual(readContactInput("   ", "KW"), { kind: "empty" });
  assert.deepEqual(readContactInput("2212 3456", "KW"), { kind: "invalid" });
  assert.deepEqual(readContactInput("5001 2345", "KW"), { kind: "valid", e164: "+96550012345" });
});

test("a Student with a number on file sends nothing extra", () => {
  assert.deepEqual(planPurchaseContact("+96550012345", false, { kind: "empty" }), { kind: "use-profile" });
});

test("a Student without a number must supply a valid one before anything is created", () => {
  assert.deepEqual(planPurchaseContact(null, false, { kind: "empty" }), { kind: "blocked", reason: "required" });
  assert.deepEqual(planPurchaseContact(null, true, { kind: "invalid" }), { kind: "blocked", reason: "invalid" });
  assert.deepEqual(planPurchaseContact(null, true, { kind: "valid", e164: "+201001234567" }), {
    kind: "send",
    e164: "+201001234567",
  });
  // A malformed stored value is treated as missing, never trusted.
  assert.deepEqual(planPurchaseContact("50012345", false, { kind: "empty" }), { kind: "blocked", reason: "required" });
});

test("choosing a different number replaces the profile number for this request", () => {
  assert.deepEqual(planPurchaseContact("+96550012345", true, { kind: "valid", e164: "+201001234567" }), {
    kind: "send",
    e164: "+201001234567",
  });
  assert.deepEqual(planPurchaseContact("+96550012345", true, { kind: "empty" }), { kind: "blocked", reason: "required" });
});

test("server refusals that name the number map to the number field", () => {
  assert.equal(contactProblem(problem("WHATSAPP_NUMBER_REQUIRED")), "required");
  assert.equal(contactProblem(problem("INVALID_WHATSAPP_NUMBER")), "invalid");
  assert.equal(contactProblem(problem("SOMETHING_ELSE")), null);
  assert.equal(contactProblem(new Error("network")), null);
});

test("only a waiting request with a server-built https wa.me link offers a handoff", () => {
  const url = "https://wa.me/201503260733?text=Request%3A%20GRX-1";
  assert.equal(pendingHandoffURL({ state: "WAITING_PAYMENT", whatsapp_url: url }), url);
  assert.equal(pendingHandoffURL({ state: "ACCESS_GRANTED", whatsapp_url: url }), null);
  assert.equal(pendingHandoffURL({ state: "CANCELLED", whatsapp_url: url }), null);
  assert.equal(pendingHandoffURL({ state: "WAITING_PAYMENT" }), null);
  assert.equal(pendingHandoffURL({ state: "WAITING_PAYMENT", whatsapp_url: "javascript:alert(1)" }), null);
  assert.equal(pendingHandoffURL({ state: "WAITING_PAYMENT", whatsapp_url: "http://wa.me/1" }), null);
  assert.equal(pendingHandoffURL({ state: "WAITING_PAYMENT", whatsapp_url: "https://evil.example/wa.me" }), null);
  assert.equal(pendingHandoffURL(null), null);
});

test("both languages carry every contact message, translated", () => {
  const keys = Object.keys(en.contact) as (keyof typeof en.contact)[];
  assert.ok(keys.length > 30);
  for (const key of keys) {
    assert.equal(typeof ar.contact[key], "string", `Arabic contact.${key}`);
    assert.ok(ar.contact[key].length > 0, `Arabic contact.${key} is empty`);
    // Digits only, identical by design in both languages.
    if (key !== "pendingPaymentCount" && key !== "numberPlaceholder") {
      assert.notEqual(ar.contact[key], en.contact[key], `Arabic contact.${key} is untranslated`);
    }
  }
  // The reminder copy agreed for this release.
  assert.equal(ar.contact.promptTitle, "أكمل بيانات التواصل الخاصة بك");
  assert.equal(ar.contact.promptAction, "إضافة رقم الواتساب");
  assert.equal(en.contact.promptTitle, "Complete Your Contact Information");
  assert.equal(en.contact.promptAction, "Add WhatsApp Number");
});

test("a provided number is never called verified", () => {
  for (const dictionary of [en.contact, ar.contact]) {
    for (const value of Object.values(dictionary)) {
      assert.ok(!/\bverified\b/i.test(value) || /not verify|does not verify|not verified/i.test(value), value);
    }
  }
  assert.ok(!/موثّق\b|تم التحقق/.test(ar.contact.statusProvided));
  assert.equal(en.contact.statusProvided, "Provided");
});

test("the reminder is derived from the profile, never a stored notification, and never blocks", () => {
  const prompt = readSource("components/contact/contact-number-prompt.tsx");
  assert.match(prompt, /getMyProfile\(locale\)/);
  assert.match(prompt, /isE164ContactNumber\(profile\.whatsapp_number\)/);
  // It hides itself the moment a number is saved anywhere on the page.
  assert.match(prompt, /addEventListener\(contactNumberSavedEvent/);
  // "Later" is per browser session, so it returns until a number exists.
  assert.match(prompt, /sessionStorage\.setItem\(contactPromptDismissalKey/);
  assert.ok(!/localStorage/.test(prompt), "the reminder is dismissed permanently");
  assert.ok(!/router\.(push|replace)|redirect\(/.test(prompt), "the reminder redirects the Student");
  assert.ok(!/Dialog|role="dialog"/.test(prompt), "the reminder is a modal");
});

test("the reminder appears where a signed-in Student lands", () => {
  assert.match(readSource("app/[locale]/learn/dashboard/page.tsx"), /<ContactNumberPrompt \/>/);
  assert.match(readSource("app/[locale]/access/page.tsx"), /<ContactNumberPrompt/);
  assert.match(readSource("components/learning/my-profile.tsx"), /<ContactNumberCard/);
});

test("D-118: the reminder email link survives sign-in and lands on the number field", () => {
  const profile = readSource("components/learning/my-profile.tsx");
  // A signed-out visitor is sent through sign-in with the exact path and query.
  assert.match(profile, /cause\.problem\.status === 401/);
  assert.match(profile, /withReturnTo\("\/login", window\.location\.pathname \+ window\.location\.search\)/);
  const card = readSource("components/contact/contact-number-card.tsx");
  assert.match(card, /get\("focus"\)/);
  assert.match(card, /focus !== "contact"/);
  assert.match(card, /getElementById\("profile-contact-number"\)/);
  assert.match(card, /scrollIntoView/);
  // Both languages name the new message in the Admin delivery log.
  assert.equal(typeof en.adminUserDetail.emailKinds["student-contact-reminder-v1"], "string");
  assert.notEqual(ar.adminUserDetail.emailKinds["student-contact-reminder-v1"], en.adminUserDetail.emailKinds["student-contact-reminder-v1"]);
});
