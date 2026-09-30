import { authenticatedDownload, authenticatedRequest, ensureAnonymousBrowser } from "./http";
import { currentCSRFToken } from "../identity/session";
import { ProblemError } from "./problem";

export type AdminLocale = "ar" | "en";
export type AccountRole = "STUDENT" | "INSTRUCTOR" | "ADMIN";
export type AccountStatus = "PENDING_VERIFICATION" | "ACTIVE" | "SUSPENDED";

export type AdminAccount = {
  id: string;
  display_name: string;
  email: string;
  role: AccountRole;
  status: AccountStatus;
  locale: AdminLocale;
  email_verified: boolean;
  created_at: string;
  institution_label: string;
  last_sign_in_activity_at?: string | null;
};

export type AdminAccountPage = {
  accounts: AdminAccount[];
  total: number;
  page: number;
  limit: number;
  has_more: boolean;
};

export type AdminAccountFilters = {
  q?: string;
  role?: AccountRole | "";
  status?: AccountStatus | "";
  institutionId?: string;
  joinedFrom?: string;
  joinedTo?: string;
  page?: number;
  limit?: number;
};

export type AdminEmailDeliveryState = "queued" | "attempted" | "delivered" | "failed";

export type AdminEmailDelivery = {
  id: string;
  kind: string;
  locale: AdminLocale;
  state: AdminEmailDeliveryState;
  recipient: string;
  queued_at: string;
  attempted_at?: string | null;
  delivered_at?: string | null;
  failed_at?: string | null;
  updated_at: string;
  attempt_count: number;
  last_error_class?: string;
};

export type AdminEmailDeliveryFilters = {
  state?: AdminEmailDeliveryState | "";
  kind?: string;
  from?: string;
  to?: string;
  page?: number;
  limit?: number;
};

export type AdminEmailDeliveryPage = {
  items: AdminEmailDelivery[];
  page: number;
  limit: number;
  has_more: boolean;
};

export type AdminMediaFailure = {
  asset_version_id: string;
  media_state: string;
  state: "failed" | "stuck";
  kind: string;
  course_title: string;
  lesson_title?: string;
  owner_display_name: string;
  failure_category?: string;
  processing_stage?: string;
  processing_progress_percent?: number;
  created_at: string;
  updated_at: string;
  retry_action?: "retry" | "retry-enhancements";
};

export type AdminMediaFailurePage = {
  items: AdminMediaFailure[];
  page: number;
  limit: number;
  has_more: boolean;
};

export type AdminAuditEvent = {
  id: string;
  occurred_at: string;
  actor_account_id?: string;
  actor_display_name: string;
  actor_role: string;
  action: string;
  module: string;
  target_type: string;
  target_id: string;
  target_label?: string;
  reason: string;
  metadata: Record<string, unknown>;
};

export type AdminAuditPage = {
  audit_events: AdminAuditEvent[];
  page: number;
  limit: number;
  has_more: boolean;
  as_of: string;
};

export type AdminAuditFilters = {
  actor?: string;
  actorAccountId?: string;
  targetType?: string;
  targetId?: string;
  action?: string;
  module?: string;
  from?: string;
  to?: string;
  asOf?: string;
  page?: number;
  limit?: number;
};

export type AuditLabelSet = {
  actions: Record<string, string>;
  modules: Record<string, string>;
  unknownTarget: string;
};

export type User360Identity = AdminAccount & {
  last_learning_activity_at?: string | null;
};

export type UserCourse = {
  id: string;
  title: string;
  lifecycle: string;
  candidate_revision_state?: string;
  enrollments_count?: number;
  completed_lessons?: number;
  total_lessons?: number;
  progress_percent?: number;
  last_watched_at?: string | null;
};

export type UserEntitlementAdjustment = {
  id: string;
  entitlement_id: string;
  old_access_ends_at: string;
  new_access_ends_at: string;
  reason: string;
  actor_account_id: string;
  support_reference?: string | null;
  adjusted_at: string;
};

export type UserEntitlement = {
  id: string;
  course_id: string;
  course_title: string;
  scope_kind: string;
  scope_id: string;
  grant_source: string;
  source_reference_label?: string;
  granted_by_display_name?: string;
  granted_at: string;
  original_access_ends_at: string;
  access_ends_at: string;
  revoked_at?: string | null;
  state: string;
  revision: number;
  adjustments: UserEntitlementAdjustment[];
};

export type UserInvitation = {
  id: string;
  course_id: string;
  course_title: string;
  state: string;
  created_by_name?: string;
  created_at: string;
  decided_at?: string | null;
  accepted_at?: string | null;
  external_reference?: string;
};

export type UserPurchaseRequest = {
  id: string;
  reference: string;
  course_title?: string;
  bundle_title?: string;
  state: string;
  requested_at: string;
  access_granted_at?: string | null;
};

export type AdminDevice = {
  id: string;
  label: string;
  browser_family: string;
  platform_family: string;
  state: string;
  first_seen_at: string;
  last_active_at: string;
  trusted_at?: string | null;
  revoked_at?: string | null;
  revocation_reason?: string;
};

export type AdminDeviceOverview = {
  devices: AdminDevice[];
  device_limit: number;
  replacement_cooldown_until?: string | null;
};

export type AdminSecurityEvent = {
  id: string;
  occurred_at: string;
  event_type: string;
  request_id: string;
  evidence: Record<string, unknown>;
};

export type AdminNote = {
  id: string;
  author_name: string;
  body: string;
  created_at: string;
};

export type AdminUser360 = {
  identity: User360Identity;
  emails: AdminEmailDelivery[];
  student?: {
    academic_profile?: {
      setup_state: string;
      enrollment_status?: string;
      institution_label?: string;
      academic_unit_label?: string;
      program_label?: string;
      curriculum_label?: string;
      current_level?: number;
    } | null;
    courses: UserCourse[];
    entitlements: UserEntitlement[];
    invitations: UserInvitation[];
    purchase_requests: UserPurchaseRequest[];
    devices: AdminDeviceOverview;
    security_events: AdminSecurityEvent[];
    audit_events: AdminAuditEvent[];
    notes: AdminNote[];
  };
  instructor?: {
    staff_status: string;
    owned_courses: UserCourse[];
    audit_events: AdminAuditEvent[];
    notes: AdminNote[];
  };
};

export type AdminCourseOption = { id: string; title: string; lifecycle: string };

export type AccessDiagnostic = {
  account_id: string;
  course_id: string;
  course_title: string;
  primary_reason_code: string;
  evaluator_reason: string;
  allowed: boolean;
  facts: { code: string; value?: string }[];
  entitlements: UserEntitlement[];
};

export function accessReasonMessage(code: string, labels: Record<string, string>): string {
  return labels[code] ?? labels.UNKNOWN ?? code;
}

export function provenanceLabel(source: string, labels: Record<string, string>): string {
  return labels[source] ?? labels.UNKNOWN ?? source;
}

export function isRecentAuthRequired(error: unknown): boolean {
  return error instanceof ProblemError && error.problem.code === "RECENT_AUTHENTICATION_REQUIRED";
}

export function buildAdminAccountQuery(filters: AdminAccountFilters): string {
  const query = new URLSearchParams();
  setText(query, "q", filters.q);
  setText(query, "role", filters.role);
  setText(query, "status", filters.status);
  setText(query, "institutionId", filters.institutionId);
  setLocalDateBoundary(query, "joinedFrom", filters.joinedFrom, false);
  setLocalDateBoundary(query, "joinedTo", filters.joinedTo, true);
  setNumber(query, "page", filters.page);
  setNumber(query, "limit", filters.limit);
  return query.toString();
}

export function buildAdminAuditQuery(filters: AdminAuditFilters): string {
  const query = new URLSearchParams();
  setText(query, "actor", filters.actor);
  setText(query, "actorAccountId", filters.actorAccountId);
  setText(query, "targetType", filters.targetType);
  setText(query, "targetId", filters.targetId);
  setText(query, "action", filters.action);
  setText(query, "module", filters.module);
  setLocalDateBoundary(query, "from", filters.from, false);
  setLocalDateBoundary(query, "to", filters.to, true);
  setText(query, "asOf", filters.asOf);
  setNumber(query, "page", filters.page);
  setNumber(query, "limit", filters.limit);
  return query.toString();
}

export function buildAdminEmailDeliveriesQuery(filters: AdminEmailDeliveryFilters = {}): string {
  const query = new URLSearchParams();
  setText(query, "state", filters.state);
  setText(query, "kind", filters.kind);
  setLocalDateBoundary(query, "from", filters.from, false);
  setLocalDateBoundary(query, "to", filters.to, true);
  setNumber(query, "page", filters.page);
  setNumber(query, "limit", filters.limit);
  return query.toString();
}

export function auditActionLabel(action: string, labels: AuditLabelSet): string {
  return labels.actions[action] ?? action;
}

export function auditModuleLabel(module: string, labels: AuditLabelSet): string {
  return labels.modules[module] ?? module;
}

export async function listAdminAccounts(
  locale: AdminLocale,
  filters: AdminAccountFilters = {},
): Promise<AdminAccountPage> {
  const query = buildAdminAccountQuery(filters);
  const response = await authenticatedRequest<AdminAccountPage>(
    "/admin/accounts" + (query ? "?" + query : ""),
    "GET",
    locale,
  );
  if (response === null) {
    throw new Error(locale === "ar" ? "لم يتم استلام دليل الحسابات" : "No account directory returned");
  }
  return response;
}

export async function listAdminEmailDeliveries(
  locale: AdminLocale,
  filters: AdminEmailDeliveryFilters = {},
): Promise<AdminEmailDeliveryPage> {
  const query = buildAdminEmailDeliveriesQuery(filters);
  const response = await authenticatedRequest<AdminEmailDeliveryPage>(
    "/admin/email-deliveries" + (query ? "?" + query : ""), "GET", locale,
  );
  if (response === null) throw new Error(locale === "ar" ? "لم يتم استلام سجل البريد" : "No email delivery log returned");
  return response;
}

export async function listAdminMediaFailures(locale: AdminLocale, page = 1, limit = 20): Promise<AdminMediaFailurePage> {
  const response = await authenticatedRequest<AdminMediaFailurePage>(
    `/admin/media/failures?page=${page}&limit=${limit}`, "GET", locale,
  );
  if (response === null) throw new Error(locale === "ar" ? "لم يتم استلام أعطال الوسائط" : "No media failures returned");
  return response;
}

export async function retryAdminMedia(assetVersionID: string, locale: AdminLocale): Promise<void> {
  await authenticatedRequest(`/media/assets/${encodeURIComponent(assetVersionID)}/retries`, "POST", locale, await resolveAdminCSRF());
}

export async function retryAdminMediaEnhancements(assetVersionID: string, locale: AdminLocale): Promise<void> {
  await authenticatedRequest(`/media/assets/${encodeURIComponent(assetVersionID)}/retry-enhancements`, "POST", locale, await resolveAdminCSRF());
}

export async function exportAdminAccounts(filters: AdminAccountFilters, locale: AdminLocale): Promise<Blob> {
  const query = buildAdminAccountQuery({ ...filters, page: undefined, limit: undefined });
  return authenticatedDownload(`/admin/accounts/export${query ? `?${query}` : ""}`, locale);
}

export async function getAdminAccount(accountID: string, locale: AdminLocale): Promise<{ identity: AdminAccount }> {
  const response = await authenticatedRequest<{ identity: AdminAccount }>(
    "/admin/accounts/" + encodeURIComponent(accountID),
    "GET",
    locale,
  );
  if (response === null) {
    throw new Error(locale === "ar" ? "لم يتم استلام هوية الحساب" : "No account identity returned");
  }
  return response;
}

export async function getAdminUser360(accountID: string, locale: AdminLocale): Promise<AdminUser360> {
  const response = await authenticatedRequest<AdminUser360>(
    "/admin/accounts/" + encodeURIComponent(accountID),
    "GET",
    locale,
  );
  if (response === null) {
    throw new Error(locale === "ar" ? "لم يتم استلام ملف المستخدم" : "No User 360 returned");
  }
  return response;
}

async function resolveAdminCSRF(csrf?: string): Promise<string> {
  if (csrf) return csrf;
  return currentCSRFToken() ?? ensureAnonymousBrowser();
}

export async function listAdminNotes(accountID: string, locale: AdminLocale): Promise<{ notes: AdminNote[] }> {
  const response = await authenticatedRequest<{ notes: AdminNote[] }>(
    `/admin/accounts/${encodeURIComponent(accountID)}/notes`, "GET", locale,
  );
  if (response === null) throw new Error(locale === "ar" ? "لم يتم استلام الملاحظات" : "No notes returned");
  return response;
}

export async function addAdminNote(accountID: string, body: string, locale: AdminLocale, csrf?: string): Promise<AdminNote> {
  const response = await authenticatedRequest<AdminNote>(
    `/admin/accounts/${encodeURIComponent(accountID)}/notes`, "POST", locale,
    await resolveAdminCSRF(csrf), { body },
  );
  if (response === null) throw new Error(locale === "ar" ? "لم تتم إضافة الملاحظة" : "No note returned");
  return response;
}

export async function revokeAccountSessions(accountID: string, reason: string, locale: AdminLocale, csrf?: string) {
  return authenticatedRequest<{ epoch: number; revoked_session_count: number }>(
    `/admin/accounts/${encodeURIComponent(accountID)}/session-revocations`, "POST", locale,
    await resolveAdminCSRF(csrf), { reason },
  );
}

export async function listAdminCourseOptions(accountID: string, locale: AdminLocale, query = "") {
  const params = new URLSearchParams();
  if (query.trim()) params.set("q", query.trim());
  const response = await authenticatedRequest<{ courses: AdminCourseOption[] }>(
    `/admin/accounts/${encodeURIComponent(accountID)}/course-options${params.size ? `?${params}` : ""}`,
    "GET", locale,
  );
  if (response === null) throw new Error(locale === "ar" ? "لم يتم استلام المقررات" : "No course options returned");
  return response;
}

export async function diagnoseAdminAccess(accountID: string, courseID: string, locale: AdminLocale) {
  const response = await authenticatedRequest<AccessDiagnostic>(
    `/admin/accounts/${encodeURIComponent(accountID)}/access-diagnostics?courseId=${encodeURIComponent(courseID)}`,
    "GET", locale,
  );
  if (response === null) throw new Error(locale === "ar" ? "لم يتم استلام التشخيص" : "No diagnostic returned");
  return response;
}

export async function listAdminSecurityEvents(accountID: string, locale: AdminLocale, page = 1, limit = 20) {
  const response = await authenticatedRequest<{
    events: AdminSecurityEvent[]; total: number; page: number; limit: number; has_more: boolean;
  }>(`/admin/accounts/${encodeURIComponent(accountID)}/security-events?page=${page}&limit=${limit}`, "GET", locale);
  if (response === null) throw new Error(locale === "ar" ? "لم يتم استلام أحداث الأمان" : "No security events returned");
  return response;
}

export async function listAdminAuditEvents(
  locale: AdminLocale,
  filters: AdminAuditFilters = {},
): Promise<AdminAuditPage> {
  const query = buildAdminAuditQuery(filters);
  const response = await authenticatedRequest<AdminAuditPage>(
    "/admin/audit-events" + (query ? "?" + query : ""),
    "GET",
    locale,
  );
  if (response === null) {
    throw new Error(locale === "ar" ? "لم يتم استلام سجل التدقيق" : "No audit events returned");
  }
  return response;
}

function setText(query: URLSearchParams, key: string, value: string | undefined): void {
  const trimmed = value?.trim();
  if (trimmed) query.set(key, trimmed);
}

function setLocalDateBoundary(
  query: URLSearchParams,
  key: string,
  value: string | undefined,
  endOfDate: boolean,
): void {
  if (!value?.trim()) return;
  const date = new Date(`${value.trim()}T00:00:00`);
  if (Number.isNaN(date.getTime())) {
    query.set(key, value.trim());
    return;
  }
  if (endOfDate) date.setDate(date.getDate() + 1);
  const offsetMinutes = -date.getTimezoneOffset();
  const sign = offsetMinutes >= 0 ? "+" : "-";
  const absoluteOffset = Math.abs(offsetMinutes);
  const offsetHours = String(Math.floor(absoluteOffset / 60)).padStart(2, "0");
  const offsetRemainder = String(absoluteOffset % 60).padStart(2, "0");
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  query.set(
    key,
    `${date.getFullYear()}-${month}-${day}T00:00:00${sign}${offsetHours}:${offsetRemainder}`,
  );
}

function setNumber(query: URLSearchParams, key: string, value: number | undefined): void {
  if (value !== undefined) query.set(key, String(value));
}
