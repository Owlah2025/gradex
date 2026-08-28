import { test } from "node:test";
import assert from "node:assert/strict";
import {
  getCoursePriceHistory,
  setCoursePrice,
  setSectionPrice,
  getOwnedCourses,
  getOwnedCourseDetail,
  delistCourse,
  restoreCourseAccess,
  assignAdminTaxonomy,
  assignInstructorTaxonomy,
  createTaxonomyTerm,
  deleteTaxonomyTerm,
  renameTaxonomyTerm,
  retireTaxonomyTerm,
} from "./catalog";

test("lifecycle wrappers send the secured route and body", async () => {
  const originalFetch = globalThis.fetch;
  const requests: Array<{ url: string; init?: RequestInit }> = [];
  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), init });
    return new Response(JSON.stringify({}), { status: 200 });
  };

  try {
    await delistCourse({ courseID: "course-123", locale: "en", csrf: "csrf-123" });
    await restoreCourseAccess({ courseID: "course-123", locale: "en", csrf: "csrf-123", reason: "Incident resolved" });

    assert.deepEqual(requests.map(({ url, init }) => ({
      url,
      method: init?.method,
      csrf: new Headers(init?.headers).get("X-CSRF-Token"),
      body: init?.body,
    })), [
      { url: "/api/v1/admin/courses/course-123/delist", method: "POST", csrf: "csrf-123", body: undefined },
      { url: "/api/v1/admin/courses/course-123/access-suspension", method: "DELETE", csrf: "csrf-123", body: JSON.stringify({ reason: "Incident resolved" }) },
    ]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("catalog API wrappers fail closed when server returns 204/null empty body", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () =>
    new Response(null, { status: 204 });

  try {
    const cases: Array<{
      name: string;
      fn: () => Promise<unknown>;
      expectedError: string;
    }> = [
      {
        name: "getCoursePriceHistory",
        fn: () => getCoursePriceHistory("course-123", "en"),
        expectedError: "No price history returned from server",
      },
      {
        name: "setCoursePrice",
        fn: () =>
          setCoursePrice({
            courseID: "course-123",
            priceMinorUnits: 25000,
            reason: "Testing",
            locale: "en",
            csrf: "csrf-token-123",
          }),
        expectedError: "Server returned an empty response for price update",
      },
      {
        name: "setSectionPrice",
        fn: () =>
          setSectionPrice({
            courseID: "course-123",
            sectionID: "sec-456",
            priceMinorUnits: 10000,
            reason: "Testing",
            locale: "en",
            csrf: "csrf-token-123",
          }),
        expectedError: "Server returned an empty response for price update",
      },
      {
        name: "getOwnedCourses",
        fn: () => getOwnedCourses("en"),
        expectedError: "No owned courses returned from server",
      },
      {
        name: "getOwnedCourseDetail",
        fn: () => getOwnedCourseDetail("course-123", "en"),
        expectedError: "No course details returned from server",
      },
    ];

    for (const tc of cases) {
      await assert.rejects(
        tc.fn,
        (err: Error) => {
          assert.equal(
            err.message,
            tc.expectedError,
            `wrapper ${tc.name} got error message "${err.message}", want "${tc.expectedError}"`
          );
          return true;
        },
        `wrapper ${tc.name} did not reject on 204/null response`
      );
    }
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("pricing mutation wrappers reject when session CSRF token is missing before fetch", async () => {
  let fetchCalled = false;
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => {
    fetchCalled = true;
    return new Response("{}", { status: 200 });
  };

  try {
    const cases: Array<{
      name: string;
      fn: () => Promise<unknown>;
    }> = [
      {
        name: "setCoursePrice",
        fn: () =>
          setCoursePrice({
            courseID: "course-123",
            priceMinorUnits: 25000,
            reason: "Testing",
            locale: "en",
            csrf: "",
          }),
      },
      {
        name: "setSectionPrice",
        fn: () =>
          setSectionPrice({
            courseID: "course-123",
            sectionID: "sec-456",
            priceMinorUnits: 10000,
            reason: "Testing",
            locale: "en",
            csrf: "",
          }),
      },
    ];

    for (const tc of cases) {
      fetchCalled = false;
      await assert.rejects(
        tc.fn,
        (err: Error) => {
          assert.equal(
            err.message,
            "CSRF token is required for price mutation",
            `wrapper ${tc.name} got error "${err.message}", want "CSRF token is required for price mutation"`
          );
          return true;
        },
        `wrapper ${tc.name} did not reject when CSRF is missing`
      );
      assert.equal(fetchCalled, false, `fetch should not be called when CSRF is missing in ${tc.name}`);
    }
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("getOwnedCourses and getOwnedCourseDetail preserve real Go nested revision and Section wire contract", async () => {
  const representativeGoResponse = {
    id: "30000000-0000-0000-0000-000000000001",
    owner_account_id: "10000000-0000-0000-0000-000000000002",
    lifecycle: "DRAFT",
    live_revision_id: null,
    editable_revision: {
      id: "40000000-0000-0000-0000-000000000001",
      course_id: "30000000-0000-0000-0000-000000000001",
      revision_number: 1,
      state: "DRAFT",
      title_ar: "عنوان المقرر الحقيقي",
      title_en: "Real Course Title",
      description_ar: "الوصف",
      description_en: "Description",
      sections: [
        {
          revision_id: "40000000-0000-0000-0000-000000000001",
          course_id: "30000000-0000-0000-0000-000000000001",
          id: "sec-id-001",
          title_ar: "قسم 1",
          title_en: "Section 1",
          position: 1,
          price_minor_units: 10000,
          lessons: [],
        },
      ],
    },
    live_revision: null,
    price_minor_units: 25000,
    created_at: "2026-07-28T20:00:00Z",
    updated_at: "2026-07-28T20:00:00Z",
  };

  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (url: string | URL | Request) => {
    const urlStr = String(url);
    if (urlStr.endsWith("/courses")) {
      return new Response(JSON.stringify([representativeGoResponse]), { status: 200 });
    }
    return new Response(JSON.stringify(representativeGoResponse), { status: 200 });
  };

  try {
    const list = await getOwnedCourses("en");
    assert.equal(list.length, 1);
    assert.equal(list[0].id, "30000000-0000-0000-0000-000000000001");
    assert.equal(list[0].price_minor_units, 25000);
    assert.equal(list[0].editable_revision?.title_en, "Real Course Title");

    const detail = await getOwnedCourseDetail("30000000-0000-0000-0000-000000000001", "en");
    assert.equal(detail.id, "30000000-0000-0000-0000-000000000001");
    assert.equal(detail.editable_revision?.sections?.length, 1);
    assert.equal(detail.editable_revision?.sections?.[0].id, "sec-id-001");
    assert.equal(detail.editable_revision?.sections?.[0].position, 1);
    assert.equal(detail.editable_revision?.sections?.[0].price_minor_units, 10000);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("taxonomy wrappers use explicit revision routes and Admin term endpoints", async () => {
  const originalFetch = globalThis.fetch;
  const requests: Array<{ url: string; init?: RequestInit }> = [];
  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), init });
    if (String(url).endsWith("/terms") && init?.method === "POST") {
      return new Response(JSON.stringify({ id: "term-1" }), { status: 201 });
    }
    if (init?.method === "DELETE") return new Response(null, { status: 204 });
    return new Response(JSON.stringify({ id: "term-1" }), { status: 200 });
  };

  try {
    const common = { locale: "en" as const, csrf: "csrf-123" };
    await assignInstructorTaxonomy({ ...common, courseID: "course-1", revisionID: "revision-1", majorTermID: "major-1", subjectTermID: "subject-1" });
    await assignAdminTaxonomy({ ...common, courseID: "course-1", revisionID: "revision-2", majorTermID: "major-1", subjectTermID: "subject-1" });
    await createTaxonomyTerm({ ...common, kind: "SUBJECT", labelAr: "فيزياء", labelEn: "Physics", academicCode: "PHY101" });
    await renameTaxonomyTerm({ ...common, termID: "term-1", labelAr: "فيزياء محدثة", labelEn: "Updated Physics" });
    await retireTaxonomyTerm({ ...common, termID: "term-1" });
    await deleteTaxonomyTerm({ ...common, termID: "term-2" });

    assert.deepEqual(requests.map(({ url, init }) => ({ url, method: init?.method, body: init?.body })), [
      { url: "/api/v1/courses/course-1/revisions/revision-1", method: "PATCH", body: JSON.stringify({ major_term_id: "major-1", subject_term_id: "subject-1" }) },
      { url: "/api/v1/admin/courses/course-1/taxonomy", method: "PUT", body: JSON.stringify({ revision_id: "revision-2", major_term_id: "major-1", subject_term_id: "subject-1" }) },
      { url: "/api/v1/admin/taxonomy/terms", method: "POST", body: JSON.stringify({ kind: "SUBJECT", label_ar: "فيزياء", label_en: "Physics", academic_code: "PHY101" }) },
      { url: "/api/v1/admin/taxonomy/terms/term-1", method: "PATCH", body: JSON.stringify({ label_ar: "فيزياء محدثة", label_en: "Updated Physics" }) },
      { url: "/api/v1/admin/taxonomy/terms/term-1/retire", method: "POST", body: undefined },
      { url: "/api/v1/admin/taxonomy/terms/term-2", method: "DELETE", body: undefined },
    ]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("taxonomy mutations fail closed before fetch without a session CSRF token", async () => {
  const originalFetch = globalThis.fetch;
  let fetchCalled = false;
  globalThis.fetch = async () => {
    fetchCalled = true;
    return new Response("{}", { status: 200 });
  };

  try {
    await assert.rejects(
      () => createTaxonomyTerm({ locale: "en", csrf: "", kind: "MAJOR", labelAr: "علوم", labelEn: "Science" }),
      /Session CSRF token is required/,
    );
    assert.equal(fetchCalled, false, "taxonomy mutation must not call fetch without CSRF");
  } finally {
    globalThis.fetch = originalFetch;
  }
});
