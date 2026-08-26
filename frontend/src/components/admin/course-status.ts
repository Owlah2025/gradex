import type { CourseLifecycleSummary } from "@/lib/api/catalog";
import type { ReviewQueueItem } from "@/lib/api/review";

/**
 * One shared vocabulary for what an Admin is looking at when they look at a Course.
 *
 * Before this module every surface answered the question its own way: the lifecycle workspace
 * concatenated the raw `lifecycle` enum into a sentence, the review queue rendered bespoke pills,
 * the Course Access picker hardcoded a green `PUBLISHED` chip, and the learning views had their own
 * badge. The words a reader sees now come from the dictionary; what this module owns is the
 * *semantics* — which state, who has to act next, and what the next action is.
 *
 * Nothing here is an authority. Every value is derived from what the server already returned, and
 * no function decides whether an action is permitted — the API refuses what it refuses.
 */

/** The lifecycle states the server actually serves. No state is invented here. */
export type AdminCourseState =
  | "DRAFT"
  | "PENDING_REVIEW"
  | "CHANGES_REQUESTED"
  | "PUBLISHED"
  | "DELISTED"
  | "ARCHIVED";

/** Who the product is waiting on. `NOBODY` means the Course is at rest, not that it is finished. */
export type AwaitingActor = "ADMIN" | "INSTRUCTOR" | "NOBODY";

/** The one action a directory row offers. `REVIEW` is the only one that opens the review workspace. */
export type CourseAction = "REVIEW" | "MANAGE" | "VIEW";

/** Visual tone, expressed in design-system `Badge` variants rather than raw colours. */
export type StatusTone = "default" | "accent" | "success" | "neutral";

/**
 * One Course as the Admin directory understands it: the lifecycle row the server returned, plus the
 * submitted revision when — and only when — the review queue actually contains one for it.
 */
export type AdminCourseRow = {
  summary: CourseLifecycleSummary;
  /**
   * The submitted revision awaiting a decision.
   *
   * Present exactly when `GET /admin/review/queue` carries this Course. It is deliberately not
   * derived from `summary.lifecycle`: an Instructor revising an already-published Course produces a
   * `PENDING_REVIEW` *revision* while the Course lifecycle stays `PUBLISHED`, so reading the
   * lifecycle would hide that Course from review entirely.
   */
  pendingReview: ReviewQueueItem | null;
};

export type CourseStatusView = {
  state: AdminCourseState;
  tone: StatusTone;
  awaiting: AwaitingActor;
  /** True only when an Admin has a review decision to take on this Course right now. */
  needsReview: boolean;
  action: CourseAction;
  /** Access is suspended, or the Course is retired. Rendered as text, never as colour alone. */
  accessSuspended: boolean;
  retired: boolean;
};

function normalizeState(lifecycle: string): AdminCourseState {
  switch (lifecycle) {
    case "DRAFT":
    case "PENDING_REVIEW":
    case "CHANGES_REQUESTED":
    case "PUBLISHED":
    case "DELISTED":
    case "ARCHIVED":
      return lifecycle;
    default:
      // An unrecognised lifecycle is treated as a draft for presentation only. It is never written
      // back anywhere, so a future server state degrades to "the Instructor still owns this"
      // instead of crashing the directory.
      return "DRAFT";
  }
}

/**
 * Who the product is waiting on, given the Course state and whether a decision is actually pending.
 *
 * The review queue wins over the lifecycle, because a pending decision is an Admin obligation
 * regardless of what the Course itself is currently doing in the catalogue.
 */
function awaitingActor(state: AdminCourseState, needsReview: boolean): AwaitingActor {
  if (needsReview) return "ADMIN";
  switch (state) {
    case "DRAFT":
    case "CHANGES_REQUESTED":
      // The Instructor owns academic identity and submission. Nothing an Admin does moves these on.
      return "INSTRUCTOR";
    case "PENDING_REVIEW":
      // Lifecycle says submitted but the queue does not carry it — the decision already landed, or
      // the queue read failed. Either way this is not an Admin obligation we can assert.
      return "NOBODY";
    default:
      return "NOBODY";
  }
}

function tone(state: AdminCourseState, needsReview: boolean): StatusTone {
  if (needsReview) return "accent";
  switch (state) {
    case "PUBLISHED":
      return "success";
    case "CHANGES_REQUESTED":
      return "default";
    default:
      return "neutral";
  }
}

function action(state: AdminCourseState, needsReview: boolean): CourseAction {
  if (needsReview) return "REVIEW";
  // Everything an Admin can still do to a live or withdrawn Course is a lifecycle command.
  if (state === "PUBLISHED" || state === "DELISTED" || state === "ARCHIVED") return "MANAGE";
  return "VIEW";
}

export function courseStatusView(row: AdminCourseRow): CourseStatusView {
  const state = normalizeState(row.summary.lifecycle);
  const needsReview = row.pendingReview !== null;
  return {
    state,
    tone: tone(state, needsReview),
    awaiting: awaitingActor(state, needsReview),
    needsReview,
    action: action(state, needsReview),
    accessSuspended: Boolean(row.summary.access_suspended_at),
    retired: Boolean(row.summary.retired_at),
  };
}

/**
 * How many Courses one directory read can return.
 *
 * Mirrors `catalog.LifecycleDirectoryLimit`. The server bounds the directory because it is a
 * working surface rather than a catalogue export, which means a full page of results is not
 * necessarily the whole catalogue — and, in particular, that "Needs review" built from this page
 * can under-report if more than this many Courses were updated more recently. The surface says so
 * rather than presenting a capped page as complete.
 */
export const DIRECTORY_PAGE_LIMIT = 50;

/** The filters the directory offers. Each maps onto states the server genuinely serves. */
export type DirectoryFilter =
  | "NEEDS_REVIEW"
  | "DRAFT"
  | "CHANGES_REQUESTED"
  | "PUBLISHED"
  | "WITHDRAWN"
  | "ALL";

export const DIRECTORY_FILTERS: DirectoryFilter[] = [
  "NEEDS_REVIEW",
  "DRAFT",
  "CHANGES_REQUESTED",
  "PUBLISHED",
  "WITHDRAWN",
  "ALL",
];

/**
 * Whether a row belongs under one filter.
 *
 * `NEEDS_REVIEW` is queue membership and nothing else — this is the distinction that keeps a DRAFT
 * Course, which requires no Admin action at all, out of the Admin's work list.
 */
export function matchesFilter(row: AdminCourseRow, filter: DirectoryFilter): boolean {
  const view = courseStatusView(row);
  switch (filter) {
    case "ALL":
      return true;
    case "NEEDS_REVIEW":
      return view.needsReview;
    case "DRAFT":
      return !view.needsReview && view.state === "DRAFT";
    case "CHANGES_REQUESTED":
      return !view.needsReview && view.state === "CHANGES_REQUESTED";
    case "PUBLISHED":
      return view.state === "PUBLISHED";
    case "WITHDRAWN":
      return view.state === "DELISTED" || view.state === "ARCHIVED" || view.retired;
    default:
      return true;
  }
}

export function filterCounts(rows: AdminCourseRow[]): Record<DirectoryFilter, number> {
  const counts = {} as Record<DirectoryFilter, number>;
  for (const filter of DIRECTORY_FILTERS) {
    counts[filter] = rows.filter((row) => matchesFilter(row, filter)).length;
  }
  return counts;
}

/**
 * Joins the lifecycle directory to the review queue.
 *
 * Both reads are the server's; this only pairs them by Course so one row can state both what the
 * Course is and whether a decision is waiting. A queue entry with no matching directory row is
 * dropped rather than synthesised — the directory read is what defines the visible set, and
 * inventing a row would mean rendering a Course whose state we never actually read.
 */
export function buildDirectory(
  summaries: CourseLifecycleSummary[],
  queue: ReviewQueueItem[],
): AdminCourseRow[] {
  const pendingByCourse = new Map<string, ReviewQueueItem>();
  for (const item of queue) {
    const existing = pendingByCourse.get(item.course_id);
    // At most one candidate revision is open per Course, but if the server ever returns more the
    // newest revision number is the one a decision would be taken against.
    if (!existing || item.revision_number > existing.revision_number) {
      pendingByCourse.set(item.course_id, item);
    }
  }
  return summaries.map((summary) => ({
    summary,
    pendingReview: pendingByCourse.get(summary.id) ?? null,
  }));
}

/** The title a human knows the Course by, in the reader's language, with the other as support. */
export function courseTitles(
  summary: CourseLifecycleSummary,
  locale: "ar" | "en",
): { primary: string; secondary: string } {
  const primary = locale === "ar" ? summary.title_ar : summary.title_en;
  const secondary = locale === "ar" ? summary.title_en : summary.title_ar;
  // A revision may legitimately carry only one language filled in so far.
  return { primary: primary || secondary, secondary: primary ? secondary : "" };
}
