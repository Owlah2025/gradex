import { authenticatedRequest } from "./http";
import { ProblemError } from "./problem";
import { publicCatalogRequest as publicRequest } from "./public-request";

/**
 * Public Subject discovery and Student demand (D-106).
 *
 * # THE TWO IDENTIFIERS, AND WHY THEY ARE NOT INTERCHANGEABLE
 *
 * A Subject carries both a `value` and a `subject_id`, and using the wrong one
 * is the defect this module exists to make unrepresentable:
 *
 *   * `value` is the public, shareable identity — the university's own official
 *     code where it has one. It belongs in a URL and on screen.
 *   * `subject_id` is the canonical row identifier. It is the *only* thing the
 *     demand endpoints accept, and a code sent there is refused as invalid.
 *
 * The demand functions below therefore take `subjectId` and nothing else, so a
 * caller holding only a code cannot reach them without first resolving it
 * through discovery.
 *
 * # WHAT A SUBJECT IS NOT
 *
 * A Subject is academic identity, never a product. Nothing here carries a
 * price, an entitlement, or a purchase. A *served* Subject links to a real
 * published Course; an *unserved* one links to nothing and offers demand
 * registration instead. No placeholder Course is ever created for one.
 */

export type SubjectCourseRef = {
  slug: string;
  title: string;
};

export type SubjectListing = {
  /** The canonical identifier. The only value the demand endpoints accept. */
  subject_id: string;
  /** The shareable identity that belongs in a URL: official code, else the id. */
  value: string;
  code?: string;
  title_ar: string;
  title_en: string;
  institution_slug: string;
  institution_name_ar: string;
  institution_name_en: string;
  /** True when at least one published Course teaches this Subject. */
  served: boolean;
  /** Non-empty exactly when `served` is true. */
  courses: SubjectCourseRef[];
};

export type SubjectPage = {
  items: SubjectListing[];
  page: number;
  page_size: number;
  total: number;
};

export type SubjectAvailability = "all" | "served" | "unserved";

export type SubjectQuery = {
  institution?: string;
  search?: string;
  availability?: SubjectAvailability;
  page?: number;
  pageSize?: number;
  /** Cancels the request when the caller's query has moved on. */
  signal?: AbortSignal;
};

/** True when a rejection is this request being cancelled, not a failure. */
export function requestAborted(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}

/**
 * Browses Subjects, including the ones no Course teaches.
 *
 * Anonymous by design: a visitor has to be able to find their own course code
 * before deciding whether Gradex is worth an account.
 */
export function getSubjects(locale: "ar" | "en", query: SubjectQuery = {}) {
  const parameters = new URLSearchParams();
  // An empty filter is omitted rather than sent empty, so "no filter" and "a
  // filter matching nothing" stay different requests.
  if (query.institution) parameters.set("institution", query.institution);
  if (query.search) parameters.set("q", query.search);
  // "all" is the absence of a filter, not a value the server knows.
  if (query.availability && query.availability !== "all")
    parameters.set("availability", query.availability);
  if (query.page && query.page > 1) parameters.set("page", String(query.page));
  if (query.pageSize) parameters.set("page_size", String(query.pageSize));
  const suffix = parameters.size === 0 ? "" : `?${parameters}`;
  return publicRequest<SubjectPage>(`/subjects${suffix}`, locale, query.signal);
}

/**
 * Reads one Subject by its Institution slug and its public value.
 *
 * `value` is the shareable code, which is what a Student's URL carries. The
 * returned listing is what supplies `subject_id` for any demand call.
 */
export function getSubject(
  institutionSlug: string,
  value: string,
  locale: "ar" | "en",
) {
  return publicRequest<SubjectListing>(
    `/subjects/${encodeURIComponent(institutionSlug)}/${encodeURIComponent(value)}`,
    locale,
  );
}

export type SubjectDemandSignal = {
  id: string;
  subject_id: string;
  institution_id: string;
  institution_slug: string;
  subject_code?: string;
  subject_title_ar: string;
  subject_title_en: string;
  note?: string;
  created_at: string;
};

type Auth = { locale: "ar" | "en"; csrf: string };

/**
 * Registers this Student's demand for a Subject.
 *
 * Takes `subjectId` — never a code. The server refuses anything that is not a
 * UUID with a 422 carrying SUBJECT_INVALID, which `subjectInvalid` detects.
 */
export function raiseSubjectDemand(
  input: Auth & { subjectId: string; note?: string },
) {
  return authenticatedRequest<SubjectDemandSignal>(
    "/me/subject-demand",
    "POST",
    input.locale,
    input.csrf,
    { subject_id: input.subjectId, note: input.note ?? "" },
  ) as Promise<SubjectDemandSignal>;
}

/** Withdraws this Student's own live signal. Answers 204. */
export function withdrawSubjectDemand(input: Auth & { subjectId: string }) {
  return authenticatedRequest<null>(
    `/me/subject-demand/${encodeURIComponent(input.subjectId)}`,
    "DELETE",
    input.locale,
    input.csrf,
  ) as Promise<null>;
}

/** The Student's own live signals. Never another Student's. */
export function listOwnSubjectDemand(locale: "ar" | "en") {
  return authenticatedRequest<{ items: SubjectDemandSignal[] }>(
    "/me/subject-demand",
    "GET",
    locale,
  ).then((body) => body?.items ?? []);
}

export type SubjectDemandCount = {
  subject_id: string;
  institution_slug: string;
  /**
   * Both names are supplied by the server. The client picks by locale rather
   * than keeping its own institution-name table, which would drift from the
   * catalog the moment an Institution is renamed.
   */
  institution_name_ar: string;
  institution_name_en: string;
  subject_code?: string;
  subject_title_ar: string;
  subject_title_en: string;
  /** Distinct Students, not clicks. */
  students: number;
  served: boolean;
};

/**
 * Aggregate demand. Admin-only — the server refuses every other principal, and
 * no public or Student surface may render these numbers.
 */
export function listSubjectDemandCounts(
  locale: "ar" | "en",
  options: { institution?: string; limit?: number } = {},
) {
  const parameters = new URLSearchParams();
  if (options.institution) parameters.set("institution", options.institution);
  if (options.limit) parameters.set("limit", String(options.limit));
  const suffix = parameters.size === 0 ? "" : `?${parameters}`;
  return authenticatedRequest<{ items: SubjectDemandCount[] }>(
    `/admin/academic/subject-demand${suffix}`,
    "GET",
    locale,
  ).then((body) => body?.items ?? []);
}

/**
 * True when the failure is "this Student already asked".
 *
 * A conflict is not an error to show as one: the Student's intent is already
 * recorded, so the correct response is to render the requested state rather
 * than a failure message.
 */
export function alreadyRequested(error: unknown): boolean {
  return error instanceof ProblemError && error.problem.status === 409;
}

/**
 * True when the server refused the Subject identifier itself.
 *
 * This should be unreachable from the UI, because every demand call is built
 * from a `subject_id` that discovery returned. It is detected anyway so the
 * failure reads as a bug rather than as "your request was rejected": a Student
 * cannot do anything about it, so the surface must not ask them to.
 */
export function subjectInvalid(error: unknown): boolean {
  if (!(error instanceof ProblemError)) return false;
  return (error.problem.errors ?? []).some(
    (violation) => violation.code === "SUBJECT_INVALID",
  );
}

/** True when the Subject is gone — retired, or never existed. */
export function subjectMissing(error: unknown): boolean {
  return error instanceof ProblemError && error.problem.status === 404;
}

/** The Subject's title in the reading language. */
export function subjectTitle(
  subject: { title_ar: string; title_en: string },
  locale: "ar" | "en",
): string {
  return locale === "ar" ? subject.title_ar : subject.title_en;
}

/** The Institution's name in the reading language. */
export function institutionName(
  subject: { institution_name_ar: string; institution_name_en: string },
  locale: "ar" | "en",
): string {
  return locale === "ar"
    ? subject.institution_name_ar
    : subject.institution_name_en;
}

/**
 * The catalogue path for one Subject.
 *
 * Built from `value`, never from `subject_id`: the URL is a thing Students
 * share and read, and a university's own course code is what they recognise.
 */
export function subjectPath(
  subject: { institution_slug: string; value: string },
  locale: "ar" | "en",
): string {
  return `/${locale}/subjects/${encodeURIComponent(subject.institution_slug)}/${encodeURIComponent(subject.value)}`;
}
