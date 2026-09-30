import assert from "node:assert/strict";
import test from "node:test";

import { authoringPlan } from "./authoring-plan";
import { mediaGuidance, pendingReadiness } from "./course-builder-guidance-state";
import type { OwnedCourseSummary } from "../../lib/api/catalog";

const position = (kind: "section" | "lesson", index: number) => `${kind}-${index}`;

function course(overrides: {
  sections?: { lessons: { video?: boolean; state?: string }[] }[];
} = {}): OwnedCourseSummary {
  return {
    id: "course-1",
    classification_model: "ACADEMIC_CATALOG",
    institution_id: "institution-1",
    subject_id: "subject-1",
    editable_revision: {
      id: "revision-1",
      title_ar: "دورة",
      title_en: "Course",
      sections: (overrides.sections ?? [{ lessons: [{ video: true }] }]).map((section, sectionIndex) => ({
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
          video_asset_state: lesson.state,
        })),
      })),
    },
  } as unknown as OwnedCourseSummary;
}

test("builder guidance names only unmet submission requirements", () => {
  const plan = authoringPlan(course({ sections: [{ lessons: [{ video: false }] }] }), "en", position);
  assert.deepEqual(pendingReadiness(plan).map((requirement) => requirement.key), ["LESSON_VIDEOS"]);
});

test("builder guidance prioritizes failed media over background processing", () => {
  const attentionPlan = authoringPlan(
    course({ sections: [{ lessons: [{ video: true, state: "PROCESS_FAILED" }] }] }),
    "en",
    position,
  );
  const processingPlan = authoringPlan(
    course({ sections: [{ lessons: [{ video: true, state: "PROCESSING" }] }] }),
    "en",
    position,
  );
  assert.equal(mediaGuidance(attentionPlan), "ATTENTION");
  assert.equal(mediaGuidance(processingPlan), "PROCESSING");
});
