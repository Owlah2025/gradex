import { authenticatedRequest } from "./http";

export type AdminReportAction = "DISMISS" | "DELIST";

export type AdminReportTarget = {
  available: boolean;
  target_type: string;
  target_label_ar?: string;
  target_label_en?: string;
  course_label_ar?: string;
  course_label_en?: string;
  course_lifecycle?: string;
  access_suspended: boolean;
  retired: boolean;
};

export type AdminReport = {
  id: string;
  reporter_display_name?: string;
  target_type: string;
  reason: string;
  explanation: string;
  created_at: string;
  status: "OPEN" | "RESOLVED";
  target: AdminReportTarget;
  resolved_at?: string;
  resolution_action?: string;
  resolution_reason?: string;
};

export type AdminReportPage = {
  items: AdminReport[];
  page: number;
  page_size: number;
  has_next: boolean;
};

export async function getAdminReports(
  locale: "ar" | "en",
  page = 1,
  pageSize = 20,
): Promise<AdminReportPage> {
  const query = new URLSearchParams({ page: String(page), page_size: String(pageSize) });
  const response = await authenticatedRequest<AdminReportPage>(
    `/admin/reports?${query.toString()}`,
    "GET",
    locale,
  );
  if (response === null) {
    throw new Error(locale === "ar" ? "لم يتم استلام قائمة البلاغات" : "No reported-content queue returned");
  }
  return response;
}

export async function getAdminReport(
  reportID: string,
  locale: "ar" | "en",
): Promise<AdminReport> {
  const response = await authenticatedRequest<AdminReport>(
    `/admin/reports/${encodeURIComponent(reportID)}`,
    "GET",
    locale,
  );
  if (response === null) {
    throw new Error(locale === "ar" ? "لم يتم استلام تفاصيل البلاغ" : "No report detail returned");
  }
  return response;
}

export async function resolveAdminReport(input: {
  reportID: string;
  action: AdminReportAction;
  reason: string;
  locale: "ar" | "en";
  csrf: string;
}): Promise<AdminReport> {
  if (!input.csrf) {
    throw new Error(input.locale === "ar" ? "رمز CSRF مفقود" : "CSRF token is required");
  }
  const response = await authenticatedRequest<AdminReport>(
    `/admin/reports/${encodeURIComponent(input.reportID)}/resolve`,
    "POST",
    input.locale,
    input.csrf,
    { action: input.action, reason: input.reason },
  );
  if (response === null) {
    throw new Error(input.locale === "ar" ? "لم يرجع الخادم حالة البلاغ" : "The server returned no report state");
  }
  return response;
}
