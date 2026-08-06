/**
 * Test-only loopback production-origin proxy for T076 (SC-001).
 *
 * WHY THIS EXISTS
 *   SC-001 is a claim about the built application, so T076 must not measure `next dev` — there,
 *   on-demand compilation and unoptimized assets dominate the figure (under the deterministic 4 Mbps
 *   profile ~86% of the elapsed time passed before the Play control even appeared).
 *
 *   But a standalone `next start` is not the deployed topology either. The browser client calls
 *   relative `/api/v1/...`, and `next.config.mjs` deliberately serves rewrites **only** in
 *   development — in production the deployed edge presents the frontend and the Go API behind one
 *   external origin. Run `next start` alone and those calls are served by Next itself and 404, which
 *   is exactly what happened: the Lesson Player rendered "This lesson could not start."
 *
 *   This proxy reproduces that one deployed property — same-origin frontend and API — for evidence
 *   only. It changes no production configuration and no production behaviour. `next.config.mjs` is
 *   untouched, and no production rewrite is added.
 *
 * WHAT IT IS NOT
 *   Not a general forward proxy. It has exactly two fixed upstreams, both validated as loopback, and
 *   it will not forward anywhere else. There is no target supplied per-request and no CONNECT support.
 *
 * LIFECYCLE
 *   1. Refuse to start unless a production build exists and every origin/port is present and sane.
 *   2. Spawn `next start` on an internal loopback port.
 *   3. Poll the internal server's real readiness URL — no fixed sleep.
 *   4. Only then bind the public port, so Playwright's `webServer.url` check cannot pass before the
 *      built app can actually serve.
 *   5. Exit non-zero if the Next child dies; kill the child on SIGINT/SIGTERM/exit so no orphan
 *      survives the run.
 *
 * LOGGING
 *   Paths and status codes only. No header, cookie, token, authorization value, signed URL, query
 *   string, or body is ever logged.
 */

import http from "http";
import fs from "fs";
import path from "path";
import { spawn } from "child_process";

const FRONTEND_DIR = path.resolve(import.meta.dirname, "..");

function fail(message) {
  console.error(`[t076-proxy] ${message}`);
  process.exit(1);
}

function requiredPort(name) {
  const raw = process.env[name];
  if (!raw) fail(`${name} is required`);
  const port = Number(raw);
  if (!Number.isInteger(port) || port < 1 || port > 65535) fail(`${name} is not a valid port: ${raw}`);
  return port;
}

/**
 * Both upstreams must be loopback. A non-loopback target would turn an evidence helper into
 * something that can reach the network, and T076's whole point is deterministic local evidence.
 */
const LOOPBACK = new Set(["127.0.0.1", "localhost", "::1"]);
function requiredLoopbackOrigin(name) {
  const raw = process.env[name];
  if (!raw) fail(`${name} is required`);
  let url;
  try {
    url = new URL(raw);
  } catch {
    return fail(`${name} is not a valid origin: ${raw}`);
  }
  if (url.protocol !== "http:") fail(`${name} must be http on loopback, got ${url.protocol}`);
  if (!LOOPBACK.has(url.hostname)) fail(`${name} must be loopback, got ${url.hostname}`);
  return { hostname: url.hostname, port: Number(url.port), origin: url.origin };
}

const publicPort = requiredPort("GRADEX_E2E_PUBLIC_FRONTEND_PORT");
const internalPort = requiredPort("GRADEX_E2E_INTERNAL_NEXT_PORT");
const api = requiredLoopbackOrigin("GRADEX_E2E_API_ORIGIN");

if (publicPort === internalPort) fail("public and internal ports must differ");

const buildId = path.join(FRONTEND_DIR, ".next", "BUILD_ID");
if (!fs.existsSync(buildId)) {
  fail(`a production build is required but ${buildId} is missing — run \`npm run build\` first`);
}

// Hop-by-hop headers are connection-scoped and must not be forwarded (RFC 9110 §7.6.1). Passing
// them through corrupts framing — a forwarded `transfer-encoding` alongside our own is how a proxy
// desynchronises a response.
const HOP_BY_HOP = new Set([
  "connection",
  "keep-alive",
  "proxy-authenticate",
  "proxy-authorization",
  "te",
  "trailer",
  "transfer-encoding",
  "upgrade",
]);

function sanitizeHeaders(headers) {
  const out = {};
  for (const [k, v] of Object.entries(headers)) {
    if (HOP_BY_HOP.has(k.toLowerCase())) continue;
    out[k] = v;
  }
  return out;
}

/** Exactly two upstreams, chosen by pathname alone. */
function upstreamFor(pathname) {
  if (pathname === "/api" || pathname.startsWith("/api/")) {
    return { hostname: api.hostname, port: api.port, label: "api" };
  }
  return { hostname: "127.0.0.1", port: internalPort, label: "next" };
}

let nextChild = null;
let shuttingDown = false;

function stopChild() {
  if (nextChild && !nextChild.killed) {
    try {
      nextChild.kill("SIGTERM");
    } catch {
      /* already gone */
    }
  }
}

function shutdown(code) {
  if (shuttingDown) return;
  shuttingDown = true;
  stopChild();
  process.exit(code);
}

process.on("SIGINT", () => shutdown(0));
process.on("SIGTERM", () => shutdown(0));
process.on("exit", stopChild);

// --- 1. the built Next server, on an internal loopback port ------------------
nextChild = spawn("npm", ["run", "start", "--", "--port", String(internalPort), "--hostname", "127.0.0.1"], {
  cwd: FRONTEND_DIR,
  env: process.env,
  stdio: ["ignore", "inherit", "inherit"],
});

nextChild.on("exit", (code, signal) => {
  if (shuttingDown) return;
  console.error(`[t076-proxy] next start exited unexpectedly (code=${code} signal=${signal})`);
  process.exit(1);
});

async function internalReady(timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const ok = await new Promise((resolve) => {
      const req = http.get({ hostname: "127.0.0.1", port: internalPort, path: "/", timeout: 2000 }, (res) => {
        res.resume();
        // Any HTTP answer proves the built server is serving; a redirect or 404 on "/" still means up.
        resolve(true);
      });
      req.on("timeout", () => {
        req.destroy();
        resolve(false);
      });
      req.on("error", () => resolve(false));
    });
    if (ok) return true;
    await new Promise((r) => setTimeout(r, 200));
  }
  return false;
}

// --- 2. the public origin -----------------------------------------------------
const server = http.createServer((clientReq, clientRes) => {
  let pathname;
  try {
    pathname = new URL(clientReq.url, "http://127.0.0.1").pathname;
  } catch {
    clientRes.writeHead(400).end();
    return;
  }

  const target = upstreamFor(pathname);
  const upstreamReq = http.request(
    {
      hostname: target.hostname,
      port: target.port,
      method: clientReq.method,
      // Full original URL, so query strings survive untouched.
      path: clientReq.url,
      headers: sanitizeHeaders(clientReq.headers),
    },
    (upstreamRes) => {
      clientRes.writeHead(upstreamRes.statusCode ?? 502, sanitizeHeaders(upstreamRes.headers));
      upstreamRes.pipe(clientRes); // streamed, so media and HTML are not buffered whole
    },
  );

  upstreamReq.on("error", (error) => {
    // Fail closed and say only which upstream and path failed — never headers or bodies.
    console.error(`[t076-proxy] ${target.label} upstream error for ${clientReq.method} ${pathname}: ${error.code ?? "error"}`);
    if (!clientRes.headersSent) clientRes.writeHead(502);
    clientRes.end();
  });

  clientReq.on("error", () => upstreamReq.destroy());
  clientReq.pipe(upstreamReq); // preserves method and body
});

server.on("error", (error) => fail(`cannot bind 127.0.0.1:${publicPort}: ${error.code ?? error.message}`));

const ready = await internalReady(180_000);
if (!ready) fail(`built Next server did not become ready on 127.0.0.1:${internalPort}`);

server.listen(publicPort, "127.0.0.1", () => {
  console.log(
    `[t076-proxy] public http://127.0.0.1:${publicPort} -> next http://127.0.0.1:${internalPort}, /api/* -> ${api.origin}`,
  );
});
