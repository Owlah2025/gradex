import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

import { publicationMode } from "./revision-workflow";

const read = (relative: string) => readFileSync(join(process.cwd(), relative), "utf8");

const revision = { id: "rev-a", state: "APPROVED", title_ar: "أ", title_en: "A", sections: [] };

/**
 * D-097. Admin review gates a Course's FIRST publication only; every later
 * revision is published by its own Instructor. The studio must offer the act
 * the server will actually accept.
 */

test("a course that has never been live is submitted for review", () => {
  assert.equal(publicationMode({ id: "c1" }), "FIRST_PUBLICATION");
  assert.equal(
    publicationMode({ id: "c1", editable_revision: { ...revision, state: "DRAFT" } }),
    "FIRST_PUBLICATION",
  );
  assert.equal(publicationMode(null), "FIRST_PUBLICATION");
  assert.equal(publicationMode(undefined), "FIRST_PUBLICATION");
});

test("a course that has been live is published by its instructor", () => {
  assert.equal(publicationMode({ id: "c1", live_revision_id: "rev-a" }), "SUBSEQUENT_PUBLICATION");
  // The list payload carries only the identifier; the detail payload expands
  // the graph. Both must read as published.
  assert.equal(publicationMode({ id: "c1", live_revision: revision }), "SUBSEQUENT_PUBLICATION");
});

test("publication history is read from the pointer, never from the lifecycle", () => {
  // A course that was published and then delisted, archived, or suspended has
  // still been published. Reading the lifecycle here would offer "Submit for
  // review" on a course the server refuses to enqueue.
  for (const lifecycle of ["DELISTED", "ARCHIVED", "PUBLISHED"]) {
    assert.equal(
      publicationMode({ id: "c1", lifecycle, live_revision_id: "rev-a" }),
      "SUBSEQUENT_PUBLICATION",
      `lifecycle ${lifecycle} must not change the publication history`,
    );
  }
  const source = read("src/components/instructor/revision-workflow.ts");
  const rule = source.slice(source.indexOf("export function publicationMode"));
  assert.doesNotMatch(rule, /lifecycle/);
});

test("the studio calls the route matching the mode it displayed", () => {
  const builder = read("src/components/instructor/course-builder.tsx");
  assert.match(builder, /const publishesDirectly = publication === "SUBSEQUENT_PUBLICATION";/);
  assert.match(builder, /publishesDirectly \? publishCourseRevision : submitCourseRevision/);
  assert.match(builder, /publishesDirectly \? instructor\.submission\.published : instructor\.submission\.submitted/);
  const api = read("src/lib/api/authoring.ts");
  assert.match(api, /path\.revision\(input\.courseID, input\.revisionID\)\}\/publish/);
});

test("the panel swaps its words, not its checks", () => {
  const panel = read("src/components/instructor/submission-panel.tsx");
  assert.match(panel, /const publishing = mode === "SUBSEQUENT_PUBLICATION";/);
  assert.match(panel, /action: labels\.publishAction,/);
  assert.match(panel, /action: labels\.submitAction,/);
  // The first-publication note and the admin-owned-price note belong only to a
  // course that is actually going to an administrator.
  assert.match(panel, /\{publishing \? null : \([\s\S]{0,400}first-publication-note/);
  assert.match(panel, /\{publishing \? null : \([\s\S]{0,200}submission-price-note/);
  // Readiness is computed once, for both modes.
  assert.equal(panel.split("submissionReadiness(").length - 1, 1);
});

test("both publication vocabularies are localized", () => {
  const en = read("src/lib/i18n/dictionaries/en.ts");
  const ar = read("src/lib/i18n/dictionaries/ar.ts");
  assert.match(en, /publishAction: "Publish changes"/);
  assert.match(ar, /publishAction: "نشر التعديلات"/);
  assert.match(en, /submitAction: "Submit for review"/);
  assert.match(ar, /submitAction: "إرسال للمراجعة"/);
  assert.match(en, /published: "Changes published\."/);
  assert.match(ar, /published: "تم نشر التعديلات\."/);
  assert.match(en, /publishServerNote: "Your changes remain private until you publish them\."/);
  assert.match(ar, /publishServerNote: "تبقى تعديلاتك خاصة حتى تنشرها\."/);
  // Nothing on the publish path may imply an administrator is still involved.
  for (const key of ["publishTitle", "publishConfirmBody", "publishServerNote"]) {
    const value = en.slice(en.indexOf(`${key}:`), en.indexOf(`${key}:`) + 260);
    assert.doesNotMatch(value, /administrator|review/i, `${key} must not imply admin review`);
  }
});
