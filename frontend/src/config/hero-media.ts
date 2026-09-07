/**
 * The hero's educational video, if one has been published.
 *
 * The landing page is built for a real video: a muted, looping, silent clip of a Gradex lesson
 * actually being studied. Until that clip exists as a Gradex-owned asset, this is empty and the
 * hero renders its own in-brand composition instead — which is the honest state, because the one
 * thing worse than no video on a page selling university courses is somebody else's video.
 *
 * To turn it on, put the encoded files under `public/media/` and name them here. Nothing else
 * changes: `HeroMedia` already carries the poster, the muted/loop/playsInline contract, the
 * reduced-motion behaviour and the mask that dissolves it into the hero band.
 *
 *   sources: [
 *     { src: "/media/hero-lesson.webm", type: "video/webm" },
 *     { src: "/media/hero-lesson.mp4", type: "video/mp4" },
 *   ],
 *   poster: "/media/hero-lesson-poster.jpg",
 *
 * Order matters: the browser takes the first type it can play, so the smaller modern encode is
 * listed first and the H.264 fallback last. Keep the clip short, silent, and under a couple of
 * megabytes — it is decorative, and it loads on the busiest page in the product.
 */

export type HeroMediaSource = {
  src: string;
  /** A full MIME type, ideally with codecs, so the browser can choose without downloading. */
  type: string;
};

export const heroMedia: {
  sources: readonly HeroMediaSource[];
  /** Shown before playback and wherever motion is refused. Required once `sources` is non-empty. */
  poster: string | null;
} = {
  /**
   * 1280x720 H.264, ten seconds, 467 KB, no audio track, `moov` ahead of `mdat` so it starts
   * streaming on the first bytes rather than after the whole file.
   *
   * No WebM alongside it yet. That is an optimisation rather than a requirement — every browser
   * this product supports decodes H.264 — and adding one needs an encoder that is not available
   * here. If it is added later it belongs *above* the MP4: the browser takes the first type it can
   * play, so the smaller modern encode has to be offered first.
   */
  sources: [{ src: "/media/hero-lesson.mp4", type: "video/mp4" }],
  poster: "/media/hero-lesson-poster.jpg",
};
