"use client";
import type { CourseRevisionWire } from "@/lib/api/catalog";
import { revisionThumbnailURL } from "@/lib/api/course-thumbnail";
import { useLocale } from "@/lib/i18n/locale-provider";
import { ThumbnailImage } from "@/components/catalog/thumbnail-image";

export function ReviewThumbnails({ courseID, candidate, live }: { courseID: string; candidate: CourseRevisionWire; live?: CourseRevisionWire }) {
  const { t: dictionary } = useLocale();
  const t = dictionary.courseThumbnail;
  const changed = (live?.thumbnail_asset_version_id ?? null) !== (candidate.thumbnail_asset_version_id ?? null);
  return <section data-testid="review-thumbnails" className="space-y-3">
    <h3 className="font-display text-base font-bold">{t.title}{changed ? ` · ${t.changed}` : ""}</h3>
    <div className="flex flex-wrap gap-6">
      {(live ? [{ revision: live, label: t.live }, { revision: candidate, label: t.candidate }] : [{ revision: candidate, label: t.candidate }]).map(({ revision, label }) =>
        <figure key={label} className="w-full max-w-[340px] space-y-2">
          <figcaption className="text-sm font-semibold">{label}</figcaption>
          <div className="relative flex h-[168px] items-center justify-center overflow-hidden rounded-lg bg-muted sm:h-[180px]">
            <p className="p-4 text-center text-sm text-muted-foreground">{revision.thumbnail_asset_version_id ? t.imageUnavailable : t.empty}</p>
            {revision.id && revision.thumbnail_asset_version_id ? <ThumbnailImage src={revisionThumbnailURL(courseID, revision.id, revision.thumbnail_asset_version_id, true)} className="absolute inset-0 size-full object-cover" /> : null}
          </div>
        </figure>)}
    </div>
  </section>;
}
