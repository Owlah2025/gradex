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

export type SectionWire = {
  id: string;
  title_ar: string;
  title_en: string;
  position: number;
  price_minor_units?: number | null;
};

export type CourseRevisionWire = {
  title_ar: string;
  title_en: string;
  sections: SectionWire[];
};

export type OwnedCourseSummary = {
  id: string;
  editable_revision?: CourseRevisionWire;
  live_revision?: CourseRevisionWire;
  price_minor_units?: number | null;
};

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
