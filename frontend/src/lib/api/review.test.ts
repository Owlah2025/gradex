import { test } from "node:test";
import assert from "node:assert/strict";
import {
  approveCourseRevision,
  getReviewCourseRevision,
  listReviewQueue,
  requestCourseRevisionChanges,
} from "./review";

const COURSE_ID = "22f215eb-42fc-4bcd-b01e-37ea967a90b8";
const REVISION_ID = "9c1f0b2a-1111-4a2b-8c3d-4e5f60718293";

/** One row exactly as `catalog.ReviewQueueItem` serializes it from Postgres. */
const serverQueueRow = {
  course_id: COURSE_ID,
  owner_account_id: "a0000000-0000-0000-0000-000000000003",
  revision_id: REVISION_ID,
  revision_number: 1,
  title_ar: "هندسة البرمجيات",
  title_en: "Software Engineering",
  submitted_at: "2026-08-10T09:15:00Z",
  course_lifecycle: "DRAFT",
  is_first_publish: true,
};

test("the review queue is read from the Admin review route and carries the server's real IDs", async () => {
  const originalFetch = globalThis.fetch;
  const requests: Array<{ url: string; method?: string }> = [];
  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), method: init?.method });
    return new Response(JSON.stringify([serverQueueRow]), { status: 200 });
  };

  try {
    const queue = await listReviewQueue("en");
    assert.deepEqual(requests, [{ url: "/api/v1/admin/review/queue", method: "GET" }]);
    assert.equal(queue.length, 1);
    assert.equal(queue[0].course_id, COURSE_ID);
    assert.equal(queue[0].revision_id, REVISION_ID);
    assert.equal(queue[0].title_en, "Software Engineering");
    assert.equal(queue[0].is_first_publish, true);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("an empty server queue is returned as an empty queue, never as substitute content", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => new Response(JSON.stringify([]), { status: 200 });

  try {
    assert.deepEqual(await listReviewQueue("en"), []);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("a bodyless review-queue response fails closed rather than reading as an empty queue", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => new Response(null, { status: 204 });

  try {
    await assert.rejects(() => listReviewQueue("en"), /No review queue returned from server/);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("approve and request-changes address the authoritative Course and revision IDs", async () => {
  const originalFetch = globalThis.fetch;
  const requests: Array<{ url: string; method?: string; csrf: string | null; body: unknown }> = [];
  globalThis.fetch = async (url, init) => {
    requests.push({
      url: String(url),
      method: init?.method,
      csrf: new Headers(init?.headers).get("X-CSRF-Token"),
      body: init?.body,
    });
    return new Response(JSON.stringify({ id: COURSE_ID, lifecycle: "PUBLISHED" }), { status: 200 });
  };

  try {
    await approveCourseRevision({ courseID: COURSE_ID, revisionID: REVISION_ID, locale: "en", csrf: "csrf-1" });
    await requestCourseRevisionChanges({
      courseID: COURSE_ID,
      revisionID: REVISION_ID,
      reason: "  Add a Lesson video  ",
      locale: "en",
      csrf: "csrf-1",
    });
    await getReviewCourseRevision(COURSE_ID, REVISION_ID, "en");

    assert.deepEqual(requests, [
      {
        url: `/api/v1/admin/review/courses/${COURSE_ID}/revisions/${REVISION_ID}/approve`,
        method: "POST",
        csrf: "csrf-1",
        body: undefined,
      },
      {
        url: `/api/v1/admin/review/courses/${COURSE_ID}/revisions/${REVISION_ID}/request-changes`,
        method: "POST",
        csrf: "csrf-1",
        // The reason is trimmed, and nothing else about it is rewritten.
        body: JSON.stringify({ reason: "Add a Lesson video" }),
      },
      {
        url: `/api/v1/admin/review/courses/${COURSE_ID}/revisions/${REVISION_ID}`,
        method: "GET",
        csrf: null,
        body: undefined,
      },
    ]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("review mutations fail closed before fetch without a session CSRF token", async () => {
  const originalFetch = globalThis.fetch;
  let fetchCalled = false;
  globalThis.fetch = async () => {
    fetchCalled = true;
    return new Response("{}", { status: 200 });
  };

  try {
    await assert.rejects(
      () => approveCourseRevision({ courseID: COURSE_ID, revisionID: REVISION_ID, locale: "en", csrf: "" }),
      /Session CSRF token is missing/,
    );
    await assert.rejects(
      () =>
        requestCourseRevisionChanges({
          courseID: COURSE_ID,
          revisionID: REVISION_ID,
          reason: "Anything",
          locale: "en",
          csrf: "",
        }),
      /Session CSRF token is missing/,
    );
    assert.equal(fetchCalled, false, "a review mutation must not call fetch without a CSRF token");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("a change request without a reason is refused before it reaches the server", async () => {
  const originalFetch = globalThis.fetch;
  let fetchCalled = false;
  globalThis.fetch = async () => {
    fetchCalled = true;
    return new Response("{}", { status: 200 });
  };

  try {
    await assert.rejects(
      () =>
        requestCourseRevisionChanges({
          courseID: COURSE_ID,
          revisionID: REVISION_ID,
          reason: "   ",
          locale: "en",
          csrf: "csrf-1",
        }),
      /Reason for change request is mandatory/,
    );
    assert.equal(fetchCalled, false, "an empty reason must not be sent to the server");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("review reads and mutations never bootstrap an anonymous browser session", async () => {
  const originalFetch = globalThis.fetch;
  const urls: string[] = [];
  globalThis.fetch = async (url) => {
    urls.push(String(url));
    return new Response(JSON.stringify([]), { status: 200 });
  };

  try {
    await listReviewQueue("en");
    assert.ok(
      !urls.some((url) => url.includes("/session/bootstrap")),
      "an Admin review read must carry session authority, not acquire anonymous admission",
    );
  } finally {
    globalThis.fetch = originalFetch;
  }
});
