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
  createStudentPurchaseRequest,
  listAdminPurchaseRequests,
  confirmPurchaseRequestPayment,
	  cancelPurchaseRequest,
} from "./access";

test("access API client forwards requests to correct backend endpoints", async () => {
  const originalFetch = globalThis.fetch;
  const requests: { url: string; method: string; body?: any; headers?: any }[] =
    [];

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
      return new Response(JSON.stringify({ csrf_token: "csrf-token-123" }), {
        status: 200,
      });
    }
    if (u.includes("/admin/courses/c-1/default-access-expiry")) {
      return new Response(
        JSON.stringify({
          course_id: "c-1",
          default_access_ends_at: "2026-09-01T00:00:00Z",
          reason: "test",
        }),
        { status: 200 },
      );
    }
    if (u.includes("/admin/course-access-invitations/inv-1/approve")) {
      return new Response(
        JSON.stringify({
          invitation: { id: "inv-1", state: "APPROVED" },
          entitlement: { id: "ent-1", state: "ACTIVE" },
        }),
        { status: 200 },
      );
    }
    if (u.includes("/admin/course-access-invitations/inv-1/reject")) {
      return new Response(
        JSON.stringify({
          id: "inv-1",
          state: "REJECTED",
          decision_reason: "reason",
        }),
        { status: 200 },
      );
    }
    if (u.includes("/admin/course-access-invitations/inv-1/cancel")) {
      return new Response(JSON.stringify({ id: "inv-1", state: "CANCELLED" }), {
        status: 200,
      });
    }
    if (u.includes("/admin/course-access-invitations/inv-1/resend")) {
      return new Response(
        JSON.stringify({ id: "inv-1", state: "PENDING_STUDENT_ACCEPTANCE" }),
        { status: 200 },
      );
    }
    if (u.includes("/admin/course-access-invitations") && method === "POST") {
      return new Response(
        JSON.stringify({
          id: "inv-1",
          state: "PENDING_STUDENT_ACCEPTANCE",
          email: "student@example.com",
          course_id: "c-1",
        }),
        { status: 201 },
      );
    }
    if (u.includes("/admin/course-access-invitations?page=1&limit=50")) {
      return new Response(
        JSON.stringify({ invitations: [], total: 0, page: 1, limit: 50 }),
        { status: 200 },
      );
    }
    if (u.includes("/admin/purchase-requests?")) {
      return new Response(
        JSON.stringify({
          purchase_requests: [
            {
              id: "pr-internal",
              reference: "GRX-1000",
              course_id: "c-1",
              email: "student@example.com",
              course_title: "Operating Systems",
              price_minor_units: 25000,
              currency: "KWD",
              state: "WAITING_PAYMENT",
              requested_at: "2026-09-01T00:00:00Z",
            },
          ],
          total: 1,
          page: 1,
          limit: 50,
        }),
        { status: 200 },
      );
    }
    if (u.includes("/admin/purchase-requests/pr-internal/confirm-payment")) {
      return new Response(
        JSON.stringify({
          purchase_request: {
            id: "pr-internal",
            reference: "GRX-1000",
            state: "INVITATION_CREATED",
          },
          invitation: { id: "inv-1", state: "PENDING_STUDENT_ACCEPTANCE" },
        }),
        { status: 200 },
      );
    }
	if (u.includes("/admin/purchase-requests/pr-internal/cancel")) {
		return new Response(JSON.stringify({ id: "pr-internal", state: "CANCELLED" }), { status: 200 });
	}
    if (u.includes("/me/purchase-requests") && method === "POST") {
      return new Response(
        JSON.stringify({
          reference: "GRX-1000",
          whatsapp_url: "https://wa.me/15550000000?text=Operating%20Systems",
          course_title: "Operating Systems",
          price_minor_units: 25000,
          currency: "KWD",
          state: "WAITING_PAYMENT",
          reused: false,
        }),
        { status: 201 },
      );
    }
    if (u.includes("/admin/entitlements/ent-1")) {
      return new Response(
        JSON.stringify({
          entitlement: { id: "ent-1", state: "ACTIVE" },
          adjustments: [],
        }),
        { status: 200 },
      );
    }
    if (u.includes("/me/course-access-invitations/inv-1/accept")) {
      return new Response(
        JSON.stringify({ id: "inv-1", state: "PENDING_ADMIN_APPROVAL" }),
        { status: 200 },
      );
    }
    if (u.includes("/me/course-access")) {
      return new Response(
        JSON.stringify({
          items: [{ course_id: "c-1", has_active_access: true }],
        }),
        { status: 200 },
      );
    }

    return new Response(JSON.stringify({}), { status: 200 });
  };

  try {
    // 1. Expiry config
    const exp = await setCourseDefaultAccessExpiry(
      "c-1",
      "2026-09-01",
      "test",
      "en",
      "csrf-token-123",
    );
    assert.equal(exp?.course_id, "c-1");

    // 2. Create invitation
    const inv = await createCourseAccessInvitation(
      "c-1",
      "student@example.com",
      undefined,
      undefined,
      "en",
      "csrf-token-123",
    );
    assert.equal(inv?.id, "inv-1");

    // 3. List admin invitations
    const list = await listAdminCourseAccessInvitations(
      1,
      50,
      "en",
      "csrf-token-123",
    );
    assert.equal(list?.total, 0);

    // 4. Approve invitation
    const app = await approveCourseAccessInvitation(
      "inv-1",
      "en",
      "csrf-token-123",
    );
    assert.equal(app?.entitlement?.id, "ent-1");

    // 5. Reject invitation
    const rej = await rejectCourseAccessInvitation(
      "inv-1",
      "reason",
      "en",
      "csrf-token-123",
    );
    assert.equal(rej?.state, "REJECTED");

    // 6. Cancel invitation
    const can = await cancelCourseAccessInvitation(
      "inv-1",
      "en",
      "csrf-token-123",
    );
    assert.equal(can?.state, "CANCELLED");

    // 7. Resend invitation
    const res = await resendCourseAccessInvitation(
      "inv-1",
      "en",
      "csrf-token-123",
    );
    assert.equal(res?.state, "PENDING_STUDENT_ACCEPTANCE");

    // 8. Entitlement detail
    const ent = await getAdminEntitlementDetail(
      "ent-1",
      "en",
      "csrf-token-123",
    );
    assert.equal(ent?.entitlement?.id, "ent-1");

    // 9. Accept student invitation
    const acc = await acceptStudentCourseAccessInvitation(
      "inv-1",
      "token-secret",
      "en",
      "csrf-token-123",
    );
    assert.equal(acc?.state, "PENDING_ADMIN_APPROVAL");

    // 10. Student access history
    const hist = await getStudentCourseAccessHistory("en", "csrf-token-123");
    assert.equal(hist?.items?.[0]?.has_active_access, true);

    // 11. A Purchase Request carries the Course identity and nothing else. The
    // browser supplies no email, no price, no payment state, and no
    // entitlement flag: every one of those is read from the server's own
    // records, and the email in particular decides where Course access is
    // eventually sent.
    const purchase = await createStudentPurchaseRequest("c-1", "en", "csrf-token-123");
    assert.equal(purchase.reference, "GRX-1000");
    assert.match(purchase.whatsapp_url, /^https:\/\/wa\.me\//);

    // 12. The Admin queue uses its human-safe reference and semantic command.
    const purchases = await listAdminPurchaseRequests(
      { query: "GRX-1000" },
      "en",
    );
    assert.equal(purchases?.purchase_requests[0]?.reference, "GRX-1000");
    const confirmed = await confirmPurchaseRequestPayment(
      "pr-internal",
      "en",
      "csrf-token-123",
    );
    assert.equal(confirmed?.purchase_request.state, "INVITATION_CREATED");
	const cancelledPurchase = await cancelPurchaseRequest(
		"pr-internal",
		"en",
		"csrf-token-123",
	);
	assert.equal(cancelledPurchase?.state, "CANCELLED");

    const studentPurchase = requests.find((request) =>
      request.url.includes("/me/purchase-requests"),
    );
    assert.deepEqual(studentPurchase?.body, { course_id: "c-1" });
    // The absence of an email field is the security property, not a detail: a
    // client-supplied address on this route would let any caller aim someone
    // else's Course access at a mailbox they control.
    const serialized = JSON.stringify(studentPurchase?.body);
    assert.equal(serialized.includes("email"), false);
    assert.equal(serialized.includes("price"), false);
    assert.equal(serialized.includes("paid"), false);
    // It is an authenticated call: the session CSRF token must travel with it.
    assert.equal(
      (studentPurchase?.headers as Record<string, string>)["X-CSRF-Token"],
      "csrf-token-123",
    );
  } finally {
    globalThis.fetch = originalFetch;
  }
});
