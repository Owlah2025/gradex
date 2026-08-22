import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import {
  approveSubjectRequestAsNew,
  linkSubjectRequest,
  listAdminSubjectRequests,
  rejectSubjectRequest,
} from "../../lib/api/subject-requests";

type Captured = { url: string; method?: string; csrf: string | null; body: unknown };

async function withStub(run: () => Promise<void>): Promise<Captured[]> {
  const originalFetch = globalThis.fetch;
  const captured: Captured[] = [];
  globalThis.fetch = async (url, init) => {
    const request = String(url);
    if (request.endsWith("/session/bootstrap")) {
      return new Response(JSON.stringify({ csrf_token: "csrf-boot" }), { status: 200 });
    }
    captured.push({
      url: request,
      method: init?.method,
      csrf: new Headers(init?.headers).get("X-CSRF-Token"),
      body: init?.body ? JSON.parse(String(init.body)) : undefined,
    });
    return new Response(JSON.stringify(request.includes("subject-requests?") ? [] : { id: "request-1", status: "PENDING" }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  };
  try {
    await run();
  } finally {
    globalThis.fetch = originalFetch;
  }
  return captured;
}

function readSource(relative: string): string {
  const root = process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend");
  return fs.readFileSync(path.join(root, relative), "utf8");
}

test("Admin Subject request queue and all three decisions use semantic Academic Catalog routes", async () => {
  const calls = await withStub(async () => {
    await listAdminSubjectRequests("en", "PENDING");
    await linkSubjectRequest({ requestID: "request-1", subjectID: "subject-1", locale: "en", csrf: "csrf-1" });
    await approveSubjectRequestAsNew({ requestID: "request-2", locale: "en", csrf: "csrf-2" });
    await rejectSubjectRequest({ requestID: "request-3", reason: "Not an official title", locale: "en", csrf: "csrf-3" });
  });
  assert.match(calls[0].url, /\/admin\/academic\/subject-requests\?status=PENDING$/);
  assert.match(calls[1].url, /\/admin\/academic\/subject-requests\/request-1\/link$/);
  assert.deepEqual(calls[1].body, { subject_id: "subject-1" });
  assert.match(calls[2].url, /\/admin\/academic\/subject-requests\/request-2\/approve-new$/);
  assert.match(calls[3].url, /\/admin\/academic\/subject-requests\/request-3\/reject$/);
  assert.deepEqual(calls[3].body, { reason: "Not an official title" });
  assert.deepEqual(calls.slice(1).map((call) => call.csrf), ["csrf-1", "csrf-2", "csrf-3"]);
});

test("Subject request UI is bilingual, requires a rejection reason, and renders no raw identity", () => {
  const source = readSource("src/components/admin/subject-request-queue.tsx");
  for (const copy of ["Subject Requests", "طلبات المواد", "Link to Existing", "Approve as New", "Reject", "رفض"]) {
    assert.ok(source.includes(copy), `missing ${copy}`);
  }
  assert.match(source, /disabled=\{busy === request\.id \|\| !\(reasons\[request\.id\]/,
    "Reject must remain disabled until a reason exists");
  assert.ok(!source.includes("UUID") && !source.includes("revision_id"));
  const jsxText = source.replace(/[A-Za-z-]+=\{[^{}]*(\{[^{}]*\}[^{}]*)*\}/g, "");
  assert.ok(!/\{\s*request\.(id|course_id|institution_id|requester_account_id)\s*\}/.test(jsxText));
});

test("Admin Academic Course review shows semantic context and classification-gates legacy repair", () => {
  const inspector = readSource("src/components/admin/submitted-revision-inspector.tsx");
  const source = inspector + readSource("src/components/admin/academic-review-context.tsx");
  for (const field of ["submitted-academic-university", "submitted-academic-subject", "submitted-academic-unit", "submitted-academic-audience"]) {
    assert.ok(source.includes(field), `missing Academic review field ${field}`);
  }
  assert.match(inspector, /isAcademicCourse\(course\)/);
  assert.match(source, /Automatic · inferred/);
  assert.match(source, /Customized/);
  assert.ok(!source.includes("Submitted Revision Inspector"));
});

test("public Academic presentation includes University and canonical Subject without adding filters", () => {
  const api = readSource("src/lib/api/public-catalog.ts");
  const surface = readSource("src/components/catalog/public-catalogue.tsx");
  assert.match(api, /university\?: PublicTaxonomy/);
  assert.match(surface, /course\.university/);
  for (const filter of ["university_id", "program_id", "academic_level", "subject_id"]) {
    assert.ok(!api.includes(filter), `T6 filter ${filter} started early`);
  }
});
