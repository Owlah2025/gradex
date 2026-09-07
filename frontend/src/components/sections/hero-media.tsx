"use client";

import * as React from "react";
import { BirdMark } from "@/components/brand/bird-mark";
import { heroMedia } from "@/config/hero-media";
import { cn } from "@/lib/utils";

/**
 * The hero's visual half: a Gradex lesson, playing.
 *
 * ## Video, and what stands in for it
 *
 * When `config/hero-media` names sources this renders a real `<video>` — muted, looping,
 * `playsInline`, `preload="metadata"`, poster first. It is decorative, so it is `aria-hidden`,
 * removed from the tab order, and stripped of controls: a screen reader gets the headline beside
 * it, which is where the meaning actually is.
 *
 * When it names none, this renders a composition built from the design tokens instead of an empty
 * box. That is the state the product ships in today, and it is deliberate — the alternative to a
 * Gradex-owned clip is somebody else's footage on the page that sells Gradex's own courses.
 *
 * ## Reduced motion
 *
 * A looping clip behind a headline is exactly the ambient movement `prefers-reduced-motion` exists
 * to stop, so autoplay is decided from the media query rather than left to the `autoplay` attribute
 * — the poster stays, the loop does not start, and nothing on the page depends on it having run.
 */
export function HeroMedia({ className }: { className?: string }) {
  const hasVideo = heroMedia.sources.length > 0;

  return (
    <div
      aria-hidden
      className={cn(
        "relative h-full w-full overflow-hidden rounded-lg lg:rounded-none",
        className,
      )}
    >
      {hasVideo ? <HeroVideo /> : <HeroLessonComposition />}
    </div>
  );
}

function HeroVideo() {
  const ref = React.useRef<HTMLVideoElement>(null);

  React.useEffect(() => {
    const video = ref.current;
    if (!video) return;
    const query = window.matchMedia("(prefers-reduced-motion: reduce)");

    function apply() {
      const video = ref.current;
      if (!video) return;
      if (query.matches) {
        video.pause();
        // Back to the poster frame, so what is left is a still image rather than an arbitrary
        // paused frame from wherever the clip happened to be.
        video.currentTime = 0;
        return;
      }
      // A rejected play() is not a failure worth surfacing: the poster is already on screen and the
      // hero reads identically without the loop.
      void video.play().catch(() => {});
    }

    apply();
    query.addEventListener("change", apply);
    return () => query.removeEventListener("change", apply);
  }, []);

  return (
    <video
      ref={ref}
      className="size-full object-cover"
      poster={heroMedia.poster ?? undefined}
      muted
      loop
      playsInline
      preload="metadata"
      disablePictureInPicture
      tabIndex={-1}
    >
      {heroMedia.sources.map((source) => (
        <source key={source.src} src={source.src} type={source.type} />
      ))}
    </video>
  );
}

/**
 * A lesson as Gradex renders one: the player and its outline, in one frame.
 *
 * This was three separately positioned pieces — a white player card, a floating outline panel and a
 * code island — each with its own hard edge, inset from the container on every side. Two things went
 * wrong with that, and both are only visible in a screenshot. The pieces read as a card collage
 * beside the headline rather than as media belonging to the hero, which is exactly what the brief
 * rules out. And the fade could not do its job: the mask runs across the *container*, so a card
 * inset from the container's edges keeps its own hard border inside the region the mask has already
 * finished fading, and in Arabic the code island landed in the fully opaque zone and sat stranded in
 * open navy.
 *
 * One surface fixes both. It fills the container and bleeds off the outer edge, so the only edge the
 * reader sees is the one the mask dissolves into the hero — and the outline lives inside the frame,
 * where a course player's outline actually lives.
 */
function HeroLessonComposition() {
  return (
    <div className="relative size-full min-h-[300px] sm:min-h-[380px] lg:min-h-0">
      {/* The frame starts at the container's own inline-start edge and bleeds past the far one.
          Both halves of that matter: the near edge has to sit where the mask has already faded to
          nothing, or it draws a hard vertical line down the hero, and the far edge has to run off
          the viewport so the frame reads as cropped media rather than as a card. In flow below `lg`
          it simply fills its box. */}
      <div className="absolute inset-y-0 start-0 end-0 flex flex-col gap-2.5 rounded-lg border border-white/10 bg-[#0e1a2b] p-3 shadow-lg lg:inset-y-[12%] lg:end-[-10%]">
        {/* Player. A share of the frame rather than a fixed ratio, so the outline below it keeps
            room to read at every hero height instead of being squeezed to a sliver. */}
        <div className="relative h-[58%] shrink-0 overflow-hidden rounded-md bg-gradient-brand max-lg:aspect-video max-lg:h-auto">
          <div className="absolute inset-0 bg-[radial-gradient(70%_70%_at_30%_20%,rgba(255,255,255,0.20),transparent_70%)]" />
          <BirdMark className="absolute bottom-3 start-3 size-9 text-white/80" />
          <span className="absolute inset-0 m-auto flex size-14 items-center justify-center rounded-pill bg-white/90 shadow-md">
            <svg viewBox="0 0 16 18" className="size-5 text-gx-blue-600 rtl:-scale-x-100">
              <path
                d="M1 1.6a1 1 0 0 1 1.53-.85l12 7.4a1 1 0 0 1 0 1.7l-12 7.4A1 1 0 0 1 1 16.4Z"
                fill="currentColor"
              />
            </svg>
          </span>
          <span className="absolute inset-x-3 bottom-2.5 h-1 rounded-pill bg-white/25">
            <span className="block h-full w-2/5 rounded-pill bg-gx-orange" />
          </span>
        </div>

        {/* Outline — what makes it a course rather than a video. */}
        <ul className="flex min-h-0 flex-1 flex-col justify-around overflow-hidden py-1">
          {[72, 54, 63, 45].map((width, index) => (
            <li key={width} className="flex items-center gap-3">
              <span
                className={cn(
                  "size-2 shrink-0 rounded-pill",
                  index === 1 ? "bg-gx-orange" : "bg-white/25",
                )}
              />
              <span
                className={cn(
                  "h-2.5 rounded-pill",
                  index === 1 ? "bg-white/70" : "bg-white/[0.18]",
                )}
                style={{ width: `${width}%` }}
              />
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
