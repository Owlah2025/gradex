import { authenticatedRequest } from "./http";

export type AdminMetricsLocale = "ar" | "en";

export type AdminMetric = {
  key: string;
  value: number | AdminTopSubjectDemand[];
  definition_key: string;
};

export type AdminTopSubjectDemand = {
  subject_id: string;
  title: string;
  students: number;
  served: boolean;
};

export type AdminMetricsOverview = {
  metrics: AdminMetric[];
  as_of: string;
};

export type AdminCourseMetric = {
  id: string;
  title: string;
  instructor: string;
  lifecycle: string;
  enrolled: number;
  started: number;
  learning_active_7d: number;
  average_progress: number;
  completed: number;
};

export type AdminInstructorMetric = {
  id: string;
  name: string;
  published_courses: number;
  total_enrollments: number;
  learning_active_students_7d: number;
};

export type AdminMetricsPage<T> = {
  items: T[];
  total: number;
  page: number;
  limit: number;
  has_more: boolean;
};

export type AdminInboxItem = {
  kind: string;
  label: string;
  age_seconds: number;
  created_at: string;
  route: string;
  target_id: string;
};

export type AdminInboxSection = {
  key: string;
  count: number;
  items: AdminInboxItem[];
};

export type AdminInbox = {
  sections: AdminInboxSection[];
};

export type AdminMetricsPageFilters = {
  sort?: string;
  direction?: "asc" | "desc";
  page?: number;
  limit?: number;
};

export function buildAdminMetricsPageQuery(filters: AdminMetricsPageFilters = {}): string {
  const query = new URLSearchParams();
  if (filters.sort) query.set("sort", filters.sort);
  if (filters.direction) query.set("direction", filters.direction);
  if (filters.page) query.set("page", String(filters.page));
  if (filters.limit) query.set("limit", String(filters.limit));
  return query.toString();
}

export function metricByKey(metrics: AdminMetric[], key: string): AdminMetric | undefined {
  return metrics.find((metric) => metric.key === key);
}

export function metricNumber(metrics: AdminMetric[], key: string): number {
  const metric = metricByKey(metrics, key);
  return typeof metric?.value === "number" ? metric.value : 0;
}

export function metricSubjectDemand(metrics: AdminMetric[], key = "subject_demand.top"): AdminTopSubjectDemand[] {
  const metric = metricByKey(metrics, key);
  return Array.isArray(metric?.value) ? metric.value : [];
}

export async function getAdminMetricsOverview(
  locale: AdminMetricsLocale,
  window?: "7d" | "30d",
): Promise<AdminMetricsOverview> {
  const query = window ? `?window=${encodeURIComponent(window)}` : "";
  const response = await authenticatedRequest<AdminMetricsOverview>(`/admin/metrics/overview${query}`, "GET", locale);
  if (response === null) {
    throw new Error(locale === "ar" ? "لم يتم استلام ملخص التحليلات" : "No analytics overview returned");
  }
  return response;
}

export async function listAdminMetricCourses(
  locale: AdminMetricsLocale,
  filters: AdminMetricsPageFilters = {},
): Promise<AdminMetricsPage<AdminCourseMetric>> {
  const query = buildAdminMetricsPageQuery(filters);
  const response = await authenticatedRequest<AdminMetricsPage<AdminCourseMetric>>(
    `/admin/metrics/courses${query ? `?${query}` : ""}`,
    "GET",
    locale,
  );
  if (response === null) {
    throw new Error(locale === "ar" ? "لم يتم استلام تحليلات المقررات" : "No course analytics returned");
  }
  return response;
}

export async function listAdminMetricInstructors(
  locale: AdminMetricsLocale,
  filters: AdminMetricsPageFilters = {},
): Promise<AdminMetricsPage<AdminInstructorMetric>> {
  const query = buildAdminMetricsPageQuery(filters);
  const response = await authenticatedRequest<AdminMetricsPage<AdminInstructorMetric>>(
    `/admin/metrics/instructors${query ? `?${query}` : ""}`,
    "GET",
    locale,
  );
  if (response === null) {
    throw new Error(locale === "ar" ? "لم يتم استلام تحليلات المدرّسين" : "No instructor analytics returned");
  }
  return response;
}

export async function getAdminInbox(locale: AdminMetricsLocale, limit = 5): Promise<AdminInbox> {
  const response = await authenticatedRequest<AdminInbox>(`/admin/inbox?limit=${encodeURIComponent(limit)}`, "GET", locale);
  if (response === null) {
    throw new Error(locale === "ar" ? "لم يتم استلام صندوق المتابعة" : "No operator inbox returned");
  }
  return response;
}
