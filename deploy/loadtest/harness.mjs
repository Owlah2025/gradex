export const REQUIRED_EVIDENCE_FIELDS = [
  "summary",
  "latency_metrics",
  "error_counters",
  "generator_metrics",
  "server_metrics",
  "postgres_metrics",
  "redis_metrics",
  "worker_metrics",
  "fixture_fingerprint",
  "release_identity",
  "run_id",
  "scenario_metrics",
];

export function validateProfile(profile) {
  if (!profile || profile.schema_version !== 1 || profile.profile !== "limited-paid-beta") {
    throw new Error("profile must be schema version 1 limited-paid-beta");
  }
  const fixture = profile.fixture;
  if (!fixture || fixture.registered_accounts !== 110 || fixture.student_accounts !== 104 ||
      fixture.entitled_students !== 50 || fixture.non_entitled_students !== 54 ||
      fixture.login_identities !== 100 || fixture.admin_accounts !== 1 ||
      fixture.instructor_accounts !== 5 || fixture.published_courses !== 8 ||
      fixture.ready_video_fixtures !== 8) {
    throw new Error("beta fixture cardinality does not match the Founder-approved envelope");
  }
  if (fixture.entitled_students + fixture.non_entitled_students !== fixture.student_accounts ||
      fixture.student_accounts + fixture.admin_accounts + fixture.instructor_accounts !== fixture.registered_accounts) {
    throw new Error("beta fixture account arithmetic is inconsistent");
  }
  if (mixTotal(profile.workload_mix) !== 1) {
    throw new Error("student mixed workload percentages must sum to exactly 1");
  }
  if (!Number.isInteger(profile.workflow_slots) || profile.workflow_slots <= 0) {
    throw new Error("student workload workflow slot count is invalid");
  }
  assertScenario(profile.scenarios.mixed_student_sustained, 20, 18, 600);
  assertScenario(profile.scenarios.mixed_student_burst, 30, 27, 60);
  if (profile.scenarios.public_catalogue.session_cookie || profile.scenarios.public_catalogue.auth_token) {
    throw new Error("anonymous catalogue scenario cannot carry authentication");
  }
  if (profile.scenarios.playback_surge.starts !== 100 ||
      profile.scenarios.playback_surge.entitled_students !== 50 ||
      profile.scenarios.playback_surge.max_starts_per_student !== 2) {
    throw new Error("playback surge cardinality is invalid");
  }
  if (profile.scenarios.privileged_operators.concurrent_operators !== 5 ||
      profile.scenarios.privileged_operators.rps !== 5 ||
      profile.scenarios.privileged_operators.duration_seconds !== 60 ||
      profile.scenarios.upload_contention.concurrent_uploads !== 3 ||
      profile.scenarios.upload_contention.duration_seconds !== 60) {
    throw new Error("operator or upload scenario cardinality is invalid");
  }
  if (profile.run.repeat_count !== 2 || profile.run.no_automatic_retry !== true) {
    throw new Error("capacity proof must require two clean runs without automatic retry");
  }
  if (profile.latency_classes.playback_authorization !== "read_control_plane" ||
      profile.latency_classes.playback_manifest !== "read_control_plane" ||
      profile.latency_classes.password_login !== "transactional_write") {
    throw new Error("latency governance classification is invalid");
  }
  if (profile.resource_gates.memory_used_fraction_max !== 0.8 ||
      profile.resource_gates.disk_used_fraction_max_exclusive !== 0.85 ||
      profile.resource_gates.disk_free_bytes_min !== 5368709120) {
    throw new Error("resource safety gates are not fail-closed at the approved thresholds");
  }
  return true;
}

function assertScenario(scenario, totalRps, authenticatedRps, durationSeconds) {
  if (!scenario || scenario.total_rps !== totalRps ||
      scenario.authenticated_student_rps !== authenticatedRps ||
      scenario.duration_seconds !== durationSeconds || scenario.workload_mix !== "student_mixed") {
    throw new Error("mixed Student scenario does not encode its approved target");
  }
}

export function mixTotal(mix) {
  if (!mix || typeof mix !== "object") return 0;
  return Number(Object.values(mix).reduce((sum, value) => sum + Number(value), 0).toFixed(6));
}

export function validateFixtureManifest(fixture, profile) {
  validateProfile(profile);
  if (!fixture || fixture.schema_version !== 2 || fixture.profile !== profile.profile ||
      fixture.registered_accounts !== profile.fixture.registered_accounts ||
      fixture.students.length !== profile.fixture.student_accounts ||
      fixture.courses.length !== profile.fixture.published_courses ||
      fixture.operators.length !== profile.fixture.admin_accounts + profile.fixture.instructor_accounts) {
    throw new Error("fixture manifest shape or cardinality is invalid");
  }
  const accounts = new Set();
  const emails = new Set();
  let entitled = 0;
  for (let index = 0; index < fixture.students.length; index += 1) {
    const student = fixture.students[index];
    if (student.index !== index || !student.account_id || !student.email ||
        accounts.has(student.account_id) || emails.has(student.email)) {
      throw new Error("beta Student identities are not deterministic and unique");
    }
    if (student.entitled) entitled += 1;
    accounts.add(student.account_id);
    emails.add(student.email);
  }
  if (entitled !== profile.fixture.entitled_students) {
    throw new Error("beta entitlement count is invalid");
  }
  if (new Set(fixture.courses.map((course) => course.course_id)).size !== fixture.courses.length) {
    throw new Error("beta Course identities are not unique");
  }
  if (fixture.students.filter((student) => student.entitled).length < profile.fixture.login_identities / 2) {
    throw new Error("fixture cannot provide the required entitled Student cohort");
  }
  return true;
}

export function validateLoginIdentities(students, requiredCount) {
  if (!Array.isArray(students) || students.length < requiredCount) {
    throw new Error("login fixture has fewer distinct Student identities than required");
  }
  const identities = students.slice(0, requiredCount).map((student) => student.account_id);
  if (new Set(identities).size !== requiredCount) {
    throw new Error("login scenario does not guarantee distinct Student identities");
  }
  return identities;
}

export function playbackAssignment(iteration, studentCount, maxStartsPerStudent) {
  if (!Number.isInteger(iteration) || iteration < 0 || studentCount <= 0 || maxStartsPerStudent <= 0) {
    throw new Error("playback assignment inputs are invalid");
  }
  return Math.floor(iteration / maxStartsPerStudent) % studentCount;
}

export function anonymousHeaders(extra = {}) {
  const headers = { Accept: "application/json", "Accept-Language": "en", ...extra };
  delete headers.Cookie;
  delete headers.Authorization;
  delete headers["X-CSRF-Token"];
  return headers;
}

export function latencyClass(endpoint) {
  if (["login", "progress_write", "upload_intent", "upload_completion"].includes(endpoint)) {
    return "transactional_write";
  }
  return "read_control_plane";
}

export function classifyResponse(endpoint, status, valid = true, transportFailed = false) {
  const counters = emptyErrorCounters();
  if (transportFailed || status === 0) {
    counters.transport_failures = 1;
    return counters;
  }
  if (status >= 400) {
    counters.unexpected_http_statuses = 1;
    if (status >= 400 && status < 500) counters.http_4xx = 1;
    if (status === 429) counters.http_429 = 1;
    if (status >= 500) counters.http_5xx = 1;
    if (status === 503) counters.http_503 = 1;
  }
  if (!valid) {
    counters.response_shape_failures = 1;
    if (["login", "login_bootstrap", "session_check"].includes(endpoint)) counters.authentication_failures = 1;
    if (["dashboard", "access_status", "course_home", "lesson_metadata", "progress_write", "playback_authorization", "playback_manifest"].includes(endpoint)) counters.entitlement_failures = 1;
    if (endpoint === "playback_manifest") counters.manifest_failures = 1;
    if (["upload_intent", "upload_completion", "direct_upload"].includes(endpoint)) counters.upload_failures = 1;
  }
  return counters;
}

export function emptyErrorCounters() {
  return {
    transport_failures: 0,
    unexpected_http_statuses: 0,
    http_4xx: 0,
    http_429: 0,
    http_5xx: 0,
    http_503: 0,
    authentication_failures: 0,
    entitlement_failures: 0,
    response_shape_failures: 0,
    manifest_failures: 0,
    dropped_iterations: 0,
    upload_failures: 0,
    worker_terminal_failures: 0,
  };
}

export function evaluateResourceGates(metrics, gates) {
  const failures = [];
  const server = metrics && metrics.server_metrics;
  const postgres = metrics && metrics.postgres_metrics;
  const redis = metrics && metrics.redis_metrics;
  const worker = metrics && metrics.worker_metrics;
  const requireNumber = (value, name) => {
    if (!Number.isFinite(value)) failures.push(`missing ${name}`);
    return Number.isFinite(value);
  };
  if (server) {
    if (requireNumber(server.host_cpu_p95_percent, "host CPU p95") && server.host_cpu_p95_percent > gates.host_cpu_p95_percent_max) failures.push("host CPU p95 exceeded");
    if (requireNumber(server.host_cpu_over_90_seconds, "host CPU >90% duration") && server.host_cpu_over_90_seconds > gates.host_cpu_over_90_max_seconds) failures.push("host CPU stayed over 90% too long");
    if (requireNumber(server.memory_used_fraction, "memory fraction") && server.memory_used_fraction > gates.memory_used_fraction_max) failures.push("memory fraction exceeded");
    if (requireNumber(server.swap_used_bytes, "swap usage") && server.swap_used_bytes > gates.swap_used_bytes_max) failures.push("swap was used");
    if (requireNumber(server.oom_events, "OOM events") && server.oom_events > gates.oom_events_max) failures.push("OOM was observed");
    if (requireNumber(server.unexpected_container_restarts, "container restarts") && server.unexpected_container_restarts > gates.unexpected_container_restarts_max) failures.push("unexpected container restart observed");
    if (requireNumber(server.disk_used_fraction, "disk used fraction") && server.disk_used_fraction >= gates.disk_used_fraction_max_exclusive) failures.push("disk warning threshold reached");
    if (requireNumber(server.disk_free_bytes, "disk free bytes") && server.disk_free_bytes < gates.disk_free_bytes_min) failures.push("disk free space below minimum");
  } else {
    failures.push("missing server metrics");
  }
  if (!postgres || postgres.safe !== true) failures.push("PostgreSQL evidence is missing or unsafe");
  if (!redis || redis.safe !== true) failures.push("Redis evidence is missing or unsafe");
  if (!worker || worker.safe !== true) failures.push("worker evidence is missing or unsafe");
  return { pass: failures.length === 0, failures };
}

export function evaluateCapacityEvidence(evidence, profile) {
  const failures = [];
  try { validateProfile(profile); } catch (error) { failures.push(error.message); }
  for (const field of REQUIRED_EVIDENCE_FIELDS) {
    if (!evidence || evidence[field] === undefined || evidence[field] === null) failures.push(`missing artifact: ${field}`);
  }
  if (!evidence || !evidence.summary || evidence.summary.complete !== true) failures.push("summary is incomplete");
  for (const name of profile.error_counters) {
    if (!evidence || !evidence.error_counters || evidence.error_counters[name] !== 0) failures.push(`error counter is non-zero or missing: ${name}`);
  }
  if (evidence && evidence.latency_metrics) {
    if (Object.keys(evidence.latency_metrics).length === 0) failures.push("latency metrics are empty");
    for (const [endpoint, metric] of Object.entries(evidence.latency_metrics)) {
      const limit = latencyClass(endpoint) === "transactional_write"
        ? profile.latency_classes.transactional_write.p95_ms_exclusive
        : profile.latency_classes.read_control_plane.p95_ms_exclusive;
      if (!metric || !Number.isFinite(metric.p95_ms) || metric.p95_ms >= limit) failures.push(`latency evidence failed: ${endpoint}`);
    }
  } else {
    failures.push("missing latency metrics");
  }
  failures.push(...scenarioFailures(evidence, profile));
  const resourceResult = evaluateResourceGates(evidence, profile.resource_gates);
  failures.push(...resourceResult.failures);
  return { pass: failures.length === 0, failures };
}

function scenarioFailures(evidence, profile) {
  const failures = [];
  const metrics = evidence && evidence.scenario_metrics;
  const scenario = evidence && evidence.scenario;
  if (!metrics || !scenario) return ["scenario metrics are missing"];
  const requireExact = (field, expected, label) => {
    if (metrics[field] !== expected) failures.push(`${label} is incomplete`);
  };
  if (scenario === "mixed-student-sustained" || scenario === "mixed-student-burst") {
    const configured = profile.scenarios[scenario === "mixed-student-sustained" ? "mixed_student_sustained" : "mixed_student_burst"];
    requireExact("application_requests", configured.total_rps * configured.duration_seconds, "mixed application request count");
    requireExact("application_successes", configured.total_rps * configured.duration_seconds, "mixed application success count");
  } else if (scenario === "public-catalogue") {
    const configured = profile.scenarios.public_catalogue;
    requireExact("application_requests", configured.rps * configured.duration_seconds, "public catalogue request count");
    requireExact("application_successes", configured.rps * configured.duration_seconds, "public catalogue success count");
  } else if (scenario === "login-surge") {
    requireExact("login_attempts", profile.scenarios.login_surge.distinct_students, "login attempt count");
    requireExact("login_bootstrap_successes", profile.scenarios.login_surge.successful_bootstraps, "login bootstrap count");
    requireExact("login_successes", profile.scenarios.login_surge.successful_logins, "login success count");
  } else if (scenario === "playback-surge") {
    requireExact("playback_attempts", profile.scenarios.playback_surge.starts, "playback start count");
    requireExact("playback_authorization_successes", profile.scenarios.playback_surge.starts, "playback authorization count");
    requireExact("playback_manifest_successes", profile.scenarios.playback_surge.starts, "playback manifest count");
  } else if (scenario === "privileged-operators") {
    requireExact("operator_requests", profile.scenarios.privileged_operators.rps * profile.scenarios.privileged_operators.duration_seconds, "operator request count");
  } else if (scenario === "upload-contention") {
    requireExact("upload_attempts", profile.scenarios.upload_contention.concurrent_uploads, "upload attempt count");
    requireExact("upload_successes", profile.scenarios.upload_contention.concurrent_uploads, "upload success count");
  } else {
    failures.push(`unknown scenario: ${scenario}`);
  }
  return failures;
}

export function validateCleanupScope(scope, profile) {
  const runID = scope && scope.run_id;
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$/.test(runID || "")) throw new Error("cleanup run id is invalid");
  if (scope.database_name !== `${profile.cleanup.database_prefix}${runID}`) throw new Error("cleanup database is outside the exact run scope");
  if (scope.storage_prefix !== `${profile.cleanup.storage_prefix}${runID}/`) throw new Error("cleanup storage prefix is outside the exact run scope");
  if (!scope.session_fixture_path || !scope.result_path || scope.session_fixture_path.includes("*") || scope.result_path.includes("*") ||
      !scope.session_fixture_path.includes(runID) || !scope.result_path.includes(runID)) throw new Error("cleanup paths must be exact, run-owned, non-wildcard paths");
  return true;
}
