import { authenticatedRequest } from "./http";

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
