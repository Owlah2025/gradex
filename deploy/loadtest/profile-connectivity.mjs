import http from "k6/http";
import { Counter, Trend } from "k6/metrics";

const target = required("GRADEX_LOADTEST_TARGET_URL").replace(/\/$/, "");
const mode = required("GRADEX_LOADTEST_PROFILE_MODE");
const transportFailures = new Counter("profile_transport_failures");
const unexpectedStatuses = new Counter("profile_unexpected_statuses");
const duration = new Trend("profile_duration", true);
const blocked = new Trend("profile_blocked", true);
const connecting = new Trend("profile_connecting", true);
const tls = new Trend("profile_tls", true);
const waiting = new Trend("profile_waiting", true);

if (mode !== "bootstrap-fan-in" && mode !== "bootstrap-window" && mode !== "edge-steady") {
  throw new Error("GRADEX_LOADTEST_PROFILE_MODE must be bootstrap-fan-in, bootstrap-window, or edge-steady");
}

export const options = {
  scenarios: mode === "bootstrap-fan-in" ? {
    profile: { executor: "per-vu-iterations", vus: 500, iterations: 1, maxDuration: "60s" },
  } : mode === "bootstrap-window" ? {
    profile: {
      executor: "constant-arrival-rate", rate: 500, timeUnit: "60s", duration: "59999ms",
      preAllocatedVUs: 16, maxVUs: 500,
    },
  } : {
    profile: {
      executor: "constant-arrival-rate", rate: 250, timeUnit: "1s", duration: "60s",
      preAllocatedVUs: 64, maxVUs: 1000,
    },
  },
  discardResponseBodies: true,
  noConnectionReuse: false,
  summaryTrendStats: ["med", "p(95)", "p(99)", "max"],
  thresholds: {
    profile_transport_failures: ["count==0"],
    profile_unexpected_statuses: ["count==0"],
    dropped_iterations: ["count==0"],
  },
};

export default function () {
  const path = mode === "edge-steady" ? "/healthz" : "/api/v1/session/bootstrap";
  const response = http.get(`${target}${path}`, {
    redirects: 0,
    timeout: "60s",
    headers: { Accept: "application/json", "User-Agent": "Gradex-LG019-Connectivity-Profile/1" },
    tags: { name: mode },
  });
  duration.add(response.timings.duration);
  blocked.add(response.timings.blocked);
  connecting.add(response.timings.connecting);
  tls.add(response.timings.tls_handshaking);
  waiting.add(response.timings.waiting);
  if (response.status === 0) transportFailures.add(1);
  else if (response.status !== 200) unexpectedStatuses.add(1);
}

function required(name) {
  const value = __ENV[name];
  if (!value) throw new Error(`${name} is required`);
  return value;
}
