import type { PublicCourse } from "@/lib/api/public-catalog";

/**
 * Course identity as an Admin recognises it.
 *
 * The Course Access journey used to require a pasted Course UUID. The launch
 * journey selects a Course by title instead, and the identifier travels
 * internally from here into the expiry and invitation commands.
 */
export type PublishedCourseOption = {
  id: string;
  title: string;
  /** The same Course's title in the other locale, when the catalogue has one. */
  alternateTitle?: string;
  instructor: string;
  subject?: string;
  studyYear?: string;
};

/**
 * Merges one published-catalogue read per locale into a single option list.
 *
 * `primary` is authoritative: it decides which Courses exist and in what
 * order. `alternate` only contributes the other-locale title, so a failed or
 * partial alternate read degrades to a single-language label rather than
 * hiding a Course the Admin must be able to grant.
 */
export function buildPublishedCourseOptions(
  primary: PublicCourse[],
  alternate: PublicCourse[] = [],
): PublishedCourseOption[] {
  const alternateTitles = new Map(alternate.map((course) => [course.id, course.title]));
  return primary.map((course) => {
    const other = alternateTitles.get(course.id);
    return {
      id: course.id,
      title: course.title,
      alternateTitle: other && other !== course.title ? other : undefined,
      instructor: course.instructor_display_name,
      subject: course.subject?.label,
      studyYear: course.study_year?.label,
    };
  });
}

/** The human-readable option text. The Course UUID is never part of it. */
export function publishedCourseOptionLabel(option: PublishedCourseOption): string {
  const titles = option.alternateTitle ? `${option.title} · ${option.alternateTitle}` : option.title;
  const qualifiers = [option.instructor, option.subject, option.studyYear].filter(Boolean);
  return qualifiers.length === 0 ? titles : `${titles} — ${qualifiers.join(" · ")}`;
}

export function findPublishedCourse(
  options: PublishedCourseOption[],
  courseID: string,
): PublishedCourseOption | undefined {
  return options.find((option) => option.id === courseID);
}

/**
 * The Course label shown for an existing invitation.
 *
 * Invitations outlive catalogue visibility, so this has to answer for a Course that is no longer
 * listed. It used to answer with the stored identifier, on the reasoning that the row must neither
 * invent a title nor silently blank the Course it belongs to — which is right, but the identifier
 * is not the third option. A raw UUID in the Course column is the leak the rest of this workspace
 * removed, and it is unreadable to the Administrator it was shown to.
 *
 * The caller supplies the words instead. "No longer listed" says the true thing — the invitation is
 * real and its Course is not in the catalogue — without guessing a title and without going blank.
 */
export function invitationCourseLabel(
  options: PublishedCourseOption[],
  courseID: string,
  unlistedLabel: string,
): string {
  return findPublishedCourse(options, courseID)?.title ?? unlistedLabel;
}
