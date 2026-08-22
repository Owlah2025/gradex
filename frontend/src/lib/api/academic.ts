import { authenticatedRequest } from "./http";
import { ProblemError } from "./problem";

/**
 * Admin Academic Catalog client (D-091, T1).
 *
 * Admin-only. There is deliberately no public or Instructor entry point here:
 * Student onboarding (T3), Instructor Subject selection (T4), and public
 * catalogue filters (T6) each own their own surface and are not opened early.
 */

export type AcademicUnitKind = "COLLEGE" | "DEPARTMENT" | "SERVICE_UNIT";

export type CurriculumStatus = "ACTIVE" | "SUPERSEDED";

export type RequirementKind =
  | "UNIVERSITY_REQUIREMENT"
  | "COLLEGE_REQUIREMENT"
  | "MAJOR_CORE"
  | "MAJOR_ELECTIVE"
  | "SUPPORTING"
  | "FREE_ELECTIVE";

export type Institution = {
  id: string;
  country_code: string;
  slug: string;
  name_ar: string;
  name_en: string;
  max_academic_level: number;
  has_foundation_stage: boolean;
  retired_at?: string | null;
};

export type AcademicUnit = {
  id: string;
  institution_id: string;
  parent_unit_id?: string | null;
  kind: AcademicUnitKind;
  slug: string;
  name_ar: string;
  name_en: string;
  retired_at?: string | null;
};

export type Program = {
  id: string;
  institution_id: string;
  owning_unit_id?: string | null;
  slug: string;
  name_ar: string;
  name_en: string;
  degree_kind: string;
  retired_at?: string | null;
};

export type Curriculum = {
  id: string;
  program_id: string;
  institution_id: string;
  version_label: string;
  effective_from_year?: number | null;
  status: CurriculumStatus;
  retired_at?: string | null;
};

export type Subject = {
  id: string;
  institution_id: string;
  owning_unit_id?: string | null;
  official_code?: string | null;
  title_ar: string;
  title_en: string;
  retired_at?: string | null;
};

export type CurriculumSubject = {
  id: string;
  curriculum_id: string;
  subject_id: string;
  requirement_kind: RequirementKind;
  recommended_level?: number | null;
  recommended_semester?: number | null;
  credits?: number | null;
  subject_official_code?: string | null;
  subject_title_ar?: string;
  subject_title_en?: string;
};

/**
 * A refused duplicate Subject carries the Subject it collided with, so the
 * surface can offer the existing row instead of reporting an opaque failure.
 */
export type DuplicateSubjectConflict = {
  code: "SUBJECT_ALREADY_EXISTS";
  existing_subject: Subject;
};

export function duplicateSubjectConflict(error: unknown): Subject | null {
  if (!(error instanceof ProblemError)) return null;
  const body = error.problem as unknown as Partial<DuplicateSubjectConflict>;
  if (body?.code !== "SUBJECT_ALREADY_EXISTS") return null;
  return body.existing_subject ?? null;
}

type Auth = { locale: "ar" | "en"; csrf: string };

// authenticatedRequest already prefixes /api/v1, so this path must not repeat
// it. Doubling the prefix produced /api/v1/api/v1/... and 404ed every call.
const base = "/admin/academic";

export function listInstitutions(locale: "ar" | "en") {
  return authenticatedRequest<Institution[]>(`${base}/institutions`, "GET", locale) as Promise<Institution[]>;
}

export function createInstitution(
  input: Auth & {
    countryCode: string;
    slug: string;
    nameAr: string;
    nameEn: string;
    maxAcademicLevel: number;
    hasFoundationStage: boolean;
  },
) {
  return authenticatedRequest<Institution>(`${base}/institutions`, "POST", input.locale, input.csrf, {
    country_code: input.countryCode,
    slug: input.slug,
    name_ar: input.nameAr,
    name_en: input.nameEn,
    max_academic_level: input.maxAcademicLevel,
    has_foundation_stage: input.hasFoundationStage,
  }) as Promise<Institution>;
}

export function retireInstitution(input: Auth & { institutionID: string }) {
  return authenticatedRequest<Institution>(
    `${base}/institutions/${encodeURIComponent(input.institutionID)}/retire`,
    "POST",
    input.locale,
    input.csrf,
  ) as Promise<Institution>;
}

export function listUnits(institutionID: string, locale: "ar" | "en") {
  return authenticatedRequest<AcademicUnit[]>(
    `${base}/institutions/${encodeURIComponent(institutionID)}/units`,
    "GET",
    locale,
  ) as Promise<AcademicUnit[]>;
}

export function createUnit(
  input: Auth & {
    institutionID: string;
    kind: AcademicUnitKind;
    slug: string;
    nameAr: string;
    nameEn: string;
    parentUnitID?: string | null;
  },
) {
  return authenticatedRequest<AcademicUnit>(
    `${base}/institutions/${encodeURIComponent(input.institutionID)}/units`,
    "POST",
    input.locale,
    input.csrf,
    {
      kind: input.kind,
      slug: input.slug,
      name_ar: input.nameAr,
      name_en: input.nameEn,
      // An absent parent is legitimate: not every institution has a department
      // layer, and some units attach straight to the institution.
      parent_unit_id: input.parentUnitID ? input.parentUnitID : null,
    },
  ) as Promise<AcademicUnit>;
}

export function retireUnit(input: Auth & { unitID: string }) {
  return authenticatedRequest<AcademicUnit>(
    `${base}/units/${encodeURIComponent(input.unitID)}/retire`,
    "POST",
    input.locale,
    input.csrf,
  ) as Promise<AcademicUnit>;
}

export function listPrograms(institutionID: string, locale: "ar" | "en") {
  return authenticatedRequest<Program[]>(
    `${base}/institutions/${encodeURIComponent(institutionID)}/programs`,
    "GET",
    locale,
  ) as Promise<Program[]>;
}

export function createProgram(
  input: Auth & {
    institutionID: string;
    slug: string;
    nameAr: string;
    nameEn: string;
    degreeKind: string;
    owningUnitID?: string | null;
  },
) {
  return authenticatedRequest<Program>(
    `${base}/institutions/${encodeURIComponent(input.institutionID)}/programs`,
    "POST",
    input.locale,
    input.csrf,
    {
      slug: input.slug,
      name_ar: input.nameAr,
      name_en: input.nameEn,
      degree_kind: input.degreeKind,
      owning_unit_id: input.owningUnitID ? input.owningUnitID : null,
    },
  ) as Promise<Program>;
}

export function listCurricula(programID: string, locale: "ar" | "en") {
  return authenticatedRequest<Curriculum[]>(
    `${base}/programs/${encodeURIComponent(programID)}/curricula`,
    "GET",
    locale,
  ) as Promise<Curriculum[]>;
}

export function createCurriculum(
  input: Auth & {
    programID: string;
    versionLabel: string;
    effectiveFromYear?: number | null;
    supersedeActive?: boolean;
  },
) {
  return authenticatedRequest<Curriculum>(
    `${base}/programs/${encodeURIComponent(input.programID)}/curricula`,
    "POST",
    input.locale,
    input.csrf,
    {
      version_label: input.versionLabel,
      effective_from_year: input.effectiveFromYear ?? null,
      supersede_active: input.supersedeActive ?? false,
    },
  ) as Promise<Curriculum>;
}

export function listSubjects(
  institutionID: string,
  locale: "ar" | "en",
  query = "",
) {
  const suffix = query === "" ? "" : `?q=${encodeURIComponent(query)}`;
  return authenticatedRequest<Subject[]>(
    `${base}/institutions/${encodeURIComponent(institutionID)}/subjects${suffix}`,
    "GET",
    locale,
  ) as Promise<Subject[]>;
}

export function createSubject(
  input: Auth & {
    institutionID: string;
    titleAr: string;
    titleEn: string;
    officialCode?: string | null;
    owningUnitID?: string | null;
  },
) {
  return authenticatedRequest<Subject>(
    `${base}/institutions/${encodeURIComponent(input.institutionID)}/subjects`,
    "POST",
    input.locale,
    input.csrf,
    {
      title_ar: input.titleAr,
      title_en: input.titleEn,
      official_code: input.officialCode ? input.officialCode : null,
      owning_unit_id: input.owningUnitID ? input.owningUnitID : null,
    },
  ) as Promise<Subject>;
}

export function retireSubject(input: Auth & { subjectID: string }) {
  return authenticatedRequest<Subject>(
    `${base}/subjects/${encodeURIComponent(input.subjectID)}/retire`,
    "POST",
    input.locale,
    input.csrf,
  ) as Promise<Subject>;
}

export function listCurriculumSubjects(curriculumID: string, locale: "ar" | "en") {
  return authenticatedRequest<CurriculumSubject[]>(
    `${base}/curricula/${encodeURIComponent(curriculumID)}/subjects`,
    "GET",
    locale,
  ) as Promise<CurriculumSubject[]>;
}

export function mapSubjectToCurriculum(
  input: Auth & {
    curriculumID: string;
    subjectID: string;
    requirementKind: RequirementKind;
    recommendedLevel?: number | null;
    recommendedSemester?: number | null;
    credits?: number | null;
  },
) {
  return authenticatedRequest<CurriculumSubject>(
    `${base}/curricula/${encodeURIComponent(input.curriculumID)}/subjects`,
    "POST",
    input.locale,
    input.csrf,
    {
      subject_id: input.subjectID,
      requirement_kind: input.requirementKind,
      recommended_level: input.recommendedLevel ?? null,
      recommended_semester: input.recommendedSemester ?? null,
      credits: input.credits ?? null,
    },
  ) as Promise<CurriculumSubject>;
}

export function unmapSubjectFromCurriculum(
  input: Auth & { curriculumID: string; subjectID: string },
) {
  return authenticatedRequest<null>(
    `${base}/curricula/${encodeURIComponent(input.curriculumID)}/subjects/${encodeURIComponent(input.subjectID)}`,
    "DELETE",
    input.locale,
    input.csrf,
  );
}

/** Display label for a Subject: official code first, then the localized title. */
export function subjectLabel(subject: Subject, locale: "ar" | "en"): string {
  const title = locale === "ar" ? subject.title_ar : subject.title_en;
  return subject.official_code ? `${subject.official_code} · ${title}` : title;
}
