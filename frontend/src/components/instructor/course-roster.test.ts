import assert from "node:assert/strict";
import { test } from "node:test";
import { getCourseRoster } from "../../lib/api/catalog";
import { courseRosterStatusLabel, courseRosterViewState } from "./course-roster-state";

const statusLabels = {
  ACTIVE: "Active",
  EXPIRED: "Expired",
  REVOKED: "Revoked",
  SUSPENDED: "Access suspended",
  DENIED: "No current access",
} as const;

test("roster view state distinguishes loading, error, empty, and populated results", () => {
  assert.equal(courseRosterViewState(true, null, 0), "loading");
  assert.equal(courseRosterViewState(false, "Request failed", 2), "error");
  assert.equal(courseRosterViewState(false, null, 0), "empty");
  assert.equal(courseRosterViewState(false, null, 1), "ready");
});

test("roster status labels remain textual for every access state", () => {
  for (const status of Object.keys(statusLabels) as Array<keyof typeof statusLabels>) {
    assert.equal(courseRosterStatusLabel(status, statusLabels), statusLabels[status]);
  }
});

test("roster API wrapper sends the selected Course and bounded page", async () => {
  const originalFetch = globalThis.fetch;
  const requests: Array<{ url: string; init?: RequestInit }> = [];
  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), init });
    return new Response(JSON.stringify({ items: [], page: 2, page_size: 5, has_next: false }), { status: 200 });
  };

  try {
    const page = await getCourseRoster("course/123", "en", 2, 5);
    assert.deepEqual(page, { items: [], page: 2, page_size: 5, has_next: false });
    assert.equal(requests[0]?.url, "/api/v1/courses/course%2F123/students?page=2&page_size=5");
    assert.equal(requests[0]?.init?.method, "GET");
    assert.equal(new Headers(requests[0]?.init?.headers).get("Accept-Language"), "en");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("roster API wrapper rejects an empty server response", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => new Response(null, { status: 204 });

  try {
    await assert.rejects(
      () => getCourseRoster("course-123", "ar"),
      (error: Error) => error.message === "لم يتم استلام قائمة الطلبة",
    );
  } finally {
    globalThis.fetch = originalFetch;
  }
});
