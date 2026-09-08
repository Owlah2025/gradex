import assert from "node:assert/strict";
import test from "node:test";
import type { CourseRevisionWire, SectionWire } from "@/lib/api/catalog";
import type { CourseWire } from "@/lib/api/authoring";
import {
  moveIdentity,
  orderLessons,
  orderSections,
  persistOptimisticOrder,
  replaceEditableRevision,
} from "./curriculum-order";

const sections: SectionWire[] = [
  { id: "s1", title_ar: "أ", title_en: "A", position: 3, lessons: [
    { id: "l1", title_ar: "١", title_en: "L1", position: 2 },
    { id: "l2", title_ar: "٢", title_en: "L2", position: 8 },
    { id: "l3", title_ar: "٣", title_en: "L3", position: 11 },
  ] },
  { id: "s2", title_ar: "ب", title_en: "B", position: 7, lessons: [
    { id: "other", title_ar: "غ", title_en: "Other", position: 0 },
  ] },
  { id: "s3", title_ar: "ج", title_en: "C", position: 12, lessons: [] },
];

test("section drag result produces a canonical identity order", () => {
  const ids = moveIdentity(sections.map((section) => section.id), "s3", "s1");
  assert.deepEqual(ids, ["s3", "s1", "s2"]);
  assert.deepEqual(orderSections(sections, ids).map(({ id, position }) => [id, position]), [
    ["s3", 0], ["s1", 1], ["s2", 2],
  ]);
});

test("lesson drag result stays inside the named section", () => {
  const ids = moveIdentity(sections[0].lessons!.map((lesson) => lesson.id), "l3", "l2");
  const reordered = orderLessons(sections[0], ids);
  assert.deepEqual(reordered.lessons?.map(({ id, position }) => [id, position]), [
    ["l1", 0], ["l3", 1], ["l2", 2],
  ]);
  assert.equal(sections[1].lessons?.[0].id, "other");
});

test("unknown or same drop targets do not invent an order", () => {
  const ids = ["s1", "s2", "s3"];
  assert.equal(moveIdentity(ids, "s1", "s1"), ids);
  assert.equal(moveIdentity(ids, "s1", "foreign"), ids);
});

test("canonical curriculum replacement preserves unrelated course and revision fields", () => {
  const revision: CourseRevisionWire = {
    id: "r1", title_ar: "server ar", title_en: "server en", description_en: "server description", sections,
  };
  const courses: CourseWire[] = [{ id: "c1", lifecycle: "DRAFT", editable_revision: revision }];
  const canonical: CourseRevisionWire = { ...revision, sections: orderSections(sections, ["s3", "s2", "s1"]) };
  const localDirtyDetails = { titleEn: "unsaved local title", descriptionEn: "unsaved local description" };
  const result = replaceEditableRevision(courses, "c1", canonical);
  assert.equal(result[0].lifecycle, "DRAFT");
  assert.deepEqual(result[0].editable_revision?.sections.map((section) => section.id), ["s3", "s2", "s1"]);
  assert.deepEqual(localDirtyDetails, { titleEn: "unsaved local title", descriptionEn: "unsaved local description" });
});

test("successful persistence replaces optimistic order with canonical server order", async () => {
  const applied: string[][] = [];
  await persistOptimisticOrder({
    before: ["s1", "s2", "s3"],
    optimistic: ["s3", "s1", "s2"],
    apply: (order) => applied.push(order),
    persist: async () => ["s3", "s1", "s2"],
  });
  assert.deepEqual(applied, [["s3", "s1", "s2"], ["s3", "s1", "s2"]]);
});

test("rejected persistence restores the previous canonical order", async () => {
  const applied: string[][] = [];
  await assert.rejects(
    persistOptimisticOrder({
      before: ["s1", "s2", "s3"],
      optimistic: ["s3", "s1", "s2"],
      apply: (order) => applied.push(order),
      persist: async () => { throw new Error("rejected"); },
    }),
    /rejected/,
  );
  assert.deepEqual(applied, [["s3", "s1", "s2"], ["s1", "s2", "s3"]]);
});
