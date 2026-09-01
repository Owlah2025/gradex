import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import type { LearningStatus } from "@/lib/api/learning";

type LearningLabels = Dictionary["learning"];

/**
 * Narrow label sets for the learning surfaces (T7, GAP-04).
 *
 * # WHY THIS EXISTS
 *
 * `report-labels.ts` already established the rule for client components: hand a component the copy
 * it renders, never the catalogue it could choose from, because "handing it the whole learning
 * dictionary would put unrelated strings — 'Active access' among them — into the markup of pages
 * that must not contain them".
 *
 * That rule was applied to client components only, on the reasonable belief that only client props
 * are serialized. They are not the only ones. In development React emits a server-component owner
 * stack for every element it renders, and that stack carries each **server** component's props
 * verbatim into the page. So a server component handed the whole dictionary publishes the whole
 * dictionary, exactly as a client component would.
 *
 * The correction is the same rule, applied one level up: **every** component receives the narrowest
 * data it renders. That makes the leak structurally impossible rather than dependent on which build
 * mode is running, which is why the S5 evidence now passes in development and production alike.
 *
 * These are `Pick`s rather than hand-written types so a renamed dictionary key is a compile error
 * here, and builders rather than casts so the narrowing is a real object at runtime — a `Pick` alone
 * would type-check while still serializing every key the value actually carries.
 */

export type StatusLabels = Pick<LearningLabels, "active" | "expired" | "activeDetail" | "expiredDetail">;
export type ProgressLabels = Pick<LearningLabels, "progress" | "completedLessons">;
export type AccessLabels = Pick<LearningLabels, "accessUntil" | "noExpiry">;
export type UnavailableLabels = Pick<LearningLabels, "unavailableTitle" | "unavailableBody">;

/**
 * What a Lesson's own state is called.
 *
 * The raw playback position deliberately left this set. "Progress: 145 seconds · Not completed" was
 * a database row read aloud: seconds are the reporter's unit, not a fact a Student has any use for,
 * and the Lesson Player already returns them to the exact second they stopped at. What remains is
 * the state itself, which is the part they can act on.
 */
export type LessonStateLabels = Pick<
  LearningLabels,
  "completed" | "lessonInProgress" | "lessonNotStarted"
>;

export type MaterialsLabels = Pick<
  LearningLabels,
  | "materials"
  | "resources"
  | "labMaterials"
  | "resource"
  | "labMaterial"
  | "openResource"
  | "openLabMaterial"
  | "download"
  | "preparingDownload"
  | "downloadUnavailable"
>;

export type CurriculumLabels = Pick<
  LearningLabels,
  "currentLessonLabel" | "completedLessons" | "files"
> &
  LessonStateLabels;

export type NavigationLabels = Pick<
  LearningLabels,
  "lessonNavigation" | "previousLesson" | "nextLesson" | "firstLesson" | "lastLesson"
>;

/** The learning frame's own copy: navigation and the two sheet controls, and nothing else. */
export type ShellLabels = {
  learningNavigation: string;
  myCourses: string;
  /** The public catalogue, so a Student can find a Course they do not yet hold. */
  catalogue: string;
  /** The start of the product, reachable without the browser's own Back button. */
  home: string;
  openMenu: string;
  closeMenu: string;
  skipToContent: string;
};

/**
 * learningStatusLabel resolves the badge text on the server.
 *
 * The badge used to receive both strings and choose between them, which meant an expired page
 * carried the active copy it deliberately does not display. Resolving here is what makes
 * `"Active access"` genuinely absent from an expired render rather than merely unrendered.
 */
export function learningStatusLabel(status: LearningStatus, labels: StatusLabels): string {
  return status === "expired" ? labels.expired : labels.active;
}

/** The same resolution for the sentence beside the badge, for the same reason. */
export function learningStatusDetail(status: LearningStatus, labels: StatusLabels): string {
  return status === "expired" ? labels.expiredDetail : labels.activeDetail;
}

export function progressLabels(labels: LearningLabels): ProgressLabels {
  return { progress: labels.progress, completedLessons: labels.completedLessons };
}

export function accessLabels(labels: LearningLabels): AccessLabels {
  return { accessUntil: labels.accessUntil, noExpiry: labels.noExpiry };
}

export function unavailableLabels(labels: LearningLabels): UnavailableLabels {
  return { unavailableTitle: labels.unavailableTitle, unavailableBody: labels.unavailableBody };
}

export function lessonStateLabels(labels: LearningLabels): LessonStateLabels {
  return {
    completed: labels.completed,
    lessonInProgress: labels.lessonInProgress,
    lessonNotStarted: labels.lessonNotStarted,
  };
}

export function materialsLabels(labels: LearningLabels): MaterialsLabels {
  return {
    materials: labels.materials,
    resources: labels.resources,
    labMaterials: labels.labMaterials,
    resource: labels.resource,
    labMaterial: labels.labMaterial,
    openResource: labels.openResource,
    openLabMaterial: labels.openLabMaterial,
    download: labels.download,
    preparingDownload: labels.preparingDownload,
    downloadUnavailable: labels.downloadUnavailable,
  };
}

export function curriculumLabels(labels: LearningLabels): CurriculumLabels {
  return {
    currentLessonLabel: labels.currentLessonLabel,
    completedLessons: labels.completedLessons,
    files: labels.files,
    ...lessonStateLabels(labels),
  };
}

export function navigationLabels(labels: LearningLabels): NavigationLabels {
  return {
    lessonNavigation: labels.lessonNavigation,
    previousLesson: labels.previousLesson,
    nextLesson: labels.nextLesson,
    firstLesson: labels.firstLesson,
    lastLesson: labels.lastLesson,
  };
}

export function shellLabels(dictionary: Dictionary): ShellLabels {
  return {
    learningNavigation: dictionary.learning.learningNavigation,
    myCourses: dictionary.learning.myCourses,
    catalogue: dictionary.learning.shellCatalogue,
    home: dictionary.learning.shellHome,
    openMenu: dictionary.meta.openMenu,
    closeMenu: dictionary.meta.closeMenu,
    skipToContent: dictionary.meta.skipToContent,
  };
}
