import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { buildProtectedServerRequest } from "./learning-server-request";

test("server protected adapter forwards only the session cookie and locale", () => {
  const request = buildProtectedServerRequest(
    "/learn/dashboard",
    "ar",
    "__Host-gradex_anon=anonymous; __Host-gradex_session=opaque; theme=dark",
  );
  const headers = request.init.headers as Record<string, string>;

  assert.match(request.url, /\/api\/v1\/learn\/dashboard$/);
  assert.equal(request.init.method, "GET");
  assert.equal(request.init.credentials, "include");
  assert.equal(request.init.cache, "no-store");
  assert.deepEqual(headers, {
    Accept: "application/json, application/problem+json",
    "Accept-Language": "ar",
    Cookie: "__Host-gradex_session=opaque",
  });
  assert.equal("Authorization" in headers, false);
  assert.equal("Host" in headers, false);
  assert.equal("X-Forwarded-For" in headers, false);
});

test("protected learning pages remain dynamic and uncached", () => {
  const frontendRoot = process.cwd().endsWith("/frontend") ? process.cwd() : join(process.cwd(), "frontend");
  const serverAdapter = readFileSync(join(frontendRoot, "src/lib/api/learning-server.ts"), "utf8");
  assert.match(serverAdapter, /^import ["']server-only["'];/m);
  const pages = [
    "src/app/[locale]/learn/dashboard/page.tsx",
    "src/app/[locale]/learn/courses/[courseId]/page.tsx",
    "src/app/[locale]/learn/courses/[courseId]/lessons/[lessonId]/page.tsx",
  ];
  for (const page of pages) {
    const source = readFileSync(join(frontendRoot, page), "utf8");
    assert.match(source, /export const dynamic = ["']force-dynamic["']/);
    assert.match(source, /export const revalidate = 0/);
  }
});
