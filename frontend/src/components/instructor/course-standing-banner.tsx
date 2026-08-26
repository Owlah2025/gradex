"use client";

import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { StatusBadge } from "@/components/common/status-badge";
import { standingTone, type InstructorCourseStanding } from "./course-standing";

type StandingLabels = Dictionary["instructor"]["standing"];
type BannerLabels = Dictionary["instructor"]["standingBanner"];

/**
 * Where the selected course stands, said in full rather than as a pill.
 *
 * The studio's header carried a single grey pill reading "In review" or "Draft" — the state word
 * and nothing else. It answered "what is this called" and left the three questions an instructor
 * actually has: is anyone waiting on me, can I still edit this, and are students seeing anything.
 *
 * All four answers come from `courseStanding`, so this component decides nothing; it only says out
 * loud what the server's facts already imply. The wire enum stays on a data attribute for support
 * conversations and tests, where it is genuinely useful, and appears nowhere a reader will meet it.
 */
export function CourseStandingBanner({
  standing,
  labels,
  bannerLabels,
}: {
  standing: InstructorCourseStanding;
  labels: StandingLabels;
  bannerLabels: BannerLabels;
}) {
  const stage = labels[standing.stage];

  return (
    <div
      className="rounded-lg border border-border bg-muted/40 p-4"
      data-testid="course-standing"
      data-course-stage={standing.stage}
      data-revision-state={standing.wire}
      data-editable={standing.editable ? "true" : "false"}
    >
      <StatusBadge
        tone={standingTone(standing.stage)}
        label={stage.label}
        detail={labels.actor[standing.actor]}
        labelTestID="revision-state"
        detailTestID="course-standing-actor"
      />
      <p className="mt-2 text-sm leading-6 text-foreground" data-testid="course-standing-meaning">
        {stage.meaning}
      </p>
      <p className="mt-1 text-sm leading-6 text-muted-foreground">
        {standing.editable ? bannerLabels.editingOpen : bannerLabels.editingClosed}
        {/* Said only where it is true: a first-publication draft has nothing live behind it. */}
        {standing.liveForStudents ? ` ${bannerLabels.studentsUnaffected}` : ""}
      </p>
    </div>
  );
}
