import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import {
  anonymousHeaders,
  classifyResponse,
  evaluateCapacityEvidence,
  evaluateResourceGates,
  latencyClass,
  mixTotal,
  playbackAssignment,
  validateCleanupScope,
  validateFixtureManifest,
  validateLoginIdentities,
  validateProfile,
} from "./harness.mjs";

const profile = JSON.parse(fs.readFileSync(new URL("./limited-paid-beta.json", import.meta.url), "utf8"));

test("beta profile parses with exact account arithmetic and workload percentages", () => {
  assert.equal(validateProfile(profile), true);
  assert.equal(mixTotal(profile.workload_mix), 1);
});

test("fixture shape keeps 110 accounts while providing 104 Students and 50 entitlements", () => {
  const fixture = makeFixture();
  assert.equal(validateFixtureManifest(fixture, profile), true);
  assert.equal(fixture.registered_accounts, 110);
  assert.equal(fixture.students.length, 104);
  assert.equal(fixture.students.filter((student) => student.entitled).length, 50);
});

test("login scenario selects 100 distinct Student identities", () => {
  const students = makeFixture().students;
  const identities = validateLoginIdentities(students, 100);
  assert.equal(new Set(identities).size, 100);
});

test("anonymous catalogue request removes session, token, and CSRF headers", () => {
  const headers = anonymousHeaders({ Cookie: "secret", Authorization: "Bearer secret", "X-CSRF-Token": "secret" });
  assert.equal(headers.Cookie, undefined);
  assert.equal(headers.Authorization, undefined);
  assert.equal(headers["X-CSRF-Token"], undefined);
});

test("latency governance assigns playback to control plane and writes to transactional class", () => {
  assert.equal(latencyClass("playback_authorization"), "read_control_plane");
  assert.equal(latencyClass("playback_manifest"), "read_control_plane");
  assert.equal(latencyClass("progress_write"), "transactional_write");
  assert.equal(latencyClass("login"), "transactional_write");
});

test("playback assignment gives each of 50 Students at most two starts", () => {
  const counts = Array.from({ length: 50 }, () => 0);
  for (let iteration = 0; iteration < 100; iteration += 1) counts[playbackAssignment(iteration, 50, 2)] += 1;
  assert.deepEqual(new Set(counts), new Set([2]));
});

test("error accounting keeps transport, 429, auth, entitlement, and manifest failures separate", () => {
  assert.equal(classifyResponse("login", 0, false, true).transport_failures, 1);
  assert.equal(classifyResponse("login", 429, false).http_429, 1);
  assert.equal(classifyResponse("login", 200, false).authentication_failures, 1);
  assert.equal(classifyResponse("playback_manifest", 200, false).manifest_failures, 1);
  assert.equal(classifyResponse("progress_write", 403, false).entitlement_failures, 1);
});

test("resource gates fail closed when required capture is missing and pass only with safe evidence", () => {
  const missing = evaluateResourceGates({}, profile.resource_gates);
  assert.equal(missing.pass, false);
  const safe = evaluateResourceGates({
    server_metrics: {
      host_cpu_p95_percent: 70,
      host_cpu_over_90_seconds: 0,
      memory_used_fraction: 0.5,
      swap_used_bytes: 0,
      oom_events: 0,
      unexpected_container_restarts: 0,
      disk_used_fraction: 0.5,
      disk_free_bytes: 10 * 1024 * 1024 * 1024,
    },
    postgres_metrics: { safe: true },
    redis_metrics: { safe: true },
    worker_metrics: { safe: true },
  }, profile.resource_gates);
  assert.equal(safe.pass, true);
});

test("capacity evaluator fails closed for missing evidence even when counters are zero", () => {
  const result = evaluateCapacityEvidence({ summary: { complete: true }, error_counters: {} }, profile);
  assert.equal(result.pass, false);
  assert.ok(result.failures.some((failure) => failure.includes("missing artifact")));
});

test("cleanup scope rejects wildcard or cross-run resources", () => {
  const valid = {
    run_id: "run-20260824",
    database_name: "gradex_playwright_e2e_run-20260824",
    storage_prefix: "capacity/run-20260824/",
    session_fixture_path: "/private/run-20260824/sessions.json",
    result_path: "/private/run-20260824/summary.json",
  };
  assert.equal(validateCleanupScope(valid, profile), true);
  assert.throws(() => validateCleanupScope({ ...valid, result_path: "/private/*" }, profile));
  assert.throws(() => validateCleanupScope({ ...valid, storage_prefix: "capacity/other/" }, profile));
});

function makeFixture() {
  return {
    schema_version: 2,
    profile: "limited-paid-beta",
    registered_accounts: 110,
    fingerprint: "sha256:fixture",
    students: Array.from({ length: 104 }, (_, index) => ({
      index,
      account_id: `student-${index}`,
      email: `student-${index}@example.test`,
      entitled: index < 50,
      course_id: index < 50 ? `course-${index % 8}` : undefined,
    })),
    courses: Array.from({ length: 8 }, (_, index) => ({ course_id: `course-${index}` })),
    operators: [
      { role: "ADMIN", index: 0, account_id: "admin", email: "admin@example.test", course_ids: [] },
      ...Array.from({ length: 5 }, (_, index) => ({ role: "INSTRUCTOR", index, account_id: `instructor-${index}`, email: `instructor-${index}@example.test`, course_ids: [`course-${index}`] })),
    ],
  };
}
