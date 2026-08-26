import type { OwnedCourseSummary } from "@/lib/api/catalog";
import { isReturnedForChanges } from "./change-request-state";

/**
 * Where one owned Course stands, told the way its Instructor needs to hear it.
 *
 * The studio already knew each Course's wire state; what it did with that knowledge was print it
 * in an amber pill. Amber for a draft, amber for a Course sitting with an Admin, amber for one
 * that came back with a reason — three different obligations wearing the same colour, and the
 * colour was the whole message. Nothing on the surface said who the product was waiting on, so
 * "in review" and "needs my attention" looked alike at a glance, which is exactly the distinction
 * an Instructor opens this screen to make.
 *
 * So a standing is not a label. It is the four things the Instructor is actually asking:
 * what happened, who moves next, can I still edit, and are Students seeing anything right now.
 * The label is one field of four, and every one of them is derived from server facts.
 */
export type InstructorCourseStage =
  /** A first-publication draft: never published, still the Instructor's to build. */
  | "DRAFT"
  /** A draft revision of a Course that is already published and still serving Students. */
  | "DRAFT_UPDATE"
  /** Submitted. The Admin holds it; the Instructor cannot edit until a decision lands. */
  | "IN_REVIEW"
  /** Returned with a reason. The Instructor edits and resubmits. */
  | "CHANGES_REQUESTED"
  /** Published with no open revision. Nothing is required. */
  | "PUBLISHED"
  /** No open revision and nothing published — the studio can offer no action here. */
  | "UNAVAILABLE";

/**
 * Who the product is waiting on.
 *
 * This is the half of a status that survives without colour, and the half the Instructor scans
 * the list for. It is deliberately three values: a Course is either mine to move, theirs to
 * decide, or finished.
 */
export type InstructorCourseActor = "INSTRUCTOR" | "ADMIN" | "NOBODY";

export type InstructorCourseStanding = {
  stage: InstructorCourseStage;
  actor: InstructorCourseActor;
  /** Whether the open revision accepts edits right now. */
  editable: boolean;
  /** Whether a published revision is serving Students while this stands. */
  liveForStudents: boolean;
  /**
   * The wire enum behind the stage, for `data-` attributes and support conversations only.
   *
   * It is never the Instructor's explanation of anything. It is kept because a support request
   * that can name the exact server state is worth far more than one that can only quote a
   * translated sentence back.
   */
  wire: string;
};

const EDITABLE_STATES = new Set(["DRAFT", "CHANGES_REQUESTED", "REJECTED"]);

function hasLiveRevision(course: OwnedCourseSummary): boolean {
  return Boolean(course.live_revision_id) || Boolean(course.live_revision?.id);
}

/**
 * Reads one Course's standing from the server's own facts.
 *
 * The revision state leads, because the revision is what the Instructor is working on; the Course
 * lifecycle is consulted only when there is no open revision to read. That ordering matters for a
 * published Course with a draft update: its lifecycle says `PUBLISHED` and its candidate says
 * `DRAFT`, and the Instructor needs to be told both — that there is unfinished work here, and that
 * Students are meanwhile unaffected.
 */
export function courseStanding(
  course: OwnedCourseSummary | null | undefined,
): InstructorCourseStanding {
  if (!course) {
    return {
      stage: "UNAVAILABLE",
      actor: "NOBODY",
      editable: false,
      liveForStudents: false,
      wire: "",
    };
  }

  const live = hasLiveRevision(course);
  const revision = course.editable_revision;
  const state = revision?.state;

  if (state !== undefined) {
    if (isReturnedForChanges(revision)) {
      return {
        stage: "CHANGES_REQUESTED",
        actor: "INSTRUCTOR",
        editable: true,
        liveForStudents: live,
        wire: state,
      };
    }
    if (state === "PENDING_REVIEW") {
      return {
        stage: "IN_REVIEW",
        actor: "ADMIN",
        editable: false,
        liveForStudents: live,
        wire: state,
      };
    }
    if (EDITABLE_STATES.has(state)) {
      return {
        stage: live ? "DRAFT_UPDATE" : "DRAFT",
        actor: "INSTRUCTOR",
        editable: true,
        liveForStudents: live,
        wire: state,
      };
    }
    // An APPROVED or SUPERSEDED candidate is a revision the Instructor no longer holds. If the
    // Course is published, that is the standing worth reporting; otherwise there is nothing here.
    return {
      stage: live ? "PUBLISHED" : "UNAVAILABLE",
      actor: "NOBODY",
      editable: false,
      liveForStudents: live,
      wire: state,
    };
  }

  if (live) {
    return {
      stage: "PUBLISHED",
      actor: "NOBODY",
      editable: false,
      liveForStudents: true,
      wire: course.lifecycle ?? "PUBLISHED",
    };
  }

  return {
    stage: "UNAVAILABLE",
    actor: "NOBODY",
    editable: false,
    liveForStudents: false,
    wire: course.lifecycle ?? "",
  };
}

/**
 * The Badge tone for a stage.
 *
 * Tone is decoration here, never the message — every surface that renders one of these also
 * renders the `actor` sentence beside it. The mapping stays local to the Instructor because
 * "waiting on an Admin" is a neutral fact for an Instructor and an obligation for an Admin, and a
 * shared enum-to-tone table would have to pick one of those readings for both.
 */
export function standingTone(
  stage: InstructorCourseStage,
): "default" | "accent" | "success" | "neutral" {
  switch (stage) {
    case "PUBLISHED":
      return "success";
    case "CHANGES_REQUESTED":
      return "accent";
    case "IN_REVIEW":
      return "default";
    default:
      return "neutral";
  }
}

/**
 * The title to show for an owned Course.
 *
 * Both authoring surfaces used to fall back to `course.id` when no revision was expanded, which
 * put a bare UUID where a Course name belongs — in the list, in the studio heading, and in the
 * price panel. A Course always has a revision carrying a title in both languages, so the fallback
 * was reachable only through a malformed payload; when that happens the honest answer is to say
 * the title is missing, not to print a database key at someone.
 */
export function courseDisplayTitle(
  course: OwnedCourseSummary,
  locale: "ar" | "en",
  untitled: string,
): string {
  const revision = course.editable_revision ?? course.live_revision;
  const title = locale === "ar" ? revision?.title_ar : revision?.title_en;
  return title?.trim() || untitled;
}
