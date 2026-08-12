import assert from "node:assert/strict";
import { test } from "node:test";
import {
  completeStaffInvitation,
  createStaffInvitation,
  listInstructorStaffAccounts,
  previewStaffInvitation,
} from "./identity";

test("staff invitation client keeps preview bearer in a header and completion in the body", async () => {
  const originalFetch = globalThis.fetch;
  const requests: Array<{
    url: string;
    method: string;
    headers: Record<string, string>;
    body?: string;
  }> = [];
  globalThis.fetch = async (url, init) => {
    requests.push({
      url: String(url),
      method: init?.method || "GET",
      headers: init?.headers as Record<string, string>,
      body: init?.body?.toString(),
    });
    if (String(url).endsWith("/preview")) {
      return new Response(
        JSON.stringify({ invited_role: "INSTRUCTOR", state: "PENDING" }),
        { status: 200 },
      );
    }
    return new Response(
      JSON.stringify({ account_id: "account-1", invited_role: "INSTRUCTOR" }),
      { status: 200 },
    );
  };
  try {
    await previewStaffInvitation("BEARER_CANARY", "ar");
    await completeStaffInvitation(
      "BEARER_CANARY",
      "اسم مستخدم",
      "correct horse battery staple",
      "ar",
    );
    assert.equal(requests[0].url, "/api/v1/staff-invitations/preview");
    assert.equal(
      requests[0].headers["X-Gradex-Invitation-Bearer"],
      "BEARER_CANARY",
    );
    assert.ok(!requests[0].url.includes("BEARER_CANARY"));
    assert.ok(!requests[1].url.includes("BEARER_CANARY"));
    assert.deepEqual(JSON.parse(requests[1].body || "{}"), {
      bearer: "BEARER_CANARY",
      display_name: "اسم مستخدم",
      password: "correct horse battery staple",
    });
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("staff operations use the narrow Instructor endpoint and never return an invitation bearer", async () => {
  const originalFetch = globalThis.fetch;
  const requests: Array<{ url: string; method: string; body?: string }> = [];
  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), method: init?.method || "GET", body: init?.body?.toString() });
    if (String(url).endsWith("/instructors")) {
      return new Response(JSON.stringify({ instructors: [{ id: "instructor-1", email: "instructor@example.com", display_name: "Instructor", status: "ACTIVE", created_at: "2026-08-12T00:00:00Z" }] }), { status: 200 });
    }
    return new Response(JSON.stringify({ id: "invite-1", email: "instructor@example.com", invited_role: "INSTRUCTOR", created_at: "2026-08-12T00:00:00Z" }), { status: 201 });
  };
  try {
    const invitation = await createStaffInvitation("instructor@example.com", "en", "csrf-canary");
    const accounts = await listInstructorStaffAccounts("en");
    assert.ok(invitation);
    assert.equal("bearer" in invitation, false);
    assert.equal(JSON.parse(requests[0].body || "{}").role, "INSTRUCTOR");
    assert.equal(requests[1].url, "/api/v1/staff-invitations/instructors");
    assert.equal(accounts.instructors[0].status, "ACTIVE");
  } finally {
    globalThis.fetch = originalFetch;
  }
});
