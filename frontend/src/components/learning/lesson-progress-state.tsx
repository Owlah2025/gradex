"use client";

import * as React from "react";
import { CheckCircle2, Circle, CirclePlay } from "lucide-react";
import { lessonState } from "./curriculum-model";
import { useConfirmedLessonProgress } from "./use-progress-store";
import type { ConfirmedLessonProgress } from "./progress-contract";

/**
 * The Lesson's own state, kept current while the Student watches.
 *
 * The page renders this from the server's read model, which is correct at load
 * and then immediately begins to age: the Student watches to the end and the
 * line still reads "in progress" until they reload. It became a client
 * component so it can follow the confirmations the player receives — the same
 * server-computed completion, arriving on the write's response instead of
 * requiring a second page load to be seen.
 *
 * Completion is still entirely the server's. Nothing here decides it, and there
 * is no control that could claim it.
 */
export type LessonProgressLabels = {
  completed: string;
  inProgress: string;
  notStarted: string;
};

export function LessonProgressState({
  lessonID,
  initial,
  labels,
}: {
  lessonID: string;
  /** The server-rendered progress this page loaded with. */
  initial: ConfirmedLessonProgress;
  /**
   * Exactly the three words this component can render.
   *
   * Narrowed rather than the whole learning dictionary: a client component's
   * props are serialized into the page, so handing it the dictionary would ship
   * copy for states this Lesson is not in — which is the leak the payload
   * contract test exists to prevent.
   */
  labels: LessonProgressLabels;
}) {
  const progress = useConfirmedLessonProgress(lessonID, initial);
  const state = lessonState(progress);
  const StateIcon =
    state === "completed" ? CheckCircle2 : state === "in-progress" ? CirclePlay : Circle;
  const stateText =
    state === "completed"
      ? labels.completed
      : state === "in-progress"
        ? labels.inProgress
        : labels.notStarted;

  return (
    <p
      data-testid="lesson-state"
      data-lesson-state={state}
      className="inline-flex items-center gap-1.5 text-sm font-semibold text-foreground"
      // The state changes without a navigation, so it is announced rather than
      // silently replaced under a screen reader that has already read it.
      aria-live="polite"
    >
      <StateIcon
        aria-hidden
        className={`size-[18px] ${state === "completed" ? "text-primary" : "text-muted-foreground"}`}
      />
      {stateText}
    </p>
  );
}
