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

export type StatusLabels = Pick<LearningLabels, "active" | "expired">;
export type ProgressLabels = Pick<LearningLabels, "progress" | "completedLessons">;
export type AccessLabels = Pick<LearningLabels, "accessUntil" | "noExpiry">;
export type UnavailableLabels = Pick<LearningLabels, "unavailableTitle" | "unavailableBody">;

export type LessonProgressLabels = Pick<
  LearningLabels,
  "positionSeconds" | "completed" | "notCompleted"
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

export type OutlineLabels = Pick<LearningLabels, "courseOutline"> &
  LessonProgressLabels &
  MaterialsLabels;

export type NavigationLabels = Pick<
  LearningLabels,
  "lessonNavigation" | "previousLesson" | "nextLesson" | "firstLesson" | "lastLesson"
>;

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

export function progressLabels(labels: LearningLabels): ProgressLabels {
  return { progress: labels.progress, completedLessons: labels.completedLessons };
}

export function accessLabels(labels: LearningLabels): AccessLabels {
  return { accessUntil: labels.accessUntil, noExpiry: labels.noExpiry };
}

export function unavailableLabels(labels: LearningLabels): UnavailableLabels {
  return { unavailableTitle: labels.unavailableTitle, unavailableBody: labels.unavailableBody };
}

export function lessonProgressLabels(labels: LearningLabels): LessonProgressLabels {
  return {
    positionSeconds: labels.positionSeconds,
    completed: labels.completed,
    notCompleted: labels.notCompleted,
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

export function outlineLabels(labels: LearningLabels): OutlineLabels {
  return {
    courseOutline: labels.courseOutline,
    ...lessonProgressLabels(labels),
    ...materialsLabels(labels),
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
