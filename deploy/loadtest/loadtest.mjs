import http from "k6/http";
import exec from "k6/execution";
import { Counter, Trend } from "k6/metrics";

const SCENARIO = required("GRADEX_LOADTEST_SCENARIO");
if (!["api-surge", "login-surge", "playback-surge"].includes(SCENARIO)) {
  throw new Error("GRADEX_LOADTEST_SCENARIO must be api-surge, login-surge, or playback-surge");
}

const TARGET = validatedTarget(required("GRADEX_LOADTEST_TARGET_URL"));
const SMOKE = __ENV.GRADEX_LOADTEST_SMOKE === "1";
const PROFILE_API_RATE = optionalProfileCount("GRADEX_LOADTEST_PROFILE_API_RATE", 249);
const PROFILE_LOGIN_COUNT = optionalProfileCount("GRADEX_LOADTEST_PROFILE_LOGIN_COUNT", 499);
if (SMOKE && (PROFILE_API_RATE !== null || PROFILE_LOGIN_COUNT !== null)) {
  throw new Error("smoke and reduced-envelope profiling modes cannot be combined");
}
if (PROFILE_API_RATE !== null && SCENARIO !== "api-surge") {
  throw new Error("GRADEX_LOADTEST_PROFILE_API_RATE applies only to api-surge");
}
if (PROFILE_LOGIN_COUNT !== null && SCENARIO !== "login-surge") {
  throw new Error("GRADEX_LOADTEST_PROFILE_LOGIN_COUNT applies only to login-surge");
}
const FIXTURE = readJSON(required("GRADEX_LOADTEST_FIXTURE_FILE"), "fixture manifest");
validateFixture(FIXTURE);
const SESSIONS = SCENARIO === "login-surge"
  ? null
  : readJSON(required("GRADEX_LOADTEST_SESSION_FILE"), "protected session manifest");
if (SESSIONS) validateSessions(SESSIONS, FIXTURE);
const PASSWORD = SCENARIO === "login-surge" ? required("GRADEX_LOADTEST_PASSWORD") : "";
const RESULT_FILE = validatedResultPath(required("GRADEX_LOADTEST_RESULT_FILE"));

const expected = SMOKE
  ? { apiRequests: 50, apiRate: 5, durationSeconds: 10, logins: 5, playbacks: 5 }
  : {
      apiRequests: (PROFILE_API_RATE || 250) * 60,
      apiRate: PROFILE_API_RATE || 250,
      durationSeconds: 60,
      logins: PROFILE_LOGIN_COUNT || 500,
      playbacks: 250,
    };
const ENVELOPE_PROFILE = PROFILE_API_RATE !== null || PROFILE_LOGIN_COUNT !== null;

const requests = new Counter("gradex_requests");
const successes = new Counter("gradex_successes");
const httpFailures = new Counter("gradex_http_failures");
const transportFailures = new Counter("gradex_transport_failures");
const serverFailures = new Counter("gradex_http_5xx");
const correctnessFailures = new Counter("gradex_correctness_failures");
const rateLimited = new Counter("gradex_rate_limited");
const applicationRequests = new Counter("gradex_application_requests");
const applicationSuccesses = new Counter("gradex_application_successes");
const loginAttempts = new Counter("gradex_login_attempts");
const loginSuccesses = new Counter("gradex_login_successes");
const loginSuccessDuration = new Trend("gradex_login_success_duration", true);
const playbackAttempts = new Counter("gradex_playback_attempts");
const playbackSuccesses = new Counter("gradex_playback_successes");
const apiSuccessDuration = new Trend("gradex_api_success_duration", true);

const endpointNames = [
  "session", "catalog_list", "catalog_detail", "learning_dashboard", "learning_course",
  "learning_lesson", "login_bootstrap", "login", "playback_authorization", "playback_manifest",
];
const endpointRequests = {};
const endpointFailures = {};
const endpointDuration = {};
for (const name of endpointNames) {
  endpointRequests[name] = new Counter(`gradex_endpoint_requests_${name}`);
  endpointFailures[name] = new Counter(`gradex_endpoint_failures_${name}`);
  endpointDuration[name] = new Trend(`gradex_endpoint_duration_${name}`, true);
}

export const options = scenarioOptions();

export default function () {
  switch (SCENARIO) {
    case "api-surge":
      runAPIRequest();
      return;
    case "login-surge":
      runLogin();
      return;
    case "playback-surge":
      runPlaybackStart();
      return;
  }
}

function scenarioOptions() {
  const thresholds = {
    gradex_http_failures: ["count==0"],
    gradex_transport_failures: ["count==0"],
    gradex_http_5xx: ["count==0"],
    gradex_correctness_failures: ["count==0"],
    dropped_iterations: ["count==0"],
  };
  let scenarios;
  if (SCENARIO === "api-surge") {
    thresholds.gradex_application_requests = [`count==${expected.apiRequests}`];
    thresholds.gradex_application_successes = [`count==${expected.apiRequests}`];
    thresholds.gradex_api_success_duration = ["p(95)<300"];
    for (const endpoint of ["session", "catalog_list", "catalog_detail", "learning_dashboard", "learning_course", "learning_lesson"]) {
      thresholds[`gradex_endpoint_duration_${endpoint}`] = ["p(95)<300"];
    }
    scenarios = {
      api_surge: {
        executor: "constant-arrival-rate", rate: expected.apiRate, timeUnit: "1s",
        duration: `${expected.durationSeconds * 1000 - 1}ms`, preAllocatedVUs: SMOKE ? 5 : 200,
        maxVUs: SMOKE ? 5 : 1000,
      },
    };
  } else if (SCENARIO === "login-surge") {
    thresholds.gradex_login_attempts = [`count==${expected.logins}`];
    thresholds.gradex_login_successes = [`count==${expected.logins}`];
    thresholds.gradex_login_success_duration = ["p(95)<800"];
    scenarios = {
      login_surge: {
        executor: "constant-arrival-rate", rate: expected.logins,
        timeUnit: `${expected.durationSeconds}s`, duration: `${expected.durationSeconds * 1000 - 1}ms`,
        preAllocatedVUs: SMOKE ? 5 : 32, maxVUs: SMOKE ? 5 : expected.logins,
      },
    };
  } else {
    thresholds.gradex_playback_attempts = [`count==${expected.playbacks}`];
    thresholds.gradex_playback_successes = [`count==${expected.playbacks}`];
    scenarios = {
      playback_surge: {
        executor: "constant-arrival-rate", rate: expected.playbacks,
        timeUnit: `${expected.durationSeconds}s`, duration: `${expected.durationSeconds * 1000 - 1}ms`,
        preAllocatedVUs: SMOKE ? 5 : 50, maxVUs: SMOKE ? 5 : 250,
      },
    };
  }
  return {
    scenarios,
    thresholds,
    discardResponseBodies: true,
    noConnectionReuse: false,
    summaryTrendStats: ["med", "p(95)", "p(99)", "max"],
  };
}

// The 250-position cycle is the exact exam-start mix. Public catalogue traffic is only 19.6%;
// the remaining 80.4% re-enters authenticated session, entitlement, Course, Lesson, progress, and
// material read paths. One session resolution per cycle stays within the shipped 120/minute
// endpoint ceiling while still verifying that pre-issued sessions remain valid.
function runAPIRequest() {
  const iteration = exec.scenario.iterationInTest;
  const session = SESSIONS.sessions[iteration % FIXTURE.active_students];
  const slot = iteration % 250;
  applicationRequests.add(1);
  let result;
  if (slot === 0) {
    result = get("session", "/api/v1/session", session, (body) =>
      body.status === "AUTHENTICATED" && body.role === "STUDENT" && typeof body.csrf_token === "string");
  } else if (slot < 25) {
    result = get("catalog_list", "/api/v1/catalog/courses", session,
      (body) => Array.isArray(body.items) && body.total >= 1);
  } else if (slot < 50) {
    result = get("catalog_detail", `/api/v1/catalog/courses/${FIXTURE.course_id}`, session,
      (body) => body.id === FIXTURE.course_id);
  } else if (slot < 100) {
    result = get("learning_dashboard", "/api/v1/learn/dashboard", session,
      (body) => Array.isArray(body.courses) && body.courses.some((course) => course.course_id === FIXTURE.course_id));
  } else if (slot < 175) {
    result = get("learning_course", `/api/v1/learn/courses/${FIXTURE.course_id}`, session,
      (body) => body.course_id === FIXTURE.course_id && Array.isArray(body.sections));
  } else {
    result = get("learning_lesson", `/api/v1/learn/courses/${FIXTURE.course_id}/lessons/${FIXTURE.lesson_id}`, session,
      (body) => body.course_id === FIXTURE.course_id && body.lesson_id === FIXTURE.lesson_id);
  }
  if (result.ok) {
    applicationSuccesses.add(1);
    apiSuccessDuration.add(result.duration);
  }
}

function runLogin() {
  const index = exec.scenario.iterationInTest;
  const student = FIXTURE.students[index];
  loginAttempts.add(1);
  const bootstrap = http.get(`${TARGET}/api/v1/session/bootstrap`, requestParams("login_bootstrap", null));
  const bootstrapResult = recordResponse("login_bootstrap", bootstrap, 200,
    (body) => typeof body.csrf_token === "string" && body.csrf_token.length > 0);
  if (!bootstrapResult.ok) return;

  const login = http.post(`${TARGET}/api/v1/sessions`, JSON.stringify({ email: student.email, password: PASSWORD }),
    requestParams("login", null, {
      "Content-Type": "application/json", Origin: TARGET, "X-CSRF-Token": bootstrapResult.body.csrf_token,
    }));
  const loginResult = recordResponse("login", login, 201,
    (body) => body.status === "AUTHENTICATED" && body.role === "STUDENT" && body.password_change_required === false);
  if (loginResult.ok) {
    loginSuccesses.add(1);
    loginSuccessDuration.add(loginResult.duration);
  }
}

function runPlaybackStart() {
  const iteration = exec.scenario.iterationInTest;
  const session = SESSIONS.sessions[iteration % FIXTURE.active_students];
  playbackAttempts.add(1);
  const authorization = http.post(
    `${TARGET}/api/v1/learn/lessons/${FIXTURE.lesson_id}/playback`, null,
    requestParams("playback_authorization", session, { Origin: TARGET, "X-CSRF-Token": session.csrf_token }),
  );
  const authorizationResult = recordResponse("playback_authorization", authorization, 200,
    (body) => body.asset_version_id === FIXTURE.asset_version_id &&
      typeof body.manifest_url === "string" &&
      body.manifest_url.startsWith("/api/v1/media/playback-manifests/") &&
      body.manifest_url.endsWith("/index.m3u8"));
  if (!authorizationResult.ok) return;

  const manifest = http.get(`${TARGET}${authorizationResult.body.manifest_url}`,
    requestParams("playback_manifest", session, { Accept: "application/vnd.apple.mpegurl" }));
  const manifestResult = recordTextResponse("playback_manifest", manifest, 200,
    (body) => body.startsWith("#EXTM3U") && body.includes("#EXTINF"));
  if (manifestResult.ok) playbackSuccesses.add(1);
  // Signed segment URLs are deliberately neither followed nor recorded. This scenario measures
  // Gradex's authentication, entitlement, exact-version grant, and manifest control plane only.
}

function get(name, path, session, validator) {
  const response = http.get(`${TARGET}${path}`, requestParams(name, session));
  return recordResponse(name, response, 200, validator);
}

function requestParams(name, session, extraHeaders = {}) {
  const headers = {
    Accept: "application/json",
    "User-Agent": "Gradex-LG019-Loadtest/1",
    ...extraHeaders,
  };
  if (session) headers.Cookie = `${session.cookie_name}=${session.cookie_value}`;
  const timeout = name === "login" || name === "login_bootstrap" ? "60s" : "10s";
  return { headers, redirects: 0, responseType: "text", tags: { name }, timeout };
}

function recordResponse(name, response, expectedStatus, validator) {
  return record(name, response, expectedStatus, (raw) => {
    try {
      return { valid: validator(JSON.parse(raw)), body: JSON.parse(raw) };
    } catch (_) {
      return { valid: false, body: null };
    }
  });
}

function recordTextResponse(name, response, expectedStatus, validator) {
  return record(name, response, expectedStatus, (raw) => ({ valid: validator(raw), body: raw }));
}

function record(name, response, expectedStatus, validate) {
  requests.add(1);
  endpointRequests[name].add(1);
  const duration = response && response.timings ? response.timings.duration : 0;
  endpointDuration[name].add(duration);
  if (!response || response.status === 0) {
    transportFailures.add(1);
    correctnessFailures.add(1);
    endpointFailures[name].add(1);
    return { ok: false, body: null, duration };
  }
  if (response.status >= 500) serverFailures.add(1);
  if (response.status === 429) rateLimited.add(1);
  if (response.status !== expectedStatus) {
    httpFailures.add(1);
    correctnessFailures.add(1);
    endpointFailures[name].add(1);
    return { ok: false, body: null, duration };
  }
  const validation = validate(response.body || "");
  if (!validation.valid) {
    correctnessFailures.add(1);
    endpointFailures[name].add(1);
    return { ok: false, body: null, duration };
  }
  successes.add(1);
  return { ok: true, body: validation.body, duration };
}

export function handleSummary(data) {
  const result = buildSummary(data);
  return {
    stdout: humanSummary(result),
    [RESULT_FILE]: `${JSON.stringify(result, null, 2)}\n`,
  };
}

function buildSummary(data) {
  const count = (name) => metricValue(data, name, "count") || 0;
  const latency = (name) => ({
    p50: metricValue(data, name, "med"), p95: metricValue(data, name, "p(95)"),
    p99: metricValue(data, name, "p(99)"), max: metricValue(data, name, "max"),
  });
  const durationSeconds = data.state.testRunDurationMs / 1000;
  const endpointResults = {};
  for (const name of endpointNames) {
    const endpointCount = count(`gradex_endpoint_requests_${name}`);
    if (endpointCount > 0) {
      endpointResults[name] = {
        requests: endpointCount,
        failures: count(`gradex_endpoint_failures_${name}`),
        latency_ms: latency(`gradex_endpoint_duration_${name}`),
      };
    }
  }
  const result = {
    schema_version: 1,
    scenario: SCENARIO,
    canonical_acceptance_run: !ENVELOPE_PROFILE,
    reduced_envelope_profile: ENVELOPE_PROFILE ? {
      api_rate: PROFILE_API_RATE,
      login_count: PROFILE_LOGIN_COUNT,
    } : null,
    smoke: SMOKE,
    target_class: isLocalTarget(TARGET) ? "local" : "remote-explicit",
    configured: {
      registered_accounts: FIXTURE.registered_accounts,
      distinct_active_students: SMOKE ? 5 : FIXTURE.active_students,
      target_api_rps: SCENARIO === "api-surge" ? expected.apiRate : null,
      target_duration_seconds: expected.durationSeconds,
      target_logins: SCENARIO === "login-surge" ? expected.logins : null,
      target_playback_starts: SCENARIO === "playback-surge" ? expected.playbacks : null,
    },
    duration_seconds: durationSeconds,
    totals: {
      requests: count("gradex_requests"),
      achieved_requests_per_second: metricValue(data, "http_reqs", "rate"),
      successes: count("gradex_successes"),
      http_failures: count("gradex_http_failures"),
      transport_failures: count("gradex_transport_failures"),
      http_5xx: count("gradex_http_5xx"),
      correctness_failures: count("gradex_correctness_failures"),
      rate_limited_429: count("gradex_rate_limited"),
      dropped_iterations: count("dropped_iterations"),
    },
    latency_ms: latency("http_req_duration"),
    scenario_metrics: {
      application_requests: count("gradex_application_requests"),
      application_successes: count("gradex_application_successes"),
      achieved_application_rps: SCENARIO === "api-surge"
        ? count("gradex_application_requests") / expected.durationSeconds : null,
      login_attempts: count("gradex_login_attempts"),
      login_successes: count("gradex_login_successes"),
      login_success_latency_ms: latency("gradex_login_success_duration"),
      playback_attempts: count("gradex_playback_attempts"),
      playback_successes: count("gradex_playback_successes"),
    },
    endpoints: endpointResults,
    pass: false,
    failure_reasons: [],
  };
  result.failure_reasons = failureReasons(result);
  result.pass = result.failure_reasons.length === 0;
  if (SCENARIO === "login-surge" && result.totals.rate_limited_429 > 0) {
    result.lg019_blocker = "Production admission returned 429 during the Founder-approved 500-login scenario; this is a capacity or policy regression.";
  }
  return result;
}

function failureReasons(result) {
  const reasons = [];
  if (result.totals.http_failures > 0) reasons.push("unexpected HTTP statuses occurred");
  if (result.totals.transport_failures > 0) reasons.push("transport failures occurred");
  if (result.totals.http_5xx > 0) reasons.push("unexpected 5xx responses occurred");
  if (result.totals.correctness_failures > 0) reasons.push("authentication, entitlement, or response correctness failed");
  if (result.totals.dropped_iterations > 0) reasons.push("the load generator could not schedule the requested work");
  if (SCENARIO === "api-surge") {
    if (result.scenario_metrics.application_requests !== expected.apiRequests) reasons.push("the API surge did not schedule exactly the required application request count");
    if (result.scenario_metrics.application_successes !== expected.apiRequests) reasons.push("the API surge did not complete every scheduled application request successfully");
    if (result.scenario_metrics.achieved_application_rps === null ||
        result.scenario_metrics.achieved_application_rps < expected.apiRate) {
      reasons.push("the API surge did not achieve the requested application request rate");
    }
    if (metricP95(result.endpoints) >= 300) reasons.push("the canonical read API p95 target of under 300 ms was not met");
  } else if (SCENARIO === "login-surge") {
    if (result.scenario_metrics.login_attempts !== expected.logins) reasons.push("the login surge did not begin exactly the required number of distinct login attempts");
    if (result.scenario_metrics.login_successes !== expected.logins) reasons.push(`exactly ${expected.logins} distinct Students did not complete login successfully`);
    const p95 = result.scenario_metrics.login_success_latency_ms.p95;
    if (p95 !== null && p95 >= 800) reasons.push("the canonical transactional-write p95 target of under 800 ms was not met by successful logins");
  } else if (result.scenario_metrics.playback_attempts !== expected.playbacks ||
      result.scenario_metrics.playback_successes !== expected.playbacks) {
    reasons.push("the exact required protected playback count did not complete authorization and manifest issuance");
  }
  return [...new Set(reasons)];
}

function metricP95(endpoints) {
  let highest = null;
  for (const value of Object.values(endpoints)) {
    if (value.latency_ms.p95 !== null && (highest === null || value.latency_ms.p95 > highest)) highest = value.latency_ms.p95;
  }
  return highest;
}

function metricValue(data, name, key) {
  const metric = data.metrics[name];
  return metric && metric.values && Number.isFinite(metric.values[key]) ? metric.values[key] : null;
}

function humanSummary(result) {
  const lines = [
    `Gradex LG-019 ${result.scenario}: ${result.pass ? "PASS" : "FAIL"}`,
    `duration=${result.duration_seconds.toFixed(2)}s requests=${result.totals.requests || 0} rps=${format(result.totals.achieved_requests_per_second)}`,
    `success=${result.totals.successes || 0} http_failures=${result.totals.http_failures || 0} transport_failures=${result.totals.transport_failures || 0} 5xx=${result.totals.http_5xx || 0} 429=${result.totals.rate_limited_429 || 0}`,
    `latency_ms p50=${format(result.latency_ms.p50)} p95=${format(result.latency_ms.p95)} p99=${format(result.latency_ms.p99)} max=${format(result.latency_ms.max)}`,
  ];
  for (const reason of result.failure_reasons) lines.push(`FAIL: ${reason}`);
  if (result.lg019_blocker) lines.push(`LG-019 BLOCKER: ${result.lg019_blocker}`);
  lines.push(`machine_result=${RESULT_FILE}`);
  return `${lines.join("\n")}\n`;
}

function format(value) {
  return value === null || value === undefined ? "n/a" : Number(value).toFixed(2);
}

function required(name) {
  const value = __ENV[name];
  if (!value) throw new Error(`${name} is required`);
  return value;
}

function optionalProfileCount(name, maximum) {
  const value = __ENV[name];
  if (!value) return null;
  if (!/^[1-9][0-9]*$/.test(value)) {
    throw new Error(`${name} must be a positive integer`);
  }
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed > maximum) {
    throw new Error(`${name} must be below the canonical threshold`);
  }
  return parsed;
}

function validatedTarget(raw) {
  const match = /^(https?):\/\/(\[[0-9A-Fa-f:.]+\]|[A-Za-z0-9.-]+)(?::([0-9]{1,5}))?$/.exec(raw);
  if (!match) {
    throw new Error("GRADEX_LOADTEST_TARGET_URL must be an absolute origin");
  }
  const protocol = `${match[1].toLowerCase()}:`;
  const host = match[2].toLowerCase();
  const port = match[3];
  if (port && Number(port) > 65535) {
    throw new Error("GRADEX_LOADTEST_TARGET_URL contains an invalid port");
  }
  if (protocol !== "https:" && !(protocol === "http:" && isLocalHost(host))) {
    throw new Error("remote load-test targets must use HTTPS");
  }
  return `${protocol}//${host}${port ? `:${port}` : ""}`;
}

function isLocalHost(host) {
  return host === "127.0.0.1" || host === "localhost" || host === "[::1]";
}

function isLocalTarget(raw) {
  return /^https?:\/\/(?:127\.0\.0\.1|localhost|\[::1\])(?::[0-9]{1,5})?$/.test(raw);
}

function validatedResultPath(path) {
  if (!/^\/results\/[A-Za-z0-9._-]+\.json$/.test(path)) {
    throw new Error("GRADEX_LOADTEST_RESULT_FILE must be a simple JSON filename under /results");
  }
  return path;
}

function readJSON(path, label) {
  try {
    return JSON.parse(open(path));
  } catch (_) {
    throw new Error(`${label} is absent or invalid JSON`);
  }
}

function validateFixture(fixture) {
  if (fixture.schema_version !== 1 || fixture.registered_accounts !== 5000 ||
      fixture.active_students !== 500 || fixture.students.length !== 500 ||
      !fixture.course_id || !fixture.lesson_id || !fixture.asset_version_id) {
    throw new Error("fixture manifest does not match the approved LG-019 population");
  }
  const accounts = new Set();
  const emails = new Set();
  for (let index = 0; index < fixture.students.length; index += 1) {
    const student = fixture.students[index];
    if (student.index !== index || !student.account_id || !student.email ||
        accounts.has(student.account_id) || emails.has(student.email)) {
      throw new Error("fixture manifest Students are not deterministic and unique");
    }
    accounts.add(student.account_id);
    emails.add(student.email);
  }
}

function validateSessions(sessions, fixture) {
  if (sessions.schema_version !== 1 || !Array.isArray(sessions.sessions) || sessions.sessions.length !== 500) {
    throw new Error("protected session manifest must contain exactly 500 sessions");
  }
  for (let index = 0; index < sessions.sessions.length; index += 1) {
    const session = sessions.sessions[index];
    const student = fixture.students[index];
    if (session.index !== index || session.account_id !== student.account_id || session.email !== student.email ||
        !session.cookie_name || !session.cookie_value || !session.csrf_token) {
      throw new Error("protected session manifest does not match the fixture population");
    }
  }
}
