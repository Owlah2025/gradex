import assert from "node:assert/strict";
import { test } from "node:test";
import { getSubjects } from "./subject-catalogue";

test("Subject discovery sends institution, Program, availability and pagination to one server query", async () => {
  const originalFetch = globalThis.fetch;
  let requestURL = "";
  let requestSignal: AbortSignal | null | undefined;
  globalThis.fetch = async (url, init) => {
    requestURL = String(url);
    requestSignal = init?.signal;
    return new Response(
      JSON.stringify({ items: [], page: 1, page_size: 12, total: 0 }),
      { status: 200 },
    );
  };
  const controller = new AbortController();

  try {
    await getSubjects("en", {
      institution: "kuwait-university",
      program: "computer-science",
      availability: "served",
      page: 2,
      pageSize: 12,
      signal: controller.signal,
    });
    const parsed = new URL(requestURL, "https://gradex.test");
    assert.equal(parsed.pathname, "/api/v1/catalog/subjects");
    assert.equal(parsed.searchParams.get("institution"), "kuwait-university");
    assert.equal(parsed.searchParams.get("program"), "computer-science");
    assert.equal(parsed.searchParams.get("availability"), "served");
    assert.equal(parsed.searchParams.get("page"), "2");
    assert.equal(parsed.searchParams.get("page_size"), "12");
    assert.equal(requestSignal, controller.signal);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("the all-availability state is represented by no server parameter", async () => {
  const originalFetch = globalThis.fetch;
  let requestURL = "";
  globalThis.fetch = async (url) => {
    requestURL = String(url);
    return new Response(
      JSON.stringify({ items: [], page: 1, page_size: 40, total: 0 }),
      { status: 200 },
    );
  };

  try {
    await getSubjects("ar", { availability: "all" });
    assert.equal(
      new URL(requestURL, "https://gradex.test").searchParams.has("availability"),
      false,
    );
  } finally {
    globalThis.fetch = originalFetch;
  }
});
