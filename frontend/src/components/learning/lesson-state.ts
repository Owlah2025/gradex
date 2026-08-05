import type { LearningStatus } from "@/lib/api/learning";

export type LessonPlaybackPlan = {
  mountPlayer: boolean;
  mountProgressReporter: boolean;
};

export function lessonPlaybackPlan(status: LearningStatus): LessonPlaybackPlan {
  const active = status === "active";
  return { mountPlayer: active, mountProgressReporter: active };
}
