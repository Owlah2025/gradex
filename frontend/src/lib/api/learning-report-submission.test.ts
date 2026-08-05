import assert from "node:assert/strict";
import { test } from "node:test";
import { submitLearningReport } from "./learning";
import { ProblemError } from "./problem";
import { classifyReportFailure } from "../../components/learning/report-dialog-state";

/**
 * T066 contract evidence: the submission call against the exact responses T063–T065 defined.
 *
 * These drive the real client through a controlled transport, so the request it builds and the
 * outcomes it produces are checked against the accepted server contract rather than against a
 * restatement of it. The browser matrix across locales, viewports, and targets is T067's and T075's.
 */

type Capture = {
  url: string;
  method: string;
  headers: Record<string, string>;
  body: unknown;
  raw: string;
};

/** installFetch replaces global fetch with a scripted responder and records what was sent. */
function installFetch(respond: (capture: Capture) => Response): { calls: Capture[]; restore: () => void } {
  const calls: Capture[] = [];
  const original = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const raw = typeof init?.body === "string" ? init.body : "";
    const capture: Capture = {
      url: String(input),
      method: init?.method ?? "GET",
      headers: (init?.headers as Record<string, string>) ?? {},
      body: raw ? JSON.parse(raw) : undefined,
      raw,
    };
    calls.push(capture);
    return respond(capture);
  }) as typeof globalThis.fetch;
  return { calls, restore: () => { globalThis.fetch = original; } };
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": status === 201 ? "application/json" : "application/problem+json" },
  });
}

function problemBody(status: number, code: string) {
  return {
    type: `https://api.gradex.com/problems/${code.toLowerCase().replace(/_/g, "-")}`,
    title: "Refused",
    status,
    detail: "Refused.",
    code,
    request_id: "0123456789abcdef0123456789abcdef",
  };
}

const CONTEXT = "grc1.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA";

test("a submission sends the opaque context unchanged to the accepted route", async () => {
  const fetchStub = installFetch(() =>
    jsonResponse(201, { report_id: "11111111-1111-1111-1111-111111111111", created_at: "2026-08-04T09:00:00Z" }),
  );
  try {
    const acknowledgement = await submitLearningReport(
      { report_context: CONTEXT, reason: "inaccurate" },
      "ar",
      "csrf-token",
    );

    assert.equal(fetchStub.calls.length, 1, "exactly one request per submission");
    const call = fetchStub.calls[0];
    assert.equal(call.url, "/api/v1/learn/reports");
    assert.equal(call.method, "POST");
    assert.equal(call.headers["Content-Type"], "application/json");
    assert.equal(call.headers["X-CSRF-Token"], "csrf-token");
    assert.equal(call.headers["Accept-Language"], "ar");

    // Byte-for-byte the context the read model issued: not trimmed, re-encoded, or shortened.
    const body = call.body as Record<string, unknown>;
    assert.equal(body.report_context, CONTEXT);
    // The body names no target: the server derives all of it from the context.
    assert.deepEqual(Object.keys(body).sort(), ["reason", "report_context"]);

    assert.deepEqual(acknowledgement, {
      report_id: "11111111-1111-1111-1111-111111111111",
      created_at: "2026-08-04T09:00:00Z",
    });
  } finally {
    fetchStub.restore();
  }
});

test("an explanation is sent only when the Student wrote one", async () => {
  const fetchStub = installFetch(() =>
    jsonResponse(201, { report_id: "id", created_at: "2026-08-04T09:00:00Z" }),
  );
  try {
    await submitLearningReport({ report_context: CONTEXT, reason: "other", explanation: "الصوت مفقود" }, "ar", "t");
    assert.equal((fetchStub.calls[0].body as Record<string, unknown>).explanation, "الصوت مفقود");

    // Whitespace is not an explanation, and an absent field is not an empty one: the server
    // rejects unknown and malformed members, so a blank value is omitted rather than sent.
    await submitLearningReport({ report_context: CONTEXT, reason: "inaccurate", explanation: "   " }, "en", "t");
    assert.deepEqual(Object.keys(fetchStub.calls[1].body as object).sort(), ["reason", "report_context"]);
  } finally {
    fetchStub.restore();
  }
});

test("a submission is never retried automatically", async () => {
  // A resent report either duplicates the Student's own open report or spends another of their
  // five hourly attempts, so recovery must always be an explicit action.
  for (const status of [409, 429, 404, 422, 500]) {
    const fetchStub = installFetch(() => jsonResponse(status, problemBody(status, "REFUSED")));
    try {
      await assert.rejects(() =>
        submitLearningReport({ report_context: CONTEXT, reason: "inaccurate" }, "en", "t"),
      );
      assert.equal(fetchStub.calls.length, 1, `status ${status} produced ${fetchStub.calls.length} requests`);
    } finally {
      fetchStub.restore();
    }
  }
});

test("each accepted refusal reaches its generic outcome", async () => {
  const expected: [number, string, string][] = [
    [409, "STATE_CONFLICT", "duplicate"],
    [429, "RATE_LIMITED", "throttled"],
    [404, "NOT_FOUND", "unavailable"],
    [400, "MALFORMED_JSON", "invalid"],
    [413, "CONTENT_TOO_LARGE", "invalid"],
    [415, "UNSUPPORTED_MEDIA_TYPE", "invalid"],
    [422, "VALIDATION_FAILED", "invalid"],
    [500, "INTERNAL", "unexpected"],
  ];
  for (const [status, code, outcome] of expected) {
    const fetchStub = installFetch(() => jsonResponse(status, problemBody(status, code)));
    try {
      const error = await submitLearningReport({ report_context: CONTEXT, reason: "inaccurate" }, "en", "t").then(
        () => null,
        (rejection: unknown) => rejection,
      );
      assert.ok(error instanceof ProblemError, `${status} should surface as a problem`);
      assert.equal(classifyReportFailure(error), outcome, `${status} ${code}`);
      // Nothing the Student sent comes back through the error.
      assert.ok(!String(error).includes(CONTEXT), `${status} error echoed the context`);
    } finally {
      fetchStub.restore();
    }
  }
});

test("a transport failure is unexpected, not a refusal, and echoes nothing", async () => {
  const original = globalThis.fetch;
  globalThis.fetch = (async () => {
    throw new TypeError("Failed to fetch");
  }) as typeof globalThis.fetch;
  try {
    const error = await submitLearningReport({ report_context: CONTEXT, reason: "inaccurate" }, "en", "t").then(
      () => null,
      (rejection: unknown) => rejection,
    );
    assert.equal(classifyReportFailure(error), "unexpected");
    assert.ok(!String(error).includes(CONTEXT));
  } finally {
    globalThis.fetch = original;
  }
});

test("the context appears in the request body and nowhere else", async () => {
  const fetchStub = installFetch(() => jsonResponse(201, { report_id: "id", created_at: "2026-08-04T09:00:00Z" }));
  try {
    await submitLearningReport({ report_context: CONTEXT, reason: "inappropriate" }, "en", "t");
    const call = fetchStub.calls[0];
    assert.ok(!call.url.includes(CONTEXT), "the context reached the URL");
    assert.ok(
      !Object.values(call.headers).some((value) => value.includes(CONTEXT)),
      "the context reached a header",
    );
    assert.ok(call.raw.includes(CONTEXT), "the body is the one place the context belongs");
  } finally {
    fetchStub.restore();
  }
});

test("an acknowledgement carries only the two accepted properties", async () => {
  // If the server ever grew a field, the client would still surface only what it types — but this
  // pins that the accepted response is what the dialog is built against.
  const fetchStub = installFetch(() =>
    jsonResponse(201, { report_id: "id", created_at: "2026-08-04T09:00:00Z" }),
  );
  try {
    const acknowledgement = await submitLearningReport({ report_context: CONTEXT, reason: "inaccurate" }, "en", "t");
    assert.deepEqual(Object.keys(acknowledgement).sort(), ["created_at", "report_id"]);
  } finally {
    fetchStub.restore();
  }
});
