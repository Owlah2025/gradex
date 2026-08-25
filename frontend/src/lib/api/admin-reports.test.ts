import assert from "node:assert/strict";
import test from "node:test";
import { ProblemError } from "./problem";
import { getAdminReports, resolveAdminReport } from "./admin-reports";

test("Admin report queue requests a bounded paginated read", async () => {
  const originalFetch = globalThis.fetch;
  let requestURL = "";
  try {
    globalThis.fetch = async (input) => {
      requestURL = String(input);
      return new Response(JSON.stringify({ items: [], page: 2, page_size: 20, has_next: false }), { status: 200 });
    };

    const page = await getAdminReports("en", 2, 20);
    assert.equal(requestURL, "/api/v1/admin/reports?page=2&page_size=20");
    assert.deepEqual(page.items, []);
    assert.equal(page.has_next, false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("Admin report resolution sends the action, reason, and session CSRF token", async () => {
  const originalFetch = globalThis.fetch;
  let request: RequestInit | undefined;
  try {
    globalThis.fetch = async (_input, init) => {
      request = init;
      return new Response(JSON.stringify({ id: "report-1", status: "RESOLVED" }), { status: 200 });
    };

    const report = await resolveAdminReport({
      reportID: "report-1",
      action: "DISMISS",
      reason: "No platform action required",
      locale: "ar",
      csrf: "csrf-1",
    });
    assert.equal(report.status, "RESOLVED");
    assert.equal(request?.method, "POST");
    assert.equal((request?.headers as Record<string, string>)["X-CSRF-Token"], "csrf-1");
    assert.deepEqual(JSON.parse(String(request?.body)), { action: "DISMISS", reason: "No platform action required" });
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("already-resolved API responses remain a typed conflict", async () => {
  const originalFetch = globalThis.fetch;
  try {
    globalThis.fetch = async () =>
      new Response(JSON.stringify({
        type: "https://api.gradex.com/problems/state-conflict",
        title: "State conflict",
        status: 409,
        code: "STATE_CONFLICT",
      }), { status: 409 });

    await assert.rejects(
      () => resolveAdminReport({ reportID: "report-1", action: "DISMISS", reason: "Reviewed", locale: "en", csrf: "csrf-1" }),
      (error: unknown) => error instanceof ProblemError && error.problem.status === 409,
    );
  } finally {
    globalThis.fetch = originalFetch;
  }
});
