import type { LearningMaterialKind } from "@/lib/api/learning";

const materialPaths: Record<LearningMaterialKind, string> = {
  resource: "resource",
  lab_material: "lab-material",
};

export function materialEntryPath(lessonID: string, kind: LearningMaterialKind): string | null {
  const path = materialPaths[kind];
  if (!path) return null;
  return `/api/v1/media/lessons/${encodeURIComponent(lessonID)}/materials/${path}`;
}
