import http from "k6/http";
import exec from "k6/execution";
import { Counter, Trend } from "k6/metrics";
import {
  anonymousHeaders,
  classifyResponse,
  latencyClass,
  playbackAssignment,
  validateFixtureManifest,
  validateLoginIdentities,
  validateProfile,
} from "./harness.mjs";

const PROFILE = readJSON(required("GRADEX_LOADTEST_PROFILE_FILE"), "capacity profile");
validateProfile(PROFILE);
const SCENARIO = required("GRADEX_LOADTEST_SCENARIO");
const SCENARIO_KEY = scenarioKey(SCENARIO);
const SCENARIO_CONFIG = PROFILE.scenarios[SCENARIO_KEY];
if (!SCENARIO_CONFIG) throw new Error(`scenario ${SCENARIO} is absent from the beta profile`);

const TARGET = validatedTarget(required("GRADEX_LOADTEST_TARGET_URL"));
const RUN_ID = required("GRADEX_LOADTEST_RUN_ID");
const RESULT_FILE = validatedResultPath(required("GRADEX_LOADTEST_RESULT_FILE"));
const FIXTURE = readJSON(required("GRADEX_LOADTEST_FIXTURE_FILE"), "beta fixture manifest");
validateFixtureManifest(FIXTURE, PROFILE);
validateLoginIdentities(FIXTURE.students, PROFILE.fixture.login_identities);
const SESSIONS = needsSessions(SCENARIO) ? readJSON(required("GRADEX_LOADTEST_SESSION_FILE"), "beta session manifest") : null;
if (SESSIONS) validateSessions(SESSIONS, FIXTURE, PROFILE);
const PASSWORD = SCENARIO === "login-surge" ? required("GRADEX_LOADTEST_PASSWORD") : "";
const UPLOAD_FIXTURE = SCENARIO === "upload-contention" ? open(required("GRADEX_LOADTEST_UPLOAD_FIXTURE_FILE"), "b") : null;
const UPLOAD_SHA256 = SCENARIO === "upload-contention" ? required("GRADEX_LOADTEST_UPLOAD_SHA256_HEX") : "";
const WORKLOAD_PLAN = workflowPlan(PROFILE.workload_mix, PROFILE.workflow_slots);

const counters = {};
for (const name of [
  "application_requests", "application_successes", "login_attempts", "login_bootstrap_successes",
  "login_successes", "playback_attempts", "playback_authorization_successes", "playback_manifest_successes",
  "operator_requests", "upload_attempts", "upload_successes", "direct_upload_successes",
]) counters[name] = new Counter(`gradex_${name}`);
for (const name of PROFILE.error_counters) counters[name] = new Counter(`gradex_error_${name}`);

const endpointNames = [
  "catalog_list", "catalog_detail", "session_check", "dashboard", "access_status", "course_home",
  "lesson_metadata", "playback_authorization", "playback_manifest", "progress_write", "login_bootstrap",
  "login", "operator_admin", "operator_instructor", "upload_intent", "direct_upload", "upload_completion",
];
const endpointRequests = {};
const endpointFailures = {};
const endpointLatency = {};
for (const name of endpointNames) {
  endpointRequests[name] = new Counter(`gradex_endpoint_requests_${name}`);
  endpointFailures[name] = new Counter(`gradex_endpoint_failures_${name}`);
  endpointLatency[name] = new Trend(`gradex_endpoint_latency_${name}`, true);
}

export const options = scenarioOptions();

export default function () {
  switch (SCENARIO) {
    case "mixed-student-sustained":
    case "mixed-student-burst":
      runMixedStudent();
      return;
    case "public-catalogue":
      runPublicCatalogue();
      return;
    case "login-surge":
      runLogin();
      return;
    case "playback-surge":
      runPlaybackFlow();
      return;
    case "privileged-operators":
      runOperator();
      return;
    case "upload-contention":
      runUpload();
      return;
    default:
      throw new Error(`unsupported beta load scenario ${SCENARIO}`);
  }
}

function scenarioOptions() {
  const thresholds = { dropped_iterations: ["count==0"] };
  for (const name of PROFILE.error_counters) thresholds[`gradex_error_${name}`] = ["count==0"];
  if (SCENARIO_KEY === "mixed_student_sustained" || SCENARIO_KEY === "mixed_student_burst" || SCENARIO_KEY === "public_catalogue") {
    const expectedRequests = SCENARIO_CONFIG.total_rps * SCENARIO_CONFIG.duration_seconds;
    thresholds.gradex_application_requests = [`count==${expectedRequests}`];
    thresholds.gradex_application_successes = [`count==${expectedRequests}`];
    for (const name of endpointNames.filter((endpoint) => !endpoint.startsWith("login") && !endpoint.startsWith("operator") && !endpoint.startsWith("upload") && endpoint !== "direct_upload")) {
      thresholds[`gradex_endpoint_latency_${name}`] = [latencyClass(name) === "transactional_write" ? "p(95)<800" : "p(95)<300"];
    }
    return {
      scenarios: {
        mixed: {
          executor: "constant-arrival-rate",
          rate: Math.round(SCENARIO_CONFIG.total_rps * (1 - PROFILE.workload_mix.playback_manifest) * 2),
          timeUnit: "2s",
          duration: `${SCENARIO_CONFIG.duration_seconds}s`,
          preAllocatedVUs: 32,
          maxVUs: 200,
        },
      },
      thresholds,
      summaryTrendStats: ["med", "p(95)", "p(99)", "max"],
    };
  }
  if (SCENARIO_KEY === "public_catalogue") {
    thresholds.gradex_application_requests = [`count==${SCENARIO_CONFIG.rps * SCENARIO_CONFIG.duration_seconds}`];
    thresholds.gradex_application_successes = [`count==${SCENARIO_CONFIG.rps * SCENARIO_CONFIG.duration_seconds}`];
    thresholds.gradex_endpoint_latency_catalog_list = ["p(95)<300"];
    thresholds.gradex_endpoint_latency_catalog_detail = ["p(95)<300"];
    return arrivalOptions("public", SCENARIO_CONFIG.rps, "1s", `${SCENARIO_CONFIG.duration_seconds}s`, thresholds, 32, 100);
  }
  if (SCENARIO_KEY === "login_surge") {
    thresholds.gradex_login_attempts = [`count==${SCENARIO_CONFIG.distinct_students}`];
    thresholds.gradex_login_bootstrap_successes = [`count==${SCENARIO_CONFIG.successful_bootstraps}`];
    thresholds.gradex_login_successes = [`count==${SCENARIO_CONFIG.successful_logins}`];
    thresholds.gradex_endpoint_latency_login = ["p(95)<800"];
    return arrivalOptions("login", SCENARIO_CONFIG.distinct_students, `${SCENARIO_CONFIG.window_seconds}s`, `${SCENARIO_CONFIG.window_seconds * 1000 - 1}ms`, thresholds, 32, 100);
  }
  if (SCENARIO_KEY === "playback_surge") {
    thresholds.gradex_playback_attempts = [`count==${SCENARIO_CONFIG.starts}`];
    thresholds.gradex_playback_authorization_successes = [`count==${SCENARIO_CONFIG.starts}`];
    thresholds.gradex_playback_manifest_successes = [`count==${SCENARIO_CONFIG.starts}`];
    thresholds.gradex_endpoint_latency_playback_authorization = ["p(95)<300"];
    thresholds.gradex_endpoint_latency_playback_manifest = ["p(95)<300"];
    return arrivalOptions("playback", SCENARIO_CONFIG.starts, `${SCENARIO_CONFIG.window_seconds}s`, `${SCENARIO_CONFIG.window_seconds * 1000 - 1}ms`, thresholds, 32, 100);
  }
  if (SCENARIO_KEY === "privileged_operators") {
    thresholds.gradex_operator_requests = ["count>0"];
    thresholds.gradex_endpoint_latency_operator_admin = ["p(95)<300"];
    thresholds.gradex_endpoint_latency_operator_instructor = ["p(95)<300"];
    return arrivalOptions("operator", SCENARIO_CONFIG.rps, "1s", `${SCENARIO_CONFIG.duration_seconds}s`, thresholds, 5, 5);
  }
  if (SCENARIO_KEY === "upload_contention") {
    thresholds.gradex_upload_attempts = [`count==${SCENARIO_CONFIG.concurrent_uploads}`];
    thresholds.gradex_upload_successes = [`count==${SCENARIO_CONFIG.concurrent_uploads}`];
    return {
      scenarios: { upload: { executor: "per-vu-iterations", vus: SCENARIO_CONFIG.concurrent_uploads, iterations: 1, maxDuration: `${SCENARIO_CONFIG.duration_seconds}s` } },
      thresholds,
      summaryTrendStats: ["med", "p(95)", "p(99)", "max"],
    };
  }
  throw new Error(`no options for ${SCENARIO}`);
}

function arrivalOptions(name, rate, timeUnit, duration, thresholds, preAllocatedVUs, maxVUs) {
  return {
    scenarios: { [name]: { executor: "constant-arrival-rate", rate, timeUnit, duration, preAllocatedVUs, maxVUs } },
    thresholds,
    summaryTrendStats: ["med", "p(95)", "p(99)", "max"],
  };
}

function runMixedStudent() {
  const session = entitledSession(exec.scenario.iterationInTest);
  const operation = WORKLOAD_PLAN[exec.scenario.iterationInTest % WORKLOAD_PLAN.length];
  if (operation === "catalog_list") {
    recordJSON("catalog_list", http.get(`${TARGET}/api/v1/catalog/courses?page=1&page_size=20`, requestParams("catalog_list", null)), 200,
      (body) => Array.isArray(body.items));
    return;
  }
  if (operation === "catalog_detail") {
    recordJSON("catalog_detail", http.get(`${TARGET}/api/v1/catalog/courses/${session.course_id}`, requestParams("catalog_detail", null)), 200,
      (body) => body.id === session.course_id);
    return;
  }
  if (operation === "session_check") {
    recordJSON("session_check", http.get(`${TARGET}/api/v1/session`, requestParams("session_check", session)), 200,
      (body) => body.status === "AUTHENTICATED" && body.role === "STUDENT");
    return;
  }
  if (operation === "dashboard") {
    recordJSON("dashboard", http.get(`${TARGET}/api/v1/learn/dashboard`, requestParams("dashboard", session)), 200,
      (body) => Array.isArray(body.courses) && body.courses.some((course) => course.course_id === session.course_id));
    return;
  }
  if (operation === "access_status") {
    recordJSON("access_status", http.get(`${TARGET}/api/v1/me/course-access`, requestParams("access_status", session)), 200,
      (body) => Array.isArray(body.items));
    return;
  }
  if (operation === "course_home") {
    recordJSON("course_home", http.get(`${TARGET}/api/v1/learn/courses/${session.course_id}`, requestParams("course_home", session)), 200,
      (body) => body.course_id === session.course_id && Array.isArray(body.sections));
    return;
  }
  if (operation === "lesson_metadata") {
    recordJSON("lesson_metadata", http.get(`${TARGET}/api/v1/learn/courses/${session.course_id}/lessons/${session.lesson_id}`, requestParams("lesson_metadata", session)), 200,
      (body) => body.course_id === session.course_id && body.lesson_id === session.lesson_id);
    return;
  }
  if (operation === "progress_write") {
    recordNoBody("progress_write", http.put(`${TARGET}/api/v1/learn/lessons/${session.lesson_id}/progress`, JSON.stringify({
      position_seconds: 1,
      asset_version_id: session.asset_version_id,
    }), requestParams("progress_write", session, { "Content-Type": "application/json", "X-CSRF-Token": session.csrf_token })), [204]);
    return;
  }
  runPlaybackFlow(session);
}

function runPublicCatalogue() {
  const iteration = exec.scenario.iterationInTest;
  const list = iteration % 10 < 7;
  const course = FIXTURE.courses[iteration % FIXTURE.courses.length];
  if (list) {
    recordJSON("catalog_list", http.get(`${TARGET}/api/v1/catalog/courses?page=1&page_size=20`, requestParams("catalog_list", null, anonymousHeaders())), 200,
      (body) => Array.isArray(body.items));
  } else {
    recordJSON("catalog_detail", http.get(`${TARGET}/api/v1/catalog/courses/${course.course_id}`, requestParams("catalog_detail", null, anonymousHeaders())), 200,
      (body) => body.id === course.course_id);
  }
}

function runLogin() {
  const index = exec.scenario.iterationInTest;
  const student = FIXTURE.students[index % PROFILE.fixture.login_identities];
  counters.login_attempts.add(1);
  const jar = http.cookieJar();
  jar.clear(TARGET);
  const bootstrap = recordJSON("login_bootstrap", http.get(`${TARGET}/api/v1/session/bootstrap`, requestParams("login_bootstrap", null, anonymousHeaders())), 200,
    (body) => typeof body.csrf_token === "string" && body.csrf_token.length > 0);
  if (!bootstrap.ok) return;
  counters.login_bootstrap_successes.add(1);
  const login = recordJSON("login", http.post(`${TARGET}/api/v1/sessions`, JSON.stringify({ email: student.email, password: PASSWORD }), requestParams("login", null, {
    ...anonymousHeaders(), "Content-Type": "application/json", Origin: TARGET, "X-CSRF-Token": bootstrap.body.csrf_token,
  })), 201, (body) => body.status === "AUTHENTICATED" && body.role === "STUDENT");
  if (login.ok) counters.login_successes.add(1);
}

function runPlaybackFlow(session = entitledSession(exec.scenario.iterationInTest, SCENARIO === "playback-surge")) {
  const iteration = exec.scenario.iterationInTest;
  counters.playback_attempts.add(1);
  const authorization = recordJSON("playback_authorization", http.post(`${TARGET}/api/v1/media/playback-authorizations`, JSON.stringify({
    lesson_id: session.lesson_id,
    asset_version_id: session.asset_version_id,
  }), requestParams("playback_authorization", session, { "Content-Type": "application/json", "X-CSRF-Token": session.csrf_token, Origin: TARGET })), 200,
    (body) => body.asset_version_id === session.asset_version_id && typeof body.playback_session === "string" &&
      typeof body.manifest_url === "string" && body.manifest_url.startsWith("/api/v1/media/playback-manifests/") &&
      body.manifest_url.endsWith("/index.m3u8"));
  if (!authorization.ok) return;
  counters.playback_authorization_successes.add(1);
  const manifest = recordText("playback_manifest", http.get(`${TARGET}${authorization.body.manifest_url}`, requestParams("playback_manifest", session, {
    Accept: "application/vnd.apple.mpegurl",
  })), 200, (body) => body.startsWith("#EXTM3U") && body.includes("#EXT-X-STREAM-INF") && body.includes("/renditions/"));
  if (manifest.ok) counters.playback_manifest_successes.add(1);
  // This control-plane scenario validates the protected master and never follows variants or signed segments.
}

function runOperator() {
  const operator = SESSIONS.operators[(exec.scenario.iterationInTest) % SESSIONS.operators.length];
  counters.operator_requests.add(1);
  if (operator.role === "ADMIN") {
    recordJSON("operator_admin", http.get(`${TARGET}/api/v1/admin/reports?page=1&page_size=20`, requestParams("operator_admin", operator)), 200,
      (body) => Array.isArray(body.items));
    return;
  }
  const courseID = operator.course_ids[exec.scenario.iterationInTest % operator.course_ids.length];
  recordJSON("operator_instructor", http.get(`${TARGET}/api/v1/courses/${courseID}/students?page=1&page_size=20`, requestParams("operator_instructor", operator)), 200,
    (body) => Array.isArray(body.items));
}

function runUpload() {
  const operator = SESSIONS.operators[(exec.scenario.iterationInTest) % PROFILE.fixture.operator_instructors + 1];
  const courseID = operator.course_ids[0];
  const course = FIXTURE.courses.find((item) => item.course_id === courseID) || FIXTURE.courses[0];
  counters.upload_attempts.add(1);
  const fixtureBody = UPLOAD_FIXTURE;
  const intent = recordJSON("upload_intent", http.post(`${TARGET}/api/v1/media/uploads`, JSON.stringify({
    course_id: course.course_id,
    revision_id: course.revision_id,
    lesson_id: course.lesson_id,
    kind: "VIDEO",
    content_type: "video/mp4",
    size_bytes: fixtureBody.byteLength,
  }), requestParams("upload_intent", operator, { "Content-Type": "application/json", "X-CSRF-Token": operator.csrf_token, Origin: TARGET })), 201,
    (body) => typeof body.asset_version_id === "string" && typeof body.upload_url === "string" && typeof body.storage_object_key === "string");
  if (!intent.ok) return;
  const direct = recordNoBody("direct_upload", http.put(intent.body.upload_url, fixtureBody, { headers: { "Content-Type": "video/mp4" }, redirects: 0, timeout: "60s", tags: { name: "direct_upload" } }), [200, 201, 204]);
  if (!direct.ok) return;
  counters.direct_upload_successes.add(1);
  const objectIdentity = directUploadObjectIdentity(direct.response.headers);
  if (!objectIdentity) {
    counters.upload_failures.add(1);
    return;
  }
  const completion = recordJSON("upload_completion", http.post(`${TARGET}/api/v1/media/uploads/${intent.body.asset_version_id}/completions`, JSON.stringify({
    provider_event_id: `${RUN_ID}-upload-${exec.scenario.iterationInTest}`,
    storage_object_key: intent.body.storage_object_key,
    storage_object_version: objectIdentity,
    content_type: "video/mp4",
    size_bytes: fixtureBody.byteLength,
    sha256_hex: UPLOAD_SHA256,
  }), requestParams("upload_completion", operator, { "Content-Type": "application/json", "X-CSRF-Token": operator.csrf_token, Origin: TARGET })), 200,
    (body) => body.asset_version_id === intent.body.asset_version_id && typeof body.state === "string");
  if (completion.ok) counters.upload_successes.add(1);
}

function directUploadObjectIdentity(headers) {
  const versionID = String(headers["X-Amz-Version-Id"] || headers["x-amz-version-id"] || "").trim();
  if (versionID) return versionID;
  const etag = String(headers.ETag || headers.etag || "").trim();
  if (etag.length < 3 || etag[0] !== '"' || etag[etag.length - 1] !== '"') return "";
  for (const character of etag.slice(1, -1)) {
    const codePoint = character.charCodeAt(0);
    if (codePoint <= 0x20 || codePoint === 0x7f || character === '"') return "";
  }
  return `etag:${etag}`;
}

function entitledSession(iteration, playback = false) {
  const studentIndex = playback
    ? playbackAssignment(iteration, PROFILE.fixture.entitled_students, SCENARIO_CONFIG.max_starts_per_student)
    : iteration % PROFILE.fixture.entitled_students;
  const student = SESSIONS.students.filter((entry) => entry.entitled)[studentIndex];
  if (!student) throw new Error("protected scenario has no entitled session");
  return student;
}

function requestParams(name, session, extraHeaders = {}) {
  const headers = { Accept: "application/json", "User-Agent": "Gradex-limited-paid-beta-harness/1", ...extraHeaders };
  if (session) headers.Cookie = `${session.cookie_name}=${session.cookie_value}`;
  return { headers, redirects: 0, responseType: "text", tags: { name }, timeout: name.includes("login") ? "60s" : "30s" };
}

function recordJSON(name, response, expectedStatus, validator) {
  return record(name, response, expectedStatus, (body) => {
    try {
      const parsed = JSON.parse(body || "");
      return { valid: validator(parsed), body: parsed };
    } catch (_) {
      return { valid: false, body: null };
    }
  });
}

function recordText(name, response, expectedStatus, validator) {
  return record(name, response, expectedStatus, (body) => ({ valid: validator(body || ""), body }));
}

function recordNoBody(name, response, expectedStatuses) {
  return record(name, response, expectedStatuses, () => ({ valid: true, body: null }));
}

function record(name, response, expectedStatus, parse) {
  const status = response && response.status ? response.status : 0;
  endpointRequests[name].add(1);
  endpointLatency[name].add(response && response.timings ? response.timings.duration : 0);
  const expected = Array.isArray(expectedStatus) ? expectedStatus.includes(status) : status === expectedStatus;
  const parsed = status === 0 ? { valid: false, body: null } : parse(response.body || "");
  const failures = classifyResponse(name, expected ? status : status, expected && parsed.valid, status === 0);
  addFailureCounters(failures);
  if (!expected || !parsed.valid) endpointFailures[name].add(1);
  if (SCENARIO_KEY === "mixed_student_sustained" || SCENARIO_KEY === "mixed_student_burst") {
    counters.application_requests.add(1);
    if (expected && parsed.valid) counters.application_successes.add(1);
  }
  return { ok: expected && parsed.valid, body: parsed.body, response };
}

function addFailureCounters(failures) {
  for (const [name, count] of Object.entries(failures)) if (count) counters[name].add(count);
}

export function handleSummary(data) {
  const result = buildSummary(data);
  return { stdout: humanSummary(result), [RESULT_FILE]: `${JSON.stringify(result, null, 2)}\n` };
}

function buildSummary(data) {
  const count = (name) => metricValue(data, name, "count") || 0;
  const trend = (name) => ({ p50_ms: metricValue(data, name, "med"), p95_ms: metricValue(data, name, "p(95)"), p99_ms: metricValue(data, name, "p(99)"), max_ms: metricValue(data, name, "max") });
  const latencyMetrics = {};
  for (const name of endpointNames) {
    if (count(`gradex_endpoint_requests_${name}`) > 0) latencyMetrics[name] = trend(`gradex_endpoint_latency_${name}`);
  }
  const errorCounters = {};
  for (const name of PROFILE.error_counters) errorCounters[name] = count(`gradex_error_${name}`);
  const result = {
    schema_version: 2,
    profile: PROFILE.profile,
    scenario: SCENARIO,
    run_id: RUN_ID,
    repetition: required("GRADEX_LOADTEST_REPETITION"),
    summary: { complete: count("dropped_iterations") === 0, pass: false },
    configured: SCENARIO_CONFIG,
    totals: { requests: count("http_reqs"), achieved_rps: metricValue(data, "http_reqs", "rate") },
    latency_metrics: latencyMetrics,
    error_counters: errorCounters,
    generator_metrics: null,
    server_metrics: null,
    postgres_metrics: null,
    redis_metrics: null,
    worker_metrics: null,
    fixture_fingerprint: FIXTURE.fingerprint || null,
    release_identity: __ENV.GRADEX_LOADTEST_RELEASE_ID || null,
    container_image_identity: __ENV.GRADEX_LOADTEST_CONTAINER_IMAGE_ID || null,
    compose_project_identity: __ENV.GRADEX_LOADTEST_COMPOSE_PROJECT || null,
    host_class: __ENV.GRADEX_LOADTEST_HOST_CLASS || null,
    storage_provider_mode: __ENV.GRADEX_LOADTEST_STORAGE_PROVIDER || null,
    environment_profile: PROFILE.profile,
    scenario_metrics: counterSummary(count),
    failure_reasons: [],
  };
  result.failure_reasons = failureReasons(result);
  result.summary.pass = result.failure_reasons.length === 0;
  return result;
}

function counterSummary(count) {
  const summary = {};
  for (const name of Object.keys(counters)) summary[name] = count(`gradex_${name}`);
  return summary;
}

function failureReasons(result) {
  const reasons = [];
  for (const [name, value] of Object.entries(result.error_counters)) if (value !== 0) reasons.push(`${name}=${value}`);
  if (SCENARIO_KEY === "login_surge" && result.scenario_metrics.login_successes !== SCENARIO_CONFIG.successful_logins) reasons.push("login success count incomplete");
  if (SCENARIO_KEY === "playback_surge" && result.scenario_metrics.playback_manifest_successes !== SCENARIO_CONFIG.starts) reasons.push("playback manifest success count incomplete");
  if ((SCENARIO_KEY === "mixed_student_sustained" || SCENARIO_KEY === "mixed_student_burst") && result.scenario_metrics.application_requests !== SCENARIO_CONFIG.total_rps * SCENARIO_CONFIG.duration_seconds) reasons.push("mixed application request count incomplete");
  return [...new Set(reasons)];
}

function metricValue(data, name, key) {
  const metric = data.metrics[name];
  return metric && metric.values && Number.isFinite(metric.values[key]) ? metric.values[key] : null;
}

function humanSummary(result) {
  return `Gradex ${result.profile} ${result.scenario}: ${result.summary.pass ? "PASS" : "FAIL"}\n` +
    `run_id=${result.run_id} requests=${result.totals.requests || 0} transport_failures=${result.error_counters.transport_failures || 0}\n` +
    result.failure_reasons.map((reason) => `FAIL: ${reason}`).join("\n") + "\n";
}

function workflowPlan(mix, scale) {
  const denominator = 1 - mix.playback_manifest;
  const scheduled = [];
  for (const [name, share] of Object.entries(mix)) {
    if (name === "playback_manifest") continue;
    const slots = Math.round((share / denominator) * scale);
    for (let index = 0; index < slots; index += 1) {
      scheduled.push({ position: (index + 0.5) / slots, operation: name === "playback_authorization" ? "playback" : name });
    }
  }
  scheduled.sort((left, right) => left.position - right.position || left.operation.localeCompare(right.operation));
  const plan = scheduled.map((entry) => entry.operation);
  if (plan.length !== scale) throw new Error(`workflow plan has ${plan.length} slots, expected ${scale}`);
  return plan;
}

function validateSessions(sessions, fixture, profile) {
  if (!sessions || sessions.schema_version !== 2 || sessions.profile !== profile.profile ||
      !Array.isArray(sessions.students) || sessions.students.length !== fixture.students.length ||
      !Array.isArray(sessions.operators) || sessions.operators.length !== profile.fixture.admin_accounts + profile.fixture.instructor_accounts) {
    throw new Error("beta session manifest shape is invalid");
  }
  for (let index = 0; index < sessions.students.length; index += 1) {
    const session = sessions.students[index];
    const student = fixture.students[index];
    if (session.index !== index || session.account_id !== student.account_id || !session.cookie_value || !session.csrf_token) throw new Error("beta session manifest does not match fixture identities");
  }
}

function scenarioKey(name) {
  return {
    "mixed-student-sustained": "mixed_student_sustained",
    "mixed-student-burst": "mixed_student_burst",
    "public-catalogue": "public_catalogue",
    "login-surge": "login_surge",
    "playback-surge": "playback_surge",
    "privileged-operators": "privileged_operators",
    "upload-contention": "upload_contention",
  }[name];
}

function needsSessions(name) { return name !== "public-catalogue" && name !== "login-surge"; }

function required(name) {
  const value = __ENV[name];
  if (!value) throw new Error(`${name} is required`);
  return value;
}

function readJSON(path, label) {
  try { return JSON.parse(open(path)); } catch (_) { throw new Error(`${label} is absent or invalid JSON`); }
}

function validatedTarget(raw) {
  const match = /^(https?):\/\/(\[[0-9A-Fa-f:.]+\]|[A-Za-z0-9.-]+)(?::([0-9]{1,5}))?$/.exec(raw);
  if (!match) throw new Error("GRADEX_LOADTEST_TARGET_URL must be an absolute origin");
  const protocol = `${match[1].toLowerCase()}:`;
  const host = match[2].toLowerCase();
  if (match[3] && Number(match[3]) > 65535) throw new Error("target port is invalid");
  if (protocol !== "https:" && !((host === "127.0.0.1" || host === "localhost" || host === "[::1]") && protocol === "http:")) throw new Error("remote beta load targets must use HTTPS");
  return `${protocol}//${host}${match[3] ? `:${match[3]}` : ""}`;
}

function validatedResultPath(raw) {
  if (!/^\/results\/[A-Za-z0-9._-]+\.json$/.test(raw)) throw new Error("result path must be a simple JSON file under /results");
  return raw;
}
