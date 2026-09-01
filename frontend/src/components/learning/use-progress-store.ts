"use client";

import { useSyncExternalStore } from "react";
import {
  progressSnapshot,
  serverProgressSnapshot,
  subscribeToProgress,
} from "./progress-store";
import type { ConfirmedCourseProgress, ConfirmedLessonProgress } from "./progress-contract";

/**
 * The Lesson's progress as the server last confirmed it, or the value the page
 * was rendered with.
 *
 * The server-rendered value is the floor, not a default to be discarded: a
 * Lesson already completed before this page load stays completed even though
 * this session has confirmed nothing about it yet.
 */
export function useConfirmedLessonProgress(
  lessonID: string,
  initial: ConfirmedLessonProgress,
): ConfirmedLessonProgress {
  const live = useSyncExternalStore(subscribeToProgress, progressSnapshot, serverProgressSnapshot).lessons[lessonID];
  if (!live) return initial;
  // Completion is write-once server-side, so a confirmation can only ever add
  // it. Taking the disjunction means a confirmation that arrives without it —
  // a rewind, say — cannot un-complete what the page already knew.
  return { position_seconds: live.position_seconds, completed: live.completed || initial.completed };
}

/** The Course aggregate as the server last confirmed it, else the rendered one. */
export function useConfirmedCourseProgress(
  initial: ConfirmedCourseProgress,
): ConfirmedCourseProgress {
  return useSyncExternalStore(subscribeToProgress, progressSnapshot, serverProgressSnapshot).course ?? initial;
}
