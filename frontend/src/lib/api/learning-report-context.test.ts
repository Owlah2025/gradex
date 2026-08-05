import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

/**
 * The report context is opaque evidence the client carries but never interprets (D-065).
 *
 * These guards are structural: they fail if any learning surface starts rendering, persisting,
 * decoding, or logging a context, which is how an encrypted token would leak back into the DOM or
 * browser storage that D-063 keeps internal identity out of.
 */

const learningSources = (): { file: string; source: string }[] => {
  const roots = [
    path.join(process.cwd(), "src/components/learning"),
    path.join(process.cwd(), "src/app"),
    path.join(process.cwd(), "src/lib/api"),
  ];
  const collected: { file: string; source: string }[] = [];
  const walk = (directory: string) => {
    if (!fs.existsSync(directory)) return;
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      const full = path.join(directory, entry.name);
      if (entry.isDirectory()) walk(full);
      else if (/\.(ts|tsx)$/.test(entry.name) && !entry.name.endsWith(".test.ts")) {
        // Comments are prose, not behaviour: a doc comment naming a forbidden sink must not be
        // mistaken for code that uses one.
        const source = fs.readFileSync(full, "utf-8").replace(/\/\*[\s\S]*?\*\//g, "").replace(/\/\/.*$/gm, "");
        collected.push({ file: full, source });
      }
    }
  };
  roots.forEach(walk);
  return collected;
};

test("report contexts are never persisted to browser storage", () => {
  for (const { file, source } of learningSources()) {
    if (!/report_context/.test(source)) continue;
    for (const sink of ["localStorage", "sessionStorage", "document.cookie"]) {
      assert.ok(
        !source.includes(sink),
        `${file} both handles a report context and touches ${sink}; a context must never be persisted`
      );
    }
  }
});

test("report contexts are never decoded, logged, or rendered into markup", () => {
  for (const { file, source } of learningSources()) {
    if (!/report_context/.test(source)) continue;
    for (const forbidden of ["atob(", "JSON.parse(report", "console.log", "data-report", "dangerouslySetInnerHTML"]) {
      assert.ok(
        !source.includes(forbidden),
        `${file} handles a report context and uses ${forbidden}; contexts are carried, never interpreted`
      );
    }
  }
});

test("report contexts are consumed only where the reporting path needs them", () => {
  // T066 introduced the reporting UI, so the consumer set grew from the type layer alone to a
  // closed list: the API types and submission call, the module that reads which targets a rendered
  // page may report, and the dialog that hands one to the request. Nothing else — no page, no view,
  // no player — touches a context, and an accidental new consumer fails here rather than being
  // found later in a rendered payload.
  const consumers = learningSources()
    .filter(({ source }) => /report_context/.test(source))
    .map(({ file }) => path.relative(process.cwd(), file))
    .sort();
  assert.deepEqual(
    consumers,
    [
      "src/components/learning/report-content-dialog.tsx",
      "src/components/learning/report-targets.ts",
      "src/lib/api/learning.ts",
    ],
    `unexpected report-context consumers: ${consumers.join(", ")}`,
  );
});

test("the reporting UI carries a context without rendering, attributing, or persisting it", () => {
  // The dialog receives a context as a prop and sends it in a request body. These are the sinks a
  // JSX component specifically could reach for, checked across every reporting module.
  const reportingSources = learningSources().filter(({ file }) =>
    /report-(content-dialog|targets|dialog-state|labels)\.tsx?$/.test(file),
  );
  assert.ok(reportingSources.length >= 3, "the reporting modules were not found");

  for (const { file, source } of reportingSources) {
    for (const sink of [
      "localStorage",
      "sessionStorage",
      "document.cookie",
      "atob(",
      "console.",
      "data-context",
      "data-report",
      "dangerouslySetInnerHTML",
      "searchParams",
      "location.hash",
      "window.location",
    ]) {
      assert.ok(!source.includes(sink), `${file} uses ${sink}; a report context must not reach it`);
    }
    // A context may be passed as a prop and read as a field; it may never be interpolated into
    // rendered output, which is what a `{…context…}` expression between JSX tags would do.
    assert.ok(
      !/>\s*\{[^}]*[Cc]ontext[^}]*\}\s*</.test(source),
      `${file} appears to render a context into markup`,
    );
  }
});
