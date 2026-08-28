"use client";

import Hls from "hls.js";
import { useEffect, useRef, useState } from "react";

type ReviewLessonPreviewProps = {
  playbackURL: string;
  locale: "ar" | "en";
};

/** Renders only the application-issued protected manifest for an active Admin review. */
export function ReviewLessonPreview({ playbackURL, locale }: ReviewLessonPreviewProps) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [unavailable, setUnavailable] = useState(false);
  const isAr = locale === "ar";

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !playbackURL) return;
    let hls: Hls | null = null;
    setUnavailable(false);

    const mediaFailed = () => setUnavailable(true);
    video.addEventListener("error", mediaFailed);

    if (Hls.isSupported()) {
      hls = new Hls();
      hls.on(Hls.Events.ERROR, (_event, data) => {
        if (data.fatal) setUnavailable(true);
      });
      hls.loadSource(playbackURL);
      hls.attachMedia(video);
    } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = playbackURL;
    } else {
      setUnavailable(true);
    }

    return () => {
      video.removeEventListener("error", mediaFailed);
      hls?.destroy();
      video.removeAttribute("src");
      video.load();
    };
  }, [playbackURL]);

  if (unavailable) {
    return (
      <p role="alert" data-testid="review-preview-unavailable" className="text-sm text-destructive">
        {isAr ? "تعذرت معاينة الفيديو المحمي." : "The protected video preview is unavailable."}
      </p>
    );
  }

  return (
    <video
      ref={videoRef}
      controls
      data-testid="review-protected-video"
      aria-label={isAr ? "معاينة فيديو الدرس" : "Lesson video preview"}
      className="w-full rounded-lg bg-card"
    />
  );
}
