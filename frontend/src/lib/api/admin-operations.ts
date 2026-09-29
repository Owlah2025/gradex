import { authenticatedRequest, ensureAnonymousBrowser } from "./http";
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
  total: number;
  page: number;
  limit: number;
  has_more: boolean;
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
  return error instanceof ProblemError && error.problem.code === "recent-authentication-required";
}

export function buildAdminAccountQuery(filters: AdminAccountFilters): string {
  const query = new URLSearchParams();
  setText(query, "q", filters.q);
  setText(query, "role", filters.role);
  setText(query, "status", filters.status);
  setText(query, "institutionId", filters.institutionId);
  setText(query, "joinedFrom", filters.joinedFrom);
  setText(query, "joinedTo", filters.joinedTo);
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
  setText(query, "from", filters.from);
  setText(query, "to", filters.to);
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

function setNumber(query: URLSearchParams, key: string, value: number | undefined): void {
  if (value !== undefined) query.set(key, String(value));
}
