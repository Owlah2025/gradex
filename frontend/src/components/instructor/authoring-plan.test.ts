import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import type { OwnedCourseSummary } from "../../lib/api/catalog";
import {
  AUTHORING_SECTION_ORDER,
  authoringPlan,
  sectionAfter,
  type AuthoringSectionKey,
} from "./authoring-plan";

const position = (kind: "section" | "lesson", index: number) =>
  kind === "section" ? `Section ${index}` : `Lesson ${index}`;

function course(overrides: {
  titleAr?: string;
  titleEn?: string;
  subject?: boolean;
  preview?: boolean;
  previewState?: string;
  sections?: { lessons: { video?: boolean }[] }[];
}): OwnedCourseSummary {
  return {
    id: "course-1",
    classification_model: "ACADEMIC_CATALOG",
    institution_id: "institution-1",
    subject_id: overrides.subject === false ? undefined : "subject-1",
    editable_revision: {
      id: "revision-1",
      title_ar: overrides.titleAr ?? "عنوان",
      title_en: overrides.titleEn ?? "Title",
      preview_asset_version_id: overrides.preview === false ? undefined : "preview-1",
      preview_asset_state: overrides.previewState,
      sections: (overrides.sections ?? [{ lessons: [{ video: true }] }]).map(
        (section, sectionIndex) => ({
          id: `section-${sectionIndex}`,
          title_ar: `قسم ${sectionIndex}`,
          title_en: `Section ${sectionIndex}`,
          position: sectionIndex,
          lessons: section.lessons.map((lesson, lessonIndex) => ({
            id: `lesson-${sectionIndex}-${lessonIndex}`,
            title_ar: `درس ${lessonIndex}`,
            title_en: `Lesson ${lessonIndex}`,
            position: lessonIndex,
            video_asset_version_id: lesson.video === false ? undefined : "video-1",
          })),
        }),
      ),
    },
  } as unknown as OwnedCourseSummary;
}

function stateOf(plan: ReturnType<typeof authoringPlan>, key: AuthoringSectionKey) {
  return plan.sections.find((section) => section.key === key)?.state;
}

test("a course meeting every client-checkable requirement is complete throughout", () => {
  const plan = authoringPlan(course({}), "en", position);
  for (const key of AUTHORING_SECTION_ORDER) {
    assert.equal(stateOf(plan, key), "COMPLETE", `${key} is not complete on a finished course`);
  }
  assert.equal(plan.completeCount, plan.totalCount);
  assert.equal(plan.totalCount, 4, "review is being counted as a workable section");
  assert.equal(plan.ready, true);
  assert.equal(plan.nextSection, "REVIEW");
});

/**
 * The disclosure state has to come from what the server holds, not from what the reader has looked
 * at. Each case below removes exactly one persisted fact and asserts that exactly the section
 * owning it stops being complete.
 */
test("each unmet requirement is owned by the section holding its controls", () => {
  const cases: { name: string; course: OwnedCourseSummary; section: AuthoringSectionKey }[] = [
    { name: "a missing title", course: course({ titleEn: "  " }), section: "BASICS" },
    { name: "a missing subject", course: course({ subject: false }), section: "DETAILS" },
    { name: "a missing preview", course: course({ preview: false }), section: "PREVIEW" },
    {
      name: "a lesson with no video",
      course: course({ sections: [{ lessons: [{ video: false }] }] }),
      section: "CURRICULUM",
    },
    { name: "no sections at all", course: course({ sections: [] }), section: "CURRICULUM" },
    {
      name: "a section with no lessons",
      course: course({ sections: [{ lessons: [] }] }),
      section: "CURRICULUM",
    },
  ];

  for (const scenario of cases) {
    const plan = authoringPlan(scenario.course, "en", position);
    assert.notEqual(
      stateOf(plan, scenario.section),
      "COMPLETE",
      `${scenario.name} left ${scenario.section} looking finished`,
    );
    for (const key of AUTHORING_SECTION_ORDER) {
      if (key === scenario.section || key === "REVIEW") continue;
      assert.equal(
        stateOf(plan, key),
        "COMPLETE",
        `${scenario.name} was blamed on ${key} as well`,
      );
    }
  }
});

// A collapsed section that is quietly holding unmet requirements is the failure this pattern
// invites. The count has to be on the header, so it has to be on the plan.
test("a section carries its own outstanding requirements so a closed header can count them", () => {
  const plan = authoringPlan(
    course({ sections: [{ lessons: [] }, { lessons: [{ video: false }] }] }),
    "en",
    position,
  );
  const curriculum = plan.sections.find((section) => section.key === "CURRICULUM");
  assert.ok(curriculum);
  assert.equal(curriculum!.outstanding.length, 2, "the empty section and the missing video");
  assert.deepEqual(
    curriculum!.outstanding.map((requirement) => requirement.key).sort(),
    ["LESSON_VIDEOS", "SECTION_LESSONS"],
  );
  // The offenders come through by title, which is how the reader finds them.
  assert.ok(
    curriculum!.outstanding.some((requirement) => requirement.offenders.length > 0),
    "no outstanding requirement names what failed it",
  );
});

test("a thumbnail that will not resolve is attention, not unstarted work", () => {
  const blocked = authoringPlan(course({}), "en", position, { thumbnailUnresolved: true });
  assert.equal(stateOf(blocked, "PREVIEW"), "ATTENTION");

  const failedPreview = authoringPlan(course({ previewState: "FAILED" }), "en", position);
  assert.equal(stateOf(failedPreview, "PREVIEW"), "ATTENTION");

  // And it is distinguishable from having simply not uploaded one yet.
  const missing = authoringPlan(course({ preview: false }), "en", position);
  assert.equal(stateOf(missing, "PREVIEW"), "INCOMPLETE");
});

test("the workflow opens on the first unfinished section", () => {
  assert.equal(authoringPlan(course({ titleAr: "" }), "en", position).nextSection, "BASICS");
  assert.equal(authoringPlan(course({ subject: false }), "en", position).nextSection, "DETAILS");
  assert.equal(authoringPlan(course({ preview: false }), "en", position).nextSection, "PREVIEW");
  assert.equal(
    authoringPlan(course({ sections: [] }), "en", position).nextSection,
    "CURRICULUM",
  );
});

/**
 * Progression moves forward from where the reader is, and never sends them back through a section
 * they have already passed. An instructor correcting one title on a finished course would otherwise
 * be walked from the top of the workflow again on every save.
 */
test("advancing goes to the next outstanding section after the one being left", () => {
  const finished = authoringPlan(course({}), "en", position);
  assert.equal(sectionAfter(finished, "BASICS"), "REVIEW", "a finished course ends at review");

  const noCurriculum = authoringPlan(course({ sections: [] }), "en", position);
  assert.equal(sectionAfter(noCurriculum, "BASICS"), "CURRICULUM");
  assert.equal(sectionAfter(noCurriculum, "CURRICULUM"), "REVIEW");

  const noSubject = authoringPlan(course({ subject: false }), "en", position);
  assert.equal(sectionAfter(noSubject, "BASICS"), "DETAILS");
  // Already past it: the outstanding DETAILS section is not reopened by leaving PREVIEW.
  assert.equal(sectionAfter(noSubject, "PREVIEW"), "REVIEW");
});

/* --------------------------------------------------- properties of the shell */

function readInstructorSource(file: string): string {
  const frontend = process.cwd().endsWith("/frontend")
    ? process.cwd()
    : path.join(process.cwd(), "frontend");
  return fs.readFileSync(
    path.join(frontend, "src", "components", "instructor", file),
    "utf8",
  );
}

/**
 * The two behaviours that make this a workflow rather than a wizard, asserted where they are
 * decided rather than through a browser: more than one section may be open, and a section closes
 * only in response to an explicit progression.
 */
test("the workflow is a disclosure, not a locked wizard", () => {
  const shell = readInstructorSource("authoring-workflow.tsx");
  assert.match(shell, /type="multiple"/, "only one authoring section can be open at a time");
  assert.ok(
    !/disabled=\{[^}]*state[^}]*\}/.test(shell),
    "the workflow disables a section by its completion state",
  );
  // Collapse happens through `advance`, which is only reachable from the two explicit progression
  // components — never from a change, blur or focus handler.
  assert.ok(
    !/onBlur|onFocus|onChange/.test(shell),
    "the workflow reacts to focus or field changes",
  );
  const advanceCallers = [...shell.matchAll(/advance\(section\)/g)];
  assert.equal(advanceCallers.length, 2, "progression is raised from somewhere unexpected");
});

test("the studio advances only after the server has accepted the save", () => {
  const builder = readInstructorSource("course-builder.tsx");
  const handler = builder.slice(
    builder.indexOf("const handleSaveRevision"),
    builder.indexOf("const handleAddSection"),
  );
  const accepted = handler.indexOf("await updateCourseRevision");
  const advanced = handler.indexOf("setBasicsAdvanceToken");
  assert.ok(accepted >= 0 && advanced > accepted, "the studio advances before the server replies");
  assert.ok(
    handler.includes('setDetailsOutcome("FAILED")'),
    "a refused save is not reported as one",
  );
  // "Saved" is never asserted from anywhere but the resolved call.
  const savedAssignments = [...builder.matchAll(/setDetailsOutcome\("SAVED"\)/g)];
  assert.equal(savedAssignments.length, 1, "the studio claims a save from more than one place");
});

test("the studio warns before losing genuinely unsaved course details", () => {
  const builder = readInstructorSource("course-builder.tsx");
  assert.match(builder, /const detailsDirty =/, "unsaved work is not derived from the server copy");
  assert.match(builder, /addEventListener\("beforeunload", warn\)/);
  assert.ok(
    !/localStorage|sessionStorage/.test(builder),
    "authored course content is being cached in the browser",
  );
});

// Feature #7 will add reordering. Nothing here may pretend it already exists.
test("no reordering affordance is offered before reordering exists", () => {
  for (const file of ["authoring-workflow.tsx", "course-builder.tsx", "curriculum-builder.tsx"]) {
    const source = readInstructorSource(file);
    assert.ok(
      !/dragg?able|onDragStart|GripVertical|moveSection|moveLesson|reorder/i.test(source),
      `${file} offers a reordering affordance that has no server behind it`,
    );
  }
});
