import type { StudentCourseAccessHistoryItem } from "../../lib/api/access";
import { studentAccessState } from "../access/access-state";

/**
 * The Dashboard's pending Course-access summary.
 *
 * It deliberately reuses F12's `studentAccessState` rather than re-reading the invitation and
 * entitlement fields: those two entities decide different things, and a second interpretation of
 * them here would be a second place to get the distinction wrong.
 *
 * The result carries counts only. The Access page owns the detail — this surface never names a
 * Course, an invitation, an identifier, or a lifecycle enum, so nothing here can leak the
 * vocabulary the Student is not meant to see.
 */
export type PendingAccessSummary = {
  /** Invitations the Student has not accepted yet. Only they can move these forward. */
  actionRequired: number;
  /** Accepted invitations with no Admin decision yet. Nothing for the Student to do. */
  awaitingApproval: number;
};

export function pendingAccessSummary(
  items: readonly StudentCourseAccessHistoryItem[] | null | undefined,
): PendingAccessSummary {
  const summary: PendingAccessSummary = { actionRequired: 0, awaitingApproval: 0 };
  if (!items) return summary;
  for (const item of items) {
    switch (studentAccessState(item)) {
      case "ACTION_REQUIRED":
        summary.actionRequired += 1;
        break;
      case "AWAITING_APPROVAL":
        summary.awaitingApproval += 1;
        break;
      default:
        break;
    }
  }
  return summary;
}

/** Whether the summary has anything worth showing at all. */
export function hasPendingAccess(summary: PendingAccessSummary): boolean {
  return summary.actionRequired > 0 || summary.awaitingApproval > 0;
}
