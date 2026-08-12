import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";
import type { PublicCourse } from "@/lib/api/public-catalog";
import {
  buildPublishedCourseOptions,
  findPublishedCourse,
  invitationCourseLabel,
  publishedCourseOptionLabel,
} from "./published-courses";

/**
 * Admin Course Access launch-journey guards.
 *
 * Founder acceptance found the Course Access surface demanding a pasted
 * `Course ID (UUID)` twice: once to configure default access expiry, once to
 * issue an invitation. The launch journey selects a published Course by title
 * and carries its identifier internally, so both the selector's own logic and
 * the page wiring that consumes it are asserted here.
 */

const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const OTHER_COURSE_ID = "c0000000-0000-0000-0000-000000000002";

const ENGLISH: PublicCourse[] = [
  {
    id: COURSE_ID,
    slug: "cs101-introduction-to-programming",
    title: "CS101: Introduction to Programming",
    instructor_display_name: "Dr. Fahd",
    subject: { label: "Computing", code: "CS 101" },
    study_year: { label: "Year 1" },
    has_preview: true,
  },
  {
    id: OTHER_COURSE_ID,
    slug: "ma102-calculus",
    title: "MA102: Calculus",
    instructor_display_name: "Dr. Noura",
    has_preview: false,
  },
];

const ARABIC: PublicCourse[] = [
  {
    id: COURSE_ID,
    slug: "cs101-introduction-to-programming",
    title: "مقدمة في البرمجة CS101",
    instructor_display_name: "د. فهد",
    subject: { label: "حاسب آلي", code: "CS 101" },
    study_year: { label: "السنة الأولى" },
    has_preview: true,
  },
];

function frontendRoot(): string {
  return process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend");
}

function readSource(relativePath: string): string {
  const full = path.join(frontendRoot(), relativePath);
  assert.ok(fs.existsSync(full), `${relativePath} is missing; this detector would pass vacuously`);
  return fs.readFileSync(full, "utf8");
}

const PAGE = "src/app/[locale]/admin/course-access/page.tsx";
const SELECTOR = "src/components/admin/published-course-selector.tsx";

test("published Courses are offered by human-readable label, never by identifier", () => {
  const options = buildPublishedCourseOptions(ENGLISH, ARABIC);
  assert.equal(options.length, 2, "every published Course must remain selectable");

  const [cs101, ma102] = options;
  assert.equal(cs101.title, "CS101: Introduction to Programming");
  assert.equal(cs101.alternateTitle, "مقدمة في البرمجة CS101", "the other-locale title must be carried");

  const label = publishedCourseOptionLabel(cs101);
  assert.match(label, /CS101: Introduction to Programming/);
  assert.match(label, /مقدمة في البرمجة CS101/, "the Arabic title must be shown where available");
  assert.match(label, /Dr\. Fahd/, "the instructor disambiguates same-titled Courses");
  assert.match(label, /Computing/);
  assert.match(label, /Year 1/);
  assert.ok(!label.includes(COURSE_ID), "the option label must not expose the Course UUID");

  // A Course with no alternate-locale row and no taxonomy still labels cleanly.
  assert.equal(ma102.alternateTitle, undefined);
  assert.equal(publishedCourseOptionLabel(ma102), "MA102: Calculus — Dr. Noura");
  assert.ok(!publishedCourseOptionLabel(ma102).includes(OTHER_COURSE_ID));
});

test("a failed or partial alternate-locale read narrows the label, never the Course list", () => {
  const options = buildPublishedCourseOptions(ENGLISH, []);
  assert.equal(options.length, 2, "the alternate read must not decide which Courses exist");
  assert.equal(options[0].alternateTitle, undefined);
  assert.equal(publishedCourseOptionLabel(options[0]).includes("CS101: Introduction to Programming"), true);
});

test("an identical title in both locales is not duplicated in the label", () => {
  const options = buildPublishedCourseOptions(
    [{ ...ENGLISH[0], subject: undefined, study_year: undefined }],
    [{ ...ENGLISH[0] }],
  );
  assert.equal(options[0].alternateTitle, undefined);
  assert.equal(publishedCourseOptionLabel(options[0]), "CS101: Introduction to Programming — Dr. Fahd");
});

test("selection resolves to the Course identity the commands need", () => {
  const options = buildPublishedCourseOptions(ENGLISH, ARABIC);
  assert.equal(findPublishedCourse(options, COURSE_ID)?.id, COURSE_ID);
  assert.equal(findPublishedCourse(options, "not-a-selected-course"), undefined);
});

test("an invitation for a Course outside the published catalogue reports its stored identifier", () => {
  const options = buildPublishedCourseOptions(ENGLISH, ARABIC);
  assert.equal(invitationCourseLabel(options, COURSE_ID), "CS101: Introduction to Programming");
  // Invitations outlive catalogue visibility. The row must not invent a title
  // or silently blank the Course it belongs to.
  const retired = "c0000000-0000-0000-0000-0000000000ff";
  assert.equal(invitationCourseLabel(options, retired), retired);
});

test("no raw Course UUID input remains in the Course Access creation journey", () => {
  const page = readSource(PAGE);

  assert.ok(!page.includes("Course ID (UUID)"), "the pasted-UUID field must be gone");
  assert.ok(
    !/placeholder="[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}"/i.test(page),
    "no field may prompt for a Course identifier by example",
  );
  for (const removedState of ["expiryCourseId", "createCourseId"]) {
    assert.ok(!page.includes(removedState), `the typed Course identifier state ${removedState} must be gone`);
  }
});

test("one selected published Course carries into both access commands", () => {
  const page = readSource(PAGE);

  assert.match(page, /<PublishedCourseSelector/, "the journey must open with a Course selector");
  assert.match(page, /getPublicCourses/, "the selector must read the published catalogue");
  assert.match(
    page,
    /setCourseDefaultAccessExpiry\(\s*selectedCourseId,/,
    "default expiry must bind to the selected Course",
  );
  assert.match(
    page,
    /createCourseAccessInvitation\(\s*selectedCourseId,/,
    "invitation creation must send the selected Course",
  );
  // Both commands read the same state, so the two operations cannot address
  // different Courses.
  assert.equal(page.match(/useState<string>\(""\);/g)?.length !== undefined, true);
  assert.match(page, /if \(!selectedCourseId \|\| !expiryDate \|\| !expiryReason\) return;/);
  assert.match(page, /if \(!selectedCourseId \|\| !createEmail\) return;/);
  assert.match(page, /disabled=\{expirySubmitting \|\| !selectedCourseId\}/);
  assert.match(page, /disabled=\{createSubmitting \|\| !selectedCourseId\}/);
});

test("the journey confirms and lists Courses by title, not by identifier", () => {
  const page = readSource(PAGE);

  assert.match(page, /Default access expiry configured for \$\{selectedCourse\?\.title/);
  assert.match(page, /Course access invitation created for .*\n?.*selectedCourse\?\.title/s);
  assert.match(page, /courseLabel\(inv\.course_id\)/, "the queue must name the Course a row belongs to");
  assert.ok(!page.includes(">Course ID</th>"), "the queue must not head a column with a raw identifier");
});

test("a Course that leaves the published catalogue cannot stay silently selected", () => {
  const page = readSource(PAGE);
  assert.match(
    page,
    /setSelectedCourseId\(\(current\) =>[\s\S]{0,160}options\.some\(\(option\) => option\.id === current\)/,
    "a refreshed catalogue must drop a selection it no longer contains",
  );
  assert.match(page, /setCourseOptions\(\[\]\);[\s\S]{0,80}setCoursesError/, "a failed load must not keep stale options");
});

// AD07: the Admin reaches an existing grant from the queue row it came from,
// and manages it there. No entitlement, enrollment, Course, or Student
// identifier is ever typed.
test("an existing grant is reached from its queue row, not by identifier", () => {
  const page = readSource(PAGE);

  assert.match(page, /inv\.entitlement_id && \(/, "a granted invitation must offer its access record");
  assert.match(page, /handleViewEntitlement\(inv\.entitlement_id as string\)/);
  assert.match(page, /data-testid=\{`manage-access-\$\{inv\.id\}`\}/);
  // No operator form that asks for an identifier.
  for (const operatorInput of ["Entitlement ID", "Enrollment ID", "Student account ID", "Student ID (UUID)"]) {
    assert.ok(!page.includes(operatorInput), `the journey must not ask for ${operatorInput}`);
  }
});

test("the access record is inspected in human terms before it is changed", () => {
  const page = readSource(PAGE);

  assert.match(page, /courseLabel\(detailModal\.entitlement\.course_id\)/, "the record must name its Course");
  assert.match(page, /data-testid="entitlement-state"/, "current status must be visible");
  assert.match(page, /data-testid="entitlement-access-ends-at"/, "current expiry must be visible");
  assert.match(page, /Originally granted until/, "the original grant instant stays visible and read-only");
  assert.match(page, /Adjustment History/, "adjustment history must stay on the record");
  // `original_access_ends_at` is never editable by any actor in any slice.
  assert.ok(
    !/original_access_ends_at["']?\s*[:=]\s*(adjust|new|input)/i.test(page),
    "original_access_ends_at must never be an input",
  );
});

test("expiry adjustment and revocation are server calls carrying a reason and the observed revision", () => {
  const page = readSource(PAGE);

  assert.match(page, /adjustEntitlementExpiry\(/, "expiry changes must go to the server");
  assert.match(page, /revokeEntitlement\(/, "revocation must go to the server");
  assert.match(
    page,
    /expectedRevision: detailModal\.entitlement\.revision/,
    "both mutations must send the revision the Admin was looking at",
  );
  // Neither operation may be simulated in component state.
  assert.ok(
    !/setDetailModal\(\{[\s\S]{0,200}state: ["']REVOKED["']/.test(page),
    "revocation must not be faked client-side",
  );
  assert.match(page, /if \(updated\) setDetailModal\(updated\)/, "the server response is the new record");

  assert.match(page, /!adjustReason\.trim\(\)/, "an expiry change requires a reason");
  assert.match(page, /!revokeReason\.trim\(\)/, "a revocation requires a reason");
});

test("revocation is confirmed explicitly and reports what it does not delete", () => {
  const page = readSource(PAGE);

  assert.match(page, /data-testid="confirm-revoke-entitlement"/, "revoke needs an explicit confirmation");
  assert.match(
    page,
    /disabled=\{detailBusy \|\| !revokeConfirming \|\| !revokeReason\.trim\(\)\}/,
    "the revoke control stays disabled until confirmed with a reason",
  );
  assert.match(page, /learning progress and access history are kept/i, "the Admin must be told what survives");
  assert.match(page, /Enrollment and progress records are retained/, "the result must say the same");
});

test("a revoked grant offers no further mutation", () => {
  const page = readSource(PAGE);
  assert.match(
    page,
    /detailModal\.entitlement\.state === "ACTIVE" \? \(/,
    "the mutation forms must render only for an active grant",
  );
  assert.match(page, /data-testid="entitlement-terminal"/, "a revoked grant must explain its terminal state");
  assert.match(page, /data-testid="entitlement-error"/, "a refused mutation must be reported in place");
  assert.match(page, /role="alert"/, "a refused mutation must be announced");
});

test("the selector states every outcome the Admin can encounter", () => {
  const selector = readSource(SELECTOR);

  for (const state of [
    "course-access-courses-loading",
    "course-access-courses-error",
    "course-access-courses-empty",
    "course-access-course-select",
    "course-access-selected-course",
  ]) {
    assert.ok(selector.includes(state), `the selector must render the ${state} state`);
  }
  assert.match(selector, /Retry loading Courses/, "a failed Course read must be recoverable");
  assert.match(selector, /No published Courses are available yet/, "an empty catalogue must say so honestly");
  assert.match(selector, /role="alert"/, "a Course-list failure must be announced");
  // The failure and empty states are mutually exclusive with the select, so a
  // failed load can never present a stale or empty Course as choosable.
  assert.match(selector, /\{!loading && !error && options\.length > 0 && \(/);
});
