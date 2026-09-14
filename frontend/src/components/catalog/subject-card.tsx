"use client";

import Link from "next/link";
import { BookOpen } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  institutionName,
  subjectPath,
  subjectTitle,
  type SubjectListing,
} from "@/lib/api/subject-catalogue";
import type { SubjectDemandAudience } from "@/lib/identity/subject-demand-authority";
import { SubjectDemandAction } from "./subject-demand-action";
import type { SubjectCopy } from "./subject-copy";

/**
 * One Subject in the catalogue grid.
 *
 * Deliberately built to the same shape as BundleCard — bordered card, muted
 * eyebrow, display-font title, one full-width action — because a Subject sits
 * beside Courses and Bundles in the same catalogue and must not read as a
 * different product from a different site.
 *
 * What differs is the action, and it differs on exactly one fact: whether a
 * published Course teaches this Subject. Served, the primary action opens the
 * real Course. Unserved, it registers demand. There is no third state and no
 * placeholder Course standing in for the missing one.
 */
export function SubjectCard({
  subject,
  copy,
  locale,
  audience,
  requested,
  onDemandChange,
}: {
  subject: SubjectListing;
  copy: SubjectCopy;
  locale: "ar" | "en";
  audience: SubjectDemandAudience;
  requested: boolean;
  onDemandChange?: (subjectId: string, requested: boolean) => void;
}) {
  const title = subjectTitle(subject, locale);
  // The first published Course is the destination. The list is ordered by the
  // server, so "first" is stable rather than incidental.
  const course = subject.courses[0];

  return (
    <article
      className="flex h-full flex-col rounded-xl border border-border bg-card p-5 text-card-foreground shadow-sm"
      data-testid="subject-card"
      data-served={subject.served ? "true" : "false"}
      data-subject-id={subject.subject_id}
    >
      <div className="flex items-start justify-between gap-3">
        <p className="text-sm font-semibold text-muted-foreground">
          <bdi>{institutionName(subject, locale)}</bdi>
        </p>
        <span
          className={
            subject.served
              ? "shrink-0 rounded-full bg-gx-success-soft px-3 py-1 text-xs font-bold text-gx-navy"
              : "shrink-0 rounded-full bg-muted px-3 py-1 text-xs font-bold text-muted-foreground"
          }
          data-testid="subject-availability"
        >
          {subject.served ? copy.served : copy.unserved}
        </span>
      </div>

      {subject.code ? (
        // The university's own code, which is how a Student recognises the
        // subject. Never the identifier.
        <p className="mt-3 font-mono text-sm font-bold text-foreground">
          <bdi>{subject.code}</bdi>
        </p>
      ) : (
        <BookOpen className="mt-3 size-5 text-muted-foreground" aria-hidden />
      )}

      <h3 className="mt-1 text-balance font-display text-lg font-bold text-foreground">
        <Link
          href={subjectPath(subject, locale)}
          className="hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
          data-testid="subject-card-link"
        >
          <bdi>{title}</bdi>
        </Link>
      </h3>

      <div className="mt-auto pt-5">
        {subject.served && course ? (
          <Button asChild className="w-full" data-testid="subject-open-course">
            <Link href={`/${locale}/catalog/${encodeURIComponent(course.slug)}`}>
              {copy.openCourse}
            </Link>
          </Button>
        ) : (
          <SubjectDemandAction
            subjectId={subject.subject_id}
            copy={copy}
            locale={locale}
            audience={audience}
            initiallyRequested={requested}
            onChange={onDemandChange}
            compact
          />
        )}
      </div>
    </article>
  );
}
