import { authenticatedRequest } from "./http";

export type SubjectRequestStatus =
  | "PENDING"
  | "APPROVED_NEW"
  | "LINKED_EXISTING"
  | "REJECTED"
  | "CANCELLED";

export type SubjectRequestWire = {
  id: string;
  requester_account_id: string;
  institution_id: string;
  course_id?: string;
  proposed_title_ar: string;
  proposed_title_en: string;
  proposed_official_code?: string;
  academic_context?: string;
  note?: string;
  status: SubjectRequestStatus;
  resolved_subject_id?: string;
  resolution_reason?: string;
  resolved_at?: string;
  requester_display_name: string;
  institution_name_ar: string;
  institution_name_en: string;
  course_title_ar?: string;
  course_title_en?: string;
  resolved_official_code?: string;
  resolved_subject_title_ar?: string;
  resolved_subject_title_en?: string;
};

type MutationInput = { locale: "ar" | "en"; csrf: string };

function requireResult<T>(result: T | null, locale: "ar" | "en"): T {
  if (result === null) {
    throw new Error(locale === "ar" ? "لم يرجع الخادم نتيجة" : "The server returned an empty result");
  }
  return result;
}

function requireCSRF(input: MutationInput): void {
  if (!input.csrf) throw new Error(input.locale === "ar" ? "رمز CSRF مفقود" : "Session CSRF token is missing");
}

export async function listOwnSubjectRequests(
  locale: "ar" | "en",
  courseID?: string,
): Promise<SubjectRequestWire[]> {
  const query = courseID ? `?course_id=${encodeURIComponent(courseID)}` : "";
  const result = await authenticatedRequest<SubjectRequestWire[]>(
    `/authoring/academic/subject-requests${query}`, "GET", locale,
  );
  return requireResult(result, locale);
}

export async function createSubjectRequest(
  input: MutationInput & {
    institutionID: string;
    courseID?: string;
    proposedOfficialCode?: string;
    proposedTitleAr: string;
    proposedTitleEn: string;
    academicContext?: string;
    note?: string;
  },
): Promise<SubjectRequestWire> {
  requireCSRF(input);
  const result = await authenticatedRequest<SubjectRequestWire>(
    "/authoring/academic/subject-requests", "POST", input.locale, input.csrf,
    {
      institution_id: input.institutionID,
      ...(input.courseID ? { course_id: input.courseID } : {}),
      ...(input.proposedOfficialCode ? { proposed_official_code: input.proposedOfficialCode } : {}),
      proposed_title_ar: input.proposedTitleAr,
      proposed_title_en: input.proposedTitleEn,
      ...(input.academicContext ? { academic_context: input.academicContext } : {}),
      ...(input.note ? { note: input.note } : {}),
    },
  );
  return requireResult(result, input.locale);
}

export async function listAdminSubjectRequests(
  locale: "ar" | "en",
  status: SubjectRequestStatus | "" = "PENDING",
): Promise<SubjectRequestWire[]> {
  const query = status ? `?status=${encodeURIComponent(status)}` : "";
  const result = await authenticatedRequest<SubjectRequestWire[]>(
    `/admin/academic/subject-requests${query}`, "GET", locale,
  );
  return requireResult(result, locale);
}

const adminPath = (requestID: string, action: string) =>
  `/admin/academic/subject-requests/${encodeURIComponent(requestID)}/${action}`;

export async function linkSubjectRequest(
  input: MutationInput & { requestID: string; subjectID: string },
): Promise<SubjectRequestWire> {
  requireCSRF(input);
  const result = await authenticatedRequest<SubjectRequestWire>(
    adminPath(input.requestID, "link"), "POST", input.locale, input.csrf,
    { subject_id: input.subjectID },
  );
  return requireResult(result, input.locale);
}

export async function approveSubjectRequestAsNew(
  input: MutationInput & { requestID: string },
): Promise<SubjectRequestWire> {
  requireCSRF(input);
  const result = await authenticatedRequest<SubjectRequestWire>(
    adminPath(input.requestID, "approve-new"), "POST", input.locale, input.csrf,
  );
  return requireResult(result, input.locale);
}

export async function rejectSubjectRequest(
  input: MutationInput & { requestID: string; reason: string },
): Promise<SubjectRequestWire> {
  requireCSRF(input);
  const result = await authenticatedRequest<SubjectRequestWire>(
    adminPath(input.requestID, "reject"), "POST", input.locale, input.csrf,
    { reason: input.reason },
  );
  return requireResult(result, input.locale);
}
