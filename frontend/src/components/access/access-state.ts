import type { StudentCourseAccessHistoryItem } from "@/lib/api/access";

/**
 * What one Course-access record means to the Student, derived from the canonical backend lifecycle.
 *
 * Two independent entities decide this and they must not be conflated:
 *
 *  - the **invitation** (`PENDING_STUDENT_ACCEPTANCE` → `PENDING_ADMIN_APPROVAL` → `APPROVED` |
 *    `REJECTED` | `CANCELLED`) is a workflow record and never grants anything; and
 *  - the **entitlement**, which only Admin approval creates, and which is what `has_active_access`
 *    and `access_ends_at` report.
 *
 * An approved invitation whose entitlement has since ended is therefore `ACCESS_ENDED`, not
 * `ACTIVE` — reading the invitation alone would tell the Student they still have a Course they
 * cannot open.
 */
export type StudentAccessState =
  /** The Student must accept. Only reachable from the invitation link. */
  | "ACTION_REQUIRED"
  /** Accepted. Gradex is waiting for an Admin; nothing more for the Student to do. */
  | "AWAITING_APPROVAL"
  /** Approved and currently usable. */
  | "ACTIVE"
  /** Was approved; the access period has since ended or was revoked. */
  | "ACCESS_ENDED"
  /** An Admin declined the request. */
  | "REJECTED"
  /** Withdrawn before any decision. */
  | "CANCELLED"
  /** A record we cannot classify; rendered neutrally rather than guessed at. */
  | "UNKNOWN";

export function studentAccessState(item: StudentCourseAccessHistoryItem): StudentAccessState {
  if (item.has_active_access) return "ACTIVE";

  switch (item.invitation?.state) {
    case "PENDING_STUDENT_ACCEPTANCE":
      return "ACTION_REQUIRED";
    case "PENDING_ADMIN_APPROVAL":
      return "AWAITING_APPROVAL";
    case "APPROVED":
      // Approved, but no active entitlement remains — the access period ended or was revoked.
      return "ACCESS_ENDED";
    case "REJECTED":
      return "REJECTED";
    case "CANCELLED":
      return "CANCELLED";
    default:
      return "UNKNOWN";
  }
}

/** Only an active record may offer a route into the Course. */
export function canOpenCourse(state: StudentAccessState): boolean {
  return state === "ACTIVE";
}

/**
 * Whether a rejection reason may be shown.
 *
 * The server exposes `decision_reason` on the Student projection only for a rejection; showing it
 * anywhere else would surface Admin decision notes against records that are not refusals.
 */
export function rejectionReason(item: StudentCourseAccessHistoryItem): string | null {
  if (studentAccessState(item) !== "REJECTED") return null;
  const reason = item.invitation?.decision_reason?.trim();
  return reason ? reason : null;
}

/**
 * Sort order for the Student's list: what needs them first, then what is usable, then history.
 * Stable and locale-independent, so the same account sees the same order in both languages.
 */
const ORDER: Record<StudentAccessState, number> = {
  ACTION_REQUIRED: 0,
  ACTIVE: 1,
  AWAITING_APPROVAL: 2,
  REJECTED: 3,
  ACCESS_ENDED: 4,
  CANCELLED: 5,
  UNKNOWN: 6,
};

export function byStudentPriority(
  a: StudentCourseAccessHistoryItem,
  b: StudentCourseAccessHistoryItem,
): number {
  const byState = ORDER[studentAccessState(a)] - ORDER[studentAccessState(b)];
  if (byState !== 0) return byState;
  return (a.course_title || "").localeCompare(b.course_title || "");
}
