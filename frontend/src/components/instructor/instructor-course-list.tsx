"use client";

import { useLocale } from "@/lib/i18n/locale-provider";
import type { CourseWire } from "@/lib/api/authoring";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { StatusBadge } from "@/components/common/status-badge";
import { EmptyState } from "@/components/common/empty-state";
import { LoadingState, SkeletonBlock } from "@/components/common/loading-state";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { academicIdentity, academicIdentitySummary } from "./academic-identity";
import { courseDisplayTitle, courseStanding, standingTone } from "./course-standing";

type CoursesLabels = Dictionary["instructor"]["courses"];
type StandingLabels = Dictionary["instructor"]["standing"];

/**
 * The Instructor's own course directory.
 *
 * This is the screen an Instructor lands on, and until now it answered one question — which
 * courses exist — with a title, a two-line description excerpt, and an amber pill. The
 * description was the part it spent the most room on and the part nobody reads about their own
 * course; the pill was the part that mattered and it said the same thing in the same colour
 * whether the course was waiting on the Instructor or on an Admin.
 *
 * What a row carries now is the set of facts the server actually has: what the course is called,
 * what it is taught for, where it stands, and who moves next. There is no "last updated" line,
 * because the owned-Course payload does not carry a timestamp and a fabricated one is worse than
 * none.
 */
export function InstructorCourseList({
  courses,
  selectedCourseID,
  loading,
  onSelect,
  onCreate,
  labels,
  standingLabels,
}: {
  courses: CourseWire[];
  selectedCourseID: string | null;
  loading: boolean;
  onSelect: (courseID: string) => void;
  onCreate: () => void;
  labels: CoursesLabels;
  standingLabels: StandingLabels;
}) {
  const { locale } = useLocale();

  if (loading) {
    return (
      <div data-testid="owned-course-list">
        <LoadingState label={labels.loading} visuallyHidden />
        <SkeletonBlock rows={3} />
      </div>
    );
  }

  if (courses.length === 0) {
    return (
      <div data-testid="owned-course-list">
        <EmptyState
          density="compact"
          title={labels.emptyTitle}
          description={labels.emptyBody}
          action={
            <Button type="button" size="sm" onClick={onCreate} data-testid="empty-create-course">
              {labels.emptyAction}
            </Button>
          }
        />
      </div>
    );
  }

  return (
    <ul className="space-y-2" data-testid="owned-course-list">
      {courses.map((course) => {
        const standing = courseStanding(course);
        const stage = standingLabels[standing.stage];
        const title = courseDisplayTitle(course, locale, labels.untitled);
        const academic = academicIdentitySummary(academicIdentity(course, locale));
        const selected = selectedCourseID === course.id;

        return (
          <li key={course.id}>
            <button
              type="button"
              onClick={() => onSelect(course.id)}
              aria-current={selected ? "true" : undefined}
              data-testid={`owned-course-${course.id}`}
              data-course-stage={standing.stage}
              data-revision-state={standing.wire}
              className={cn(
                "w-full rounded-lg border p-4 text-start transition-colors",
                "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
                selected
                  ? "border-primary bg-gx-blue-50"
                  : "border-border bg-card hover:border-primary/40",
              )}
            >
              <h3 className="font-display text-sm font-bold leading-snug text-foreground">
                {/* The title may be Arabic beside a Latin subject code, or the reverse. */}
                <bdi>{title}</bdi>
              </h3>

              <p
                className="mt-1 text-xs text-muted-foreground"
                data-testid={`owned-course-academic-${course.id}`}
              >
                <bdi>{academic || labels.academicUnset}</bdi>
              </p>

              <StatusBadge
                className="mt-3"
                size="sm"
                tone={standingTone(standing.stage)}
                label={stage.label}
                detail={standingLabels.actor[standing.actor]}
                labelTestID={`owned-course-standing-${course.id}`}
                detailTestID={`owned-course-actor-${course.id}`}
              />

              {/* The affordance, not a second button: selecting the row *is* opening the course,
                  and a nested control inside a button is not operable anyway. */}
              <p className="mt-2 text-xs font-semibold text-primary">{stage.action}</p>
            </button>
          </li>
        );
      })}
    </ul>
  );
}
