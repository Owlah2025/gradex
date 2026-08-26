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
 * One Course as the Admin directory understands it.
 *
 * The fields are normalised rather than holding a raw directory row, because a Course can reach
 * this surface from either of two server reads. The lifecycle directory is bounded
 * (`DIRECTORY_PAGE_LIMIT`); the review queue is not, and it is the authority on pending decisions.
 * A Course awaiting review that falls outside the directory's page still has to appear, so it is
 * built from its queue entry instead.
 *
 * Nothing here is synthesised. A queue entry carries the Course's titles and its
 * `course_lifecycle`, so a queue-only row states read state from a different endpoint, not invented
 * state. The two fields the queue genuinely does not carry — the owner's display name and the last
 * update stamp — are left absent and omitted from the render rather than filled with a placeholder
 * that would read as data.
 */
export type AdminCourseRow = {
  id: string;
  titleAr: string;
  titleEn: string;
  /** Empty when this Course is known only from the review queue, which carries no owner name. */
  ownerDisplayName: string;
  lifecycle: string;
  /** Absent when this Course is known only from the review queue, which carries no update stamp. */
  updatedAt: string | null;
  accessSuspendedAt?: string;
  retiredAt?: string;
  /**
   * The submitted revision awaiting a decision.
   *
   * Present exactly when `GET /admin/review/queue` carries this Course. It is deliberately not
   * derived from the lifecycle: an Instructor revising an already-published Course produces a
   * `PENDING_REVIEW` *revision* while the Course lifecycle stays `PUBLISHED`, so reading the
   * lifecycle would hide that Course from review entirely.
   */
  pendingReview: ReviewQueueItem | null;
  /**
   * True when this Course was outside the bounded directory page and is shown from its queue entry.
   * The row then knows less about the Course, and says less, rather than guessing the rest.
   */
  fromQueueOnly: boolean;
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
  const state = normalizeState(row.lifecycle);
  const needsReview = row.pendingReview !== null;
  return {
    state,
    tone: tone(state, needsReview),
    awaiting: awaitingActor(state, needsReview),
    needsReview,
    action: action(state, needsReview),
    accessSuspended: Boolean(row.accessSuspendedAt),
    retired: Boolean(row.retiredAt),
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
 * Combines the two server reads into one set of rows.
 *
 * The lifecycle directory is bounded by `DIRECTORY_PAGE_LIMIT` and ordered by recency, so it is a
 * page of the catalogue rather than all of it. The review queue is the server's complete set of
 * pending decisions and is not bounded by that page.
 *
 * "Needs review" must therefore never be a subset of the directory page. Every queue entry produces
 * a row: joined onto its directory row when the page happens to contain it, and built from the
 * queue entry itself when it does not. Otherwise a Course awaiting a decision would silently vanish
 * from the Admin's work list as soon as fifty other Courses were touched more recently — the queue
 * would still hold it, and nothing on screen would say so.
 *
 * Directory order is preserved, and queue-only Courses are appended, so browsing stays ordered by
 * recency while the actionable set stays complete.
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

  const rows: AdminCourseRow[] = summaries.map((summary) => ({
    id: summary.id,
    titleAr: summary.title_ar,
    titleEn: summary.title_en,
    ownerDisplayName: summary.owner_display_name,
    lifecycle: summary.lifecycle,
    updatedAt: summary.updated_at,
    accessSuspendedAt: summary.access_suspended_at,
    retiredAt: summary.retired_at,
    pendingReview: pendingByCourse.get(summary.id) ?? null,
    fromQueueOnly: false,
  }));

  const listed = new Set(summaries.map((summary) => summary.id));
  for (const item of pendingByCourse.values()) {
    if (listed.has(item.course_id)) continue;
    rows.push({
      id: item.course_id,
      titleAr: item.title_ar,
      titleEn: item.title_en,
      // The queue carries no owner name and no update stamp. Both stay absent.
      ownerDisplayName: "",
      lifecycle: item.course_lifecycle,
      updatedAt: null,
      pendingReview: item,
      fromQueueOnly: true,
    });
  }

  return rows;
}

/** The title a human knows the Course by, in the reader's language, with the other as support. */
export function courseTitles(
  row: Pick<AdminCourseRow, "titleAr" | "titleEn">,
  locale: "ar" | "en",
): { primary: string; secondary: string } {
  const primary = locale === "ar" ? row.titleAr : row.titleEn;
  const secondary = locale === "ar" ? row.titleEn : row.titleAr;
  // A revision may legitimately carry only one language filled in so far.
  return { primary: primary || secondary, secondary: primary ? secondary : "" };
}
