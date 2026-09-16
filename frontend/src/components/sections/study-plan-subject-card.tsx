"use client";

import { SubjectDemandAction } from "@/components/catalog/subject-demand-action";
import { subjectCopy } from "@/components/catalog/subject-copy";
import {
  CourseCard,
  type CourseCardLabels,
} from "@/components/sections/course-carousel";
import type { SubjectListing } from "@/lib/api/subject-catalogue";
import type { SubjectDemandAudience } from "@/lib/identity/subject-demand-authority";
import { cn } from "@/lib/utils";

type StudyPlanCardCopy = {
  served: string;
  unserved: string;
  viewCourse: string;
};

const artVariants = [
  {
    background: "bg-gx-blue-50",
    ink: "text-gx-blue-700",
    line: "border-gx-blue-200",
  },
  {
    background: "bg-[#f3f0ff]",
    ink: "text-[#514487]",
    line: "border-[#cfc6ee]",
  },
  {
    background: "bg-[#edf7f3]",
    ink: "text-[#315f52]",
    line: "border-[#b9d9ce]",
  },
] as const;

function stableVariant(subject: SubjectListing): number {
  const value = subject.code || subject.value || subject.subject_id;
  let hash = 0;
  for (let index = 0; index < value.length; index += 1) {
    hash = (hash * 31 + value.charCodeAt(index)) >>> 0;
  }
  return hash % artVariants.length;
}

function subjectTitle(subject: SubjectListing, locale: "ar" | "en"): string {
  return locale === "ar" ? subject.title_ar : subject.title_en;
}

export function StudyPlanSubjectCard({
  subject,
  locale,
  copy,
  courseLabels,
  audience,
  requested,
  onDemandChange,
}: {
  subject: SubjectListing;
  locale: "ar" | "en";
  copy: StudyPlanCardCopy;
  courseLabels: CourseCardLabels;
  audience: SubjectDemandAudience;
  requested: boolean;
  onDemandChange: (subjectId: string, requested: boolean) => void;
}) {
  if (subject.served && subject.primary_course) {
    return (
      <CourseCard
        course={subject.primary_course}
        href={`/${locale}/catalog/${encodeURIComponent(subject.primary_course.slug)}`}
        locale={locale}
        labels={courseLabels}
        statusLabel={copy.served}
        actionLabel={copy.viewCourse}
        testID="study-plan-served-course"
      />
    );
  }

  // `served` is never inferred from the projection. The API contract guarantees
  // a projection for every served Subject; the section rejects a broken payload
  // before this component is rendered.
  const art = artVariants[stableVariant(subject)];
  const code = subject.code || "—";

  return (
    <article
      className="flex h-full w-full flex-col overflow-hidden rounded-lg border border-border bg-card text-card-foreground shadow-sm"
      data-testid="study-plan-unserved-subject"
    >
      <div className={cn("relative h-[168px] shrink-0 overflow-hidden sm:h-[180px]", art.background)}>
        <div
          aria-hidden
          className={cn(
            "absolute -end-8 -top-7 size-28 rounded-full border",
            art.line,
          )}
        />
        <div
          aria-hidden
          className={cn(
            "absolute -bottom-10 start-9 size-28 rotate-12 rounded-2xl border",
            art.line,
          )}
        />
        <span
          aria-hidden
          dir="ltr"
          className={cn(
            "absolute inset-x-4 bottom-3 truncate font-display text-[clamp(2.8rem,6vw,4.8rem)] font-black leading-none opacity-[0.12]",
            art.ink,
          )}
        >
          <bdi>{code}</bdi>
        </span>
        {subject.code ? (
          <span className="absolute start-3 top-3 rounded-pill bg-card/90 px-2.5 py-1 font-mono text-xs font-bold text-foreground shadow-sm">
            <bdi dir="ltr">{subject.code}</bdi>
          </span>
        ) : null}
      </div>

      <div className="flex flex-1 flex-col p-4">
        <h3 className="min-h-[2.6em] overflow-hidden text-balance font-display text-[16px] font-bold leading-[1.3] text-foreground [display:-webkit-box] [-webkit-box-orient:vertical] [-webkit-line-clamp:2]">
          <bdi>{subjectTitle(subject, locale)}</bdi>
        </h3>
        <p
          className="mt-2 text-[13px] font-bold text-muted-foreground"
          data-testid="subject-availability"
        >
          {copy.unserved}
        </p>
        <div className="mt-auto pt-5">
          <SubjectDemandAction
            subjectId={subject.subject_id}
            copy={subjectCopy[locale]}
            locale={locale}
            audience={audience}
            initiallyRequested={requested}
            onChange={onDemandChange}
            compact
          />
        </div>
      </div>
    </article>
  );
}
