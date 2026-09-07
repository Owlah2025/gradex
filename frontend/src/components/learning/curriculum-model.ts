import type { CourseHomeSection, LearningProgress } from "@/lib/api/learning";

/**
 * The Student's view of a Course's contents, and nothing else.
 *
 * # WHY A SEPARATE MODEL
 *
 * `CourseHome` also carries the opaque report context (D-065), and the Lesson surfaces now render
 * the same contents beside the Lesson itself. Handing either surface the whole read model would
 * publish the whole read model — the defect `learning-label-sets.ts` documents for copy, applied
 * here to data. So the pages narrow once, on the server, and every curriculum surface consumes this
 * shape instead.
 *
 * # WHAT IS AND IS NOT DERIVED
 *
 * Completion is the server's, read verbatim from `progress.completed`. A Lesson counts as *started*
 * only when the server has persisted a playback position for it: "opened once" is not a state this
 * product records, so it is not a state this model invents.
 *
 * Section counts are arithmetic over those same server flags, not a second opinion about progress.
 * The Course-level figure always comes from `CourseHome.progress`, which the server computes over
 * the whole qualifying graph — deriving a course percentage here as well is exactly how two
 * disagreeing progress numbers end up on one screen.
 */
export type CurriculumLessonState = "completed" | "in-progress" | "not-started";

export type CurriculumLesson = {
  lessonID: string;
  title: string;
  state: CurriculumLessonState;
  /** Downloadable items attached to this Lesson, counted so the row can say they exist. */
  materialCount: number;
};

export type CurriculumSection = {
  sectionID: string;
  title: string;
  lessons: CurriculumLesson[];
  completedLessons: number;
  totalLessons: number;
};

export function lessonState(progress: LearningProgress): CurriculumLessonState {
  if (progress.completed) return "completed";
  return progress.position_seconds > 0 ? "in-progress" : "not-started";
}

/**
 * Narrows the Course Home sections to what a curriculum renders.
 *
 * Server order is preserved exactly. The read model is already ordered by the authored section and
 * lesson positions, and re-sorting here would mean the outline could disagree with the previous /
 * next pointers the server issues for the same graph.
 */
export function courseCurriculum(sections: CourseHomeSection[]): CurriculumSection[] {
  return sections.map((section) => {
    const lessons: CurriculumLesson[] = section.lessons.map((lesson) => ({
      lessonID: lesson.lesson_id,
      title: lesson.title,
      state: lessonState(lesson.progress),
      materialCount: lesson.resources.length + lesson.lab_materials.length,
    }));
    return {
      sectionID: section.section_id,
      title: section.title,
      lessons,
      completedLessons: lessons.filter((lesson) => lesson.state === "completed").length,
      totalLessons: lessons.length,
    };
  });
}

/**
 * The sections a curriculum opens with.
 *
 * A short Course opens whole: collapsing three sections hides the entire Course to save nothing. A
 * long one opens only where the Student is, plus the first section when they are nowhere in
 * particular, so a forty-lesson Course arrives scannable rather than as one unbroken column.
 *
 * The section holding the current Lesson is always open, whatever the length — a curriculum that
 * hides where the reader is standing is worse than no curriculum.
 */
export const curriculumFullyOpenLimit = 8;

export function initiallyOpenSections(
  sections: CurriculumSection[],
  currentLessonID?: string | null,
): string[] {
  if (sections.length === 0) return [];
  if (sections.length <= curriculumFullyOpenLimit) {
    return sections.map((section) => section.sectionID);
  }
  const current = currentLessonID
    ? sections.find((section) => section.lessons.some((lesson) => lesson.lessonID === currentLessonID))
    : undefined;
  return [(current ?? sections[0]).sectionID];
}

/** True once every Lesson the server counts is complete, and only then. */
export function courseIsComplete(progress: {
  completed_lessons: number;
  total_lessons: number;
}): boolean {
  return progress.total_lessons > 0 && progress.completed_lessons >= progress.total_lessons;
}

/**
 * Where a Student re-entering a Course should land.
 *
 * # WHY THIS IS NOT A NEW PROGRESS SEMANTIC
 *
 * It invents nothing. It reads the server's own ordering — sections by authored position, Lessons
 * by authored position, exactly as the read model delivers them — and the server's own per-Lesson
 * flags, and returns one identifier. Nothing is persisted, nothing is claimed about the Student,
 * and no Lesson is reachable through this that a Student could not already reach by clicking the
 * same row in the contents. It is a choice of first click, not a statement of fact.
 *
 * The order of preference is the order a person would use: the Lesson already part-watched, then
 * the first one not yet finished, then the first one there is. A finished Course therefore reopens
 * at its beginning rather than at a dead end past its end.
 */
export function resumeLessonID(sections: CurriculumSection[]): string | null {
  const lessons = sections.flatMap((section) => section.lessons);
  if (lessons.length === 0) return null;
  const started = lessons.find((lesson) => lesson.state === "in-progress");
  if (started) return started.lessonID;
  const unfinished = lessons.find((lesson) => lesson.state !== "completed");
  return (unfinished ?? lessons[0]).lessonID;
}

/** True when the Student has begun this Course at all, which decides how the entry control reads. */
export function courseIsStarted(sections: CurriculumSection[]): boolean {
  return sections.some((section) =>
    section.lessons.some((lesson) => lesson.state !== "not-started"),
  );
}
