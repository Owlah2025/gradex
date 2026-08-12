import assert from "node:assert/strict";
import { test } from "node:test";
import type { SectionWire } from "@/lib/api/catalog";
import { pricingScopeLabel, submittedSectionLabel } from "./pricing-sections";

const SECTION: SectionWire = {
  id: "3f8b1d40-2222-4c3d-9e1f-8a7b6c5d4e3f",
  title_ar: "المعادلات التفاضلية",
  title_en: "Differential Equations",
  position: 2,
};

test("Section pricing labels show both submitted titles without exposing identity", () => {
  for (const locale of ["ar", "en"] as const) {
    const label = submittedSectionLabel(SECTION, locale);
    assert.match(label, /المعادلات التفاضلية/);
    assert.match(label, /Differential Equations/);
    assert.ok(!label.includes(SECTION.id));
    assert.equal(pricingScopeLabel(SECTION.id, [SECTION], locale), label);
  }
});

test("an unavailable historical Section identity is not displayed", () => {
  assert.equal(pricingScopeLabel("removed-section-id", [], "en"), "Unavailable Section");
  assert.equal(pricingScopeLabel("removed-section-id", [], "ar"), "قسم غير متاح");
});
