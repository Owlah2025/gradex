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

  // A queue row still opens the submitted-revision inspector, but through the workspace's own
  // address rather than through component state. The route is what makes a review reloadable,
  // linkable and reachable from the Courses directory; holding it in `useState` meant a refresh
  // mid-review discarded the whole context.
  assert.match(
    source,
    /href=\{`\/\$\{locale\}\/admin\/courses\/\$\{item\.course_id\}\/review`\}/,
    "a queue row must link to the addressable review workspace for that Course",
  );
  assert.ok(
    !source.includes("SubmittedRevisionInspector"),
    "the queue must not also mount the inspector inline, or a review would have two entry points",
  );
  assert.ok(
    readSource("src/components/admin/routed-course-review.tsx").includes("SubmittedRevisionInspector"),
    "the routed review workspace must mount the submitted-revision inspector",
  );

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

  const routed = readSource("src/components/admin/routed-course-review.tsx");
  assert.match(
    routed,
    /entry\.course_id === courseID/,
    "the routed workspace must establish its context from the server's queue, not from a link parameter",
  );
  assert.match(
    routed,
    /key=\{item\.revision_id\}/,
    "a newly resolved revision must receive a fresh workspace state",
  );

  for (const removedBridge of [
    "administer-course-id",
    "administer-review-item-",
    "Administer a Course by UUID",
    "catalog-administration",
    "PricingModal",
    "LifecycleControls",
  ]) {
    assert.ok(!source.includes(removedBridge), `the pending-review journey must not expose ${removedBridge}`);
  }

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

test("one selected revision carries every pending-review administration identity", () => {
  const source = readSource("src/components/admin/submitted-revision-inspector.tsx");
  const taxonomy = readSource("src/components/admin/taxonomy-override-form.tsx");
  const pricing = readSource("src/components/admin/pricing-form.tsx");

  assert.match(source, /<TaxonomyOverrideForm[\s\S]{0,240}courseID=\{item\.course_id\}/);
  assert.match(source, /<TaxonomyOverrideForm[\s\S]{0,240}revisionID=\{item\.revision_id\}/);
  assert.match(source, /<PricingPanel[\s\S]{0,180}courseID=\{item\.course_id\}/);
  assert.match(source, /<PricingPanel[\s\S]{0,180}sections=\{revision\.sections\}/);
  assert.match(source, /review-taxonomy-error/, "a taxonomy load failure must stay inside its panel");
  assert.ok(!taxonomy.includes("taxonomy-override-revision"), "revision identity must not be editable");
  assert.ok(!taxonomy.includes("defaultRevisionID"), "the override must not retain a second revision selector");
  assert.match(pricing, /data-testid="pricing-section-select"/, "Section pricing must use a human selector");
  assert.ok(!pricing.includes("Section Identity ID"), "Section identity must not be a text input");
  assert.ok(!source.includes("submitted-course-id"), "Course identity must not be primary interface content");
  assert.ok(!source.includes("submitted-revision-id"), "revision identity must not be primary interface content");
});

// Founder acceptance found taxonomy administration disappearing with the pricing dialog.
// The replacement makes both operations siblings in the selected-revision workspace.
test("the founder pricing/taxonomy regression stays unified without dialog state", () => {
  const source = readSource("src/components/admin/submitted-revision-inspector.tsx");
  const taxonomyAt = source.indexOf("<TaxonomyOverrideForm");
  const pricingAt = source.indexOf("<PricingPanel");

  assert.ok(taxonomyAt > 0, "the selected revision must expose optional taxonomy override");
  assert.ok(pricingAt > taxonomyAt, "pricing must share the selected revision workspace");
  assert.ok(!source.includes("pricingModal"), "pricing visibility must not control taxonomy visibility");
});

test("a completed review locks before queue refresh and clears its workspace", () => {
  const inspector = readSource("src/components/admin/submitted-revision-inspector.tsx");
  const routed = readSource("src/components/admin/routed-course-review.tsx");
  const lockedAt = inspector.indexOf("setReviewed(true)");
  const refreshedAt = inspector.indexOf("await onReviewed(success)", lockedAt);

  assert.ok(lockedAt > 0, "successful review must lock the decision controls");
  assert.ok(refreshedAt > lockedAt, "the decision must lock before the queue refresh callback");
  assert.match(inspector, /const canAct = canReview && !reviewed/);

  // The workspace is a route now, so a completed review is confirmed in place and left
  // deliberately. Redirecting on success would destroy the message that tells the Admin the
  // decision landed; the directory re-reads the queue and the lifecycle directory when it mounts,
  // so returning shows the server's new state rather than anything held here.
  assert.match(
    routed,
    /onReviewed=\{async \(\) => \{\s*setDecided\(true\);\s*\}\}/,
    "a completed review must be confirmed in the workspace rather than redirected away",
  );
  assert.match(
    routed,
    /review-decision-recorded/,
    "a recorded decision must be stated, with a route back to the directory",
  );
});

// The price-before-publication invariant is enforced by the server. The
// workspace's only job is to make the refusal actionable, in both locales,
// without re-implementing the rule client-side.
test("an approval refused for a missing Course price names the remedy", () => {
  const source = readSource("src/components/admin/submitted-revision-inspector.tsx");

  assert.match(source, /COURSE_PRICE_REQUIRED/, "the inspector must recognise the server's pricing violation");
  assert.match(
    source,
    /isCoursePriceRequired\(cause\)/,
    "the pricing remedy must be chosen from the failure, not from local readiness state",
  );
  assert.match(source, /Set the Course price/, "the English refusal must name the remedy");
  assert.match(source, /سعر الدورة/, "the Arabic refusal must name the remedy");
  // The server's own reason is still shown; the remedy is added, not substituted.
  assert.match(source, /const message = describeApiError\(cause, locale\)/);

  // No client-side gate may stand in for the server: approval stays enabled
  // and the server decides.
  assert.ok(
    !/disabled=\{[^}]*price/i.test(source),
    "the Approve control must not be gated on client-side pricing state",
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
  // Submission moved into its own panel alongside the readiness checklist, so the guarantee the
  // founder's manual test produced is asserted where it now lives. It is unchanged: the reason
  // renders beside the control, is scrolled to, takes focus, and is announced.
  const panel = readSource("src/components/instructor/submission-panel.tsx");

  const submitIndex = panel.indexOf('data-testid="submit-for-review"');
  const errorIndex = panel.indexOf('data-testid="submit-error"');
  assert.ok(submitIndex > 0, "the Submit control must be present");
  assert.ok(errorIndex > submitIndex, "the failure region must render beside the Submit control, not only at the top");

  assert.match(panel, /rejectionRef\.current\?\.scrollIntoView/, "the failure must be brought into view");
  assert.match(panel, /rejectionRef\.current\?\.focus\(\)/, "the failure must take focus");
  assert.match(panel, /role="alert"/, "the failure must be announced");

  // The server's reason is still never replaced by invented wording. Codes it publishes are
  // translated one-for-one from `catalog/validation.go`; anything unrecognised keeps the server's
  // own text rather than being dropped.
  const builder = readSource("src/components/instructor/course-builder.tsx");
  assert.match(builder, /describeSubmissionRejection\(/);
  assert.match(builder, /detail: translated\.hasUntranslated \? message : null/);
  const readiness = readSource("src/components/instructor/submission-readiness.ts");
  assert.match(readiness, /hasUntranslated = true/, "an unmapped violation code must not be swallowed");
});
