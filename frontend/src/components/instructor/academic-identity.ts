import type { OwnedCourseSummary } from "@/lib/api/catalog";

/**
 * A Course's academic identity, in the words a university uses for it.
 *
 * The server already sends this expanded on every owned-Course payload — institution name, subject
 * title, official code, and the units the subject hangs under, each in both languages. The studio
 * was not reading it. It held `institution_id` and `subject_id`, and to show the Instructor what
 * their own Course was for, it issued a second request per Course to re-fetch the Subject it had
 * already been given.
 *
 * Everything here is a plain read of that payload. A term the server did not send is omitted
 * rather than rendered blank or filled with a placeholder, because "we do not know which programme
 * this belongs to" and "this belongs to no programme" are different statements and only one of
 * them is true.
 */
export type AcademicIdentity = {
  institution: string;
  subject?: string;
  /** The university's own code for the subject, e.g. "CHEM 201". Latin even in Arabic copy. */
  subjectCode?: string;
  /** Department, faculty — whichever units the server expanded, nearest first. */
  units: string[];
};

function pick(locale: "ar" | "en", ar: string | undefined, en: string | undefined): string {
  const value = locale === "ar" ? ar : en;
  return value?.trim() ?? "";
}

export function academicIdentity(
  course: Pick<OwnedCourseSummary, "academic_context">,
  locale: "ar" | "en",
): AcademicIdentity | null {
  const context = course.academic_context;
  if (!context) return null;

  const institution = pick(locale, context.institution_name_ar, context.institution_name_en);
  if (!institution) return null;

  const subjectWire = context.subject;
  const subject = subjectWire
    ? pick(locale, subjectWire.title_ar, subjectWire.title_en)
    : "";

  // Nearest unit first: a subject sits in a department, which sits in a faculty. Reading outward
  // is how someone names their own course out loud.
  const units = subjectWire
    ? [
        pick(locale, subjectWire.owning_unit_name_ar, subjectWire.owning_unit_name_en),
        pick(locale, subjectWire.parent_unit_name_ar, subjectWire.parent_unit_name_en),
      ].filter(Boolean)
    : [];

  return {
    institution,
    subject: subject || undefined,
    subjectCode: subjectWire?.official_code?.trim() || undefined,
    units,
  };
}

/**
 * The one-line form, for a list row where a stacked hierarchy would not fit.
 *
 * Joined with a middle dot rather than a slash or an arrow: a slash reads as "either", and an
 * arrow reverses meaning between the two writing directions this product ships in.
 */
export function academicIdentitySummary(identity: AcademicIdentity | null): string {
  if (!identity) return "";
  return [identity.institution, identity.subject].filter(Boolean).join(" · ");
}
