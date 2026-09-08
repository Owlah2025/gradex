import type { CourseRevisionWire, SectionWire } from "@/lib/api/catalog";
import type { CourseWire } from "@/lib/api/authoring";

export function moveIdentity(ids: string[], activeID: string, overID: string): string[] {
  const from = ids.indexOf(activeID);
  const to = ids.indexOf(overID);
  if (from < 0 || to < 0 || from === to) return ids;
  const next = [...ids];
  const [moved] = next.splice(from, 1);
  next.splice(to, 0, moved);
  return next;
}

export function orderSections(sections: SectionWire[], ids: string[]): SectionWire[] {
  const byID = new Map(sections.map((section) => [section.id, section]));
  return ids.map((id, position) => ({ ...byID.get(id)!, position }));
}

export function orderLessons(section: SectionWire, ids: string[]): SectionWire {
  const lessons = section.lessons ?? [];
  const byID = new Map(lessons.map((lesson) => [lesson.id, lesson]));
  return {
    ...section,
    lessons: ids.map((id, position) => ({ ...byID.get(id)!, position })),
  };
}

export function replaceEditableRevision(
  courses: CourseWire[],
  courseID: string,
  revision: CourseRevisionWire,
): CourseWire[] {
  return courses.map((course) =>
    course.id === courseID && course.editable_revision?.id === revision.id
      ? { ...course, editable_revision: revision }
      : course,
  );
}

export async function persistOptimisticOrder<T>({
  before,
  optimistic,
  apply,
  persist,
}: {
  before: T;
  optimistic: T;
  apply: (revision: T) => void;
  persist: () => Promise<T>;
}): Promise<void> {
  apply(optimistic);
  try {
    apply(await persist());
  } catch (cause) {
    apply(before);
    throw cause;
  }
}
