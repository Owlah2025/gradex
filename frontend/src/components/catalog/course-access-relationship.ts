import type { StudentCourseAccessHistoryItem } from "@/lib/api/access";
import { studentAccessState, type StudentAccessState } from "../access/access-state";

/**
 * What the visitor looking at one Course Details page may do about access.
 *
 * Deliberately built on the ST-07 state model rather than a second mapping: the Access page and
 * Course Details must never disagree about the same Course. The only states added here are the two
 * that exist on a public page and nowhere else.
 */
export type CourseAccessRelationship =
  /** Not signed in. The Course is public; access is explained, never offered. */
  | "ANONYMOUS"
  /** Signed in, but this Course has no invitation and no entitlement. */
  | "NO_ACCESS"
  /** The Student's access state for this Course, as the Access page reports it. */
  | StudentAccessState
  /** The access lookup failed. Never silently downgraded to "no access". */
  | "UNAVAILABLE";

export type AccessLookup =
  | { status: "anonymous" }
  | { status: "loaded"; items: StudentCourseAccessHistoryItem[] }
  | { status: "failed" };

/**
 * Resolves this Course's relationship from the Student's own access records.
 *
 * `GET /me/course-access` already collapses a Course's records into one item — an active
 * entitlement is folded in alongside the invitation — so precedence is settled server-side and
 * `studentAccessState` applies its own rule that an active entitlement outranks whatever the
 * invitation says. Should the payload ever carry more than one row for a Course, the row with
 * active access wins here too, because that is the one that decides whether the Course can be
 * opened; ordering alone is never trusted.
 *
 * A failed lookup resolves to `UNAVAILABLE`, never `NO_ACCESS`. Guessing "no access" would hide a
 * real entitlement behind a transient error and tell an entitled Student they had nothing.
 */
export function courseAccessRelationship(
  lookup: AccessLookup,
  courseID: string,
): CourseAccessRelationship {
  if (lookup.status === "anonymous") return "ANONYMOUS";
  if (lookup.status === "failed") return "UNAVAILABLE";

  const matches = lookup.items.filter((item) => item.course_id === courseID);
  if (matches.length === 0) return "NO_ACCESS";

  const active = matches.find((item) => item.has_active_access);
  return studentAccessState(active ?? matches[0]);
}

/** Only an active entitlement may offer entry to the Course. */
export function offersCourseEntry(relationship: CourseAccessRelationship): boolean {
  return relationship === "ACTIVE";
}

/**
 * Whether to offer a route to the Access page.
 *
 * Shown where the Student has a real record to inspect and something to understand — never for an
 * anonymous visitor, and never where a lookup failed and we do not know what they have.
 */
export function offersAccessStatus(relationship: CourseAccessRelationship): boolean {
  return (
    relationship === "ACTION_REQUIRED" ||
    relationship === "AWAITING_APPROVAL" ||
    relationship === "ACCESS_ENDED" ||
    relationship === "REJECTED" ||
    relationship === "CANCELLED"
  );
}
