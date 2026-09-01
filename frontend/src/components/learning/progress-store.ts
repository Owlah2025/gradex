import type {
  ConfirmedCourseProgress,
  ConfirmedLessonProgress,
  ProgressConfirmation,
} from "./progress-contract";

/**
 * The confirmed-progress store for the Lesson screen.
 *
 * The player sits several levels below the Lesson's state badge and beside the
 * Course contents, and the page that renders all three is a server component.
 * There is no shared React state to lift into, so the confirmations the player
 * receives are published here and the sibling surfaces subscribe.
 *
 * It holds only what the server has already confirmed. Nothing is written here
 * optimistically, so no surface can display a completion the server has not
 * recorded — which is the same rule the server-rendered page follows, and the
 * reason a Lesson cannot be marked complete from the browser.
 *
 * Everything is keyed by Lesson because a Course's contents list many, and only
 * the one being watched has news.
 */
export type Snapshot = {
  lessons: Record<string, ConfirmedLessonProgress>;
  course: ConfirmedCourseProgress | null;
};

const empty: Snapshot = { lessons: {}, course: null };
let snapshot: Snapshot = empty;
const listeners = new Set<() => void>();

/**
 * The external-store subscription.
 *
 * Exported because it is the store's contract, not an internal: `useSyncExternalStore`
 * consumes it here, and a test observing "did anything a reader can see move?"
 * has to consume the same signal rather than a parallel approximation of it.
 */
export function subscribeToProgress(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/**
 * Records one confirmation and notifies subscribers.
 *
 * A confirmation that changes nothing does not replace the snapshot. That is
 * not a micro-optimization: `useSyncExternalStore` compares snapshots by
 * identity, so publishing a new object every fifteen seconds would re-render
 * every subscriber on every ordinary progress tick, whether or not anything
 * the reader can see has moved.
 */
export function publishProgressConfirmation(confirmation: ProgressConfirmation): void {
  const known = snapshot.lessons[confirmation.lessonID];
  const lessonChanged =
    !known ||
    known.completed !== confirmation.lesson.completed ||
    known.position_seconds !== confirmation.lesson.position_seconds;
  const courseChanged =
    confirmation.course !== null &&
    (snapshot.course === null ||
      snapshot.course.completed_lessons !== confirmation.course.completed_lessons ||
      snapshot.course.total_lessons !== confirmation.course.total_lessons ||
      snapshot.course.percent !== confirmation.course.percent);
  if (!lessonChanged && !courseChanged) return;

  snapshot = {
    lessons: lessonChanged
      ? { ...snapshot.lessons, [confirmation.lessonID]: confirmation.lesson }
      : snapshot.lessons,
    course: courseChanged ? confirmation.course : snapshot.course,
  };
  for (const listener of listeners) listener();
}

/** The current confirmed state. Treat as immutable: identity is the change signal. */
export function progressSnapshot(): Snapshot {
  return snapshot;
}

/**
 * The server-rendered snapshot.
 *
 * Always the empty one: nothing has been confirmed during a server render, and
 * returning the live module value would let one request's progress leak into
 * another's HTML.
 */
export function serverProgressSnapshot(): Snapshot {
  return empty;
}

/** Test-only reset so one module instance cannot leak state between cases. */
export function resetProgressStoreForTest(): void {
  snapshot = empty;
  listeners.clear();
}
