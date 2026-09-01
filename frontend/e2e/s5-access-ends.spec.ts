import { test, expect, type BrowserContext, type Page } from "@playwright/test";
import { execFileSync } from "child_process";
import fs from "fs";
import { RUN_STATE_FILE_PATH, SEED_BINARY_PATH } from "../src/lib/api/e2e-infrastructure";
import { queryProgress, requireProgressRow, type ProgressSnapshot } from "../src/lib/api/e2e-progress";
import { AUTHORIZATION_FLAG, expectAbsent, tokenLabel } from "./authority-leak";

/**
 * T042 — access ending mid-session (SC-005).
 *
 * Each scenario begins **fully authorised inside an open authenticated browser session**, then has
 * its authority taken away server-side while that session stays open. That ordering is the whole
 * point: seeding a Student as already unauthorised proves the read model refuses a state it was
 * born in, which is T043's evidence. T042 proves the far stronger property — that authority is
 * re-evaluated on the *next* protected action, so a session that was legitimately authorised a
 * moment ago cannot keep acting on that stale decision.
 *
 * Everything is real: real Next.js routes, the real Go API, real PostgreSQL on production
 * migrations, production authentication and session middleware, the production S4 evaluator, and
 * the production playback and Progress endpoints. No protected route is intercepted or mocked.
 */

const SHARED_COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const SHARED_LESSON_ID = "30000000-0000-0000-0000-000000000001";
const EMERGENCY_COURSE_ID = "c3000000-0000-0000-0000-000000000001";
const EMERGENCY_LESSON_ID = "33000000-0000-0000-0000-000000000001";

/** The single representative configuration: T042 is authority revalidation, not a visual matrix. */
const LOCALE = "en";
const VIEWPORT = { width: 1280, height: 900 };

type AuthorityCondition = "expire-entitlement" | "revoke-entitlement" | "suspend-account" | "emergency-suspend-course";

type Scenario = {
  key: string;
  title: string;
  mutation: AuthorityCondition;
  studentAccountID: string;
  email: string;
  courseID: string;
  lessonID: string;
  /**
   * D-063's retained-expired presentation applies to an expired Entitlement only. Every other
   * ending renders the generic unavailable state, which names no cause.
   */
  presentation: "retained-expired" | "generic-unavailable";
  /** Deterministic fixture identity, known to the test runner only, for the supplementary write. */
  assetVersionID: string;
};

const SCENARIOS: Scenario[] = [
  {
    key: "expiry",
    title: "Entitlement expires",
    mutation: "expire-entitlement",
    studentAccountID: "a3000000-0000-0000-0000-000000000001",
    email: "student-access-ends-1@example.test",
    courseID: SHARED_COURSE_ID,
    lessonID: SHARED_LESSON_ID,
    presentation: "retained-expired",
    assetVersionID: "60000000-0000-0000-0000-000000000001",
  },
  {
    key: "revocation",
    title: "Entitlement is revoked",
    mutation: "revoke-entitlement",
    studentAccountID: "a3000000-0000-0000-0000-000000000002",
    email: "student-access-ends-2@example.test",
    courseID: SHARED_COURSE_ID,
    lessonID: SHARED_LESSON_ID,
    presentation: "generic-unavailable",
    assetVersionID: "60000000-0000-0000-0000-000000000001",
  },
  {
    key: "account-suspension",
    title: "Student Account is suspended",
    mutation: "suspend-account",
    studentAccountID: "a3000000-0000-0000-0000-000000000003",
    email: "student-access-ends-3@example.test",
    courseID: SHARED_COURSE_ID,
    lessonID: SHARED_LESSON_ID,
    presentation: "generic-unavailable",
    assetVersionID: "60000000-0000-0000-0000-000000000001",
  },
  {
    key: "emergency-suspension",
    title: "Emergency Course access suspension becomes active",
    mutation: "emergency-suspend-course",
    studentAccountID: "a3000000-0000-0000-0000-000000000004",
    email: "student-access-ends-4@example.test",
    courseID: EMERGENCY_COURSE_ID,
    lessonID: EMERGENCY_LESSON_ID,
    presentation: "generic-unavailable",
    assetVersionID: "63000000-0000-0000-0000-000000000001",
  },
];

/** Authority internals that must never reach the browser. */
const FORBIDDEN_IN_BROWSER: (string | RegExp)[] = [
  "asset_version_id",
  "entitlement_id",
  "enrollment_id",
  "revision_id",
  "can_play",
  "can_update_progress",
  AUTHORIZATION_FLAG,
  "capability",
  "evaluator",
  "object_key",
  "storage_object_key",
  "bucket",
  "X-Amz-Signature",
];

function seederEnv() {
  const state = JSON.parse(fs.readFileSync(RUN_STATE_FILE_PATH, "utf-8"));
  return {
    dbName: state.dbName as string,
    env: {
      ...process.env,
      GRADEX_E2E_ALLOW_DATABASE_RESET: "1",
      GRADEX_E2E_ADMIN_DB_URL: "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable",
      GRADEX_E2E_TARGET_DB_NAME: state.dbName,
      GRADEX_E2E_TARGET_DB_URL: `postgres://gradex:gradex@localhost:5432/${state.dbName}?sslmode=disable`,
      DATABASE_URL: "postgres://gradex:gradex@localhost:5432/gradex?sslmode=disable",
    } as NodeJS.ProcessEnv,
  };
}

/**
 * Applies one allowlisted authority mutation test-runner-side against the isolated per-run
 * database. The browser never receives database credentials, no production mutation endpoint
 * exists, and production evaluation is untouched — only the authority rows the real evaluator
 * reads are changed.
 */
function applyAuthorityMutation(scenario: Scenario): { operation: string; rows_affected: number; applied_at: string } {
  const { dbName, env } = seederEnv();
  const args = ["-access-mutation", scenario.mutation, "-dbname", dbName, "-student", scenario.studentAccountID];
  if (scenario.mutation !== "suspend-account") args.push("-course", scenario.courseID);

  const output = execFileSync(SEED_BINARY_PATH, args, { env, encoding: "utf-8" });
  const result = JSON.parse(output.trim());
  // Exactly one authority row changed: a mutation that matched nothing would let the scenario
  // "pass" against authority that was never actually taken away.
  expect(result.rows_affected).toBe(1);
  expect(result.operation).toBe(scenario.mutation);
  return result;
}

type LearningStateSnapshot = {
  entitlement: { found: boolean; state: string; access_ends_at: string; revoked_at?: string | null };
  enrollment: { found: boolean; created_at: string };
  progress: { lesson_identity_id: string; max_position_seconds: number; last_position_seconds: number; completed: boolean }[];
};

function readLearningState(scenario: Scenario): LearningStateSnapshot {
  const { dbName, env } = seederEnv();
  const output = execFileSync(
    SEED_BINARY_PATH,
    ["-query-learning-state", "-dbname", dbName, "-student", scenario.studentAccountID, "-course", scenario.courseID],
    { env, encoding: "utf-8" }
  );
  return JSON.parse(output.trim()) as LearningStateSnapshot;
}

function progressQuery(scenario: Scenario) {
  return {
    studentAccountID: scenario.studentAccountID,
    courseID: scenario.courseID,
    lessonIdentityID: scenario.lessonID,
  };
}

type Session = { csrfToken: string };

/** Production authentication only: bootstrap for CSRF, then the real login route. */
async function authenticateScenarioStudent(context: BrowserContext, scenario: Scenario): Promise<Session> {
  const page = await context.newPage();
  await page.goto(`/${LOCALE}/catalog`);

  const login = await page.evaluate(async (email) => {
    const bootstrap = await fetch("/api/v1/session/bootstrap", { method: "GET", credentials: "include" });
    const { csrf_token } = await bootstrap.json();
    const response = await fetch("/api/v1/sessions", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", Accept: "application/json", "X-CSRF-Token": csrf_token },
      body: JSON.stringify({ email, password: "StudentPassword123!" }),
    });
    return { status: response.status, body: await response.json() };
  }, scenario.email);

  expect(login.status).toBe(201);
  expect(login.body.role).toBe("STUDENT");
  await page.close();
  return { csrfToken: login.body.csrf_token };
}

type RawResponse = { status: number; headers: Record<string, string>; body: string };

/**
 * A real request issued from the authenticated browser context.
 *
 * `page.request` sends over the wire through the context's own cookie jar, so it carries the
 * production session cookie and passes through production middleware and the real evaluator
 * exactly as the application's own calls do. Nothing is intercepted, stubbed, or routed.
 */
async function requestFromSession(
  page: Page,
  input: { method: string; path: string; csrfToken?: string; body?: unknown }
): Promise<RawResponse> {
  const response = await page.request.fetch(input.path, {
    method: input.method,
    maxRedirects: 0,
    failOnStatusCode: false,
    headers: {
      Accept: "application/json, application/problem+json",
      "Cache-Control": "no-store",
      ...(input.body ? { "Content-Type": "application/json" } : {}),
      ...(input.csrfToken ? { "X-CSRF-Token": input.csrfToken } : {}),
    },
    ...(input.body ? { data: JSON.stringify(input.body) } : {}),
  });

  const headers: Record<string, string> = {};
  for (const [key, value] of Object.entries(response.headers())) headers[key.toLowerCase()] = value;
  return { status: response.status(), headers, body: await response.text() };
}

/** Waits until the player is genuinely playing real HLS media, without a blind sleep. */
async function waitForPlayableMedia(page: Page): Promise<void> {
  await expect(page.locator("video")).toBeVisible();
  await expect
    .poll(
      () =>
        page.evaluate(() => {
          const video = document.querySelector("video");
          return video ? video.readyState : 0;
        }),
      { timeout: 30_000, intervals: [250, 250, 500] }
    )
    .toBeGreaterThanOrEqual(2);
}

/**
 * Drives one real Progress write through the production reporter by seeking the media element —
 * the reporter's own `seeked` trigger. No component internal is called.
 */
async function reportProgressThroughPlayer(
  page: Page,
  lessonID: string,
  targetSeconds: number
): Promise<{ status: number; headers: Record<string, string> }> {
  // Match on the payload, not merely the URL. The player emits its own writes — notably the
  // resume-position write on `loadedmetadata` — and matching any Progress PUT would capture one of
  // those instead, silently comparing a baseline the test never asked for.
  const responsePromise = page.waitForResponse((response) => {
    if (!response.url().includes(`/learn/lessons/${lessonID}/progress`)) return false;
    if (response.request().method() !== "PUT") return false;
    const payload = response.request().postData();
    if (!payload) return false;
    try {
      return Math.abs(JSON.parse(payload).position_seconds - targetSeconds) <= 0.5;
    } catch {
      return false;
    }
  }, { timeout: 30_000 });

  await page.evaluate((seconds) => {
    const video = document.querySelector("video");
    if (video) video.currentTime = seconds;
  }, targetSeconds);

  const response = await responsePromise;
  const headers: Record<string, string> = {};
  for (const [key, value] of Object.entries(response.headers())) headers[key.toLowerCase()] = value;

  // Status and headers only. The reporter never reads its own response body, so Chromium does not
  // retain it and `response.text()` never resolves for these. The denial *body* is compared
  // byte-for-byte from a supplementary direct request instead — this observation still proves the
  // real production reporter attempted the write and was refused.
  return { status: response.status(), headers };
}

function expectUniformDenial(response: RawResponse | { status: number; headers: Record<string, string>; body: string }, what: string) {
  expect(response.status, `${what} must use the uniform refusal status`).toBe(404);
  expect(response.headers["content-type"], `${what} content type`).toContain("application/problem+json");
  expect(response.headers["cache-control"], `${what} must not be cached`).toContain("no-store");
  expect(response.headers["location"], `${what} must not redirect`).toBeUndefined();

  // The denial names no cause: no evaluator reason, no authority internals, no signed target.
  const lowered = response.body.toLowerCase();
  for (const term of ["expired", "revoked", "suspend", "entitlement", "enrollment", "evaluator", "scope", "retired"]) {
    expect(lowered, `${what} body leaked the authority cause "${term}"`).not.toContain(term);
  }
  for (const term of FORBIDDEN_IN_BROWSER) {
    expectAbsent(lowered, typeof term === "string" ? term.toLowerCase() : term);
  }
}

/** Collected across scenarios so denial equivalence is compared on raw, unnormalised bytes. */
const deniedProgressBodies = new Map<string, string>();
const deniedPlaybackBodies = new Map<string, string>();
const deniedPlaybackHeaders = new Map<string, Record<string, string>>();

test.describe.configure({ timeout: 180_000 });

test.describe("T042 — access ending mid-session denies the next issuance and the next Progress write", () => {
  test.use({ viewport: VIEWPORT, locale: LOCALE });

  // One test, four scenarios. The byte-equivalence comparison at the end needs every condition's
  // denial in hand, and Playwright discards a worker after any failure — so collecting across
  // separate tests would silently compare an empty set. Each scenario is a reported step.
  test("access ending mid-session denies the next issuance and the next Progress write, uniformly", async ({
    browser,
  }) => {
    for (const scenario of SCENARIOS) {
      await test.step(scenario.title, async () => {
      const context = await browser.newContext({ viewport: VIEWPORT });
      try {
        // No protected route may be intercepted: every authority outcome below comes from the
        // real Go API reading real PostgreSQL.
        const session = await authenticateScenarioStudent(context, scenario);
        const page = await context.newPage();

        const requests: { url: string; method: string }[] = [];
        page.on("request", (request) => requests.push({ url: request.url(), method: request.method() }));

        // ---------- Baseline: fully authorised, inside this session ----------
        const lessonUrl = `/${LOCALE}/learn/courses/${scenario.courseID}/lessons/${scenario.lessonID}`;
        const playbackAuthorised = page.waitForResponse(
          (response) =>
            response.url().includes(`/learn/lessons/${scenario.lessonID}/playback`) &&
            response.request().method() === "POST"
        );
        const masterManifest = page.waitForResponse((response) => response.url().includes(".m3u8"));

        await page.goto(lessonUrl);
        expect((await playbackAuthorised).status(), "baseline playback authorisation").toBe(200);
        expect((await masterManifest).status()).toBeLessThan(400);
        await waitForPlayableMedia(page);
        await expect(page.locator("[data-player-controls]")).toBeVisible();

        // A real, accepted production Progress write through the production reporter.
        const acceptedWrite = await reportProgressThroughPlayer(page, scenario.lessonID, 9);
        // 200 with the canonical state, not 204: the browser needs the
        // completion and the Course aggregate the server just computed, or the
        // visible progress stays stale until a reload.
        expect(acceptedWrite.status, "baseline Progress write must be accepted").toBe(200);

        const baseline = requireProgressRow(
          queryProgress(progressQuery(scenario)),
          `${scenario.key} baseline Progress row`
        );
        const authorityBefore = readLearningState(scenario);
        expect(authorityBefore.enrollment.found).toBe(true);
        expect(authorityBefore.entitlement.found).toBe(true);
        expect(authorityBefore.entitlement.state).toBe("ACTIVE");

        // Pause so no periodic tick moves the position underneath the comparison.
        await page.evaluate(() => document.querySelector("video")?.pause());

        // ---------- Mid-session authority mutation ----------
        // The browser stays open, the session is not replaced, nothing is reloaded first.
        const mutation = applyAuthorityMutation(scenario);
        expect(mutation.rows_affected).toBe(1);

        // ---------- The next Progress write is refused ----------
        // The real production reporter attempts the write; its status and headers carry the
        // uniform-denial contract.
        const deniedWrite = await reportProgressThroughPlayer(page, scenario.lessonID, 17);
        expect(deniedWrite.status, `${scenario.key}: the reporter's next write must be refused`).toBe(404);
        expect(deniedWrite.headers["content-type"]).toContain("application/problem+json");
        expect(deniedWrite.headers["cache-control"]).toContain("no-store");
        expect(deniedWrite.headers["location"]).toBeUndefined();

        // A supplementary direct write from the same authenticated context supplies the comparable
        // denial body. It supplements the reporter attempt above rather than replacing it: the
        // reporter never reads its own response body, so Chromium does not retain it.
        const deniedWriteBody = await requestFromSession(page, {
          method: "PUT",
          path: `/api/v1/learn/lessons/${scenario.lessonID}/progress`,
          csrfToken: session.csrfToken,
          body: { position_seconds: 18, asset_version_id: scenario.assetVersionID },
        });
        expectUniformDenial(deniedWriteBody, `${scenario.key} next Progress write body`);
        deniedProgressBodies.set(scenario.key, deniedWriteBody.body);

        // ---------- The next playback issuance is refused ----------
        const deniedPlayback = await requestFromSession(page, {
          method: "POST",
          path: `/api/v1/learn/lessons/${scenario.lessonID}/playback`,
          csrfToken: session.csrfToken,
        });
        expectUniformDenial(deniedPlayback, `${scenario.key} next playback issuance`);
        // No signed target of any kind. The problem document's `type` URI is a stable problem
        // identifier, not a media target, so the check names what a leak would actually look like.
        for (const marker of ["manifest", ".m3u8", ".ts", "X-Amz", "Signature", "Expires=", "127.0.0.1"]) {
          expect(deniedPlayback.body, `playback denial leaked "${marker}"`).not.toContain(marker);
        }
        deniedPlaybackBodies.set(scenario.key, deniedPlayback.body);
        deniedPlaybackHeaders.set(scenario.key, deniedPlayback.headers);

        // No new media traffic followed the denial.
        const mediaAfterDenial = requests.filter((r) => r.url.includes(".m3u8") || r.url.includes(".ts")).length;
        await page.waitForTimeout(500);
        expect(
          requests.filter((r) => r.url.includes(".m3u8") || r.url.includes(".ts")).length,
          "a denied issuance must not be followed by new manifest, variant, or segment requests"
        ).toBe(mediaAfterDenial);

        // ---------- PostgreSQL is unchanged apart from the intended mutation ----------
        const afterProgress = requireProgressRow(
          queryProgress(progressQuery(scenario)),
          `${scenario.key} Progress after denial`
        );
        expectProgressUnchanged(baseline, afterProgress, scenario.key);

        const authorityAfter = readLearningState(scenario);
        expect(authorityAfter.enrollment.found, "Enrollment must survive access ending").toBe(true);
        expect(authorityAfter.enrollment.created_at, "Enrollment must not be rewritten").toBe(
          authorityBefore.enrollment.created_at
        );
        expect(authorityAfter.progress.length, "no Progress row may be created or removed").toBe(
          authorityBefore.progress.length
        );
        // The Entitlement changed only where this scenario intended it to.
        if (scenario.mutation === "revoke-entitlement") {
          expect(authorityAfter.entitlement.state).toBe("REVOKED");
        } else if (scenario.mutation === "expire-entitlement") {
          expect(authorityAfter.entitlement.state).toBe("ACTIVE");
          expect(authorityAfter.entitlement.access_ends_at).not.toBe(authorityBefore.entitlement.access_ends_at);
        } else {
          expect(authorityAfter.entitlement.state).toBe(authorityBefore.entitlement.state);
          expect(authorityAfter.entitlement.access_ends_at).toBe(authorityBefore.entitlement.access_ends_at);
        }

        // ---------- The next authoritative read reflects the new state ----------
        const playbackAfterReload: string[] = [];
        page.on("request", (request) => {
          if (request.url().includes("/playback")) playbackAfterReload.push(request.url());
        });
        await page.goto(lessonUrl, { waitUntil: "load" });

        if (scenario.presentation === "retained-expired") {
          // D-063: retained expired metadata and retained Progress stay visible; nothing is authorised.
          await expect(page.locator("p", { hasText: /Access expired/i })).toBeVisible();
        } else {
          // Generic unavailable: no cause is named anywhere on the page.
          await expect(page.getByRole("heading", { name: "Learning is unavailable" })).toBeVisible();
          const visibleText = ((await page.locator("body").textContent()) ?? "").toLowerCase();
          for (const term of ["revoked", "suspended", "entitlement", "enrollment", "emergency"]) {
            expect(visibleText, `generic unavailable page named the cause "${term}"`).not.toContain(term);
          }
        }

        // Stale active UI is gone: no player, no reporter, no material actions, no new issuance.
        await expect(page.locator("video")).toHaveCount(0);
        await expect(page.locator("[data-player-controls]")).toHaveCount(0);
        await expect(page.locator("[data-lesson-player]")).toHaveCount(0);
        expect(playbackAfterReload, "the fresh authoritative read must not request playback").toEqual([]);
        await expect(
          page.locator(`a[href*="/materials/resource"], a[href*="/materials/lab-material"]`)
        ).toHaveCount(0);

        // The stable S4 material entry routes refuse uniformly from this same session.
        for (const kind of ["resource", "lab-material"]) {
          const material = await requestFromSession(page, {
            method: "GET",
            path: `/api/v1/media/lessons/${scenario.lessonID}/materials/${kind}`,
          });
          expectUniformDenial(material, `${scenario.key} ${kind} entry route`);
        }

        // ---------- Information hiding and browser storage ----------
        const pageText = ((await page.locator("body").textContent()) ?? "").toLowerCase();
        for (const term of FORBIDDEN_IN_BROWSER) {
          expectAbsent(
            pageText,
            typeof term === "string" ? term.toLowerCase() : term,
            `authority internal "${tokenLabel(term)}" reached the DOM`,
          );
        }
        const storage = await page.evaluate(() => ({
          local: JSON.stringify(Object.entries(localStorage)),
          session: JSON.stringify(Object.entries(sessionStorage)),
        }));
        for (const blob of [storage.local, storage.session]) {
          const lowered = blob.toLowerCase();
          for (const term of ["x-amz-signature", "asset_version", "manifest", "__host-", "entitlement"]) {
            expect(lowered, `browser storage retained "${term}"`).not.toContain(term);
          }
        }

        // No horizontal overflow at the representative viewport.
        expect(
          await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)
        ).toBe(true);
      } finally {
        await context.close();
      }
      });
    }

    expectDenialsAreUniform();
  });

  // Every authority outcome above must come from the real Go API reading real PostgreSQL. This
  // asserts it structurally rather than by inspection: if any protected route were ever stubbed,
  // the denials would prove nothing about production evaluation.
  test("no protected route is intercepted, stubbed, or mocked in any S5 suite", () => {
    const suiteDir = __dirname;
    const specs = fs.readdirSync(suiteDir).filter((name) => name.startsWith("s5-") && name.endsWith(".spec.ts"));
    expect(specs.length, "the S5 suites must be discoverable for this audit").toBeGreaterThanOrEqual(5);

    for (const spec of specs) {
      const source = fs.readFileSync(`${suiteDir}/${spec}`, "utf-8");
      // Assembled at runtime so this audit does not match its own assertion text.
      const verbs = ["page." + "route(", "context." + "route(", "routeFrom" + "HAR(", "." + "fulfill(", "." + "abort("];
      for (const interceptor of verbs) {
        expect(source, `${spec} must not install ${interceptor}`).not.toContain(interceptor);
      }
      for (const protectedPrefix of ["/api/v1/learn/", "/api/v1/media/", "/api/v1/session/", "/api/v1/sessions"]) {
        expect(source.includes(`route("**${protectedPrefix}`), `${spec} intercepts ${protectedPrefix}`).toBe(false);
        expect(source.includes(`route('**${protectedPrefix}`), `${spec} intercepts ${protectedPrefix}`).toBe(false);
      }
    }
  });

});

/** Byte-identical protected-unavailable denial across every authority-ending condition. */
function expectDenialsAreUniform() {
    expect(deniedProgressBodies.size, "all four scenarios must have contributed a denied Progress body").toBe(
      SCENARIOS.length
    );
    expect(deniedPlaybackBodies.size).toBe(SCENARIOS.length);

    // Raw bytes, compared without normalisation or reconstruction.
    const progressBodies = [...deniedProgressBodies.values()];
    const playbackBodies = [...deniedPlaybackBodies.values()];
    for (const body of progressBodies) expect(body).toBe(progressBodies[0]);
    for (const body of playbackBodies) expect(body).toBe(playbackBodies[0]);

    // Headers that carry the contract are equivalent too.
    const headerSets = [...deniedPlaybackHeaders.values()];
    for (const headers of headerSets) {
      expect(headers["content-type"]).toBe(headerSets[0]["content-type"]);
      expect(headers["cache-control"]).toBe(headerSets[0]["cache-control"]);
      expect(headers["location"]).toBeUndefined();
    }

    // Nothing in the shared denial distinguishes which condition ended access.
    const lowered = playbackBodies[0].toLowerCase();
    for (const term of ["expired", "revoked", "suspend", "emergency", "entitlement", "scope"]) {
      expect(lowered).not.toContain(term);
    }
}

function expectProgressUnchanged(before: ProgressSnapshot, after: ProgressSnapshot, description: string) {
  expect(after.found, `${description}: the Progress row must still exist`).toBe(true);
  expect(after.position_seconds, `${description}: resume point must not move`).toBe(before.position_seconds);
  expect(after.max_position_seconds, `${description}: monotonic maximum must not move`).toBe(
    before.max_position_seconds
  );
  expect(after.completed, `${description}: completion must not change`).toBe(before.completed);
  expect(after.completed_at).toBe(before.completed_at);
  expect(after.asset_version_id).toBe(before.asset_version_id);
  expect(after.updated_at, `${description}: a denied write must not touch the row`).toBe(before.updated_at);
}
