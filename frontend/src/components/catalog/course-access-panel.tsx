"use client";

import Link from "next/link";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import {
  offersAccessStatus,
  offersCourseEntry,
  type CourseAccessRelationship,
} from "./course-access-relationship";

type Labels = Dictionary["access"];

/**
 * The access section on public Course Details.
 *
 * Its whole job is to answer "what do I do next about this Course" truthfully. Gradex takes no
 * payment, so there is no purchase path here in any state — the only actions that exist are entering
 * a Course the Student already holds, or looking at a record they already have.
 *
 * Showing or hiding the entry link is presentation only. Course Home enforces the entitlement
 * server-side; nothing here is a security boundary.
 */
export function CourseAccessPanel({
  relationship,
  courseID,
  labels,
  locale,
  onRetry,
}: {
  relationship: CourseAccessRelationship;
  courseID: string;
  labels: Labels;
  locale: "ar" | "en";
  onRetry: () => void;
}) {
  const copy = labels.courseDetails;

  const message: Record<CourseAccessRelationship, string> = {
    ANONYMOUS: copy.anonymous,
    NO_ACCESS: copy.noAccess,
    ACTION_REQUIRED: copy.actionRequired,
    AWAITING_APPROVAL: copy.awaitingApproval,
    ACTIVE: copy.active,
    ACCESS_ENDED: copy.accessEnded,
    REJECTED: copy.rejected,
    CANCELLED: copy.cancelled,
    UNKNOWN: copy.noAccess,
    UNAVAILABLE: copy.unavailable,
  };

  return (
    <section
      data-testid="course-access-panel"
      data-access-relationship={relationship}
      aria-labelledby="course-access-heading"
      className="mt-10 rounded-lg border border-border bg-card p-5"
    >
      <h2 id="course-access-heading" className="font-display text-lg font-bold text-foreground">
        {copy.heading}
      </h2>

      <p data-testid="course-access-message" className="mt-2 leading-6 text-muted-foreground">
        {message[relationship]}
      </p>

      {/* How access is obtained, stated once. Never shown where the Student already holds it. */}
      {relationship !== "ACTIVE" && relationship !== "UNAVAILABLE" ? (
        <p data-testid="course-access-how" className="mt-3 leading-6 text-muted-foreground">
          {copy.howItWorks}
        </p>
      ) : null}

      <div className="mt-4 flex flex-wrap gap-3">
        {offersCourseEntry(relationship) ? (
          <Link
            href={`/${locale}/learn/courses/${courseID}`}
            data-testid="course-access-go-to-course"
            className="inline-flex rounded-md border border-border px-4 py-2 font-semibold text-foreground hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          >
            {labels.goToCourse}
          </Link>
        ) : null}

        {offersAccessStatus(relationship) ? (
          <Link
            href={`/${locale}/access`}
            data-testid="course-access-view-status"
            className="inline-flex rounded-md border border-border px-4 py-2 font-semibold text-foreground hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          >
            {copy.viewStatus}
          </Link>
        ) : null}

        {relationship === "ANONYMOUS" ? (
          <Link
            href="/login"
            data-testid="course-access-sign-in"
            className="inline-flex rounded-md border border-border px-4 py-2 font-semibold text-foreground hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          >
            {copy.signIn}
          </Link>
        ) : null}

        {relationship === "UNAVAILABLE" ? (
          <button
            type="button"
            onClick={onRetry}
            data-testid="course-access-retry"
            className="inline-flex rounded-md border border-border px-4 py-2 font-semibold text-foreground hover:bg-accent"
          >
            {copy.retry}
          </button>
        ) : null}
      </div>
    </section>
  );
}
