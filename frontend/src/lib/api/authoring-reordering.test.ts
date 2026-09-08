import assert from "node:assert/strict";
import test from "node:test";
import { reorderLessons, reorderSections } from "./authoring";

type CapturedRequest = { url: string; init?: RequestInit };

async function captureRequest(run: () => Promise<unknown>): Promise<CapturedRequest> {
  const originalFetch = globalThis.fetch;
  let captured: CapturedRequest | undefined;
  globalThis.fetch = async (input, init) => {
    captured = { url: String(input), init };
    return new Response(JSON.stringify({ id: "revision-1", title_ar: "عنوان", title_en: "Title", sections: [] }), {
      status: 200, headers: { "Content-Type": "application/json" },
    });
  };
  try {
    await run();
  } finally {
    globalThis.fetch = originalFetch;
  }
  assert.ok(captured);
  return captured;
}

test("section reorder sends the complete ordered set to the scoped revision route", async () => {
  const request = await captureRequest(() => reorderSections({
    courseID: "course/1", revisionID: "revision/1", sectionIDs: ["s3", "s1", "s2"],
    locale: "en", csrf: "csrf",
  }));
  assert.equal(request.url, "/api/v1/courses/course%2F1/revisions/revision%2F1/sections/order");
  assert.equal(request.init?.method, "PATCH");
  assert.deepEqual(JSON.parse(String(request.init?.body)), { section_ids: ["s3", "s1", "s2"] });
});

test("lesson reorder scopes the complete ordered set to one encoded section", async () => {
  const request = await captureRequest(() => reorderLessons({
    courseID: "course/1", revisionID: "revision/1", sectionID: "section/1",
    lessonIDs: ["l3", "l1", "l2"], locale: "ar", csrf: "csrf",
  }));
  assert.equal(request.url, "/api/v1/courses/course%2F1/revisions/revision%2F1/sections/section%2F1/lessons/order");
  assert.equal(request.init?.method, "PATCH");
  assert.deepEqual(JSON.parse(String(request.init?.body)), { lesson_ids: ["l3", "l1", "l2"] });
});
