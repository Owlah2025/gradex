import assert from "node:assert/strict";
import { test } from "node:test";
import { getPublicCoursePreview, getPublicCourses } from "./public-catalog";

test("public catalogue search forwards the raw query to the existing list route", async () => {
  const originalFetch = globalThis.fetch;
  let requestURL = "";
  globalThis.fetch = async (url) => {
    requestURL = String(url);
    return new Response(JSON.stringify({ items: [], page: 1, page_size: 20, total: 0 }), { status: 200 });
  };

  try {
    const rawQuery = "أحياء Biology ١٠١";
    await getPublicCourses("ar", rawQuery);
    assert.equal(new URL(requestURL, "https://gradex.test").pathname, "/api/v1/catalog/courses");
    assert.equal(new URL(requestURL, "https://gradex.test").searchParams.get("q"), rawQuery);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("public preview authorization is requested by Course, never a browser-supplied asset version", async () => {
  const originalFetch = globalThis.fetch;
  let requestURL = "";
  let authorizationHeader: string | null = null;
  globalThis.fetch = async (url, init) => {
    requestURL = String(url);
    authorizationHeader = new Headers(init?.headers).get("authorization");
    return new Response(JSON.stringify({ url: "https://signed.example/preview.mp4", expires_at: "2026-08-21T12:05:00Z" }), { status: 200 });
  };

  try {
    const preview = await getPublicCoursePreview("course-public-id", "ar");
    assert.equal(preview.url, "https://signed.example/preview.mp4");
    assert.equal(new URL(requestURL, "https://gradex.test").pathname, "/api/v1/media/courses/course-public-id/preview");
    assert.equal(authorizationHeader, null);
    assert.equal(requestURL.includes("asset"), false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});
