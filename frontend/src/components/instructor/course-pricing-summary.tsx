"use client";

import { useLocale } from "@/lib/i18n/locale-provider";
import { formatFils } from "@/lib/formatters/currency";
import type { CourseWire } from "@/lib/api/authoring";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { WorkspaceSection } from "@/components/layout/workspace-page";

/**
 * What this course costs, and whose decision that is.
 *
 * The panel this replaces opened the studio with the heading "Official Server Prices (Read-only
 * Server State)" and a control labelled "Refresh Server Reads" — a screen written in the language
 * of whoever built the endpoint. It re-fetched the whole owned-Course list on mount and offered a
 * second course selector beside the one that already existed, so the Instructor's first sight of
 * their own studio was two lists of the same courses where only one of them opened anything.
 *
 * The price itself is not the Instructor's to set. `LaunchCourse` is an Admin command, and the
 * submission validator never reads a price — an Instructor can and must submit without one. The
 * old panel said none of that. It presented a price as a bare read-only figure, which invites
 * exactly the wrong conclusion: that a price is something the Instructor is waiting on before they
 * can submit.
 *
 * So this states the ownership in a sentence, and shows the figure as what it is — a fact about
 * the course, rendered in body ink. It was previously `text-emerald-600`, which measured 3.77:1 on
 * white and 3.60:1 on the panel's own slate background: under AA in both places, to decorate a
 * number that was never a success state.
 */
export function CoursePricingSummary({
  course,
  labels,
}: {
  course: CourseWire;
  labels: Dictionary["instructor"]["price"];
}) {
  const { locale } = useLocale();
  const revision = course.editable_revision ?? course.live_revision;
  const sections = (revision?.sections ?? []).filter(
    (section) => section.price_minor_units !== null && section.price_minor_units !== undefined,
  );
  const priced = course.price_minor_units !== null && course.price_minor_units !== undefined;

  return (
    <WorkspaceSection
      title={labels.heading}
      description={labels.adminOwned}
      headingLevel="h3"
      testID="course-pricing-summary"
    >
      {/*
        A `<dl>` may only contain `dt`, `dd`, and `div` wrapping a pair. The note and the
        section list were sitting directly inside one, which axe reports as `definition-list`,
        and left the section rows' own `dt`/`dd` orphaned. Each list now holds only pairs.
      */}
      <div className="rounded-lg border border-border bg-card p-4">
        <dl>
          <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
            <dt className="text-sm text-muted-foreground">{labels.courseLabel}</dt>
            <dd
              className="font-display text-base font-bold text-foreground"
              data-testid="course-price-value"
            >
              {/* Arabic copy beside Latin numerals: isolated so the dinar suffix does not jump. */}
              <bdi>{formatFils(course.price_minor_units, locale)}</bdi>
            </dd>
          </div>
        </dl>
        {!priced ? (
          <p className="mt-1 text-xs text-muted-foreground" data-testid="course-price-unset">
            {labels.unset}
          </p>
        ) : null}

        {sections.length > 0 ? (
          <div className="mt-4 border-t border-border pt-3">
            <p className="font-display text-xs font-bold uppercase tracking-wide text-muted-foreground">
              {labels.sectionsLabel}
            </p>
            <dl className="mt-2 space-y-1.5">
              {sections.map((section) => (
                <div
                  key={section.id}
                  className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-0.5 text-sm"
                  /* Not `section-…`: the curriculum owns that prefix, and specs scan it to
                     find an authored section. A price row is not one. */
                  data-testid={`price-for-section-${section.id}`}
                >
                  <dt className="min-w-0 text-muted-foreground">
                    <bdi>{locale === "ar" ? section.title_ar : section.title_en}</bdi>
                  </dt>
                  <dd className="font-semibold text-foreground">
                    <bdi>{formatFils(section.price_minor_units, locale)}</bdi>
                  </dd>
                </div>
              ))}
            </dl>
          </div>
        ) : null}
      </div>
    </WorkspaceSection>
  );
}
