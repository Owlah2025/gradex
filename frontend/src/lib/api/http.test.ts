import assert from "node:assert/strict";
import test from "node:test";

import { postJSON } from "./http";

test("postJSON refreshes an anonymous CSRF token once after a stale-token rejection", async () => {
  const originalFetch = globalThis.fetch;
  const calls: Array<{ url: string; token: string | null }> = [];
  let bootstrapCount = 0;
  let postCount = 0;

  globalThis.fetch = async (input, init) => {
    const url = String(input);
    const headers = new Headers(init?.headers);
    if (url.endsWith("/session/bootstrap")) {
      bootstrapCount += 1;
      return new Response(JSON.stringify({ csrf_token: `csrf-${bootstrapCount}` }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }
    postCount += 1;
    calls.push({ url, token: headers.get("X-CSRF-Token") });
    if (postCount === 1) {
      return new Response(
        JSON.stringify({ code: "CSRF_VALIDATION_FAILED", status: 403, title: "rejected", type: "about:blank" }),
        { status: 403, headers: { "Content-Type": "application/problem+json" } },
      );
    }
    return new Response(JSON.stringify({ code: "OK" }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  };

  try {
    const result = await postJSON<{ code: string }>("/sessions", { email: "a@example.test" }, "en");
    assert.deepEqual(result, { code: "OK" });
    assert.equal(bootstrapCount, 2);
    assert.deepEqual(calls.map((call) => call.token), ["csrf-1", "csrf-2"]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});
