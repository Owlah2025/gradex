import { authenticatedRequest } from "./http";

export type PriceChangeRecord = {
  id: string;
  course_id: string;
  section_id?: string;
  old_value_minor_units?: number | null;
  new_value_minor_units: number;
  changed_by_account_id: string;
  reason: string;
  changed_at: string;
};

export type SetCoursePriceInput = {
  courseID: string;
  priceMinorUnits: number;
  reason: string;
  locale: "ar" | "en";
  csrf: string;
};

export type SetSectionPriceInput = {
  courseID: string;
  sectionID: string;
  priceMinorUnits: number;
  reason: string;
  locale: "ar" | "en";
  csrf: string;
};

export type LessonFileWire = {
  id: string;
  kind: "RESOURCE" | "LAB_MATERIAL";
  asset_version_id: string;
  display_name_ar: string;
  display_name_en: string;
  position: number;
};

export type LessonWire = {
  id: string;
  section_id?: string;
  title_ar: string;
  title_en: string;
  position: number;
  video_asset_version_id?: string;
  files?: LessonFileWire[];
};

export type SectionWire = {
  id: string;
  title_ar: string;
  title_en: string;
  position: number;
  price_minor_units?: number | null;
  lessons?: LessonWire[];
};

export type ProgramAudienceWire = {
	program_id: string;
	name_ar: string;
	name_en: string;
	recommended_level?: number;
	recommended_semester?: number;
};

export type RevisionAudienceWire = {
	mode: "AUTOMATIC" | "CUSTOMIZED";
	programs: ProgramAudienceWire[];
};

export type CourseRevisionWire = {
	id?: string;
	course_id?: string;
	state?: string;
	revision_number?: number;
	title_ar: string;
	title_en: string;
	description_ar?: string;
	description_en?: string;
	major_term_id?: string;
	subject_term_id?: string;
	study_year?: string;
	/** Separate public-preview Asset Version; never a Lesson asset. */
	preview_asset_version_id?: string;
	/**
	 * The Admin's change-request/rejection reason for this revision.
	 *
	 * Written by the review mutation (`catalog/review.go`) and already served to the owning
	 * Instructor by `GET /courses` and `GET /courses/:id`. It is retained on the row after a
	 * resubmission, so it describes the *last decision*, not the current state — read it only
	 * alongside `state`, never on its own.
	 */
	review_reason?: string;
	/** When the last review decision was recorded. Same retention caveat as `review_reason`. */
	reviewed_at?: string;
	audience?: RevisionAudienceWire;
	sections: SectionWire[];
};

export type TaxonomyKind = "MAJOR" | "SUBJECT";

export type TaxonomyTerm = {
	id: string;
	kind: TaxonomyKind;
	label_ar: string;
	label_en: string;
	academic_code?: string;
	retired_at?: string;
};

export type OwnedCourseSummary = {
  id: string;
  owner_account_id?: string;
  lifecycle?: string;
  /**
   * The published revision's id.
   *
   * Present on both the owned-Course *list* and *detail* payloads. `live_revision` below is the
   * expanded graph and is populated only by the detail read (`GetOwnedCourse`), so anything that
   * merely needs to know whether a Course has a published revision must read this field.
   */
  live_revision_id?: string;
  editable_revision?: CourseRevisionWire;
  live_revision?: CourseRevisionWire;
  price_minor_units?: number | null;

  /**
   * D-093 academic identity.
   *
   * `classification_model` is the server's own answer to which authority owns
   * this Course's academic identity. Presentation branches on it and NEVER on
   * whether `subject_id` happens to be null: a legacy Course and a subject-less
   * Academic draft both have no Subject, and only this field tells them apart.
   *
   * It is read-only. No authoring call sends it, and the server derives it at
   * creation, so a Course cannot be moved between models from the client.
   */
  classification_model?: "LEGACY_TAXONOMY" | "ACADEMIC_CATALOG";
  institution_id?: string;
  subject_id?: string;
  academic_context?: {
    institution_name_ar: string;
    institution_name_en: string;
    subject?: {
      official_code?: string;
      title_ar: string;
      title_en: string;
      owning_unit_name_ar?: string;
      owning_unit_name_en?: string;
      parent_unit_name_ar?: string;
      parent_unit_name_en?: string;
    };
  };
};

/** True when the Course's academic identity is the canonical Academic Catalog. */
export function isAcademicCourse(course: Pick<OwnedCourseSummary, "classification_model">): boolean {
  return course.classification_model === "ACADEMIC_CATALOG";
}

export type OwnedCourseDetail = OwnedCourseSummary;

export type CourseLifecycle = "PUBLISHED" | "DELISTED" | "ARCHIVED";
export type SuspensionCause = "LEGAL" | "SECURITY" | "MALWARE" | "SEVERE_MODERATION";

type LifecycleRequest = {
  courseID: string;
  locale: "ar" | "en";
  csrf: string;
};

async function lifecycleRequest(
  input: LifecycleRequest,
  path: string,
  method: "POST" | "DELETE",
  body?: Record<string, string>,
): Promise<void> {
  if (!input.csrf) {
    throw new Error(input.locale === "ar" ? "رمز CSRF مفقود" : "CSRF token is required");
  }
  const res = await authenticatedRequest<unknown>(
    `/admin/courses/${encodeURIComponent(input.courseID)}${path}`,
    method,
    input.locale,
    input.csrf,
    body,
  );
  if (res === null && method !== "DELETE") {
    throw new Error(input.locale === "ar" ? "لم يرجع الخادم نتيجة" : "Server returned an empty result");
  }
}

export function delistCourse(input: LifecycleRequest) { return lifecycleRequest(input, "/delist", "POST"); }
export function relistCourse(input: LifecycleRequest) { return lifecycleRequest(input, "/relist", "POST"); }
export function retireCourse(input: LifecycleRequest) { return lifecycleRequest(input, "/retire", "POST"); }
export function archiveCourse(input: LifecycleRequest) { return lifecycleRequest(input, "/archive", "POST"); }
export function reassignCourseOwner(input: LifecycleRequest & { ownerAccountID: string }) { return lifecycleRequest(input, "/owner", "POST", { owner_account_id: input.ownerAccountID }); }
export function suspendCourseAccess(input: LifecycleRequest & { cause: SuspensionCause; reason: string }) { return lifecycleRequest(input, "/access-suspension", "POST", { cause: input.cause, reason: input.reason }); }
export function restoreCourseAccess(input: LifecycleRequest & { reason: string }) { return lifecycleRequest(input, "/access-suspension", "DELETE", { reason: input.reason }); }

export async function getCoursePriceHistory(
  courseID: string,
  locale: "ar" | "en",
): Promise<PriceChangeRecord[]> {
  const res = await authenticatedRequest<PriceChangeRecord[]>(
    `/admin/courses/${encodeURIComponent(courseID)}/price-history`,
    "GET",
    locale,
  );
  if (res === null) {
    throw new Error(
      locale === "ar"
        ? "لم يتم استلام سجل الأسعار من الخادم"
        : "No price history returned from server",
    );
  }
  return res;
}

export async function setCoursePrice(
  input: SetCoursePriceInput,
): Promise<PriceChangeRecord> {
  if (!input.csrf) {
    throw new Error(
      input.locale === "ar"
        ? "رمز CSRF الجلسة مطلوب لإجراء التعديل"
        : "CSRF token is required for price mutation",
    );
  }
  const res = await authenticatedRequest<PriceChangeRecord>(
    `/admin/courses/${encodeURIComponent(input.courseID)}/price`,
    "PUT",
    input.locale,
    input.csrf,
    { price_minor_units: input.priceMinorUnits, reason: input.reason },
  );
  if (res === null) {
    throw new Error(
      input.locale === "ar"
        ? "فشل الخادم في إرجاع نتيجة التحديث"
        : "Server returned an empty response for price update",
    );
  }
  return res;
}

export async function setSectionPrice(
  input: SetSectionPriceInput,
): Promise<PriceChangeRecord> {
  if (!input.csrf) {
    throw new Error(
      input.locale === "ar"
        ? "رمز CSRF الجلسة مطلوب لإجراء التعديل"
        : "CSRF token is required for price mutation",
    );
  }
  const res = await authenticatedRequest<PriceChangeRecord>(
    `/admin/courses/${encodeURIComponent(input.courseID)}/sections/${encodeURIComponent(input.sectionID)}/price`,
    "PUT",
    input.locale,
    input.csrf,
    { price_minor_units: input.priceMinorUnits, reason: input.reason },
  );
  if (res === null) {
    throw new Error(
      input.locale === "ar"
        ? "فشل الخادم في إرجاع نتيجة التحديث"
        : "Server returned an empty response for price update",
    );
  }
  return res;
}

export async function getOwnedCourses(
  locale: "ar" | "en",
): Promise<OwnedCourseSummary[]> {
  const res = await authenticatedRequest<OwnedCourseSummary[]>(
    "/courses",
    "GET",
    locale,
  );
  if (res === null) {
    throw new Error(
      locale === "ar"
        ? "لم يتم استلام قائمة الدورات المملوكة من الخادم"
        : "No owned courses returned from server",
    );
  }
  return res;
}

export async function getOwnedCourseDetail(
  courseID: string,
  locale: "ar" | "en",
): Promise<OwnedCourseDetail> {
  const res = await authenticatedRequest<OwnedCourseDetail>(
    `/courses/${encodeURIComponent(courseID)}`,
    "GET",
    locale,
  );
  if (res === null) {
    throw new Error(
      locale === "ar"
        ? "لم يتم استلام تفاصيل الدورة من الخادم"
        : "No course details returned from server",
    );
  }
	return res;
}

export async function getTaxonomyTerms(locale: "ar" | "en"): Promise<TaxonomyTerm[]> {
	const res = await authenticatedRequest<TaxonomyTerm[]>("/taxonomy/terms", "GET", locale);
	if (res === null) {
		throw new Error(locale === "ar" ? "لم يتم استلام مصطلحات التصنيف" : "No taxonomy terms returned");
	}
	return res;
}

type TaxonomyMutationInput = {
	locale: "ar" | "en";
	csrf: string;
};

function requireTaxonomyCSRF(input: TaxonomyMutationInput): void {
	if (!input.csrf) {
		throw new Error(input.locale === "ar" ? "رمز CSRF للجلسة مفقود" : "Session CSRF token is required");
	}
}

export async function assignInstructorTaxonomy(input: TaxonomyMutationInput & {
	courseID: string;
	revisionID: string;
	majorTermID: string;
	subjectTermID: string;
}): Promise<void> {
	requireTaxonomyCSRF(input);
	const res = await authenticatedRequest<unknown>(
		`/courses/${encodeURIComponent(input.courseID)}/revisions/${encodeURIComponent(input.revisionID)}`,
		"PATCH",
		input.locale,
		input.csrf,
		{ major_term_id: input.majorTermID, subject_term_id: input.subjectTermID },
	);
	if (res === null) {
		throw new Error(input.locale === "ar" ? "لم يرجع الخادم التعديل" : "Server returned an empty taxonomy update");
	}
}

export async function assignAdminTaxonomy(input: TaxonomyMutationInput & {
	courseID: string;
	revisionID: string;
	majorTermID: string;
	subjectTermID: string;
}): Promise<void> {
	requireTaxonomyCSRF(input);
	const res = await authenticatedRequest<unknown>(
		`/admin/courses/${encodeURIComponent(input.courseID)}/taxonomy`,
		"PUT",
		input.locale,
		input.csrf,
		{ revision_id: input.revisionID, major_term_id: input.majorTermID, subject_term_id: input.subjectTermID },
	);
	if (res === null) {
		throw new Error(input.locale === "ar" ? "لم يرجع الخادم التعديل" : "Server returned an empty taxonomy update");
	}
}

export async function createTaxonomyTerm(input: TaxonomyMutationInput & {
	kind: TaxonomyKind;
	labelAr: string;
	labelEn: string;
	academicCode?: string;
}): Promise<TaxonomyTerm> {
	requireTaxonomyCSRF(input);
	const res = await authenticatedRequest<TaxonomyTerm>("/admin/taxonomy/terms", "POST", input.locale, input.csrf, {
		kind: input.kind,
		label_ar: input.labelAr,
		label_en: input.labelEn,
		academic_code: input.academicCode || undefined,
	});
	if (res === null) {
		throw new Error(input.locale === "ar" ? "لم يرجع الخادم المصطلح" : "Server returned an empty taxonomy term");
	}
	return res;
}

export async function renameTaxonomyTerm(input: TaxonomyMutationInput & {
	termID: string;
	labelAr: string;
	labelEn: string;
}): Promise<TaxonomyTerm> {
	requireTaxonomyCSRF(input);
	const res = await authenticatedRequest<TaxonomyTerm>(
		`/admin/taxonomy/terms/${encodeURIComponent(input.termID)}`,
		"PATCH",
		input.locale,
		input.csrf,
		{ label_ar: input.labelAr, label_en: input.labelEn },
	);
	if (res === null) {
		throw new Error(input.locale === "ar" ? "لم يرجع الخادم المصطلح" : "Server returned an empty taxonomy term");
	}
	return res;
}

export async function retireTaxonomyTerm(input: TaxonomyMutationInput & { termID: string }): Promise<TaxonomyTerm> {
	requireTaxonomyCSRF(input);
	const res = await authenticatedRequest<TaxonomyTerm>(
		`/admin/taxonomy/terms/${encodeURIComponent(input.termID)}/retire`,
		"POST",
		input.locale,
		input.csrf,
	);
	if (res === null) {
		throw new Error(input.locale === "ar" ? "لم يرجع الخادم المصطلح" : "Server returned an empty taxonomy term");
	}
	return res;
}

export async function deleteTaxonomyTerm(input: TaxonomyMutationInput & { termID: string }): Promise<void> {
	requireTaxonomyCSRF(input);
	await authenticatedRequest<unknown>(
		`/admin/taxonomy/terms/${encodeURIComponent(input.termID)}`,
		"DELETE",
		input.locale,
		input.csrf,
	);
}
