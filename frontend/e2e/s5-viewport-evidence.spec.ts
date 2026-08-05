import fs from "fs";
import path from "path";
import { expect, test, type Page } from "@playwright/test";
import { authenticateRotatingStudent, studentFor, viewportEvidenceTestSlot } from "./rotating-students";

/**
 * T075 — retained rendered RTL/LTR viewport evidence for every S5 screen (SC-010).
 *
 * The S2 T066 standard this task cites is a *rendered* standard: a repeatable Playwright Chromium
 * run over the locale x viewport matrix, asserting direction, absence of horizontal overflow, and
 * that the screen's primary capability is present — with the rendered output retained rather than
 * merely asserted and discarded. S2 satisfied "retained" with the HTML report alone because its
 * matrix was three viewports of one screen family. S5's matrix is four screens across four
 * viewports in two directions, so this spec additionally writes one PNG per
 * (screen, locale, viewport) to an external evidence directory.
 *
 * What this spec is NOT. T060 and T061 already own the behavioural proofs for Course Home and the
 * Lesson Player — authored ordering, real S4 playback authorization, Progress semantics, WCAG
 * scans. Re-asserting those here would duplicate them. This spec owns the two things the existing
 * matrix does not provide:
 *
 *   1. rendered evidence that is *retained* for every S5 screen, not only asserted; and
 *   2. viewport x direction coverage for the two S5 screens that had none — ST05, the Student
 *      Dashboard in its active state, and the Report Content modal, whose only prior coverage was
 *      component-level unit tests, which are not rendered-browser evidence.
 *
 * Screens (spec.md §Scope: "ST05 (learning half), ST06, ST07, and the Report Content modal").
 *
 * Every screen is reached through the real stack: the run-owned Go API against a per-run
 * PostgreSQL database, production session middleware, and the production protected-learning
 * routes. No protected endpoint is mocked.
 *
 * T076 owns time-to-first-frame. This spec renders the player and asserts its controls are present
 * and reachable; it measures nothing and sets no timing threshold.
 */

const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const LESSON_ID = "30000000-0000-0000-0000-000000000001";

/** The four supported viewports (SC-010). Named exactly as the success criterion names them. */
const VIEWPORTS = [
  { name: "phone", width: 390, height: 844 },
  { name: "tablet", width: 768, height: 1024 },
  { name: "laptop", width: 1280, height: 900 },
  { name: "desktop", width: 1440, height: 1000 },
];

const LOCALES = ["en", "ar"] as const;

/**
 * Where the rendered cells are written.
 *
 * Never inside the repository: the frontend `.gitignore` excludes `playwright-report/` and
 * `test-results/`, and the repository tracks no image evidence, so writing a PNG set into the tree
 * would invent a storage convention it does not have.
 *
 * Overridable because a local run and a CI run need different destinations for the same artifact. A
 * local run keeps its default scratch path; the CI job points this at the directory it uploads
 * through `actions/upload-artifact`, and that upload — not the directory — is what makes the
 * evidence durable.
 */
const EVIDENCE_DIR =
  process.env.GRADEX_T075_EVIDENCE_DIR || "/var/tmp/gradex-s5-e2e-evidence/t075-rendered";

/**
 * Values that must never reach a client surface. This is the read-model prohibition list the
 * expired-access matrix uses, plus the report-context and revision-binding names that D-065 keeps
 * encrypted and D-063 keeps out of public read models entirely.
 */
const PROHIBITED_IN_RENDERED_OUTPUT = [
  "revision_id",
  "asset_version_id",
  "target_revision_ref",
  "course_revision_id",
  "media_asset_version_id",
  "entitlement_id",
  "enrollment_id",
  "session_id",
  "object_key",
  "storage_object_key",
  "storage_path",
  "bucket",
  "playback_session",
  "trusted_duration",
  "queue_position",
  "moderation_state",
  "report_status",
  "remaining_quota",
  "can_report",
  // The context's *name as an addressable surface*. The token reaches the client as a React prop
  // by design (D-065), but `report_context` appearing as a field name, a `data-` attribute, or a
  // storage key means something handed it to the DOM where any script can read and copy it —
  // which the dialog's own contract forbids: "never rendered, never placed in an attribute, never
  // persisted". Normalized matching is what makes `data-report-context` fall inside this term.
  "report_context",
];

/** D-046 (community, deferred to S18) and the S17 office-hours deferral, in both scripts. */
const DEFERRED_SURFACE_TERMS = ["community", "discord", "telegram", "office hours", "coming soon", "قريبا", "مجتمع"];

const LABELS = {
  en: {
    dashboardTitle: "Your learning",
    openCourse: "Open course",
    courseTitle: "CS101: Introduction to Programming",
    courseOutline: "Course outline",
    lessonTitle: "Lesson 1: Introduction",
    reportAction: "Report",
    reportDialogTitle: "Report content",
    reportReasonLabel: "Reason",
    reportCancel: "Cancel",
  },
  ar: {
    dashboardTitle: "تعلّمك",
    openCourse: "فتح المقرر",
    courseTitle: "مقدمة في البرمجة",
    courseOutline: "مخطط المقرر",
    lessonTitle: "الدرس الأول: مرحباً بك",
    reportAction: "إبلاغ",
    reportDialogTitle: "الإبلاغ عن محتوى",
    reportReasonLabel: "السبب",
    reportCancel: "إلغاء",
  },
} as const;

/**
 * Writes the retained artifact. The filename encodes the full matrix coordinate so a reviewer can
 * find one cell without opening the report, and so a missing cell is visible as a missing file
 * rather than as a silently shorter set.
 */
async function retain(page: Page, screen: string, locale: string, viewport: string) {
  fs.mkdirSync(EVIDENCE_DIR, { recursive: true });
  const file = path.join(EVIDENCE_DIR, `${screen}__${locale}__${viewport}.png`);
  await page.screenshot({ path: file, fullPage: true });
  const { size } = fs.statSync(file);
  // A zero-byte or near-empty PNG is a capture that failed quietly. Evidence that cannot be
  // opened is not evidence.
  expect(size, `retained evidence ${file} must be a real image`).toBeGreaterThan(1024);
}

/**
 * The responsive invariant SC-010 actually rests on: the page never scrolls sideways. Asserted on
 * the document, so a single overflowing child anywhere in the tree fails it.
 */
async function expectNoHorizontalOverflow(page: Page, where: string) {
  const overflow = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    innerWidth: window.innerWidth,
  }));
  expect(
    overflow.scrollWidth,
    `${where}: horizontal overflow — scrollWidth ${overflow.scrollWidth} exceeds viewport ${overflow.innerWidth}`
  ).toBeLessThanOrEqual(overflow.innerWidth);
}

/**
 * Matching is semantic rather than literal, in the same shape as the T071/T072 detectors: text is
 * lowercased with every non-alphanumeric character removed before comparison, so `report_context`,
 * `reportContext`, `data-report-context`, and `report context` are one concept. A detector that
 * matched the underscore spelling alone would miss the most likely leak of all — an internal value
 * dropped into a `data-` attribute, where the delimiter is a hyphen by construction.
 */
function normalize(text: string): string {
  // Letters and digits of ANY script are kept — an ASCII-only class would erase the Arabic terms
  // entirely, and a `not.toContain("")` assertion is vacuously false for every input, which would
  // switch the Arabic half of this check off while still reporting green.
  return text.toLowerCase().replace(/[^\p{L}\p{N}]/gu, "");
}

/**
 * Information hiding, checked across every surface a value could escape through: served markup,
 * the URL, and both web-storage areas. The accessible tree is markup, so it is covered by the
 * content check.
 *
 * The opaque report context itself is deliberately NOT forbidden here. D-065 has the client carry
 * it as evidence it can neither read nor forge, so its presence in the page is the design; what
 * must never appear is the internal binding it encrypts, or a field *name* that implies the client
 * was handed one to interpret.
 */
async function expectNothingLeaked(page: Page, where: string) {
  const html = normalize(await page.content());
  for (const field of PROHIBITED_IN_RENDERED_OUTPUT) {
    expect(html, `${where}: rendered output must not contain ${field}`).not.toContain(normalize(field));
  }

  const url = normalize(page.url());
  for (const field of PROHIBITED_IN_RENDERED_OUTPUT) {
    expect(url, `${where}: URL must not carry ${field}`).not.toContain(normalize(field));
  }

  const storage = await page.evaluate(() => ({
    local: JSON.stringify(Object.entries(localStorage)),
    session: JSON.stringify(Object.entries(sessionStorage)),
  }));
  // The learning surfaces persist a locale preference and nothing else; a report context or any
  // internal identifier reaching storage would survive navigation and outlive the render it was
  // minted for.
  for (const field of [...PROHIBITED_IN_RENDERED_OUTPUT, "report_context"]) {
    expect(normalize(storage.local), `${where}: localStorage must not carry ${field}`).not.toContain(
      normalize(field)
    );
    expect(normalize(storage.session), `${where}: sessionStorage must not carry ${field}`).not.toContain(
      normalize(field)
    );
  }

  for (const term of DEFERRED_SURFACE_TERMS) {
    expect(html, `${where}: deferred surface "${term}" must not render (D-046 / S17)`).not.toContain(
      normalize(term)
    );
  }
}

// Real media, real database round trips, and Next.js dev-mode first compilation of four routes in
// two locales are legitimately slower than the 30 s default.
test.describe.configure({ timeout: 180_000 });

test.describe("T075 — S5 rendered RTL/LTR viewport evidence (SC-010)", () => {
  for (const [viewportIndex, vp] of VIEWPORTS.entries()) {
    test.describe(`Viewport: ${vp.name} (${vp.width}x${vp.height})`, () => {
      test.use({ viewport: { width: vp.width, height: vp.height } });

      test(`every S5 screen renders and is retained in both directions at ${vp.name}`, async ({
        context,
        page,
      }, testInfo) => {
        // Its own Student, so this matrix neither inherits nor donates Progress, and spends no
        // budget the Lesson Player matrix depends on.
        await authenticateRotatingStudent(context, studentFor(testInfo, viewportEvidenceTestSlot(viewportIndex)));

        // A console error on a screen whose whole purpose is to render correctly is a defect in
        // the evidence, not noise. Collected across the whole walk and asserted at the end so the
        // failure names every screen that produced one.
        const consoleErrors: string[] = [];
        page.on("console", (message) => {
          if (message.type() === "error") consoleErrors.push(`${page.url()} :: ${message.text()}`);
        });

        // Protected requests that fail are equally a defect: this walk exercises only authorized
        // paths, so no protected response should be a refusal.
        const failedProtectedResponses: string[] = [];
        page.on("response", (response) => {
          const url = response.url();
          if (!url.includes("/api/v1/learn/")) return;
          if (response.status() >= 400) failedProtectedResponses.push(`${response.status()} ${url}`);
        });

        for (const locale of LOCALES) {
          const isRTL = locale === "ar";
          const direction = isRTL ? "rtl" : "ltr";
          const t = LABELS[locale];

          // ------------------------------------------------ ST05 — Student Dashboard (learning half)
          await page.goto(`/${locale}/learn/dashboard`);
          await page.waitForLoadState("networkidle");

          const dashboardMain = page.locator("main");
          await expect(dashboardMain).toHaveAttribute("dir", direction);
          await expect(page.getByRole("heading", { name: t.dashboardTitle, level: 1 })).toBeVisible();

          // The Dashboard's capability is reaching a Course. It must survive every viewport —
          // FR-027's "no Student capability missing at any viewport".
          const openCourse = page.getByRole("link", { name: t.openCourse }).first();
          await expect(openCourse).toBeVisible();
          await expect(openCourse).toBeEnabled();

          await expectNoHorizontalOverflow(page, `ST05 ${locale} ${vp.name}`);
          await expectNothingLeaked(page, `ST05 ${locale} ${vp.name}`);
          await retain(page, "st05-dashboard", locale, vp.name);

          // --------------------------------------------------------------- ST06 — Course Home
          await page.goto(`/${locale}/learn/courses/${COURSE_ID}`);
          await page.waitForLoadState("networkidle");

          const courseMain = page.locator("main");
          await expect(courseMain).toHaveAttribute("dir", direction);
          await expect(page.getByRole("heading", { name: t.courseTitle, level: 1 })).toBeVisible();
          await expect(page.getByRole("navigation", { name: t.courseOutline })).toBeVisible();

          await expectNoHorizontalOverflow(page, `ST06 ${locale} ${vp.name}`);
          await expectNothingLeaked(page, `ST06 ${locale} ${vp.name}`);
          await retain(page, "st06-course-home", locale, vp.name);

          // ------------------------------------------- Report Content modal (Course target, ST06)
          const reportTrigger = page.getByRole("button", { name: new RegExp(t.reportAction) }).first();
          await expect(reportTrigger).toBeVisible();
          await reportTrigger.click();

          const dialog = page.getByRole("dialog");
          await expect(dialog).toBeVisible();
          // The dialog carries its own direction, so an RTL modal opened from an RTL page is not
          // relying on inheritance that a portal would break.
          await expect(dialog).toHaveAttribute("dir", direction);
          await expect(dialog.getByText(t.reportDialogTitle, { exact: true })).toBeVisible();

          // Localized, not an English fallback and not a raw backend enum.
          await expect(dialog.getByText(t.reportReasonLabel, { exact: false }).first()).toBeVisible();
          const dialogText = (await dialog.textContent()) ?? "";
          expect(dialogText, `report modal ${locale}: raw reason enum must not surface`).not.toContain(
            "suspected_copyright_violation"
          );
          expect(dialogText, `report modal ${locale}: raw reason enum must not surface`).not.toContain(
            "broken_unavailable"
          );

          // Focus moved into the dialog rather than staying on the page behind it.
          const focusInsideDialog = await page.evaluate(() => {
            const content = document.querySelector('[role="dialog"]');
            return Boolean(content && document.activeElement && content.contains(document.activeElement));
          });
          expect(focusInsideDialog, `report modal ${locale} ${vp.name}: focus must enter the dialog`).toBe(true);

          // The modal fits its viewport: it must scroll inside itself rather than widen the page.
          const dialogBox = await dialog.boundingBox();
          expect(dialogBox, "the report dialog must have a layout box").not.toBeNull();
          expect(
            dialogBox!.width,
            `report modal ${locale} ${vp.name}: dialog is wider than the viewport`
          ).toBeLessThanOrEqual(vp.width);
          await expectNoHorizontalOverflow(page, `report modal ${locale} ${vp.name}`);

          // The context that binds this report to the rendered instance never reaches the client
          // as anything a page can read back.
          await expectNothingLeaked(page, `report modal ${locale} ${vp.name}`);
          await retain(page, "report-modal", locale, vp.name);

          // Escape closes an unsubmitted report and returns focus to the trigger that opened it.
          await page.keyboard.press("Escape");
          await expect(dialog).toBeHidden();
          await expect(reportTrigger).toBeFocused();

          // ------------------------------------------------------------- ST07 — Lesson Player
          const playbackResponse = page.waitForResponse(
            (r) => r.url().includes(`/learn/lessons/${LESSON_ID}/playback`) && r.request().method() === "POST"
          );
          await page.goto(`/${locale}/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);

          // Real S4 playback authorization, not a stub.
          expect((await playbackResponse).status()).toBe(200);

          const playerMain = page.locator("main");
          await expect(playerMain).toHaveAttribute("dir", direction);
          await expect(page.getByRole("heading", { level: 1 })).toContainText(t.lessonTitle);

          // The player's capability at every viewport: platform-owned controls present, and the
          // native control set not leaking in their place.
          const playerControls = page.locator("[data-player-controls]");
          await expect(playerControls).toBeVisible();
          const video = page.locator("video");
          await expect(video).toBeVisible();
          await expect(video).not.toHaveAttribute("controls");

          await expectNoHorizontalOverflow(page, `ST07 ${locale} ${vp.name}`);
          await expectNothingLeaked(page, `ST07 ${locale} ${vp.name}`);
          await retain(page, "st07-lesson-player", locale, vp.name);

          // Leave no media running into the next locale's walk.
          await page.evaluate(() => document.querySelector("video")?.pause());
        }

        expect(failedProtectedResponses, "no protected learning request may fail on an authorized walk").toEqual(
          []
        );
        expect(consoleErrors, "no S5 screen may log a console error while rendering").toEqual([]);
      });
    });
  }
});
