import { expect, type Page } from "@playwright/test";

/**
 * The Instructor authoring studio presents its five parts as disclosures, opened on arrival at the
 * first one that is not finished and advanced only by an explicit progression (see
 * `src/components/instructor/authoring-workflow.tsx`).
 *
 * Every suite below this helper predates that workflow and asserts against the panels themselves —
 * the curriculum, the media uploads, the submission checklist — rather than against how they are
 * revealed. Opening all five at the top of those flows keeps them testing what they were written to
 * test. The workflow's own behaviour, which is precisely what this helper suppresses, is asserted
 * in `instructor-authoring-workflow.spec.ts` and must not be proved here.
 */
export const AUTHORING_SECTIONS = [
  "BASICS",
  "DETAILS",
  "PREVIEW",
  "CURRICULUM",
  "REVIEW",
] as const;

export type AuthoringSection = (typeof AUTHORING_SECTIONS)[number];

/** Opens one authoring disclosure, and does nothing if it is already open. */
export async function openAuthoringSection(page: Page, section: AuthoringSection): Promise<void> {
  const toggle = page.getByTestId(`authoring-toggle-${section}`);
  await expect(toggle).toBeVisible();
  if ((await toggle.getAttribute("aria-expanded")) === "true") return;
  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-expanded", "true");
}

/**
 * Opens every authoring disclosure for the course currently selected in the studio.
 *
 * Call it once the studio has a selected course. It is idempotent, so calling it again after a
 * reload or a course switch is safe.
 */
export async function openAuthoringSections(page: Page): Promise<void> {
  // A revision that is with an administrator has no authoring workflow at all, and several of the
  // flows below pass through that state. Absence is therefore not a failure here: there is nothing
  // to open. Anything the caller then asserts against an authoring panel still fails on its own
  // locator, which is where that failure belongs.
  const workflow = page.getByTestId("authoring-workflow");
  const present = await workflow
    .waitFor({ state: "visible", timeout: 10_000 })
    .then(() => true)
    .catch(() => false);
  if (!present) return;
  for (const section of AUTHORING_SECTIONS) {
    await openAuthoringSection(page, section);
  }
}
