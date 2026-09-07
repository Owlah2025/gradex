import type { OwnedCourseSummary } from "@/lib/api/catalog";
// Value imports must resolve without the `@/` alias: the node test build emits CommonJS with no
// path mapping, and only `import type` is erased before it gets there.
import {
  submissionReadiness,
  type ReadinessKey,
  type ReadinessRequirement,
} from "./submission-readiness";

/**
 * The authoring workflow, derived from the course the server actually holds.
 *
 * Authoring V2 presents one long form as five disclosures that open and close as the work
 * progresses. That only helps if "this part is done" means something — a disclosure that collapses
 * because it was visited, or because a field lost focus, is a moving target rather than a summary.
 * So every state here is read from persisted domain facts:
 *
 *   BASICS      the revision's two titles, which is what a course is called
 *   DETAILS     the academic identity requirements the server checks at submission
 *   PREVIEW     whether a public preview asset is attached, and whether the thumbnail resolved
 *   CURRICULUM  the section, lesson and lesson-video requirements the server checks
 *   REVIEW      whether every client-checkable requirement is currently met
 *
 * The requirement rows come from `submissionReadiness`, which is itself a reading of
 * `catalog/validation.go`. Nothing new is invented here and no requirement is added: this module
 * only decides which disclosure owns which existing requirement, so a collapsed section can say
 * how many of its own things are outstanding instead of hiding them.
 *
 * The server remains authoritative. A complete plan is not a promise that submission will be
 * accepted — three of the server's checks depend on state no client can see — and the submission
 * control still goes to the server and still reports its refusal.
 */
export const AUTHORING_SECTION_ORDER = [
  "BASICS",
  "DETAILS",
  "PREVIEW",
  "CURRICULUM",
  "REVIEW",
] as const;

export type AuthoringSectionKey = (typeof AUTHORING_SECTION_ORDER)[number];

/**
 * `ATTENTION` is not "incomplete with emphasis". It is reserved for a section holding a state the
 * instructor has to resolve rather than simply has not reached yet — today, a course thumbnail that
 * failed to resolve, which blocks submission outright. Collapsing that into `INCOMPLETE` would put
 * a blocking problem behind a closed disclosure looking like unstarted work.
 */
export type AuthoringSectionState = "COMPLETE" | "INCOMPLETE" | "ATTENTION";

/** The workable sections. `REVIEW` reports on the others and is never counted as one of them. */
export const AUTHORING_WORK_SECTIONS = AUTHORING_SECTION_ORDER.filter(
  (key) => key !== "REVIEW",
) as Exclude<AuthoringSectionKey, "REVIEW">[];

const READINESS_OWNER: Record<ReadinessKey, AuthoringSectionKey> = {
  ACADEMIC_INSTITUTION: "DETAILS",
  ACADEMIC_SUBJECT: "DETAILS",
  LEGACY_MAJOR: "DETAILS",
  LEGACY_SUBJECT: "DETAILS",
  // The study-year control is part of the revision form, so the section that owns the control owns
  // the requirement. A section reporting an outstanding item whose field lives somewhere else is
  // worse than not reporting it: it sends the reader to the wrong panel.
  LEGACY_STUDY_YEAR: "BASICS",
  SECTIONS: "CURRICULUM",
  SECTION_LESSONS: "CURRICULUM",
  LESSON_VIDEOS: "CURRICULUM",
};

export type AuthoringSectionPlan = {
  key: AuthoringSectionKey;
  state: AuthoringSectionState;
  /** The section's own outstanding requirements, so a collapsed header can count them. */
  outstanding: ReadinessRequirement[];
};

export type AuthoringPlan = {
  sections: AuthoringSectionPlan[];
  /** Workable sections that are complete, and how many there are. Review is not one of them. */
  completeCount: number;
  totalCount: number;
  /**
   * Where the work continues: the first workable section that is not complete, or `REVIEW` when
   * they all are. This is what the shell opens on arrival and what it advances to on a successful
   * progression.
   */
  nextSection: AuthoringSectionKey;
  /** Every client-checkable requirement is met. The server may still refuse. */
  ready: boolean;
  sectionCount: number;
  lessonCount: number;
};

function blank(value: string | null | undefined): boolean {
  return !value || value.trim().length === 0;
}

export function authoringPlan(
  course: OwnedCourseSummary,
  locale: "ar" | "en",
  positionLabel: (kind: "section" | "lesson", position: number) => string,
  options?: {
    /**
     * The thumbnail could not be resolved, which is the one authoring state that disables
     * submission without appearing in the server's readiness rules. It is surfaced rather than
     * silently folded into "not finished".
     */
    thumbnailUnresolved?: boolean;
  },
): AuthoringPlan {
  const readiness = submissionReadiness(course, locale, positionLabel);
  const revision = course.editable_revision;
  const sections = revision?.sections ?? [];
  const lessonCount = sections.reduce(
    (total, section) => total + (section.lessons ?? []).length,
    0,
  );

  const outstandingBySection = new Map<AuthoringSectionKey, ReadinessRequirement[]>();
  for (const requirement of readiness.requirements) {
    if (requirement.met) continue;
    const owner = READINESS_OWNER[requirement.key];
    const held = outstandingBySection.get(owner) ?? [];
    held.push(requirement);
    outstandingBySection.set(owner, held);
  }

  // A course is created with both titles, so BASICS normally arrives complete — which is the point.
  // The instructor lands on a studio that has already closed the part they finished at creation
  // rather than on five open panels.
  const titlesMissing = [revision?.title_ar, revision?.title_en].filter(blank).length;

  const previewAttached = Boolean(revision?.preview_asset_version_id);
  const previewFailed = revision?.preview_asset_state === "FAILED";
  const thumbnailUnresolved = options?.thumbnailUnresolved === true;

  const stateOf = (key: AuthoringSectionKey): AuthoringSectionState => {
    switch (key) {
      case "BASICS":
        return titlesMissing === 0 && (outstandingBySection.get("BASICS") ?? []).length === 0
          ? "COMPLETE"
          : "INCOMPLETE";
      case "PREVIEW":
        if (thumbnailUnresolved || previewFailed) return "ATTENTION";
        return previewAttached ? "COMPLETE" : "INCOMPLETE";
      case "REVIEW":
        return readiness.ready ? "COMPLETE" : "INCOMPLETE";
      default:
        return (outstandingBySection.get(key) ?? []).length === 0 ? "COMPLETE" : "INCOMPLETE";
    }
  };

  const plans: AuthoringSectionPlan[] = AUTHORING_SECTION_ORDER.map((key) => ({
    key,
    state: stateOf(key),
    outstanding: outstandingBySection.get(key) ?? [],
  }));

  const workable = plans.filter((plan) => plan.key !== "REVIEW");
  const nextSection =
    workable.find((plan) => plan.state !== "COMPLETE")?.key ?? ("REVIEW" as AuthoringSectionKey);

  return {
    sections: plans,
    completeCount: workable.filter((plan) => plan.state === "COMPLETE").length,
    totalCount: workable.length,
    nextSection,
    ready: readiness.ready,
    sectionCount: sections.length,
    lessonCount,
  };
}

/**
 * The section to open after a successful progression out of `from`.
 *
 * Deliberately forward-looking rather than "the first incomplete section anywhere": an instructor
 * who saves the basics of a course whose curriculum is already built should land on what comes
 * after the basics, not be sent back to a section they passed. When nothing after `from` is
 * outstanding, the review section is where the work ends up.
 */
export function sectionAfter(
  plan: AuthoringPlan,
  from: AuthoringSectionKey,
): AuthoringSectionKey {
  const order = AUTHORING_SECTION_ORDER;
  const start = order.indexOf(from);
  if (start < 0) return plan.nextSection;
  for (let index = start + 1; index < order.length; index += 1) {
    const candidate = plan.sections.find((section) => section.key === order[index]);
    if (!candidate) continue;
    if (candidate.key === "REVIEW" || candidate.state !== "COMPLETE") return candidate.key;
  }
  return "REVIEW";
}
