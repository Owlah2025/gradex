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

const bundle = () => readSource("components/catalog/bundle-detail.tsx");
const handoff = () => readSource("lib/browser/external-handoff.ts");

test("WhatsApp is reached from the confirmation and from nowhere else", () => {
  const source = panel();
  // The old form navigated to WhatsApp on the first press of a button labelled
  // "I want to buy", before anything had been confirmed and before any request
  // existed. The handoff is still downstream of the confirm handler.
  const confirmBody = source.slice(source.indexOf("async function confirm()"));
  assert.match(
    confirmBody,
    /createStudentPurchaseRequest\(courseId, locale\)[\s\S]*handoff\.complete\(result\.whatsapp_url\)/,
  );
  // The URL is the server's, never assembled here.
  assert.ok(!source.includes("wa.me"), "the panel builds a WhatsApp URL itself");
});

test("neither purchase surface navigates the Gradex page away", () => {
  // This is the whole point of the change: pressing "buy" used to replace the
  // page the Student was reading. Nothing may navigate the current document.
  for (const [name, source] of [["course panel", panel()], ["bundle detail", bundle()]] as const) {
    assert.ok(
      !/window\.location\.assign\(/.test(source),
      `${name} still navigates the Gradex document away`,
    );
    assert.ok(!/window\.location\.href\s*=/.test(source), `${name} assigns location.href`);
    assert.ok(!/location\.replace\(/.test(source), `${name} replaces the Gradex document`);
  }
});

test("both purchase surfaces open the handoff context inside the click", () => {
  // Opening it after the await spends the user activation the popup needs, so
  // the open must appear before the request in both handlers.
  for (const [name, source, call] of [
    ["course panel", panel(), "createStudentPurchaseRequest("],
    ["bundle detail", bundle(), "createStudentBundlePurchaseRequest("],
  ] as const) {
    assert.match(source, /import \{ openHandoffContext \} from "@\/lib\/browser\/external-handoff"/);
    const openedAt = source.indexOf("openHandoffContext()");
    const requestedAt = source.indexOf(call);
    assert.ok(openedAt > 0, `${name} never opens a handoff context`);
    assert.ok(
      openedAt < requestedAt,
      `${name} opens the handoff context after the request, losing user activation`,
    );
  }
});

test("the handoff opens a separate context and severs its opener", () => {
  const source = handoff();
  assert.match(source, /window\.open\(""\s*,\s*"_blank"\)/);
  // `noopener` in the features string makes Chrome return null, so the opener
  // is severed on the handle instead. Both reach the same end state.
  assert.ok(!/window\.open\([^)]*noopener/.test(source), "the handle would be discarded by noopener");
  assert.match(source, /\.opener = null/);
  const complete = source.slice(source.indexOf("complete(url: string)"));
  assert.ok(
    complete.indexOf(".opener = null") < complete.indexOf("location.replace(url)"),
    "the opener is severed after navigation, leaving the external page a window reference",
  );
});

test("a failed request leaves no orphan blank context", () => {
  for (const [name, source] of [["course panel", panel()], ["bundle detail", bundle()]] as const) {
    const catchBody = source.slice(source.indexOf("} catch"));
    assert.ok(catchBody.includes("handoff.abandon()"), `${name} leaves a blank tab open on failure`);
  }
  assert.match(handoff(), /abandon\(\)[\s\S]*\.close\(\)/);
});

test("a blocked popup falls back to a safe explicit link", () => {
  for (const [name, source, testID] of [
    ["course panel", panel(), "purchase-handoff-fallback"],
    ["bundle detail", bundle(), "bundle-handoff-fallback"],
  ] as const) {
    assert.ok(source.includes(`data-testid="${testID}"`), `${name} has no fallback link`);
    const anchor = source.slice(source.indexOf(`data-testid="${testID}"`) - 400, source.indexOf(`data-testid="${testID}"`) + 80);
    assert.match(anchor, /target="_blank"/, `${name} fallback does not open a new context`);
    assert.match(anchor, /rel="noopener noreferrer"/, `${name} fallback is not a safe external link`);
  }
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
    "handoffBlocked",
    "handoffFallback",
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
