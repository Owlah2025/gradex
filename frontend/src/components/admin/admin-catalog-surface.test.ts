import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

/**
 * Admin Catalog production-surface guards.
 *
 * Founder manual acceptance found `/en/admin/catalog` rendering a Course that
 * does not exist ("Introduction to Programming" / `demo-course-1`) out of
 * component state, and taxonomy administration gated on the pricing modal
 * being open. Both defects are structural, so both are asserted structurally:
 * the properties hold for the shipped source, not merely for one rendered
 * path a test happened to exercise.
 */

function frontendRoot(): string {
  return process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend");
}

function readSource(relativePath: string): string {
  const full = path.join(frontendRoot(), relativePath);
  assert.ok(fs.existsSync(full), `${relativePath} is missing; this detector would pass vacuously`);
  return fs.readFileSync(full, "utf8");
}

/** Every production source file under one authoritative root. */
function productionFiles(relativeRoot: string): string[] {
  const root = path.join(frontendRoot(), relativeRoot);
  assert.ok(
    fs.existsSync(root) && fs.statSync(root).isDirectory(),
    `scan root ${relativeRoot} is missing; this detector would pass vacuously`,
  );
  const collected: string[] = [];
  const walk = (directory: string) => {
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      const full = path.join(directory, entry.name);
      if (entry.isDirectory()) {
        walk(full);
        continue;
      }
      if (/\.(ts|tsx)$/.test(entry.name) && !/\.test\.(ts|tsx)$/.test(entry.name)) {
        collected.push(full);
      }
    }
  };
  walk(root);
  return collected.sort();
}

const normalize = (source: string): string => source.toLowerCase().replace(/[^a-z0-9]+/g, "");

/**
 * The removed fixture, plus the spellings a reintroduction would most
 * plausibly use. Deliberately specific: the Admin surface legitimately uses
 * `placeholder` on its inputs, and forbidding that word would be noise.
 */
const fixtureConcepts = [
  "democourse",
  "coursedemo",
  "demodata",
  "demoqueue",
  "demoreview",
  "mockcourse",
  "fakecourse",
  "sampledata",
  "sampledcourse",
  "seedqueue",
  "fallbackqueue",
  "introductiontoprogramming",
];

/**
 * The Arabic half of the removed fixture. It is matched literally rather than
 * through `normalize`, which keeps only `[a-z0-9]` and would reduce Arabic to
 * the empty string — a substring of everything, and so a detector that passes
 * nothing.
 */
const literalFixtureStrings = ["مقدمة في البرمجة بالعربية"];

const adminScanRoots = ["src/components/admin", "src/app/[locale]/admin"];

test("the Admin Catalog surface carries no demo Course fixture", () => {
  let scanned = 0;
  for (const relativeRoot of adminScanRoots) {
    const files = productionFiles(relativeRoot);
    assert.ok(files.length > 0, `scan root ${relativeRoot} matched no files; the detector would pass vacuously`);
    scanned += files.length;
    for (const file of files) {
      const source = fs.readFileSync(file, "utf8");
      const normalized = normalize(source);
      const relative = path.relative(frontendRoot(), file);
      for (const concept of fixtureConcepts) {
        assert.ok(
          !normalized.includes(normalize(concept)),
          `${relative} carries the demo Catalog concept "${concept}"; the Admin queue must render only ` +
            `Course revisions the server reports as PENDING_REVIEW`,
        );
      }
      for (const literal of literalFixtureStrings) {
        assert.ok(
          !source.includes(literal),
          `${relative} carries the demo Catalog title "${literal}"`,
        );
      }
    }
  }
  assert.ok(scanned >= 2, `the Admin surface scan covered only ${scanned} files`);
});

test("the fixture scan recognises the spellings a reintroduction would use", () => {
  // A concept that normalizes to the empty string is a substring of every
  // file, and would make the scan pass while detecting nothing.
  for (const concept of fixtureConcepts) {
    assert.ok(normalize(concept).length > 0, `fixture concept "${concept}" normalizes away`);
  }
  for (const literal of literalFixtureStrings) {
    assert.ok(literal.trim().length > 0, "a literal fixture string must be non-empty");
  }

  for (const spelling of [
    "demo-course-1",
    "demoCourse",
    "DEMO_COURSE_1",
    "course-demo-1",
    "Introduction to Programming",
    "mock course",
    "fallback queue",
  ]) {
    const normalized = normalize(spelling);
    assert.ok(
      fixtureConcepts.some((concept) => normalized.includes(normalize(concept))),
      `the fixture scan missed "${spelling}"`,
    );
  }
  for (const benign of ["pricingModal", "reviewQueue", "lifecycleControls", "taxonomyTerm", "placeholder"]) {
    const normalized = normalize(benign);
    for (const concept of fixtureConcepts) {
      assert.ok(!normalized.includes(normalize(concept)), `the fixture scan false-positived on "${benign}"`);
    }
  }
});

test("the review queue is server state, not component state", () => {
  const source = readSource("src/components/admin/review-queue.tsx");

  assert.match(
    source,
    /from "@\/lib\/api\/review"/,
    "the queue must be read through the Admin review API client",
  );
  assert.ok(source.includes("listReviewQueue"), "the Admin surface must call the real queue endpoint wrapper");
  assert.ok(source.includes("SubmittedRevisionInspector"), "a queue row must open the submitted-revision inspector");

  // No seeded array: the only initial queue is the empty one, replaced by the
  // server's response.
  assert.match(
    source,
    /useState<ReviewQueueItem\[\]>\(\[\]\)/,
    "the queue must start empty and be filled from the server",
  );
  assert.ok(
    !/useState<ReviewQueueItem\[\]>\(\[\s*\{/.test(source),
    "the queue must not be initialised from a literal Course",
  );

  assert.match(source, /setInspectedItem\(item\)/, "the selected queue row must provide its real IDs to inspection");

  // An empty server response renders an empty state, and says so.
  assert.match(source, /review-queue-empty/, "an empty queue must render an honest empty state");
});

test("the submitted-revision inspector renders only the returned graph and keeps review controls fail-closed", () => {
  const source = readSource("src/components/admin/submitted-revision-inspector.tsx");

  for (const call of ["getReviewCourseRevision", "getTaxonomyTerms", "getMediaAssetStatus", "previewAdminLesson"]) {
    assert.ok(source.includes(call), `the inspector must use the real ${call} client`);
  }
  for (const field of [
    "submitted-title-ar", "submitted-title-en", "submitted-description-ar", "submitted-description-en",
    "submitted-study-year", "submitted-major", "submitted-subject", "submitted-revision-state",
    "submitted-section-", "submitted-lesson-", "submitted-lesson-media-state-", "submitted-lesson-materials-",
    "approve-inspected-revision", "request-changes-inspected-revision", "preview-submitted-lesson-",
  ]) {
    assert.ok(source.includes(field), `the submitted graph must render ${field}`);
  }
  assert.match(source, /course\.id !== item\.course_id/, "a mismatched Course detail must fail closed");
  assert.match(source, /revision\.id !== item\.revision_id/, "a mismatched revision detail must fail closed");
  assert.match(source, /mediaStates\[assetVersionID\] !== "READY"/, "non-ready media must not request a preview");
  for (const state of ["PROCESSING", "SCAN_PASSED", "FAILED", "QUARANTINED", "UNAVAILABLE", "NO_VIDEO"]) {
    assert.ok(source.includes(state), `the inspector must describe ${state} instead of attempting protected playback`);
  }
  assert.ok(!source.includes("storage_object_key"), "the inspector must not expose object-storage keys");
  assert.ok(!source.includes("presigned"), "the inspector must not expose presigned upload material");
});

test("taxonomy and lifecycle administration are not gated on the pricing modal", () => {
  const source = readSource("src/components/admin/review-queue.tsx");

  // The exact defective rendering, and every near variant of it.
  assert.ok(
    !/pricing\w*\s*&&\s*<\s*TaxonomyControls/i.test(source),
    "taxonomy administration must not be conditioned on pricing state",
  );
  assert.ok(
    !/pricing\w*\s*&&\s*<\s*LifecycleControls/i.test(source),
    "lifecycle administration must not be conditioned on pricing state",
  );

  assert.match(
    source,
    /taxonomyControlsVisible\(catalogState\)[^\n]*&&[\s\S]{0,120}<TaxonomyControls/,
    "taxonomy administration must follow the administered Course",
  );
  assert.match(
    source,
    /lifecycleControlsVisible\(catalogState\)[^\n]*&&[\s\S]{0,120}<LifecycleControls/,
    "lifecycle administration must follow the administered Course",
  );
  assert.match(
    source,
    /pricingModalVisible\(catalogState\)/,
    "the pricing dialog must be driven by its own open/closed state",
  );
  assert.match(
    source,
    /onClose=\{\(\) => setCatalogState\(closePricingModal\)\}/,
    "closing the dialog must close only the dialog",
  );
});

test("the Instructor surface calls no Admin taxonomy mutation", () => {
  // Admin taxonomy vocabulary and override are capability-gated server-side
  // (`CATALOG_TAXONOMY`), and the Instructor studio must not reach for them:
  // an Instructor assigns taxonomy on its own revision through the authoring
  // route, and creates no vocabulary at all.
  const adminOnlyMutations = [
    "assignAdminTaxonomy",
    "createTaxonomyTerm",
    "renameTaxonomyTerm",
    "retireTaxonomyTerm",
    "deleteTaxonomyTerm",
    "/admin/taxonomy/terms",
    "/admin/courses/",
    "/admin/review/",
    "TaxonomyTermManagement",
    "TaxonomyOverrideForm",
  ];

  const files = productionFiles("src/components/instructor");
  assert.ok(files.length > 0, "the Instructor surface scan matched no files");
  for (const file of files) {
    const source = fs.readFileSync(file, "utf8");
    const relative = path.relative(frontendRoot(), file);
    for (const mutation of adminOnlyMutations) {
      assert.ok(
        !source.includes(mutation),
        `${relative} reaches for the Admin-only capability "${mutation}"`,
      );
    }
  }

  // And the Instructor path it does use is the owned-revision one.
  const panel = readSource("src/components/instructor/taxonomy-assignment-panel.tsx");
  assert.match(panel, /assignInstructorTaxonomy/, "the Instructor assigns taxonomy on its own revision");
});

test("an Instructor submission failure is reported at the Submit control", () => {
  const source = readSource("src/components/instructor/course-builder.tsx");

  const submitIndex = source.indexOf('data-testid="submit-for-review"');
  const errorIndex = source.indexOf('data-testid="submit-error"');
  assert.ok(submitIndex > 0, "the Submit control must be present");
  assert.ok(errorIndex > submitIndex, "the failure region must render beside the Submit control, not only at the top");

  assert.match(source, /submitErrorRef\.current\?\.scrollIntoView/, "the failure must be brought into view");
  assert.match(source, /submitErrorRef\.current\?\.focus\(\)/, "the failure must take focus");
  assert.match(source, /role="alert"/, "the failure must be announced");
  // The server's reason is what is shown: it is never replaced by local wording.
  assert.match(source, /const message = describeApiError\(cause, locale\)/);
  assert.match(source, /setSubmitError\(message\)/);
});
