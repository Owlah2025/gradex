"use client";

import Link from "next/link";
import { useState } from "react";
import { Layers3 } from "lucide-react";
import type { PublicBundle, PublicBundleMember } from "@/lib/api/public-catalog";
import { ThumbnailImage } from "./thumbnail-image";
import { PriceDisplay } from "./price-display";
import {
  bundleCourseCount,
  bundleDetailHref,
  bundleOverflowLabel,
  bundleStackLayout,
  clampActiveMember,
} from "./bundle-presentation";
import { useLocale } from "@/lib/i18n/locale-provider";

function MemberArtwork({ member }: { member: PublicBundleMember }) {
  return member.thumbnail?.card_url ? (
    <ThumbnailImage src={member.thumbnail.card_url} className="h-full w-full object-cover" />
  ) : (
    <div className="flex h-full w-full items-center justify-center bg-muted">
      <Layers3 className="size-7 text-muted-foreground" aria-hidden />
    </div>
  );
}

export function BundleCard({ bundle, locale }: { bundle: PublicBundle; locale: "ar" | "en" }) {
  const { t } = useLocale();
  // Active member state is owned per card instance, so two Bundles rendered
  // side by side never share a selection. React keys the instances by Bundle id
  // in the lists above, which is what keeps this state attached to the right
  // Bundle across re-orders.
  const [activeMember, setActiveMember] = useState(0);
  const active = clampActiveMember(bundle.members.length, activeMember);
  const layout = bundleStackLayout(bundle.members.length, active);
  const countLabel = bundleCourseCount(bundle.course_count, t.bundles.courseCount);
  const activeCourse = bundle.members[active];

  return (
    <article
      className="flex h-full flex-col rounded-xl border border-border bg-card p-5 text-card-foreground shadow-sm"
      data-testid="bundle-card"
      data-bundle-id={bundle.id}
    >
      {/* The stack itself carries the group semantics. A `display: contents`
          wrapper would be dropped from the accessibility tree in some engines,
          which is exactly where an "it works but screen readers see nothing"
          regression hides. */}
      <div
        className="relative h-32"
        data-testid="bundle-stack"
        data-active-member={String(active)}
        {...(layout.slots.length > 1 ? { role: "group" as const, "aria-label": t.bundles.stackLabel } : {})}
      >
        {layout.slots.length === 0 ? (
          <div className="flex h-28 items-center justify-center rounded-lg bg-muted">
            <Layers3 className="size-8 text-muted-foreground" aria-hidden />
          </div>
        ) : null}

        {/* One Course is one piece of artwork. A stack of one is a lie about
            what the Bundle contains, and it invites a promote interaction that
            has nowhere to go. */}
        {layout.isSingle ? (
          <div className="absolute inset-x-0 top-0 h-28 overflow-hidden rounded-lg bg-muted ring-2 ring-card" data-testid="bundle-stack-single">
            <MemberArtwork member={bundle.members[0]} />
          </div>
        ) : null}

        {!layout.isSingle && layout.slots.length > 0 ? (
          <>
            {layout.slots.map((slot) => {
              const member = bundle.members[slot.index];
              return (
                <button
                  key={member.course_id}
                  type="button"
                  // Selecting is not navigating. Clicking a Course behind the
                  // front one brings it forward and nothing else; the front
                  // Course gets its own explicit link below. Without that split
                  // a background tap is ambiguous, and on touch it reads as an
                  // accidental navigation.
                  onClick={() => setActiveMember(slot.index)}
                  aria-pressed={slot.isActive}
                  data-testid="bundle-stack-member"
                  data-member-index={String(slot.index)}
                  data-active={slot.isActive ? "true" : "false"}
                  data-depth={String(slot.depth)}
                  className="absolute top-0 h-28 overflow-hidden rounded-lg bg-muted ring-2 ring-card transition-[inset-inline-start,transform,box-shadow] duration-300 ease-out focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary motion-reduce:transition-none data-[active=true]:shadow-md"
                  style={{
                    // Every value is derived from the member count and the
                    // active member. No slot table, so 2 members lay out as
                    // deliberately as 4. `inset-inline-start` is logical, so
                    // RTL mirrors the offsets without reversing the data.
                    width: `${slot.widthPercent}%`,
                    insetInlineStart: `${slot.insetInlineStartPercent}%`,
                    zIndex: slot.zIndex,
                    transform: `scale(${slot.scale})`,
                  }}
                >
                  <MemberArtwork member={member} />
                  <span className="sr-only">{member.title}</span>
                </button>
              );
            })}
          </>
        ) : null}
      </div>

      <p className="mt-3 text-sm font-semibold text-muted-foreground">
        {countLabel}
        {layout.overflow > 0 ? (
          <span className="ms-2 rounded-full bg-muted px-2 py-0.5 text-xs" data-testid="bundle-stack-overflow">
            {bundleOverflowLabel(layout.overflow, t.bundles.moreCourses)}
          </span>
        ) : null}
      </p>
      <h3 className="mt-1 text-balance font-display text-xl font-bold text-foreground"><bdi>{bundle.title}</bdi></h3>

      {activeCourse ? (
        <p className="mt-2 text-sm text-muted-foreground" data-testid="bundle-active-member">
          <bdi>{activeCourse.title}</bdi>
        </p>
      ) : null}

      <PriceDisplay price={bundle.price} locale={locale} className="mt-4" compact />

      <div className="mt-5 flex flex-wrap gap-2">
        <Link
          href={bundleDetailHref(locale, bundle.slug || bundle.id)}
          className="inline-flex min-h-11 items-center justify-center rounded-md bg-primary px-4 py-2 text-sm font-bold text-primary-foreground hover:bg-primary/90 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
        >
          {t.bundles.view}
        </Link>
        {activeCourse ? (
          <Link
            href={`/${locale}/catalog/${encodeURIComponent(activeCourse.slug || activeCourse.course_id)}`}
            data-testid="bundle-view-active-course"
            className="inline-flex min-h-11 items-center justify-center rounded-md border border-border px-4 py-2 text-sm font-semibold text-foreground hover:bg-muted focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
          >
            {t.bundles.viewCourse}
          </Link>
        ) : null}
      </div>
    </article>
  );
}
