import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

/**
 * The rules the academic personalisation surface must not break, asserted against the shipped
 * source rather than against behaviour.
 *
 * These are structural on purpose. The failures they guard against are not ones a rendering test
 * would notice — a name comparison quietly added to bridge slugs to identifiers still renders
 * perfectly, and still writes the wrong Program onto a real account. What has to be provable is
 * that the code *cannot* do it, and that is a property of the source.
 */

// The suite runs from the compiled output, so `__dirname` is the build directory and not the
// source. Anchoring on the working directory is how the other structural specs here reach the
// shipped `.tsx` files, which are never compiled into that build at all.
const FRONTEND_ROOT = process.cwd().endsWith("/frontend")
  ? process.cwd()
  : path.join(process.cwd(), "frontend");
const SOURCE_ROOT = path.join(FRONTEND_ROOT, "src");

function sourceFiles(...directories: string[]): { path: string; text: string }[] {
  const files: { path: string; text: string }[] = [];
  for (const directory of directories) {
    const absolute = path.join(SOURCE_ROOT, directory);
    for (const entry of fs.readdirSync(absolute, { withFileTypes: true })) {
      if (!entry.isFile()) continue;
      if (!/\.tsx?$/.test(entry.name) || entry.name.endsWith(".test.ts")) continue;
      files.push({
        path: `${directory}/${entry.name}`,
        text: fs.readFileSync(path.join(absolute, entry.name), "utf8"),
      });
    }
  }
  assert.ok(files.length > 0, "no source files were found to check");
  return files;
}

function read(relative: string): string {
  return fs.readFileSync(path.join(SOURCE_ROOT, relative), "utf8");
}

/**
 * The two academic option lists must never meet in one module.
 *
 * The public catalogue names entities by slug. The authenticated option lists return the internal
 * identifiers `PUT /me/academic-profile` requires and carry no slug at all. A module holding both
 * lists is one line away from bridging them on `name_ar` — which would write the wrong Program onto
 * a real account and tell the Student it was theirs.
 *
 * The rule is about those two *lists*, named precisely, not about the modules that contain them.
 * Reading a Student's own saved profile beside the public catalogue is not the hazard and is what
 * the precedence rule actually requires: the account's answer has to be able to outrank a browser
 * preference, which means something has to hold both.
 */
const PUBLIC_OPTION_READS = [
  "getPublicInstitutions",
  "getPublicPrograms",
  "getPublicSubjects",
  "getPublicLevels",
];
const AUTHENTICATED_OPTION_READS = [
  "listInstitutionOptions",
  "listCollegeOptions",
  "listProgramOptions",
];

test("no module reads the public option lists and the authenticated ones together", () => {
  const offenders: string[] = [];
  const walk = (directory: string) => {
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      const absolute = path.join(directory, entry.name);
      if (entry.isDirectory()) {
        if (entry.name !== "node_modules") walk(absolute);
        continue;
      }
      if (!/\.tsx?$/.test(entry.name) || entry.name.endsWith(".test.ts")) continue;
      const text = fs.readFileSync(absolute, "utf8");
      const reads = (names: string[]) =>
        names.some((name) => new RegExp(`\\b${name}\\b`).test(text));
      if (reads(PUBLIC_OPTION_READS) && reads(AUTHENTICATED_OPTION_READS)) {
        offenders.push(path.relative(SOURCE_ROOT, absolute));
      }
    }
  };
  walk(SOURCE_ROOT);
  assert.deepEqual(offenders, []);
});

/** Nothing on the anonymous side may compare, search, or key on a translated display name. */
test("the anonymous academic surface never matches on a localized name", () => {
  const nameField = /(name_ar|name_en|title_ar|title_en|institution_name|program_name|college_name)/;
  const comparison =
    /(===|!==|==\s|\.includes\(|\.startsWith\(|\.indexOf\(|\.localeCompare\(|\.toLowerCase\(|\.normalize\()/;
  const offenders: string[] = [];
  for (const file of sourceFiles("components/academic", "lib/academic")) {
    for (const [index, line] of file.text.split("\n").entries()) {
      // Comments are prose about this rule, not code that could break it.
      const code = line.replace(/^\s*(\/\/|\*|\/\*).*$/, "");
      if (nameField.test(code) && comparison.test(code)) {
        offenders.push(`${file.path}:${index + 1}`);
      }
    }
  }
  assert.deepEqual(offenders, [], "a localized name is being compared as if it were an identifier");
});

/** The browsing preference is never written to the account, from anywhere on the anonymous side. */
test("nothing in the anonymous academic surface writes an academic profile", () => {
  const writes = /(saveAcademicProfile|skipAcademicOnboarding|"PUT"|academic-profile\/skip)/;
  const offenders = sourceFiles("components/academic", "lib/academic")
    .filter((file) => writes.test(file.text))
    .map((file) => file.path);
  assert.deepEqual(offenders, []);
});

/**
 * The provider reads the Student's profile so the account's answer can outrank a browser
 * preference. Reading is the whole of its relationship with that contract.
 */
test("the shared provider only reads the academic profile", () => {
  const provider = read("components/academic/academic-context-provider.tsx");
  assert.match(provider, /getAcademicProfile/);
  assert.doesNotMatch(provider, /saveAcademicProfile|skipAcademicOnboarding/);
});

/**
 * The anonymous context reaches the onboarding form as guidance beside the real options. It must
 * never reach the fields those options populate: there is no slug-to-identifier contract, so any
 * value derived from it would be a guess written to a real account.
 */
test("the onboarding form never preselects an authenticated field from the anonymous context", () => {
  const form = read("components/learning/academic-profile-form.tsx");
  const setters = /(setInstitutionID|setCollegeID|setProgramChoice|institutionID:|programID|academicUnitID)/;
  const offenders = form
    .split("\n")
    .map((line, index) => ({ line, number: index + 1 }))
    .filter(({ line }) => /\banonymous\b/.test(line) && setters.test(line))
    .map(({ number }) => `academic-profile-form.tsx:${number}`);
  assert.deepEqual(offenders, []);
  // And the guidance itself is present, so the Student is asked to confirm rather than to start over.
  assert.match(form, /academic-profile-handoff/);
});

/** Browser storage holds discovery preferences. It never holds anything that authenticates anyone. */
test("no credential material goes near the stored context", () => {
  // Comments stripped first: this module's own documentation explains why `sessionStorage` was
  // rejected, and a guard that cannot tell prose from code would report that as a credential.
  const storage = read("lib/academic/anonymous-context.ts")
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/\/\/.*$/gm, "");
  assert.doesNotMatch(storage, /csrf|Cookie|token|password|sessionStorage/i);
});

/** A stored record without its version cannot be told apart from one written by another release. */
test("the stored shape is versioned", () => {
  const storage = read("lib/academic/anonymous-context.ts");
  assert.match(storage, /ACADEMIC_CONTEXT_VERSION/);
  assert.match(storage, /record\.version !== ACADEMIC_CONTEXT_VERSION/);
});

/** A public page must never call an authenticated academic surface for its option lists. */
test("the picker reads the public catalogue and nothing else", () => {
  const picker = read("components/academic/academic-context-picker.tsx");
  assert.match(picker, /getPublicInstitutions/);
  assert.match(picker, /getPublicPrograms/);
  assert.doesNotMatch(picker, /\/admin\//);
  assert.doesNotMatch(picker, /listInstitutionOptions|listProgramOptions|listCollegeOptions/);
});

/** Every string a reader sees comes from the shared dictionary, in both languages. */
test("the academic surface hardcodes no reader-facing English", () => {
  const suspicious = /(?:^|[^\w.])"[A-Z][a-z]+ [a-z]/;
  const offenders: string[] = [];
  for (const file of sourceFiles("components/academic")) {
    for (const [index, line] of file.text.split("\n").entries()) {
      if (/^\s*(\*|\/\/|\/\*)/.test(line)) continue;
      if (/\bthrow new Error\(/.test(line)) continue;
      if (suspicious.test(line)) offenders.push(`${file.path}:${index + 1}`);
    }
  }
  assert.deepEqual(offenders, []);
});
