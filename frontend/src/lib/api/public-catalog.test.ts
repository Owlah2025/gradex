import assert from "node:assert/strict";
import { test } from "node:test";
import { getPublicCourses } from "./public-catalog";

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
