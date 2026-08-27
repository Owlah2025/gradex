import assert from "node:assert/strict";
import test from "node:test";

import type { OwnedCourseSummary } from "@/lib/api/catalog";
import { courseDisplayTitle, courseStanding, standingTone } from "./course-standing";
import { academicIdentity, academicIdentitySummary } from "./academic-identity";

const revision = (state: string): OwnedCourseSummary["editable_revision"] => ({
  id: "rev-1",
  state,
  title_ar: "مقدمة في الكيمياء",
  title_en: "Introduction to Chemistry",
  sections: [],
});

test("a never-published draft is the Instructor's to move", () => {
  const standing = courseStanding({ id: "c1", editable_revision: revision("DRAFT") });
  assert.equal(standing.stage, "DRAFT");
  assert.equal(standing.actor, "INSTRUCTOR");
  assert.equal(standing.editable, true);
  assert.equal(standing.liveForStudents, false);
});

test("a draft behind a published revision is a draft update, and Students are unaffected", () => {
  // The distinction the old single amber pill could not make: both are DRAFT on the wire, but only
  // one of them has Students currently reading something.
  const standing = courseStanding({
    id: "c1",
    live_revision_id: "rev-live",
    editable_revision: revision("DRAFT"),
  });
  assert.equal(standing.stage, "DRAFT_UPDATE");
  assert.equal(standing.actor, "INSTRUCTOR");
  assert.equal(standing.editable, true);
  assert.equal(standing.liveForStudents, true);
});

test("a submitted revision is with the Admin and is not editable", () => {
  const standing = courseStanding({ id: "c1", editable_revision: revision("PENDING_REVIEW") });
  assert.equal(standing.stage, "IN_REVIEW");
  assert.equal(standing.actor, "ADMIN");
  assert.equal(standing.editable, false);
});

test("both return paths come back to the Instructor as changes requested", () => {
  // CHANGES_REQUESTED is the first-publish path; REJECTED is the published-Course revision path.
  for (const state of ["CHANGES_REQUESTED", "REJECTED"]) {
    const standing = courseStanding({ id: "c1", editable_revision: revision(state) });
    assert.equal(standing.stage, "CHANGES_REQUESTED", state);
    assert.equal(standing.actor, "INSTRUCTOR", state);
    assert.equal(standing.editable, true, state);
  }
});

test("a published Course with no open revision requires nothing", () => {
  const standing = courseStanding({ id: "c1", live_revision_id: "rev-live", lifecycle: "PUBLISHED" });
  assert.equal(standing.stage, "PUBLISHED");
  assert.equal(standing.actor, "NOBODY");
  assert.equal(standing.editable, false);
  assert.equal(standing.liveForStudents, true);
});

test("live_revision_id alone is enough — the list payload never expands the graph", () => {
  // ListOwnedCourses returns the id but not the expanded revision. Reading the graph would report
  // every Course in the directory as never published.
  assert.equal(courseStanding({ id: "c1", live_revision_id: "rev-live" }).liveForStudents, true);
  assert.equal(
    courseStanding({ id: "c1", live_revision: { id: "rev-live", title_ar: "", title_en: "", sections: [] } })
      .liveForStudents,
    true,
  );
});

test("an approved or superseded candidate on a published Course reports the Course, not the candidate", () => {
  for (const state of ["APPROVED", "SUPERSEDED"]) {
    const standing = courseStanding({
      id: "c1",
      live_revision_id: "rev-live",
      editable_revision: revision(state),
    });
    assert.equal(standing.stage, "PUBLISHED", state);
    assert.equal(standing.editable, false, state);
  }
});

test("nothing open and nothing live offers no action", () => {
  assert.equal(courseStanding({ id: "c1" }).stage, "UNAVAILABLE");
  assert.equal(courseStanding(null).stage, "UNAVAILABLE");
  assert.equal(courseStanding(undefined).actor, "NOBODY");
});

test("the wire enum is carried for support but is never a stage", () => {
  const standing = courseStanding({ id: "c1", editable_revision: revision("REJECTED") });
  assert.equal(standing.wire, "REJECTED");
  assert.notEqual(standing.stage, standing.wire);
});

/**
 * This assertion used to say the opposite: that no stage may reach for the success tone, because
 * `gx-success` measured 4.39:1 on white and 3.94:1 on its own soft ground and this surface refused
 * to add a new instance of a known defect. The token has since been split — the pill's text is
 * `gx-success-strong` at 4.85:1 — and the AA guarantee is proved arithmetically in
 * `design-tokens.test.ts` rather than avoided here. So the mapping is now free to say what the
 * product means, and this test pins that meaning instead of the workaround.
 */
test("a standing's tone says where the Course is in its journey", () => {
  // Published is the end of the journey, and the only stage that is an achievement.
  assert.equal(standingTone("PUBLISHED"), "success");
  // Changes requested is the one stage that asks the Instructor for something.
  assert.equal(standingTone("CHANGES_REQUESTED"), "accent");
  // Everything still in flight is decoration, distinguished by the actor line beside it.
  for (const stage of ["DRAFT", "DRAFT_UPDATE", "IN_REVIEW", "UNAVAILABLE"] as const) {
    assert.equal(standingTone(stage), "neutral", stage);
  }
});

test("a Course title is never a UUID", () => {
  const course: OwnedCourseSummary = { id: "8f14e45f-ceea-467a-9a3f-6d4a1f3f0000" };
  assert.equal(courseDisplayTitle(course, "en", "Untitled course"), "Untitled course");
  assert.equal(courseDisplayTitle(course, "ar", "مقرر بلا عنوان"), "مقرر بلا عنوان");
});

test("a blank title falls through to the untitled label rather than rendering empty", () => {
  const course: OwnedCourseSummary = {
    id: "c1",
    editable_revision: { id: "r", title_ar: "   ", title_en: "   ", sections: [] },
  };
  assert.equal(courseDisplayTitle(course, "en", "Untitled course"), "Untitled course");
});

test("the editable revision's title wins over the live one — it is what is being worked on", () => {
  const course: OwnedCourseSummary = {
    id: "c1",
    editable_revision: { id: "r2", title_ar: "الجديد", title_en: "New title", sections: [] },
    live_revision: { id: "r1", title_ar: "القديم", title_en: "Old title", sections: [] },
  };
  assert.equal(courseDisplayTitle(course, "en", "Untitled"), "New title");
  assert.equal(courseDisplayTitle(course, "ar", "بلا عنوان"), "الجديد");
});

test("academic identity is read from the payload the server already sends", () => {
  const identity = academicIdentity(
    {
      academic_context: {
        institution_name_ar: "جامعة الكويت",
        institution_name_en: "Kuwait University",
        subject: {
          official_code: "CHEM 201",
          title_ar: "الكيمياء العضوية",
          title_en: "Organic Chemistry",
          owning_unit_name_ar: "قسم الكيمياء",
          owning_unit_name_en: "Department of Chemistry",
          parent_unit_name_ar: "كلية العلوم",
          parent_unit_name_en: "Faculty of Science",
        },
      },
    },
    "en",
  );
  assert.equal(identity?.institution, "Kuwait University");
  assert.equal(identity?.subject, "Organic Chemistry");
  assert.equal(identity?.subjectCode, "CHEM 201");
  // Nearest unit first: department, then faculty.
  assert.deepEqual(identity?.units, ["Department of Chemistry", "Faculty of Science"]);
});

test("units the server did not expand are omitted, not blanked", () => {
  const identity = academicIdentity(
    {
      academic_context: {
        institution_name_ar: "جامعة الكويت",
        institution_name_en: "Kuwait University",
        subject: { title_ar: "الكيمياء", title_en: "Chemistry" },
      },
    },
    "en",
  );
  assert.deepEqual(identity?.units, []);
  assert.equal(identity?.subjectCode, undefined);
});

test("a Course with no academic context reports none rather than a half-built identity", () => {
  assert.equal(academicIdentity({}, "en"), null);
  assert.equal(academicIdentitySummary(null), "");
});

test("a subject-less academic draft still names its university", () => {
  // T4-D: a Course may be created against a university while its Subject request is pending.
  const identity = academicIdentity(
    {
      academic_context: {
        institution_name_ar: "جامعة الكويت",
        institution_name_en: "Kuwait University",
      },
    },
    "en",
  );
  assert.equal(identity?.institution, "Kuwait University");
  assert.equal(identity?.subject, undefined);
  assert.equal(academicIdentitySummary(identity), "Kuwait University");
});
