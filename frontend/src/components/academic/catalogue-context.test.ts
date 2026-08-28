import assert from "node:assert/strict";
import { test } from "node:test";

import {
  catalogueHrefForContext,
  contextForSelection,
  selectionForContext,
  urlCarriesAcademicSelection,
} from "./catalogue-context";
import {
  academicContext,
  sameAcademicContext,
} from "../../lib/academic/anonymous-context";
import {
  applySelection,
  clearedSelection,
  readSelection,
  type CatalogueSelection,
} from "../catalog/academic-filter-state";

/**
 * The bridge between the remembered academic context and the catalogue URL.
 *
 * These assertions are about agreement: the two representations have to describe the same thing in
 * both directions, or the context bar ends up naming a university the catalogue is not filtered by.
 */

const KU = academicContext("kuwait-university", "computer-science", {
  institutionAr: "جامعة الكويت",
  institutionEn: "Kuwait University",
  programAr: "علوم الحاسوب",
  programEn: "Computer Science",
});

function selection(overrides: Partial<CatalogueSelection> = {}): CatalogueSelection {
  return { ...clearedSelection(), ...overrides };
}

test("a context becomes a catalogue selection naming exactly it", () => {
  assert.deepEqual(selectionForContext(KU), {
    institution: "kuwait-university",
    program: "computer-science",
    level: "",
    subject: "",
    query: "",
  });
});

test("a context produces a shareable, locale-addressable catalogue link", () => {
  assert.equal(
    catalogueHrefForContext("en", KU),
    "/en/catalog?institution=kuwait-university&program=computer-science",
  );
  assert.equal(
    catalogueHrefForContext("ar", KU),
    "/ar/catalog?institution=kuwait-university&program=computer-science",
  );
});

test("the link an Arabic reader gets and the link an English reader gets carry the same identity", () => {
  const en = new URL(catalogueHrefForContext("en", KU), "https://gradex.test");
  const ar = new URL(catalogueHrefForContext("ar", KU), "https://gradex.test");
  assert.equal(en.searchParams.get("institution"), ar.searchParams.get("institution"));
  assert.equal(en.searchParams.get("program"), ar.searchParams.get("program"));
  // Nothing in either URL is a translated label.
  for (const url of [en, ar]) {
    for (const value of url.searchParams.values()) {
      assert.match(value, /^[a-z0-9-]+$/, `${value} is not a slug`);
    }
  }
});

test("a round trip through the URL preserves the context exactly", () => {
  const href = catalogueHrefForContext("en", KU);
  const restored = contextForSelection(
    readSelection(new URL(href, "https://gradex.test").search),
    KU,
  );
  assert.equal(sameAcademicContext(restored, KU), true);
});

test("a selection with no university implies no context", () => {
  assert.equal(contextForSelection(clearedSelection(), KU), null);
  assert.equal(contextForSelection(selection({ query: "algebra" }), KU), null);
});

test("clearing the filters is what forgets the context", () => {
  // The catalogue's reset control produces exactly this selection, and it must not resolve back
  // into the context it just dropped.
  assert.equal(contextForSelection(clearedSelection(), KU), null);
});

test("level and subject are browsing refinements and never enter the stored identity", () => {
  const narrowed = selection({
    institution: "kuwait-university",
    program: "computer-science",
    level: "3",
    subject: "0418-320",
  });
  const stored = contextForSelection(narrowed, null);
  assert.equal(sameAcademicContext(stored, KU), true);
  assert.equal(Object.keys(stored!).sort().join(","), "institutionSlug,names,programSlug");
});

test("cached names follow their slug and are dropped the moment it changes", () => {
  const otherProgram = contextForSelection(
    selection({ institution: "kuwait-university", program: "physics" }),
    KU,
  );
  assert.equal(otherProgram?.names.institutionEn, "Kuwait University");
  assert.equal(otherProgram?.names.programEn, "", "a program name outlived its program");

  const otherInstitution = contextForSelection(
    selection({ institution: "gulf-university", program: "computer-science" }),
    KU,
  );
  assert.equal(otherInstitution?.names.institutionEn, "");
  assert.equal(otherInstitution?.names.programEn, "");
});

test("changing the university drops the program before it can be stored", () => {
  const changed = applySelection(selectionForContext(KU), {
    institution: "gulf-university",
  });
  const stored = contextForSelection(changed, KU);
  assert.equal(stored?.institutionSlug, "gulf-university");
  assert.equal(stored?.programSlug, "", "a program survived a change of university");
});

test("a URL naming anything academic is left alone; a bare one may be seeded", () => {
  assert.equal(urlCarriesAcademicSelection(""), false);
  assert.equal(urlCarriesAcademicSelection("?q=algebra"), false);
  assert.equal(urlCarriesAcademicSelection("?institution=kuwait-university"), true);
  assert.equal(
    urlCarriesAcademicSelection("?institution=kuwait-university&level=3"),
    true,
  );
  // A dependent value with no university is not a selection the catalogue can honour, and
  // readSelection already drops it.
  assert.equal(urlCarriesAcademicSelection("?program=computer-science"), false);
});
