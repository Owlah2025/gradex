import assert from "node:assert/strict";
import { test } from "node:test";

import {
  ACADEMIC_CONTEXT_STORAGE_KEY,
  ACADEMIC_CONTEXT_VERSION,
  academicContext,
  academicContextNames,
  clearAcademicContext,
  parseAcademicContext,
  readAcademicContext,
  resolveAcademicSource,
  sameAcademicContext,
  serializeAcademicContext,
  validateAcademicContext,
  writeAcademicContext,
  type ContextStorage,
} from "./anonymous-context";

/**
 * The anonymous academic context.
 *
 * Everything the browser hands back is treated as hostile, so the parser is tested against the
 * shapes another tab, an older release, or a hand-edited value can actually produce — not only
 * against what this code writes.
 */

function fakeStorage(initial: Record<string, string> = {}) {
  const entries = new Map(Object.entries(initial));
  const storage: ContextStorage & { entries: Map<string, string> } = {
    entries,
    getItem: (key) => entries.get(key) ?? null,
    setItem: (key, value) => void entries.set(key, value),
    removeItem: (key) => void entries.delete(key),
  };
  return storage;
}

/** A storage that refuses everything, as a private-mode browser does. */
function hostileStorage(): ContextStorage {
  return {
    getItem() {
      throw new Error("SecurityError");
    },
    setItem() {
      throw new Error("QuotaExceededError");
    },
    removeItem() {
      throw new Error("SecurityError");
    },
  };
}

const KU = academicContext("kuwait-university", "computer-science", {
  institutionAr: "جامعة الكويت",
  institutionEn: "Kuwait University",
  programAr: "علوم الحاسوب",
  programEn: "Computer Science",
});

test("a written context survives a round trip through storage", () => {
  const storage = fakeStorage();
  writeAcademicContext(storage, KU);
  assert.deepEqual(readAcademicContext(storage), KU);
  assert.ok(storage.entries.has(ACADEMIC_CONTEXT_STORAGE_KEY));
});

test("the stored record carries its version", () => {
  const record = JSON.parse(serializeAcademicContext(KU));
  assert.equal(record.version, ACADEMIC_CONTEXT_VERSION);
  assert.equal(record.institutionSlug, "kuwait-university");
  assert.equal(record.programSlug, "computer-science");
});

test("a record from another version is discarded rather than migrated", () => {
  const stale = JSON.stringify({
    version: ACADEMIC_CONTEXT_VERSION + 1,
    institutionSlug: "kuwait-university",
    programSlug: "computer-science",
    names: {},
  });
  assert.equal(parseAcademicContext(stale), null);
});

test("corrupt and impossible stored values parse to null instead of throwing", () => {
  for (const raw of [
    null,
    undefined,
    "",
    "not json at all",
    "{",
    "[]",
    '"a string"',
    "42",
    "null",
    JSON.stringify({ version: ACADEMIC_CONTEXT_VERSION }),
    // A program with no institution cannot be validated against anything, so it is dropped whole.
    JSON.stringify({
      version: ACADEMIC_CONTEXT_VERSION,
      institutionSlug: "",
      programSlug: "computer-science",
      names: {},
    }),
  ]) {
    assert.equal(parseAcademicContext(raw as string | null), null, String(raw));
  }
});

test("a record with a garbled name cache keeps its identity and loses only the names", () => {
  const raw = JSON.stringify({
    version: ACADEMIC_CONTEXT_VERSION,
    institutionSlug: "kuwait-university",
    programSlug: "computer-science",
    names: "not an object",
  });
  const parsed = parseAcademicContext(raw);
  assert.equal(parsed?.institutionSlug, "kuwait-university");
  assert.equal(parsed?.programSlug, "computer-science");
  assert.deepEqual(parsed?.names, {
    institutionAr: "",
    institutionEn: "",
    programAr: "",
    programEn: "",
  });
});

test("a storage that refuses every operation degrades to no context, never to a throw", () => {
  const storage = hostileStorage();
  assert.equal(readAcademicContext(storage), null);
  assert.doesNotThrow(() => writeAcademicContext(storage, KU));
  assert.doesNotThrow(() => clearAcademicContext(storage));
});

test("no storage at all — the server render — is an ordinary state", () => {
  assert.equal(readAcademicContext(null), null);
  assert.doesNotThrow(() => writeAcademicContext(null, KU));
});

test("clearing actually removes the record", () => {
  const storage = fakeStorage();
  writeAcademicContext(storage, KU);
  clearAcademicContext(storage);
  assert.equal(storage.entries.has(ACADEMIC_CONTEXT_STORAGE_KEY), false);
  assert.equal(readAcademicContext(storage), null);
});

test("writing a context with no institution clears rather than storing an empty one", () => {
  const storage = fakeStorage();
  writeAcademicContext(storage, KU);
  writeAcademicContext(storage, academicContext("", "computer-science"));
  assert.equal(readAcademicContext(storage), null);
});

test("a program is never stored without its institution", () => {
  assert.equal(academicContext("", "computer-science").programSlug, "");
});

test("identity is the slug pair — the display cache is not part of it", () => {
  const sameIdentityOtherNames = academicContext(
    "kuwait-university",
    "computer-science",
    { institutionEn: "Something else entirely" },
  );
  assert.equal(sameAcademicContext(KU, sameIdentityOtherNames), true);
  assert.equal(
    sameAcademicContext(KU, academicContext("kuwait-university", "physics")),
    false,
  );
  assert.equal(sameAcademicContext(KU, null), false);
  assert.equal(sameAcademicContext(null, null), true);
});

test("one stored record renders in both languages, so a locale switch cannot change identity", () => {
  const storage = fakeStorage();
  writeAcademicContext(storage, KU);
  const restored = readAcademicContext(storage)!;
  assert.deepEqual(academicContextNames(restored, "en"), {
    institution: "Kuwait University",
    program: "Computer Science",
  });
  assert.deepEqual(academicContextNames(restored, "ar"), {
    institution: "جامعة الكويت",
    program: "علوم الحاسوب",
  });
  // The bytes on disk never changed, and neither did the identity.
  assert.equal(restored.institutionSlug, KU.institutionSlug);
  assert.equal(restored.programSlug, KU.programSlug);
});

test("a retired university invalidates the whole context", () => {
  assert.equal(validateAcademicContext(KU, ["other-university"], null), null);
});

test("a vanished program clears only itself and leaves the university selected", () => {
  const validated = validateAcademicContext(KU, ["kuwait-university"], ["physics"]);
  assert.equal(validated?.institutionSlug, "kuwait-university");
  assert.equal(validated?.programSlug, "");
  // The program's cached names go with it; the university's stay.
  assert.equal(validated?.names.programEn, "");
  assert.equal(validated?.names.institutionEn, "Kuwait University");
});

test("an option list that has not loaded is not evidence that anything was retired", () => {
  assert.deepEqual(validateAcademicContext(KU, null, null), KU);
  assert.deepEqual(validateAcademicContext(KU, ["kuwait-university"], null), KU);
});

test("validating nothing yields nothing", () => {
  assert.equal(validateAcademicContext(null, ["kuwait-university"], []), null);
});

test("a completed account profile outranks anything a browser is holding", () => {
  assert.equal(
    resolveAcademicSource({ anonymous: KU, profileSetupState: "COMPLETED" }),
    "profile",
  );
});

test("a profile that asserts nothing leaves the browsing preference standing", () => {
  for (const state of ["NOT_STARTED", "SKIPPED", null] as const) {
    assert.equal(
      resolveAcademicSource({ anonymous: KU, profileSetupState: state }),
      "anonymous",
      String(state),
    );
  }
});

test("no context and no profile is its own state, not a silent default", () => {
  assert.equal(
    resolveAcademicSource({ anonymous: null, profileSetupState: null }),
    "none",
  );
  assert.equal(
    resolveAcademicSource({ anonymous: null, profileSetupState: "SKIPPED" }),
    "none",
  );
});
