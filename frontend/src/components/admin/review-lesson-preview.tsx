"use client";

import { ProtectedHLSPlayer } from "@/components/media/protected-hls-player";

type ReviewLessonPreviewProps = {
  playbackURL: string;
  locale: "ar" | "en";
};

/**
 * Renders only the application-issued protected manifest for an active Admin review.
 *
 * The HLS attachment, fatal-error handling and teardown live in ProtectedHLSPlayer, shared with the
 * anonymous Lesson preview player. The behaviour here is unchanged — same test ids, same alert
 * copy, same accessible name — so the Admin review expectations still describe this component.
 */
export function ReviewLessonPreview({ playbackURL, locale }: ReviewLessonPreviewProps) {
  const isAr = locale === "ar";
  return (
    <ProtectedHLSPlayer
      manifestURL={playbackURL}
      label={isAr ? "معاينة فيديو الدرس" : "Lesson video preview"}
      unavailableLabel={
        isAr ? "تعذرت معاينة الفيديو المحمي." : "The protected video preview is unavailable."
      }
      testID="review-protected-video"
      unavailableTestID="review-preview-unavailable"
    />
  );
}
