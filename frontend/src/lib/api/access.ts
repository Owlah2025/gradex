import {
  authenticatedRequest,
  ensureAnonymousBrowser,
  getJSON,
  postJSON,
} from "./http";
import { currentCSRFToken } from "../identity/session";

export interface CourseAccessInvitation {
  id: string;
  normalized_email: string;
  email: string;
  course_id: string;
  /** The grant this invitation produced, once an Admin approved it. */
  entitlement_id?: string | null;
  created_by_account_id: string;
  decided_by_account_id?: string | null;
  accepted_by_account_id?: string | null;
  state:
    | "PENDING_STUDENT_ACCEPTANCE"
    | "PENDING_ADMIN_APPROVAL"
    | "APPROVED"
    | "REJECTED"
    | "CANCELLED";
  decision_reason?: string | null;
  admin_note?: string | null;
  external_reference?: string | null;
  created_at: string;
  accepted_at?: string | null;
  decided_at?: string | null;
  cancelled_at?: string | null;
}

export interface StudentCourseAccessInvitation {
  id: string;
  course_id: string;
  state:
    | "PENDING_STUDENT_ACCEPTANCE"
    | "PENDING_ADMIN_APPROVAL"
    | "APPROVED"
    | "REJECTED"
    | "CANCELLED";
  decision_reason?: string | null;
  created_at: string;
  accepted_at?: string | null;
  decided_at?: string | null;
  cancelled_at?: string | null;
}

export interface Entitlement {
  id: string;
  student_account_id: string;
  scope_kind: string;
  scope_id: string;
  course_id: string;
  grant_source: string;
  source_invitation_id?: string | null;
  original_access_ends_at: string;
  access_ends_at: string;
  revoked_at?: string | null;
  retirement_eligibility_at: string;
  state: "ACTIVE" | "REVOKED" | "EXPIRED";
  revision: number;
  created_at: string;
  updated_at: string;
}

export interface EntitlementAdjustment {
  id: string;
  entitlement_id: string;
  old_access_ends_at: string;
  new_access_ends_at: string;
  reason: string;
  actor_account_id: string;
  support_reference?: string | null;
  adjusted_at: string;
}

export interface AdminEntitlementDetail {
  entitlement: Entitlement;
  adjustments: EntitlementAdjustment[];
}

export interface ApproveInvitationResult {
  invitation: CourseAccessInvitation;
  entitlement: Entitlement;
}

export interface StudentCourseAccessHistoryItem {
  course_id: string;
  /**
   * The Course as the Student knows it, already resolved to the request's language by the server.
   * The Course id stays available for building links, but is never product-visible copy.
   */
  course_title: string;
  has_active_access: boolean;
  access_ends_at?: string | null;
  invitation?: StudentCourseAccessInvitation | null;
}

export interface StudentCourseAccessHistoryResponse {
  items: StudentCourseAccessHistoryItem[];
}

export interface AdminInvitationListResponse {
  invitations: CourseAccessInvitation[];
  total: number;
  page: number;
  limit: number;
}

export type PurchaseRequestState =
  "WAITING_PAYMENT" | "INVITATION_CREATED" | "ACCESS_GRANTED" | "CANCELLED";

export interface PurchaseRequest {
  id: string;
  reference: string;
  course_id: string;
  email: string;
  price_minor_units: number;
  currency: "KWD";
  state: PurchaseRequestState;
  course_title?: string;
  invitation_id?: string | null;
  requested_at: string;
  payment_confirmed_at?: string | null;
  invitation_created_at?: string | null;
  access_granted_at?: string | null;
}

export interface PurchaseRequestListResponse {
  purchase_requests: PurchaseRequest[];
  total: number;
  page: number;
  limit: number;
}

export interface PurchaseRequestCreated {
  reference: string;
  whatsapp_url: string;
  /** Resolved to the request's language by the server. */
  course_title: string;
  price_minor_units: number;
  currency: string;
  state: PurchaseRequestState;
  /** True when an already-active request was returned instead of a new one. */
  reused: boolean;
}

export interface ConfirmPurchaseRequestResult {
  purchase_request: PurchaseRequest;
  invitation: CourseAccessInvitation;
}

async function resolveCSRF(csrf?: string): Promise<string> {
  if (csrf) return csrf;
  const current = currentCSRFToken();
  if (current) return current;
  return ensureAnonymousBrowser();
}

export async function setCourseDefaultAccessExpiry(
  courseId: string,
  date: string,
  reason: string,
  lang: "ar" | "en" = "en",
  csrf?: string,
) {
  const token = await resolveCSRF(csrf);
  return authenticatedRequest<{
    course_id: string;
    default_access_ends_at: string;
    reason: string;
  }>(`/admin/courses/${courseId}/default-access-expiry`, "PUT", lang, token, {
    date,
    reason,
  });
}

export async function createCourseAccessInvitation(
  courseId: string,
  email: string,
  adminNote?: string,
  externalReference?: string,
  lang: "ar" | "en" = "en",
  csrf?: string,
) {
  const token = await resolveCSRF(csrf);
  return authenticatedRequest<CourseAccessInvitation>(
    "/admin/course-access-invitations",
    "POST",
    lang,
    token,
    {
      course_id: courseId,
      email,
      admin_note: adminNote || undefined,
      external_reference: externalReference || undefined,
    },
  );
}

export async function listAdminCourseAccessInvitations(
  page = 1,
  limit = 50,
  lang: "ar" | "en" = "en",
  csrf?: string,
) {
  return authenticatedRequest<AdminInvitationListResponse>(
    `/admin/course-access-invitations?page=${page}&limit=${limit}`,
    "GET",
    lang,
    csrf,
  );
}

export async function approveCourseAccessInvitation(
  invitationId: string,
  lang: "ar" | "en" = "en",
  csrf?: string,
) {
  const token = await resolveCSRF(csrf);
  return authenticatedRequest<ApproveInvitationResult>(
    `/admin/course-access-invitations/${invitationId}/approve`,
    "POST",
    lang,
    token,
  );
}

export async function rejectCourseAccessInvitation(
  invitationId: string,
  reason: string,
  lang: "ar" | "en" = "en",
  csrf?: string,
) {
  const token = await resolveCSRF(csrf);
  return authenticatedRequest<CourseAccessInvitation>(
    `/admin/course-access-invitations/${invitationId}/reject`,
    "POST",
    lang,
    token,
    { reason },
  );
}

export async function cancelCourseAccessInvitation(
  invitationId: string,
  lang: "ar" | "en" = "en",
  csrf?: string,
) {
  const token = await resolveCSRF(csrf);
  return authenticatedRequest<CourseAccessInvitation>(
    `/admin/course-access-invitations/${invitationId}/cancel`,
    "POST",
    lang,
    token,
  );
}

export async function resendCourseAccessInvitation(
  invitationId: string,
  lang: "ar" | "en" = "en",
  csrf?: string,
) {
  const token = await resolveCSRF(csrf);
  return authenticatedRequest<CourseAccessInvitation>(
    `/admin/course-access-invitations/${invitationId}/resend`,
    "POST",
    lang,
    token,
  );
}

/**
 * Moves an existing grant's effective expiry. A later Kuwait-local date
 * extends access, an earlier one shortens it; the server is authoritative for
 * both (BR-026).
 */
export async function adjustEntitlementExpiry(
  entitlementId: string,
  date: string,
  reason: string,
  options: { supportReference?: string; expectedRevision?: number } = {},
  lang: "ar" | "en" = "en",
  csrf?: string,
) {
  const token = await resolveCSRF(csrf);
  return authenticatedRequest<AdminEntitlementDetail>(
    `/admin/entitlements/${entitlementId}/expiry`,
    "PUT",
    lang,
    token,
    {
      date,
      reason,
      support_reference: options.supportReference || undefined,
      expected_revision: options.expectedRevision,
    },
  );
}

export async function revokeEntitlement(
  entitlementId: string,
  reason: string,
  options: { supportReference?: string; expectedRevision?: number } = {},
  lang: "ar" | "en" = "en",
  csrf?: string,
) {
  const token = await resolveCSRF(csrf);
  return authenticatedRequest<AdminEntitlementDetail>(
    `/admin/entitlements/${entitlementId}/revocation`,
    "POST",
    lang,
    token,
    {
      reason,
      support_reference: options.supportReference || undefined,
      expected_revision: options.expectedRevision,
    },
  );
}

export async function getAdminEntitlementDetail(
  entitlementId: string,
  lang: "ar" | "en" = "en",
  csrf?: string,
) {
  return authenticatedRequest<AdminEntitlementDetail>(
    `/admin/entitlements/${entitlementId}`,
    "GET",
    lang,
    csrf,
  );
}

export async function listStudentCourseAccessInvitations(
  lang: "ar" | "en" = "en",
  csrf?: string,
) {
  return authenticatedRequest<StudentCourseAccessInvitation[]>(
    "/me/course-access-invitations",
    "GET",
    lang,
    csrf,
  );
}

export async function getStudentCourseAccessInvitation(
  id: string,
  lang: "ar" | "en" = "en",
  csrf?: string,
) {
  return authenticatedRequest<StudentCourseAccessInvitation>(
    `/me/course-access-invitations/${id}`,
    "GET",
    lang,
    csrf,
  );
}

export async function acceptStudentCourseAccessInvitation(
  id: string,
  acceptanceToken: string,
  lang: "ar" | "en" = "en",
  csrf?: string,
) {
  const token = await resolveCSRF(csrf);
  return authenticatedRequest<StudentCourseAccessInvitation>(
    `/me/course-access-invitations/${id}/accept`,
    "POST",
    lang,
    token,
    { acceptance_token: acceptanceToken },
  );
}

export async function getStudentCourseAccessHistory(
  lang: "ar" | "en" = "en",
  csrf?: string,
) {
  return authenticatedRequest<StudentCourseAccessHistoryResponse>(
    "/me/course-access",
    "GET",
    lang,
    csrf,
  );
}

/**
 * Records the signed-in Student's intent to buy one Course.
 *
 * The body carries the Course and nothing else. There is deliberately no email
 * parameter: the address on a purchase request decides where Course access is
 * eventually sent, and the server reads it from the authenticated Account. An
 * earlier version of this call took an address from an anonymous browser and
 * returned a WhatsApp handoff in the same response, which let any visitor put
 * any mailbox into the sales queue.
 *
 * This is also the only call that produces the WhatsApp URL. Nothing before the
 * Student's explicit confirmation reaches it.
 */
export async function createStudentPurchaseRequest(
  courseId: string,
  lang: "ar" | "en" = "en",
  csrf?: string,
) {
  const token = await resolveCSRF(csrf);
  return authenticatedRequest<PurchaseRequestCreated>(
    "/me/purchase-requests",
    "POST",
    lang,
    token,
    { course_id: courseId },
  ) as Promise<PurchaseRequestCreated>;
}

export async function listAdminPurchaseRequests(
  options: {
    page?: number;
    limit?: number;
    query?: string;
    state?: PurchaseRequestState;
  } = {},
  lang: "ar" | "en" = "en",
) {
  const params = new URLSearchParams({
    page: String(options.page ?? 1),
    limit: String(options.limit ?? 50),
  });
  if (options.query) params.set("q", options.query);
  if (options.state) params.set("state", options.state);
  return authenticatedRequest<PurchaseRequestListResponse>(
    `/admin/purchase-requests?${params}`,
    "GET",
    lang,
  );
}

export async function confirmPurchaseRequestPayment(
  requestId: string,
  lang: "ar" | "en" = "en",
  csrf?: string,
) {
  const token = await resolveCSRF(csrf);
  return authenticatedRequest<ConfirmPurchaseRequestResult>(
    `/admin/purchase-requests/${requestId}/confirm-payment`,
    "POST",
    lang,
    token,
  );
}

export async function cancelPurchaseRequest(
  requestId: string,
  lang: "ar" | "en" = "en",
  csrf?: string,
) {
  const token = await resolveCSRF(csrf);
  return authenticatedRequest<PurchaseRequest>(
    `/admin/purchase-requests/${requestId}/cancel`,
    "POST",
    lang,
    token,
  );
}
