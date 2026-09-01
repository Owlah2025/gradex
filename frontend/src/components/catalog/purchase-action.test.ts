import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import { en } from "../../lib/i18n/dictionaries/en";
import { ar } from "../../lib/i18n/dictionaries/ar";

function readSource(relative: string): string {
  const root = process.cwd().endsWith("/frontend")
    ? process.cwd()
    : path.join(process.cwd(), "frontend");
  return fs.readFileSync(path.join(root, "src", relative), "utf8");
}

const panel = () => readSource("components/catalog/purchase-action.tsx");

test("WhatsApp is reached from the confirmation and from nowhere else", () => {
  const source = panel();
  // The old form navigated to WhatsApp on the first press of a button labelled
  // "I want to buy", before anything had been confirmed and before any request
  // existed. There is exactly one navigation now, and it is downstream of the
  // confirm handler.
  const navigations = source.match(/window\.location\.assign\(/g) ?? [];
  assert.equal(navigations.length, 1, "WhatsApp is navigated to from more than one place");
  const confirmBody = source.slice(source.indexOf("async function confirm()"));
  assert.match(confirmBody, /createStudentPurchaseRequest\(courseId, locale\)[\s\S]*window\.location\.assign\(result\.whatsapp_url\)/);
  // The URL is the server's, never assembled here.
  assert.ok(!source.includes("wa.me"), "the panel builds a WhatsApp URL itself");
});

test("an anonymous visitor is sent into the auth journey, not into a request", () => {
  const source = panel();
  assert.match(source, /if \(!authenticated\) \{/);
  assert.match(source, /data-testid="purchase-sign-in-required"/);
  assert.match(source, /withReturnTo\("\/login", destination\)/);
  assert.match(source, /withReturnTo\("\/register", destination\)/);
  // The anonymous branch returns before anything that could create a request.
  const anonymousBranch = source.slice(
    source.indexOf("if (!authenticated) {"),
    source.indexOf("if (!open) {"),
  );
  assert.ok(
    !anonymousBranch.includes("createStudentPurchaseRequest"),
    "an anonymous visitor can reach the create call",
  );
  assert.ok(
    !anonymousBranch.includes("window.location.assign"),
    "an anonymous visitor can reach WhatsApp",
  );
});

test("the purchase intent travels in the URL so it survives the whole journey", () => {
  const source = panel();
  assert.match(source, /export const purchaseIntentParameter = "purchase"/);
  assert.match(source, /params\.set\(purchaseIntentParameter, "1"\)/);
  // Coming back from sign-in lands on the confirmation rather than on a button
  // the Student has already pressed once.
  assert.match(source, /searchParams\.get\(purchaseIntentParameter\) === "1"/);
  // Cancelling takes the flag back out, so a reload does not reopen what was
  // cancelled.
  assert.match(source, /params\.delete\(purchaseIntentParameter\)/);
});

test("the confirmation states what is being requested before it requests it", () => {
  const source = panel();
  assert.match(source, /data-testid="purchase-confirmation"/);
  assert.match(source, /data-testid="purchase-course-title"/);
  assert.match(source, /data-testid="purchase-price"/);
  assert.match(source, /formatFils\(priceMinorUnits, locale\)/);
  // Both values come from the Course as the server describes it, and neither
  // is sent back on confirm — the body carries the Course id and nothing else.
  const confirmBody = source.slice(source.indexOf("async function confirm()"));
  assert.ok(!/price/.test(confirmBody.split("catch")[0]), "the confirm call carries a price");
  assert.ok(!/email/.test(confirmBody.split("catch")[0]), "the confirm call carries an email");
});

test("the panel collects no email address at all", () => {
  const source = panel();
  // The address decides where Course access is eventually sent. Accepting it
  // from the browser is what let any visitor aim someone else's access at a
  // mailbox they control.
  assert.ok(!/type="email"/.test(source), "the panel asks for an email address");
  assert.ok(!/validEmail/.test(source), "the panel validates an email address");
  const client = readSource("lib/api/access.ts");
  const call = client.slice(client.indexOf("export async function createStudentPurchaseRequest"));
  assert.match(call, /\{ course_id: courseId \}/);
  assert.ok(!call.slice(0, call.indexOf("}")).includes("email"), "the client sends an email");
});

test("a double submit cannot create two purchase requests", () => {
  const source = panel();
  assert.match(source, /const inFlight = React\.useRef\(false\)/);
  assert.match(source, /if \(inFlight\.current\) return/);
  assert.match(source, /disabled=\{submitting\}/);
});

test("both dictionaries carry every purchase message the panel can render", () => {
  for (const key of [
    "heading",
    "intro",
    "action",
    "signInRequiredTitle",
    "signInRequiredBody",
    "signIn",
    "createAccount",
    "courseLabel",
    "priceLabel",
    "submit",
    "submitting",
    "cancel",
    "failed",
    "alreadyActive",
    "notPurchasable",
  ] as const) {
    assert.equal(typeof en.access.purchase[key], "string", `English access.purchase.${key}`);
    assert.equal(typeof ar.access.purchase[key], "string", `Arabic access.purchase.${key}`);
    assert.notEqual(
      ar.access.purchase[key],
      en.access.purchase[key],
      `Arabic access.purchase.${key} is untranslated`,
    );
  }
});

test("only a state that still needs access offers the purchase panel", () => {
  const summary = readSource("components/catalog/course-access-summary.tsx");
  // An active entitlement must never be offered a purchase, and ANONYMOUS is
  // the one awaiting-access state with no session — it leads into the auth
  // journey rather than into a confirmation.
  assert.match(summary, /AWAITING_ACCESS as readonly string\[\]\)\.includes\(relationship\)/);
  assert.match(summary, /authenticated=\{relationship !== "ANONYMOUS"\}/);
  const awaiting = summary.slice(summary.indexOf("const AWAITING_ACCESS"), summary.indexOf("] as const"));
  assert.ok(!awaiting.includes('"ACTIVE"'), "an entitled Student is offered a purchase");
  assert.ok(!awaiting.includes('"AWAITING_APPROVAL"'), "a pending Student is offered a duplicate");
});

test("the publicly visible purchase copy carries no gateway vocabulary", () => {
  // Gradex has no checkout. The public catalogue suite asserts that no
  // gateway-shaped word reaches an anonymous reader, and the CTA is the one
  // string on that surface which is about buying — so it is the one most
  // likely to reintroduce the vocabulary. Locking it here means a copy change
  // fails in the unit suite rather than in a browser run.
  const publiclyVisible = [en.access.purchase.action, ar.access.purchase.action];
  for (const term of [
    "checkout",
    "cart",
    "coupon",
    "buy now",
    "payment",
    "الدفع",
    "السلة",
    "قسيمة",
    // The bare Arabic imperative "buy". "شراء" — a purchase *request* — is the
    // honest word for what this product does and is deliberately allowed.
    "اشتر",
  ]) {
    for (const copy of publiclyVisible) {
      assert.ok(
        !copy.toLowerCase().includes(term),
        `the purchase CTA reads as gateway commerce: ${copy} contains ${term}`,
      );
    }
  }
  // And it still says what it does.
  assert.match(en.access.purchase.action, /buy/i);
  assert.ok(ar.access.purchase.action.includes("شراء"));
});
