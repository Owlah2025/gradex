import assert from "node:assert/strict";
import { test } from "node:test";
import { ProblemError } from "./problem";
import {
  requestCourseHome,
  requestLearningDashboard,
  requestLessonReadModel,
  requestPlayback,
} from "./learning";

test("playback authorization is a no-store Student request without a reusable URL input", async () => {
  const originalFetch = globalThis.fetch;
  const requests: Array<{ url: string; init?: RequestInit }> = [];
  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), init });
    return new Response(JSON.stringify({
      playback_session: "session-1", manifest_url: "https://media.example/manifest.m3u8", asset_version_id: "version-1", expires_at: "2026-08-01T12:00:00Z",
    }), { status: 200 });
  };
  try {
    await requestPlayback("lesson / id", "en", "csrf-token");
    assert.deepEqual(requests, [{
      url: "/api/v1/learn/lessons/lesson%20%2F%20id/playback",
      init: {
        method: "POST", credentials: "same-origin", cache: "no-store",
        headers: {
          Accept: "application/json, application/problem+json", "Accept-Language": "en", "X-CSRF-Token": "csrf-token",
        }, body: undefined,
      },
    }]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("read-model clients preserve D-063 paths, credentials, locale, and wire shapes", async () => {
  const originalFetch = globalThis.fetch;
  const requests: Array<{ url: string; init?: RequestInit }> = [];
  const responses = [
    {
      name: "dashboard",
      call: () => requestLearningDashboard("ar"),
      url: "/api/v1/learn/dashboard",
      locale: "ar",
      body: { courses: [] },
    },
    {
      name: "course home",
      call: () => requestCourseHome("course/id", "en"),
      url: "/api/v1/learn/courses/course%2Fid",
      locale: "en",
      body: {
        course_id: "course-id", title: "Course", learning_status: "expired", expires_at: null,
        progress: { completed_lessons: 0, total_lessons: 1, percent: 0 }, sections: [{
          section_id: "section-id", title: "Section", lessons: [{
            lesson_id: "lesson-id", title: "Lesson", progress: { position_seconds: 0, completed: false },
            materials: [{ kind: "resource" }, { kind: "lab_material" }],
          }],
        }],
      },
    },
    {
      name: "lesson",
      call: () => requestLessonReadModel("course-id", "lesson/id", "ar"),
      url: "/api/v1/learn/courses/course-id/lessons/lesson%2Fid",
      locale: "ar",
      body: {
        course_id: "course-id", lesson_id: "lesson-id", section: { section_id: "section-id", title: "Section" },
        title: "Lesson", learning_status: "active", expires_at: "2026-12-22T20:59:59Z",
        progress: { position_seconds: 12.5, completed: false },
        navigation: { previous_lesson_id: null, next_lesson_id: "next-id" },
        materials: [{ kind: "resource" }],
      },
    },
  ];
  let responseIndex = 0;
  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), init });
    return new Response(JSON.stringify(responses[responseIndex++].body), { status: 200 });
  };
  try {
    for (const response of responses) {
      const result = await response.call();
      assert.deepEqual(result, response.body, response.name);
    }
    assert.deepEqual(requests.map(({ url }) => url), responses.map(({ url }) => url));
    for (const [index, { init }] of requests.entries()) {
      assert.equal(init?.method, "GET");
      assert.equal(init?.credentials, "same-origin");
      assert.equal(init?.cache, "no-store");
      assert.deepEqual(init?.headers, {
        Accept: "application/json, application/problem+json",
        "Accept-Language": responses[index].locale,
      });
    }
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("read-model clients preserve the existing ProblemError path", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => new Response(JSON.stringify({
    type: "https://gradex.example/problems/not-found",
    title: "Not found",
    status: 404,
    code: "PROTECTED_UNAVAILABLE",
  }), { status: 404 });
  try {
    await assert.rejects(requestCourseHome("course-id", "en"), (error: unknown) => error instanceof ProblemError);
  } finally {
    globalThis.fetch = originalFetch;
  }
});
