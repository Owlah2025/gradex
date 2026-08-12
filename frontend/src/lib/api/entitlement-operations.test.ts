import assert from "node:assert/strict";
import { test } from "node:test";
import { adjustEntitlementExpiry, revokeEntitlement } from "./access";

/**
 * AD07 elevated-Admin operations, at the client boundary.
 *
 * Both operations address one existing grant by the identifier the Admin
 * surface already holds, carry the required reason, and send the revision the
 * Admin was looking at so a concurrent change is refused server-side rather
 * than overwritten.
 */

type CapturedRequest = { url: string; method: string; body: Record<string, unknown> };

async function captureRequests(
  run: () => Promise<unknown>,
  response: Record<string, unknown>,
): Promise<CapturedRequest[]> {
  const originalFetch = globalThis.fetch;
  const captured: CapturedRequest[] = [];
  globalThis.fetch = (async (url: unknown, init?: RequestInit) => {
    const target = String(url);
    if (target.includes("/session/bootstrap")) {
      return new Response(JSON.stringify({ csrf_token: "csrf-token-123" }), { status: 200 });
    }
    captured.push({
      url: target,
      method: init?.method || "GET",
      body: init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : {},
    });
    return new Response(JSON.stringify(response), { status: 200 });
  }) as typeof globalThis.fetch;
  try {
    await run();
  } finally {
    globalThis.fetch = originalFetch;
  }
  return captured;
}

const ENTITLEMENT_ID = "0a000000-0000-4000-8000-00000000e001";

test("an expiry adjustment sends the date, reason and the observed revision", async () => {
  const requests = await captureRequests(
    () =>
      adjustEntitlementExpiry(
        ENTITLEMENT_ID,
        "2026-12-31",
        "Semester extended for the whole cohort",
        { supportReference: "SUPPORT-1", expectedRevision: 3 },
        "en",
        "csrf-token-123",
      ),
    { entitlement: { id: ENTITLEMENT_ID, state: "ACTIVE", revision: 4 }, adjustments: [] },
  );

  assert.equal(requests.length, 1);
  const [request] = requests;
  assert.equal(request.method, "PUT");
  assert.equal(
    new URL(request.url, "https://gradex.test").pathname,
    `/api/v1/admin/entitlements/${ENTITLEMENT_ID}/expiry`,
  );
  assert.equal(request.body.date, "2026-12-31");
  assert.equal(request.body.reason, "Semester extended for the whole cohort");
  assert.equal(request.body.support_reference, "SUPPORT-1");
  assert.equal(request.body.expected_revision, 3);
});

test("a revocation sends its required reason and never an expiry", async () => {
  const requests = await captureRequests(
    () =>
      revokeEntitlement(
        ENTITLEMENT_ID,
        "Access ended after out-of-band refund",
        { expectedRevision: 2 },
        "en",
        "csrf-token-123",
      ),
    { entitlement: { id: ENTITLEMENT_ID, state: "REVOKED", revision: 3 }, adjustments: [] },
  );

  assert.equal(requests.length, 1);
  const [request] = requests;
  assert.equal(request.method, "POST");
  assert.equal(
    new URL(request.url, "https://gradex.test").pathname,
    `/api/v1/admin/entitlements/${ENTITLEMENT_ID}/revocation`,
  );
  assert.equal(request.body.reason, "Access ended after out-of-band refund");
  assert.equal(request.body.expected_revision, 2);
  assert.ok(!("date" in request.body), "revocation must not carry an expiry");
  // Revocation is a lifecycle transition, not a deletion.
  assert.notEqual(request.method, "DELETE");
});

test("an omitted support reference is left out rather than sent empty", async () => {
  const requests = await captureRequests(
    () => adjustEntitlementExpiry(ENTITLEMENT_ID, "2026-12-31", "reason", {}, "en", "csrf-token-123"),
    { entitlement: { id: ENTITLEMENT_ID, state: "ACTIVE", revision: 2 }, adjustments: [] },
  );
  assert.equal(requests[0].body.support_reference, undefined);
  assert.equal(requests[0].body.expected_revision, undefined);
});
