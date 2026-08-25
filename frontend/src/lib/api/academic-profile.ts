import { authenticatedRequest } from "./http";

/**
 * Student academic profile and onboarding options (D-092, T3).
 *
 * Discovery-only personalisation data. Nothing here participates in an access
 * decision: a Student with no profile reaches every Course they hold an
 * entitlement for, and changing a profile changes nothing but discovery.
 *
 * Every route is scoped to the signed-in Student by the server. There is no
 * account parameter, because there is no shape of call that should reach
 * another Student's profile.
 */

export type SetupState = "NOT_STARTED" | "SKIPPED" | "COMPLETED";

export type EnrollmentStatus =
  "ENROLLED" | "UNDECLARED" | "FOUNDATION" | "NON_DEGREE";

export type AcademicProfile = {
  setup_state: SetupState;
  enrollment_status?: EnrollmentStatus;
  institution_id?: string;
  institution_name?: string;
  /** The institution's own level bound. Never assume a number here. */
  max_academic_level?: number;
  has_foundation_stage?: boolean;
  /** Present only for Program-less states; the College an undeclared Student named. */
  academic_unit_id?: string;
  academic_unit_name?: string;
  program_id?: string;
  program_name?: string;
  /**
   * The Program's public slug. Supplied so the public catalogue can rank
   * results for this Student's Program without the client ever handling an
   * internal identifier. It is a discovery hint and grants nothing.
   */
  program_slug?: string;
  /** Derived context for display. The Student never chooses a Department. */
  department_name?: string;
  college_name?: string;
  curriculum_version_label?: string;
  current_level?: number;
};

export type InstitutionOption = {
  id: string;
  name_ar: string;
  name_en: string;
  country_code: string;
  max_academic_level: number;
  has_foundation_stage: boolean;
};

export type CollegeOption = { id: string; name_ar: string; name_en: string };

export type ProgramOption = {
  id: string;
  name_ar: string;
  name_en: string;
  department_name_ar?: string;
  department_name_en?: string;
};

type Auth = { locale: "ar" | "en"; csrf: string };

const base = "/me";

export function getAcademicProfile(locale: "ar" | "en") {
  return authenticatedRequest<AcademicProfile>(
    `${base}/academic-profile`,
    "GET",
    locale,
  ) as Promise<AcademicProfile>;
}

export function listInstitutionOptions(locale: "ar" | "en") {
  return authenticatedRequest<InstitutionOption[]>(
    `${base}/academic-options/institutions`,
    "GET",
    locale,
  ) as Promise<InstitutionOption[]>;
}

export function listCollegeOptions(institutionID: string, locale: "ar" | "en") {
  return authenticatedRequest<CollegeOption[]>(
    `${base}/academic-options/institutions/${encodeURIComponent(institutionID)}/colleges`,
    "GET",
    locale,
  ) as Promise<CollegeOption[]>;
}

export function listProgramOptions(
  institutionID: string,
  collegeID: string,
  locale: "ar" | "en",
) {
  return authenticatedRequest<ProgramOption[]>(
    `${base}/academic-options/institutions/${encodeURIComponent(institutionID)}` +
      `/programs?college_id=${encodeURIComponent(collegeID)}`,
    "GET",
    locale,
  ) as Promise<ProgramOption[]>;
}

export type SaveAcademicProfileInput = Auth & {
  institutionID: string;
  enrollmentStatus: EnrollmentStatus;
  /** Required for ENROLLED, absent otherwise. */
  programID?: string | null;
  /** The College, for a Student who has not declared a major. */
  academicUnitID?: string | null;
  currentLevel?: number | null;
};

export function saveAcademicProfile(input: SaveAcademicProfileInput) {
  return authenticatedRequest<AcademicProfile>(
    `${base}/academic-profile`,
    "PUT",
    input.locale,
    input.csrf,
    {
      institution_id: input.institutionID,
      enrollment_status: input.enrollmentStatus,
      program_id: input.programID ?? "",
      academic_unit_id: input.academicUnitID ?? "",
      // Level is genuinely optional: a Student is never forced to know their
      // regulatory standing.
      current_level: input.currentLevel ?? null,
      // curriculum_id is deliberately never sent. The server resolves the study
      // plan and refuses a client-supplied one.
    },
  ) as Promise<AcademicProfile>;
}

/** An explicit deferral, not an empty save. */
export function skipAcademicOnboarding(input: Auth) {
  return authenticatedRequest<AcademicProfile>(
    `${base}/academic-profile/skip`,
    "POST",
    input.locale,
    input.csrf,
  ) as Promise<AcademicProfile>;
}

/**
 * Whether to invite a Student to complete their profile. Only NOT_STARTED
 * qualifies: a Student who chose to defer is not asked again on every visit.
 */
export function shouldPromptOnboarding(
  profile: AcademicProfile | null,
): boolean {
  return profile?.setup_state === "NOT_STARTED";
}

/** Localised level labels, generated from the institution's own bound. */
export function academicLevelLabels(
  maxLevel: number,
  locale: "ar" | "en",
): { value: number; label: string }[] {
  const ordinalsAr = [
    "الأول",
    "الثاني",
    "الثالث",
    "الرابع",
    "الخامس",
    "السادس",
    "السابع",
    "الثامن",
    "التاسع",
    "العاشر",
    "الحادي عشر",
    "الثاني عشر",
  ];
  const safeMax =
    Number.isFinite(maxLevel) && maxLevel > 0
      ? Math.min(maxLevel, ordinalsAr.length)
      : 0;
  return Array.from({ length: safeMax }, (_, index) => {
    const value = index + 1;
    return {
      value,
      label:
        locale === "ar" ? `المستوى ${ordinalsAr[index]}` : `Level ${value}`,
    };
  });
}
