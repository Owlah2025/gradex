import assert from "node:assert/strict";
import { test } from "node:test";
import {
  createCourseAccessInvitation,
  listAdminCourseAccessInvitations,
  approveCourseAccessInvitation,
  rejectCourseAccessInvitation,
  cancelCourseAccessInvitation,
  resendCourseAccessInvitation,
  setCourseDefaultAccessExpiry,
  getAdminEntitlementDetail,
  listStudentCourseAccessInvitations,
  getStudentCourseAccessInvitation,
  acceptStudentCourseAccessInvitation,
  getStudentCourseAccessHistory,
} from "./access";

test("access API client forwards requests to correct backend endpoints", async () => {
  const originalFetch = globalThis.fetch;
  const requests: { url: string; method: string; body?: any; headers?: any }[] = [];

  globalThis.fetch = async (url, init) => {
    const u = String(url);
    const method = init?.method || "GET";
    let body = null;
    if (init?.body) {
      try {
        body = JSON.parse(String(init.body));
      } catch {
        body = String(init.body);
      }
    }
    requests.push({ url: u, method, body, headers: init?.headers });

    if (u.includes("/session/bootstrap")) {
      return new Response(JSON.stringify({ csrf_token: "csrf-token-123" }), { status: 200 });
    }
    if (u.includes("/admin/courses/c-1/default-access-expiry")) {
      return new Response(JSON.stringify({ course_id: "c-1", default_access_ends_at: "2026-09-01T00:00:00Z", reason: "test" }), { status: 200 });
    }
    if (u.includes("/admin/course-access-invitations/inv-1/approve")) {
      return new Response(JSON.stringify({ invitation: { id: "inv-1", state: "APPROVED" }, entitlement: { id: "ent-1", state: "ACTIVE" } }), { status: 200 });
    }
    if (u.includes("/admin/course-access-invitations/inv-1/reject")) {
      return new Response(JSON.stringify({ id: "inv-1", state: "REJECTED", decision_reason: "reason" }), { status: 200 });
    }
    if (u.includes("/admin/course-access-invitations/inv-1/cancel")) {
      return new Response(JSON.stringify({ id: "inv-1", state: "CANCELLED" }), { status: 200 });
    }
    if (u.includes("/admin/course-access-invitations/inv-1/resend")) {
      return new Response(JSON.stringify({ id: "inv-1", state: "PENDING_STUDENT_ACCEPTANCE" }), { status: 200 });
    }
    if (u.includes("/admin/course-access-invitations") && method === "POST") {
      return new Response(JSON.stringify({ id: "inv-1", state: "PENDING_STUDENT_ACCEPTANCE", email: "student@example.com", course_id: "c-1" }), { status: 201 });
    }
    if (u.includes("/admin/course-access-invitations?page=1&limit=50")) {
      return new Response(JSON.stringify({ invitations: [], total: 0, page: 1, limit: 50 }), { status: 200 });
    }
    if (u.includes("/admin/entitlements/ent-1")) {
      return new Response(JSON.stringify({ entitlement: { id: "ent-1", state: "ACTIVE" }, adjustments: [] }), { status: 200 });
    }
    if (u.includes("/me/course-access-invitations/inv-1/accept")) {
      return new Response(JSON.stringify({ id: "inv-1", state: "PENDING_ADMIN_APPROVAL" }), { status: 200 });
    }
    if (u.includes("/me/course-access")) {
      return new Response(JSON.stringify({ items: [{ course_id: "c-1", has_active_access: true }] }), { status: 200 });
    }

    return new Response(JSON.stringify({}), { status: 200 });
  };

  try {
    // 1. Expiry config
    const exp = await setCourseDefaultAccessExpiry("c-1", "2026-09-01", "test", "en", "csrf-token-123");
    assert.equal(exp?.course_id, "c-1");

    // 2. Create invitation
    const inv = await createCourseAccessInvitation("c-1", "student@example.com", undefined, undefined, "en", "csrf-token-123");
    assert.equal(inv?.id, "inv-1");

    // 3. List admin invitations
    const list = await listAdminCourseAccessInvitations(1, 50, "en", "csrf-token-123");
    assert.equal(list?.total, 0);

    // 4. Approve invitation
    const app = await approveCourseAccessInvitation("inv-1", "en", "csrf-token-123");
    assert.equal(app?.entitlement?.id, "ent-1");

    // 5. Reject invitation
    const rej = await rejectCourseAccessInvitation("inv-1", "reason", "en", "csrf-token-123");
    assert.equal(rej?.state, "REJECTED");

    // 6. Cancel invitation
    const can = await cancelCourseAccessInvitation("inv-1", "en", "csrf-token-123");
    assert.equal(can?.state, "CANCELLED");

    // 7. Resend invitation
    const res = await resendCourseAccessInvitation("inv-1", "en", "csrf-token-123");
    assert.equal(res?.state, "PENDING_STUDENT_ACCEPTANCE");

    // 8. Entitlement detail
    const ent = await getAdminEntitlementDetail("ent-1", "en", "csrf-token-123");
    assert.equal(ent?.entitlement?.id, "ent-1");

    // 9. Accept student invitation
    const acc = await acceptStudentCourseAccessInvitation("inv-1", "token-secret", "en", "csrf-token-123");
    assert.equal(acc?.state, "PENDING_ADMIN_APPROVAL");

    // 10. Student access history
    const hist = await getStudentCourseAccessHistory("en", "csrf-token-123");
    assert.equal(hist?.items?.[0]?.has_active_access, true);

  } finally {
    globalThis.fetch = originalFetch;
  }
});
