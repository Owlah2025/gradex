import { authenticatedRequest } from "./http";

/**
 * Instructor academic reads for Subject-first Course authoring (D-091 §9, T4-B).
 *
 * These call the Instructor's own `/authoring/academic/*` projection, which sits
 * behind content-management authority. They are read-only by construction: this
 * module deliberately exposes no way to create, edit, retire, or map a canonical
 * Subject, because an Instructor selects from the Admin-owned catalog and never
 * invents one.
 *
 * Nothing here hardcodes a university. Kuwait University is the only launch
 * Institution, but it arrives as data.
 */

export type InstitutionOption = {
  id: string;
  name_ar: string;
  name_en: string;
  country_code: string;
};

export type SubjectProgramAssociation = {
  program_id: string;
  name_ar: string;
  name_en: string;
  /**
   * Present only where the Academic Catalog actually carries placement. Kuwait
   * University publishes a suggested study plan for Computer Science and Data
   * Science & AI but not for the other launch Programs, so these are frequently
   * absent and must never be filled in from a course number.
   */
  recommended_level?: number;
  recommended_semester?: number;
};

export type AuthoringSubject = {
  id: string;
  official_code?: string;
  title_ar: string;
  title_en: string;
  unit_name_ar?: string;
  unit_name_en?: string;
  college_name_ar?: string;
  college_name_en?: string;
  /** The inferred audience. An empty array is a truthful answer, not an error. */
  programs: SubjectProgramAssociation[];
};

const base = "/authoring/academic";

export async function listAuthoringInstitutions(locale: "ar" | "en"): Promise<InstitutionOption[]> {
  const result = await authenticatedRequest<InstitutionOption[]>(`${base}/institutions`, "GET", locale);
  return result ?? [];
}

export async function searchAuthoringSubjects(input: {
  institutionID: string;
  query: string;
  locale: "ar" | "en";
}): Promise<AuthoringSubject[]> {
  const result = await authenticatedRequest<AuthoringSubject[]>(
    `${base}/institutions/${encodeURIComponent(input.institutionID)}/subjects?q=${encodeURIComponent(input.query)}`,
    "GET",
    input.locale,
  );
  return result ?? [];
}

export async function getAuthoringSubject(input: {
  institutionID: string;
  subjectID: string;
  locale: "ar" | "en";
}): Promise<AuthoringSubject | null> {
  return authenticatedRequest<AuthoringSubject>(
    `${base}/institutions/${encodeURIComponent(input.institutionID)}/subjects/${encodeURIComponent(input.subjectID)}`,
    "GET",
    input.locale,
  );
}

/** Localized Subject title. The official code is language-neutral. */
export function subjectTitle(subject: AuthoringSubject, locale: "ar" | "en"): string {
  return locale === "ar" ? subject.title_ar : subject.title_en;
}

/**
 * The Subject as a human reads it: `0418-320 · Principles of Computer Systems`.
 * Never an identifier — an Instructor should not have to recognise a UUID.
 */
export function subjectLabel(subject: AuthoringSubject, locale: "ar" | "en"): string {
  const title = subjectTitle(subject, locale);
  return subject.official_code ? `${subject.official_code} · ${title}` : title;
}

/** Department · College, where the catalog knows them. Descriptive only. */
export function subjectContext(subject: AuthoringSubject, locale: "ar" | "en"): string {
  const unit = locale === "ar" ? subject.unit_name_ar : subject.unit_name_en;
  const college = locale === "ar" ? subject.college_name_ar : subject.college_name_en;
  return [unit, college].filter(Boolean).join(" · ");
}

export function programName(program: SubjectProgramAssociation, locale: "ar" | "en"): string {
  return locale === "ar" ? program.name_ar : program.name_en;
}
