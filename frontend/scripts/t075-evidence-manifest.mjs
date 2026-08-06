/**
 * T075 rendered-evidence manifest, integrity check, and sensitive-data audit (SC-010).
 *
 * The repository had no durable retained-evidence mechanism, which is what blocked T075: the
 * Playwright HTML report and the 32 rendered PNGs were written to ignored or ephemeral paths and
 * nothing carried them anywhere. The CI evidence job closes that by uploading one directory as a
 * commit-associated GitHub Actions artifact. This script is what makes that artifact trustworthy:
 * it describes the set, proves the set is complete and undamaged, and refuses the upload if an
 * actual secret is inside it.
 *
 * Three modes, run in this order by the workflow:
 *
 *   generate  write manifest.json describing every artifact, with sizes and SHA-256 digests
 *   verify    prove the 32-cell matrix is exactly covered and every manifest claim holds
 *   audit     prove no real credential, token, cookie, or connection string is being uploaded
 *   sanitize-log  copy a Playwright log into the artifact with long opaque runs redacted
 *
 * `verify` and `audit` are separate from `generate` on purpose: a manifest that described whatever
 * it happened to find would pass by construction. `verify` re-derives the expected matrix from the
 * success criterion and checks the directory against that, so a missing cell fails rather than
 * shrinking the manifest.
 *
 * Usage:
 *   node scripts/t075-evidence-manifest.mjs generate <evidence-dir>
 *   node scripts/t075-evidence-manifest.mjs verify   <evidence-dir>
 *   node scripts/t075-evidence-manifest.mjs audit    <evidence-dir>
 *   node scripts/t075-evidence-manifest.mjs sanitize-log <raw-log> <evidence-dir>
 */

import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

const MANIFEST_SCHEMA = "gradex.t075.rendered-evidence/1";
const MANIFEST_NAME = "manifest.json";
const REPORT_RELATIVE = path.join("playwright-report", "index.html");
const RENDERED_DIR = "rendered";

/** The SC-010 matrix, declared rather than discovered. 4 x 2 x 4 = 32. */
const SCREENS = ["st05-dashboard", "st06-course-home", "report-modal", "st07-lesson-player"];
const LOCALES = [
  { locale: "en", direction: "ltr" },
  { locale: "ar", direction: "rtl" },
];
const VIEWPORTS = [
  { name: "phone", width: 390, height: 844 },
  { name: "tablet", width: 768, height: 1024 },
  { name: "laptop", width: 1280, height: 900 },
  { name: "desktop", width: 1440, height: 1000 },
];
const EXPECTED_CELLS = SCREENS.length * LOCALES.length * VIEWPORTS.length;

function fail(message) {
  console.error(`t075-evidence: ${message}`);
  process.exit(1);
}

function expectedMatrix() {
  const cells = [];
  for (const screen of SCREENS) {
    for (const { locale, direction } of LOCALES) {
      for (const viewport of VIEWPORTS) {
        cells.push({
          path: path.posix.join(RENDERED_DIR, `${screen}__${locale}__${viewport.name}.png`),
          screen,
          locale,
          direction,
          viewport: viewport.name,
          width: viewport.width,
          height: viewport.height,
        });
      }
    }
  }
  return cells;
}

function sha256(buffer) {
  return createHash("sha256").update(buffer).digest("hex");
}

/**
 * PNG geometry read from the IHDR chunk rather than trusted from the filename, so a file that was
 * captured at the wrong viewport is caught instead of described.
 */
function readPNG(absolute) {
  const bytes = fs.readFileSync(absolute);
  const signature = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
  if (bytes.length < 24 || !bytes.subarray(0, 8).equals(signature)) {
    return { valid: false, bytes };
  }
  return {
    valid: true,
    bytes,
    width: bytes.readUInt32BE(16),
    height: bytes.readUInt32BE(20),
  };
}

function playwrightVersion(frontendDir) {
  try {
    const pkg = JSON.parse(
      fs.readFileSync(path.join(frontendDir, "node_modules", "@playwright", "test", "package.json"), "utf8"),
    );
    return pkg.version ?? "unknown";
  } catch {
    return "unknown";
  }
}

function chromiumVersion(frontendDir) {
  try {
    const out = execFileSync("npx", ["playwright", "--version"], { cwd: frontendDir, encoding: "utf8" });
    return out.trim();
  } catch {
    return "unknown";
  }
}

// ---------------------------------------------------------------------------- generate

function generate(dir) {
  const frontendDir = path.resolve(import.meta.dirname, "..");
  const reportAbsolute = path.join(dir, REPORT_RELATIVE);
  if (!fs.existsSync(reportAbsolute)) fail(`HTML report is missing at ${REPORT_RELATIVE}`);
  const reportBytes = fs.readFileSync(reportAbsolute);

  const screenshots = [];
  for (const cell of expectedMatrix()) {
    const absolute = path.join(dir, cell.path);
    if (!fs.existsSync(absolute)) fail(`rendered cell is missing: ${cell.path}`);
    const png = readPNG(absolute);
    if (!png.valid) fail(`not a valid PNG: ${cell.path}`);
    screenshots.push({
      ...cell,
      rendered_width: png.width,
      rendered_height: png.height,
      bytes: png.bytes.length,
      sha256: sha256(png.bytes),
    });
  }

  const manifest = {
    schema: MANIFEST_SCHEMA,
    task: "T075",
    success_criterion: "SC-010",
    // Supplied by the workflow. Absent locally, which is itself the honest signal that a local run
    // is not a retained CI artifact.
    commit_sha: process.env.GITHUB_SHA ?? null,
    workflow_run_id: process.env.GITHUB_RUN_ID ?? null,
    workflow_run_attempt: process.env.GITHUB_RUN_ATTEMPT ?? null,
    generated_at_utc: new Date().toISOString(),
    playwright_version: playwrightVersion(frontendDir),
    chromium_runner_version: chromiumVersion(frontendDir),
    spec: "e2e/s5-viewport-evidence.spec.ts",
    expected_cells: EXPECTED_CELLS,
    actual_cells: screenshots.length,
    screens: SCREENS,
    locales: LOCALES,
    viewports: VIEWPORTS,
    html_report: {
      path: REPORT_RELATIVE.split(path.sep).join("/"),
      bytes: reportBytes.length,
      sha256: sha256(reportBytes),
    },
    screenshots,
    // Filled by `audit`, so a manifest can never claim an audit that did not run.
    sensitive_data_audit: { status: "not-run" },
  };

  fs.writeFileSync(path.join(dir, MANIFEST_NAME), `${JSON.stringify(manifest, null, 2)}\n`);
  console.log(`t075-evidence: wrote ${MANIFEST_NAME} — ${screenshots.length}/${EXPECTED_CELLS} cells`);
}

// ------------------------------------------------------------------------------ verify

function verify(dir) {
  const manifestPath = path.join(dir, MANIFEST_NAME);
  if (!fs.existsSync(manifestPath)) fail(`${MANIFEST_NAME} is missing`);
  const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
  if (manifest.schema !== MANIFEST_SCHEMA) fail(`unexpected manifest schema: ${manifest.schema}`);

  const problems = [];

  const reportAbsolute = path.join(dir, REPORT_RELATIVE);
  if (!fs.existsSync(reportAbsolute)) {
    problems.push("HTML report is missing");
  } else {
    const bytes = fs.readFileSync(reportAbsolute);
    if (bytes.length !== manifest.html_report?.bytes) problems.push("HTML report size disagrees with the manifest");
    if (sha256(bytes) !== manifest.html_report?.sha256) problems.push("HTML report digest disagrees with the manifest");
    if (!bytes.includes("playwrightReportBase64")) problems.push("HTML report carries no embedded report payload");
  }

  // Every expected cell must be present exactly once, and nothing may claim a cell twice.
  const byPath = new Map();
  for (const entry of manifest.screenshots ?? []) {
    if (byPath.has(entry.path)) problems.push(`manifest lists ${entry.path} more than once`);
    byPath.set(entry.path, entry);
  }

  const seenDigests = new Map();
  for (const cell of expectedMatrix()) {
    const entry = byPath.get(cell.path);
    if (!entry) {
      problems.push(`manifest is missing expected cell ${cell.path}`);
      continue;
    }
    byPath.delete(cell.path);

    const absolute = path.join(dir, cell.path);
    if (!fs.existsSync(absolute)) {
      problems.push(`manifest references a nonexistent file: ${cell.path}`);
      continue;
    }
    const png = readPNG(absolute);
    if (!png.valid) {
      problems.push(`invalid PNG: ${cell.path}`);
      continue;
    }
    if (png.width !== cell.width) {
      problems.push(`${cell.path}: width ${png.width} does not match viewport ${cell.viewport} (${cell.width})`);
    }
    if (png.bytes.length !== entry.bytes) problems.push(`${cell.path}: size disagrees with the manifest`);
    const digest = sha256(png.bytes);
    if (digest !== entry.sha256) problems.push(`${cell.path}: digest disagrees with the manifest`);
    // Two cells resolving to the same bytes means one screen was captured twice, or a placeholder
    // was written for both — a complete-looking set that proves less than it appears to.
    const duplicate = seenDigests.get(digest);
    if (duplicate) problems.push(`${cell.path} is byte-identical to ${duplicate}`);
    seenDigests.set(digest, cell.path);
    if (entry.screen !== cell.screen || entry.locale !== cell.locale || entry.direction !== cell.direction) {
      problems.push(`${cell.path}: manifest metadata disagrees with its matrix coordinate`);
    }
  }
  for (const extra of byPath.keys()) problems.push(`manifest lists an unexpected cell: ${extra}`);

  const renderedDir = path.join(dir, RENDERED_DIR);
  const onDisk = fs.existsSync(renderedDir) ? fs.readdirSync(renderedDir).filter((f) => f.endsWith(".png")) : [];
  if (onDisk.length !== EXPECTED_CELLS) {
    problems.push(`${RENDERED_DIR}/ holds ${onDisk.length} PNGs, expected exactly ${EXPECTED_CELLS}`);
  }
  if (manifest.actual_cells !== EXPECTED_CELLS || manifest.expected_cells !== EXPECTED_CELLS) {
    problems.push(`manifest cell counts are not ${EXPECTED_CELLS}/${EXPECTED_CELLS}`);
  }

  if (problems.length) {
    for (const p of problems) console.error(`t075-evidence: ${p}`);
    fail(`${problems.length} integrity problem(s)`);
  }
  console.log(`t075-evidence: integrity ok — ${EXPECTED_CELLS}/${EXPECTED_CELLS} cells, report verified`);
}

// ------------------------------------------------------------------------------- audit

/**
 * Values, not names.
 *
 * The rendered evidence legitimately contains the *names* of forbidden fields: the detector's own
 * assertion titles say "must not contain report_context", and the report embeds the spec source
 * that lists them. Failing on those would make the audit unusable and it would be switched off.
 * These patterns therefore match shapes that only an actual credential has.
 */
const SECRET_PATTERNS = [
  { name: "Set-Cookie header", re: /set-cookie\s*:/i },
  { name: "__Host- session cookie assignment", re: /__Host-[A-Za-z0-9_-]*\s*=\s*[A-Za-z0-9._~+/-]{8,}/ },
  { name: "Cookie request header with a value", re: /\bcookie\s*:\s*[A-Za-z0-9_-]+=[A-Za-z0-9._~+/-]{8,}/i },
  { name: "Bearer token", re: /\bbearer\s+[A-Za-z0-9._~+/-]{16,}/i },
  { name: "PostgreSQL connection string with credentials", re: /postgres(?:ql)?:\/\/[^\s:@"]+:[^\s:@"]+@/i },
  { name: "Redis connection string with credentials", re: /redis:\/\/[^\s:@"]*:[^\s:@"]+@/i },
  { name: "AWS/MinIO style signed URL", re: /[?&]X-Amz-Signature=[A-Za-z0-9%]+/i },
  { name: "signed playback token query parameter", re: /[?&](?:token|sig|signature)=[A-Za-z0-9._~+/-]{20,}/i },
  { name: "Playwright storage state", re: /"cookies"\s*:\s*\[\s*\{[^}]*"value"\s*:/ },
  { name: "private key block", re: /-----BEGIN [A-Z ]*PRIVATE KEY-----/ },
];

/** Files that must never be inside the artifact at all, whatever their contents. */
const FORBIDDEN_FILENAMES = [/^\.env/i, /storage-state.*\.json$/i, /(^|[.-])secrets?\.(json|ya?ml|txt)$/i];

function walk(dir, base = dir, out = []) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const absolute = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(absolute, base, out);
    else out.push({ absolute, relative: path.relative(base, absolute) });
  }
  return out;
}

function audit(dir) {
  const files = walk(dir);
  const findings = [];

  for (const file of files) {
    const basename = path.basename(file.relative);
    for (const pattern of FORBIDDEN_FILENAMES) {
      if (pattern.test(basename)) findings.push(`${file.relative}: forbidden file in artifact`);
    }
    // PNGs are scanned as bytes; anything else as text. A PNG carrying a credential would carry it
    // in a metadata chunk, which a byte scan still sees.
    const bytes = fs.readFileSync(file.absolute);
    const text = bytes.toString("latin1");
    for (const { name, re } of SECRET_PATTERNS) {
      const match = text.match(re);
      // Report the pattern that fired and where — never the captured value.
      if (match) findings.push(`${file.relative}: ${name} detected at offset ${match.index}`);
    }
  }

  const manifestPath = path.join(dir, MANIFEST_NAME);
  const result = {
    status: findings.length ? "failed" : "passed",
    files_scanned: files.length,
    patterns_checked: SECRET_PATTERNS.length + FORBIDDEN_FILENAMES.length,
    findings: findings.length,
    audited_at_utc: new Date().toISOString(),
    note: "Field names such as report_context appear in assertion titles and spec source and are not values; only credential-shaped values fail this audit.",
  };

  if (fs.existsSync(manifestPath)) {
    const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
    manifest.sensitive_data_audit = result;
    fs.writeFileSync(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`);
  }

  if (findings.length) {
    for (const f of findings) console.error(`t075-evidence: ${f}`);
    fail(`sensitive-data audit failed with ${findings.length} finding(s) — refusing upload`);
  }
  console.log(`t075-evidence: sensitive-data audit passed — ${files.length} files, no credential-shaped value`);
}

// ------------------------------------------------------------------- sanitize-log

/**
 * Copies a Playwright run log into the artifact with anything token-shaped removed.
 *
 * Hosted job logs need admin rights to read and artifacts need authentication, so a failing run was
 * effectively undiagnosable: the API even reports the test step as `success`, because
 * `continue-on-error` rewrites the step conclusion while `steps.t075.outcome` holds the truth. A log
 * inside the diagnostic artifact fixes that.
 *
 * It cannot be the raw log. On failure Playwright prints the received value of the assertion that
 * failed, and `expectNothingLeaked` compares against the whole normalized page — which legitimately
 * carries the encrypted report context, since D-065 has the client hold it. Publishing that verbatim
 * would put a live context token in an artifact. So every run of opaque characters long enough to be
 * a token is replaced by its length, and every line is capped: enough to identify the failing test
 * and assertion, never enough to carry a secret.
 */
function sanitizeLog(rawPath, dir) {
  const MAX_LINE = 500;
  const OPAQUE_RUN = /[A-Za-z0-9_-]{40,}/g;
  if (!fs.existsSync(rawPath)) fail(`raw log is missing: ${rawPath}`);

  const outDir = path.join(dir, "logs");
  fs.mkdirSync(outDir, { recursive: true });
  const outPath = path.join(outDir, "t075-playwright.log");

  let redactions = 0;
  let truncated = 0;
  const lines = fs.readFileSync(rawPath, "utf8").split(/\r?\n/).map((line) => {
    let out = line.replace(OPAQUE_RUN, (m) => {
      redactions += 1;
      return `<redacted-opaque-${m.length}-chars>`;
    });
    if (out.length > MAX_LINE) {
      truncated += 1;
      out = `${out.slice(0, MAX_LINE)}… <line truncated>`;
    }
    return out;
  });

  fs.writeFileSync(outPath, `${lines.join("\n")}\n`);
  console.log(
    `t075-evidence: sanitized log -> logs/t075-playwright.log (${lines.length} lines, ${redactions} opaque run(s) redacted, ${truncated} line(s) truncated)`,
  );

  // A concise, already-sanitized failure digest for the workflow summary and annotations.
  const failing = lines.filter((l) => /^\s*\d+\)|✘|Error:|expect\(/.test(l)).slice(0, 40);
  if (failing.length) {
    fs.writeFileSync(path.join(outDir, "t075-failure-digest.txt"), `${failing.join("\n")}\n`);
    console.log(`t075-evidence: failure digest -> logs/t075-failure-digest.txt (${failing.length} line(s))`);
  }
}

// --------------------------------------------------------------------------------- main

const [mode, arg1, arg2] = process.argv.slice(2);
if (!mode || !arg1) fail("usage: t075-evidence-manifest.mjs <generate|verify|audit|sanitize-log> ...");
const dir = mode === "sanitize-log" ? arg2 : arg1;
if (!dir) fail("usage: t075-evidence-manifest.mjs sanitize-log <raw-log> <evidence-dir>");
if (!fs.existsSync(dir)) fail(`evidence directory does not exist: ${dir}`);

switch (mode) {
  case "generate":
    generate(dir);
    break;
  case "verify":
    verify(dir);
    break;
  case "audit":
    audit(dir);
    break;
  case "sanitize-log":
    sanitizeLog(arg1, dir);
    break;
  default:
    fail(`unknown mode: ${mode}`);
}
