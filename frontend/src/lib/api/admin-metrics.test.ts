import assert from "node:assert/strict";
import test from "node:test";
import {
  buildAdminMetricsPageQuery,
  defaultAdminCourseMetricsFilters,
  listAdminMetricCourses,
  metricNumber,
  metricSubjectDemand,
  metricByKey,
} from "./admin-metrics";

test("admin metrics query builder keeps sorting and pagination explicit", () => {
  const query = new URLSearchParams(
    buildAdminMetricsPageQuery({ sort: "average_progress", direction: "desc", page: 2, limit: 10 }),
  );
  assert.equal(query.get("sort"), "average_progress");
  assert.equal(query.get("direction"), "desc");
  assert.equal(query.get("page"), "2");
  assert.equal(query.get("limit"), "10");
});

test("default analytics course load sends the title sort explicitly", async () => {
  const originalFetch = globalThis.fetch;
  let requestURL = "";
  globalThis.fetch = async (url) => {
    requestURL = String(url);
    return new Response(JSON.stringify({ items: [], total: 0, page: 1, limit: 10, has_more: false }), { status: 200 });
  };

  try {
    await listAdminMetricCourses("en", defaultAdminCourseMetricsFilters());
    const parsed = new URL(requestURL, "https://gradex.test");
    assert.equal(parsed.pathname, "/api/v1/admin/metrics/courses");
    assert.equal(parsed.searchParams.get("sort"), "title");
    assert.equal(parsed.searchParams.get("direction"), "asc");
    assert.equal(parsed.searchParams.get("page"), "1");
    assert.equal(parsed.searchParams.get("limit"), "10");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("metric helpers distinguish numeric headlines from structured demand", () => {
  const metrics = [
    { key: "students.total", value: 12, definition_key: "students.total" },
    {
      key: "subject_demand.top",
      value: [{ subject_id: "subject-1", title: "Calculus", students: 4, served: false }],
      definition_key: "subject_demand.top",
    },
  ];
  assert.equal(metricNumber(metrics, "students.total"), 12);
  assert.equal(metricNumber(metrics, "subject_demand.top"), 0);
  assert.deepEqual(metricSubjectDemand(metrics), [
    { subject_id: "subject-1", title: "Calculus", students: 4, served: false },
  ]);
  assert.equal(metricByKey(metrics, "missing"), undefined);
});
