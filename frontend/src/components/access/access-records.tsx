"use client";

import Link from "next/link";
import type { StudentCourseAccessHistoryItem } from "@/lib/api/access";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { formatLearningExpiry } from "@/lib/formatters/learning";
import { canOpenCourse, rejectionReason, studentAccessState } from "./access-state";

type AccessLabels = Dictionary["access"];

/**
 * One Course-access record as the Student reads it.
 *
 * The Course title is the subject. The Course id stays in a data attribute so tests and support can
 * still identify the row, but it is never product-visible copy — neither is the wire state, which is
 * translated through the dictionary before it reaches the page.
 */
export function AccessRecord({
  item,
  labels,
  locale,
}: {
  item: StudentCourseAccessHistoryItem;
  labels: AccessLabels;
  locale: "ar" | "en";
}) {
  const state = studentAccessState(item);
  const copy = labels.state[state];
  const reason = rejectionReason(item);
  const expiry = item.access_ends_at ? formatLearningExpiry(item.access_ends_at, locale) : null;

  return (
    <article
      data-testid={`access-record-${item.course_id}`}
      data-access-state={state}
      className="rounded-lg border border-border bg-card p-5"
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <h2 className="font-display text-lg font-bold text-foreground">
          {item.course_title}
        </h2>
        {/* State is carried by text, not by colour alone. */}
        <span
          data-testid={`access-state-${item.course_id}`}
          className="rounded-pill border border-border px-3 py-1 text-sm font-semibold text-foreground"
        >
          {copy.label}
        </span>
      </div>

      <p className="mt-2 leading-6 text-muted-foreground">{copy.body}</p>

      {reason ? (
        <p className="mt-3 text-sm text-foreground">
          <span className="font-semibold">{labels.reasonLabel}:</span> {reason}
        </p>
      ) : null}

      {canOpenCourse(state) && expiry ? (
        <p className="mt-3 text-sm text-muted-foreground">
          {labels.accessUntil}: <time dateTime={expiry.dateTime}>{expiry.text}</time>
        </p>
      ) : null}

      {canOpenCourse(state) ? (
        <Link
          href={`/${locale}/learn/courses/${item.course_id}`}
          data-testid={`go-to-course-${item.course_id}`}
          className="mt-4 inline-flex rounded-md border border-border px-4 py-2 font-semibold text-foreground hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          {labels.goToCourse}
        </Link>
      ) : null}
    </article>
  );
}
