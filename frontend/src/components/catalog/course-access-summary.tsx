"use client";

import { useEffect, useState } from "react";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import type { PublicCourseDetail } from "@/lib/api/public-catalog";
import { Button } from "@/components/ui/button";
import { LoadingState } from "@/components/common/loading-state";
import { formatFils } from "@/lib/formatters/currency";
import { CourseAccessPanel } from "./course-access-panel";
import {
  courseAccessRelationship,
  type AccessLookup,
} from "./course-access-relationship";
import { PurchaseAction } from "./purchase-action";

/** The id the mobile bar scrolls to, and the anchor the access column owns. */
export const ACCESS_SECTION_ID = "course-access";

/**
 * The states in which a visitor still has to obtain access, and so is offered the request handoff.
 *
 * Held here rather than inline so the mobile bar and the panel cannot disagree about whether there
 * is anything to act on.
 */
const AWAITING_ACCESS = [
  "ANONYMOUS",
  "NO_ACCESS",
  "ACCESS_ENDED",
  "REJECTED",
  "CANCELLED",
] as const;

/**
 * "What does this cost, and what do I do next" — answered in one place.
 *
 * Gradex has no checkout. There is no cart, no gateway, no coupon field and no card form anywhere in
 * this component, because none of those exists in the product: access begins with an administrator
 * invitation, and the one approved handoff is the manual request that records an email address and
 * continues the conversation off-platform. The CTA therefore describes the real path rather than a
 * marketplace-shaped imitation of one.
 *
 * The price is labelled as guidance for the same reason — it is what the course is listed at, not a
 * figure the visitor is about to be charged by this page.
 */
export function CourseAccessSummary({
  course,
  lookup,
  copy,
  accessLabels,
  priceLabel,
  loadingLabel,
  locale,
  onRetry,
}: {
  course: PublicCourseDetail;
  /** `null` until the Student's own access records have settled. */
  lookup: AccessLookup | null;
  copy: Dictionary["courseDetail"];
  accessLabels: Dictionary["access"];
  /** The catalogue's own price wording, shared with the list. */
  priceLabel: string;
  loadingLabel: string;
  locale: "ar" | "en";
  onRetry: () => void;
}) {
  const relationship = lookup ? courseAccessRelationship(lookup, course.id) : null;

  return (
    <aside
      id={ACCESS_SECTION_ID}
      aria-label={copy.accessRegion}
      data-testid="course-access-summary"
      className="mt-8 scroll-mt-24 lg:sticky lg:top-20 lg:col-start-2 lg:row-span-2 lg:row-start-1 lg:mt-0 lg:max-h-[calc(100vh-6rem)] lg:self-start lg:overflow-y-auto"
    >
      <div className="rounded-lg border border-border bg-card p-5 shadow-sm">
        {course.price ? (
          <div data-testid="course-access-price">
            <p className="text-sm text-muted-foreground">{priceLabel}</p>
            <p className="mt-1 font-display text-[26px] font-extrabold leading-none text-foreground">
              <bdi>{formatFils(course.price.minor_units, locale)}</bdi>
            </p>
          </div>
        ) : null}

        {/* Rendered only once the access lookup has settled, so the state shown is never a guess. */}
        {relationship === null ? (
          <LoadingState
            label={loadingLabel}
            className={course.price ? "mt-4 py-0" : "py-0"}
            testID="course-access-loading"
          />
        ) : (
          <>
            <CourseAccessPanel
              relationship={relationship}
              courseID={course.id}
              labels={accessLabels}
              locale={locale}
              onRetry={onRetry}
              className={course.price ? "mt-6 border-t border-border pt-5" : ""}
            />
            {(AWAITING_ACCESS as readonly string[]).includes(relationship) ? (
              <PurchaseAction
                courseId={course.id}
                courseTitle={course.title}
                priceMinorUnits={course.price ? course.price.minor_units : null}
                locale={locale}
                labels={accessLabels.purchase}
                // ANONYMOUS is the one awaiting-access state with no session,
                // and it is the state that must lead into the auth journey
                // rather than into a confirmation.
                authenticated={relationship !== "ANONYMOUS"}
                className="mt-5 w-full"
              />
            ) : null}
          </>
        )}
      </div>
    </aside>
  );
}

/**
 * A way back to the access card once it has scrolled away on a phone.
 *
 * It is a link to the section, not a second copy of the CTA. Duplicating the real action would put
 * two controls on the page that must be kept in step with the same access state, and the state is
 * the part that is easy to get wrong; a jump link cannot drift.
 *
 * It hides itself whenever the access card or the footer is on screen, so it never covers the
 * content it points at and never sits on top of the page's own ending.
 */
export function MobileAccessBar({
  label,
  priceLabel,
  price,
  locale,
}: {
  label: string;
  priceLabel: string;
  price: PublicCourseDetail["price"];
  locale: "ar" | "en";
}) {
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    const watched = [
      document.getElementById(ACCESS_SECTION_ID),
      document.querySelector("footer"),
    ].filter((element): element is HTMLElement => element !== null);
    if (watched.length === 0) return;

    const onScreen = new Map<Element, boolean>();
    const observer = new IntersectionObserver((entries) => {
      for (const entry of entries) onScreen.set(entry.target, entry.isIntersecting);
      setVisible(![...onScreen.values()].some(Boolean));
    });
    for (const element of watched) observer.observe(element);
    return () => observer.disconnect();
  }, []);

  if (!visible) return null;

  return (
    <div
      data-testid="course-access-mobile-bar"
      className="fixed inset-x-0 bottom-0 z-40 border-t border-border bg-background/95 px-5 py-3 pb-[calc(0.75rem+env(safe-area-inset-bottom))] backdrop-blur-md lg:hidden"
    >
      <div className="mx-auto flex max-w-container items-center justify-between gap-4">
        {price ? (
          <p className="min-w-0">
            <span className="block text-xs text-muted-foreground">{priceLabel}</span>
            <span className="font-display text-[17px] font-bold text-foreground">
              <bdi>{formatFils(price.minor_units, locale)}</bdi>
            </span>
          </p>
        ) : (
          <span />
        )}
        <Button asChild size="sm">
          <a href={`#${ACCESS_SECTION_ID}`} data-testid="course-access-mobile-jump">
            {label}
          </a>
        </Button>
      </div>
    </div>
  );
}
