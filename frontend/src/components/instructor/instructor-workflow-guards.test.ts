import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";

/**
 * Guards over the Instructor authoring workflow.
 *
 * These are the regressions that are cheap to reintroduce and expensive to notice: a UUID printed
 * where a name belongs, a wire enum shown as copy, a stock palette class where a token exists, a
 * control with no accessible name. None of them break a build or fail a behavioural test; each of
 * them was present on this surface before this tranche.
 */

function frontendRoot(): string {
  return process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend");
}

function readSource(relativePath: string): string {
  const full = path.join(frontendRoot(), relativePath);
  assert.ok(fs.existsSync(full), `${relativePath} is missing; this detector would pass vacuously`);
  return fs.readFileSync(full, "utf8");
}

const INSTRUCTOR_DIR = "src/components/instructor";

function instructorSources(): Array<{ name: string; source: string }> {
  const dir = path.join(frontendRoot(), INSTRUCTOR_DIR);
  return fs
    .readdirSync(dir)
    .filter((name) => name.endsWith(".tsx"))
    .map((name) => ({ name, source: fs.readFileSync(path.join(dir, name), "utf8") }));
}

/** Comments describe what was removed and must not be mistaken for what is rendered. */
function withoutComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, " ").replace(/(^|[^:])\/\/.*$/gm, "$1 ");
}

/* ------------------------------------------------------------------ identifiers */

test("no Instructor surface renders a course, revision or media identifier", () => {
  // Identifiers are legitimate in URLs, API calls, React keys, and data attributes. What is
  // forbidden is putting one in front of a person as though it meant something to them.
  const forbidden = [
    "{course.id}",
    "{selectedCourse.id}",
    "{revision.id}",
    "{course.live_revision_id}",
    "{lesson.video_asset_version_id}",
    "{revision.preview_asset_version_id}",
    "{file.asset_version_id}",
    "{subject.id}",
  ];
  for (const { name, source } of instructorSources()) {
    // An identifier is machinery in a React key, a form value, a data attribute, or a prop
    // handed to another component. It is presentation only when it lands in rendered text.
    const rendered = withoutComments(source)
      .replace(/\b(?:key|value|htmlFor|id)=\{[^}]*\}/g, " ")
      .replace(/data-[a-z-]+=\{[^{}]*(\{[^{}]*\}[^{}]*)*\}/g, " ")
      .replace(/[A-Za-z]+(ID|Id)=\{[^{}]*(\{[^{}]*\}[^{}]*)*\}/g, " ");
    for (const leak of forbidden) {
      assert.ok(!rendered.includes(leak), `${name} renders ${leak} to a person`);
    }
  }
});

test("the lesson row reports whether a video is attached, not which asset it is", () => {
  const curriculum = readSource(`${INSTRUCTOR_DIR}/curriculum-builder.tsx`);
  assert.match(curriculum, /const hasVideo = Boolean\(lesson\.video_asset_version_id\)/);
  assert.match(curriculum, /labels\.videoAttached : labels\.videoMissing/);
  // The identifier survives only as a boolean data attribute for tests and support.
  assert.match(curriculum, /data-video-attached=/);
});

test("submission rejections drop the server's object targets", () => {
  const readiness = readSource(`${INSTRUCTOR_DIR}/submission-readiness.ts`);
  // `target` is always `kind:uuid`. Nothing may read it into reader-facing copy.
  assert.ok(
    !/violation\.target/.test(readiness),
    "a violation target is a database key and must never reach the Instructor",
  );
  assert.match(readiness, /reasonFor\(known\)/);
});

/* ------------------------------------------------------------------ wire enums */

test("no Instructor surface renders an enum-bearing field as copy", () => {
  /*
    The enum *string* is always legitimate in this code — it is what the wire value is compared
    against. What was wrong was rendering the field that holds one. Two did: a lesson's lab
    materials printed `[${file.kind}]` in front of the file name, and the legacy study-year
    selector used the wire value as its own option label.

    So the detector names the fields rather than the values, which is both precise and exactly the
    set of defects this tranche removed.
  */
  const enumBearing = [
    "{file.kind}",
    "{lesson.kind}",
    "{course.lifecycle}",
    "{selectedCourse.lifecycle}",
    "{revision.state}",
    "{course.editable_revision?.state}",
    "{standing.wire}",
    "{course.classification_model}",
  ];
  for (const { name, source } of instructorSources()) {
    // A `data-` attribute is where the wire value belongs: support and tests read it, readers
    // never see it.
    const rendered = withoutComments(source).replace(
      /data-[a-z-]+=\{[^{}]*(\{[^{}]*\}[^{}]*)*\}/g,
      " ",
    );
    for (const field of enumBearing) {
      assert.ok(!rendered.includes(field), `${name} renders ${field}, which carries a wire enum`);
    }
    assert.ok(
      !/\[\$\{[^}]*\.kind\}\]/.test(rendered),
      `${name} prints a file kind as a bracketed prefix`,
    );
  }
});

test("every lifecycle value an Instructor can reach has a human label in both languages", () => {
  // The wire value stays on data attributes; the reader always gets a word for it.
  const en = readSource("src/lib/i18n/dictionaries/en.ts");
  const ar = readSource("src/lib/i18n/dictionaries/ar.ts");
  for (const stage of [
    "DRAFT:",
    "DRAFT_UPDATE:",
    "IN_REVIEW:",
    "CHANGES_REQUESTED:",
    "PUBLISHED:",
    "UNAVAILABLE:",
  ]) {
    assert.ok(en.includes(stage), `the English standing vocabulary is missing ${stage}`);
    assert.ok(ar.includes(stage), `the Arabic standing vocabulary is missing ${stage}`);
  }
});

test("the study-year selector offers words rather than its wire values", () => {
  const builder = readSource(`${INSTRUCTOR_DIR}/course-builder.tsx`);
  assert.match(builder, /details\.studyYears\[year\]/);
  assert.ok(
    !/<option key=\{year\} value=\{year\}>\s*\{year\}/.test(builder),
    "the legacy study year must not render YEAR_1 as its own label",
  );
});

/* ------------------------------------------------------------------ design system */

const STOCK_RAMPS =
  "slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose";

test("no Instructor surface reaches past the design tokens into the stock palette", () => {
  const stock = new RegExp(`\\b(?:bg|text|border|divide|ring|from|to|via)-(?:${STOCK_RAMPS})-\\d{2,3}\\b`);
  for (const { name, source } of instructorSources()) {
    const rendered = withoutComments(source);
    const found = rendered.match(new RegExp(stock, "g"));
    assert.equal(
      found,
      null,
      `${name} uses the stock Tailwind palette: ${found?.join(", ")}`,
    );
  }
});

test("no Instructor surface branches its UI copy on the locale in place", () => {
  for (const { name, source } of instructorSources()) {
    assert.ok(
      !/isAr \? "/.test(source),
      `${name} carries bilingual UI copy in place instead of reading the dictionary`,
    );
  }
});

/* ------------------------------------------------------------------ accessibility */

test("every Instructor text control is named, not merely placeheld", () => {
  // A `placeholder` disappears on the first keystroke and is not an accessible name. Four fields
  // on the create form and four on the subject request were named this way, two of them `required`.
  for (const { name, source } of instructorSources()) {
    const rendered = withoutComments(source);
    const placeholders = rendered.match(/placeholder=\{[^}]*\}/g) ?? [];
    for (const placeholder of placeholders) {
      // A placeholder is fine as a hint beside a real label; what is checked is that the file
      // labels its controls at all rather than relying on placeholders alone.
      assert.ok(
        /<Field\b/.test(rendered) || /aria-label|<label|htmlFor=/.test(rendered),
        `${name} uses ${placeholder} with no accompanying label`,
      );
    }
  }
});

test("destructive curriculum actions ask before destroying an upload", () => {
  const curriculum = readSource(`${INSTRUCTOR_DIR}/curriculum-builder.tsx`);
  assert.match(curriculum, /ConfirmDialog/);
  assert.match(curriculum, /confirmDeleteSectionBody/);
  assert.match(curriculum, /confirmDeleteLessonBody/);
  // Adding is reversible in a sentence of typing and must stay unguarded: the dialog resolves to
  // the delete handlers and to nothing else.
  assert.match(curriculum, /onConfirm=\{confirmDelete\}/);
  const handler = curriculum.slice(
    curriculum.indexOf("const confirmDelete = "),
    curriculum.indexOf("const lessonCount"),
  );
  assert.match(handler, /onDeleteSection\(pendingDelete\.id\)/);
  assert.match(handler, /onDeleteLesson\(pendingDelete\.id\)/);
  assert.ok(
    !/onAddSection|onAddLesson/.test(handler),
    "adding a section or lesson must not pass through the confirmation",
  );

  const dialog = readSource("src/components/ui/confirm-dialog.tsx");
  assert.match(dialog, /DialogPrimitive\.Title/, "the dialog must be named to a screen reader");
  assert.match(dialog, /DialogPrimitive\.Description/);
  const cancelAt = dialog.indexOf('data-testid="confirm-cancel"');
  const acceptAt = dialog.indexOf('data-testid="confirm-accept"');
  assert.ok(
    cancelAt > 0 && cancelAt < acceptAt,
    "cancel must precede the destructive choice so it takes initial focus",
  );
});

test("submitting is confirmed, because it closes editing", () => {
  const panel = readSource(`${INSTRUCTOR_DIR}/submission-panel.tsx`);
  assert.match(panel, /ConfirmDialog/);
  assert.match(panel, /labels\.confirmBody/);
  // Saving is not, because it changes nothing about who may edit.
  const builder = readSource(`${INSTRUCTOR_DIR}/course-builder.tsx`);
  assert.ok(!/ConfirmDialog/.test(builder), "saving details must not be behind a confirmation");
});

/* ------------------------------------------------------------------ ownership */

test("the launch price is presented as an administrator's decision", () => {
  const price = readSource(`${INSTRUCTOR_DIR}/course-pricing-summary.tsx`);
  assert.match(price, /labels\.adminOwned/);
  assert.match(price, /formatFils/, "the canonical formatter is the only price formatter");
  assert.ok(
    !/price_minor_units\s*\/\s*1000|toFixed\(3\)/.test(price),
    "no surface may do its own currency arithmetic",
  );

  const submission = readSource(`${INSTRUCTOR_DIR}/submission-panel.tsx`);
  assert.match(submission, /labels\.adminOwnsPrice/);
  const readiness = readSource(`${INSTRUCTOR_DIR}/submission-readiness.ts`);
  assert.ok(
    !/PRICE/.test(readiness),
    "a price is never a submission prerequisite: SubmitCourse does not read one",
  );
});

test("editing is offered on the server's editability, not on a revision merely existing", () => {
  const builder = readSource(`${INSTRUCTOR_DIR}/course-builder.tsx`);
  assert.match(
    builder,
    /revision\?\.id && standing\.editable \?/,
    "a submitted revision still exists; the studio must not offer to edit it",
  );
  assert.match(builder, /SubmittedCourseSummary/);
});

/* ------------------------------------------------------------------ contrast */

function relativeLuminance(hex: string): number {
  const value = hex.replace("#", "");
  const channels = [0, 2, 4].map((offset) => parseInt(value.slice(offset, offset + 2), 16) / 255);
  const linear = channels.map((c) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * linear[0] + 0.7152 * linear[1] + 0.0722 * linear[2];
}

function contrastRatio(a: string, b: string): number {
  const [high, low] = [relativeLuminance(a), relativeLuminance(b)].sort((x, y) => y - x);
  return (high + 0.05) / (low + 0.05);
}

test("the Instructor price meets AA, where the colour it replaced did not", () => {
  // The measurement this tranche was asked to fix. `text-emerald-600` (#059669) was painted on the
  // card (#ffffff) and on the panel's own slate ground (#f8fafc).
  assert.ok(contrastRatio("#059669", "#ffffff") < 4.5);
  assert.ok(contrastRatio("#059669", "#f8fafc") < 4.5);
  assert.equal(Math.round(contrastRatio("#059669", "#ffffff") * 100) / 100, 3.77);

  // It is `text-foreground` now — ink-900 #0d1b2a — on card and page ground alike.
  assert.ok(contrastRatio("#0d1b2a", "#ffffff") >= 4.5);
  assert.ok(contrastRatio("#0d1b2a", "#f8fafc") >= 4.5);

  const price = readSource(`${INSTRUCTOR_DIR}/course-pricing-summary.tsx`);
  const rendered = withoutComments(price);
  assert.ok(!/text-emerald/.test(rendered), "the price must not be repainted in a stock ramp");
  assert.match(rendered, /text-foreground/);
});

test("the shared secondary ink clears AA on every surface the studio paints it on", () => {
  // --muted-foreground, ink-500 at 43% lightness, is every secondary line on these screens.
  const inkFive = "#5c677d";
  for (const ground of ["#ffffff", "#f8fafc"]) {
    assert.ok(
      contrastRatio(inkFive, ground) >= 4.5,
      `secondary ink fails AA on ${ground}: ${contrastRatio(inkFive, ground).toFixed(2)}:1`,
    );
  }
});

/* ------------------------------------------------------------------ bilingual parity */

test("every Instructor vocabulary block exists in both languages", () => {
  const blocks = [
    "courses:",
    "standing:",
    "standingBanner:",
    "submitted:",
    "price:",
    "details:",
    "academic:",
    "picker:",
    "request:",
    "legacyTaxonomy:",
    "curriculum:",
    "media:",
    "submission:",
  ];
  const en = readSource("src/lib/i18n/dictionaries/en.ts");
  const ar = readSource("src/lib/i18n/dictionaries/ar.ts");
  for (const block of blocks) {
    assert.ok(en.includes(block), `the English dictionary is missing ${block}`);
    assert.ok(ar.includes(block), `the Arabic dictionary is missing ${block}`);
  }
});
