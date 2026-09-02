import {
  isMediaProcessing,
  recoverMediaPhase,
  type RecoveredMediaPhase,
} from "./media-upload-phase";

/**
 * Lesson video's view of the shared media recovery model. The rules live in
 * media-upload-phase so the public preview, which now has the same durable
 * completion + background processing lifecycle, cannot drift into a second
 * slightly different idea of what a reload should show.
 */
export type RecoveredLessonVideoPhase = RecoveredMediaPhase;

export const recoverLessonVideoPhase = recoverMediaPhase;

export const isLessonVideoProcessing = isMediaProcessing;
