"use client";

import { useEffect, useState } from "react";
import type { PlaybackWatermark } from "@/lib/api/learning";
import { cn } from "@/lib/utils";
import {
  WATERMARK_CLOCK_TICK_MS,
  WATERMARK_FADE_MS,
  WATERMARK_ZONE_CLASSES,
  watermarkClock,
  watermarkDetailLine,
  watermarkHoldMS,
  watermarkSeed,
  watermarkZoneAt,
} from "./video-watermark-model";

/**
 * The Student-specific watermark drawn over protected Lesson video.
 *
 * What it is for: if a Lesson recording appears somewhere it should not, the frames carry the
 * Account that was watching. That is deterrence and attribution, and it is the honest limit of it.
 * It is **not** DRM and it prevents no capture at all — a browser cannot stop an OS screenshot,
 * OBS, a desktop or GPU capture, or a phone pointed at the screen, and nothing in this component
 * pretends otherwise.
 *
 * What it must not become: an obstacle to learning. Gradex is a teaching product, so this is small,
 * faint, and mostly forgotten. It never rests over the centre of the picture, never covers the
 * control bar, moves roughly once a minute rather than continuously, and cannot be clicked,
 * selected, focused, or read out by a screen reader.
 *
 * Every value it draws is decided by the server on the playback authorization. The component takes
 * that object and nothing else — never the signed-in session, never a client-side profile — so
 * there is no client state that could put the wrong Student's identity on the picture.
 */
export function VideoWatermark({ watermark }: { watermark: PlaybackWatermark }) {
  const seed = watermarkSeed(watermark.code);
  const [step, setStep] = useState(0);
  const [fading, setFading] = useState(false);
  /**
   * Read in an effect, never during render.
   *
   * The clock is the one value here that differs between a server pass and a client pass, so it
   * starts absent and is filled in after mount. That is what keeps the first client render
   * identical to the markup it hydrates.
   */
  const [clock, setClock] = useState<string | null>(null);

  /**
   * One hold timer per position, cleared on unmount and re-armed on every move.
   *
   * The move is two beats — fade out, then relocate and fade back in — so the watermark never jumps
   * across the picture in view of the Student. It is three state updates per position, roughly one
   * position every 35-55 seconds, which is far below anything the media element or hls.js notices.
   * Nothing in here touches the video, the source, or the network.
   */
  useEffect(() => {
    let cancelled = false;
    let fadeTimer: ReturnType<typeof setTimeout> | null = null;
    const holdTimer = setTimeout(() => {
      if (cancelled) return;
      setFading(true);
      fadeTimer = setTimeout(() => {
        if (cancelled) return;
        setFading(false);
        setStep((current) => current + 1);
      }, WATERMARK_FADE_MS);
    }, watermarkHoldMS(seed, step));
    return () => {
      cancelled = true;
      clearTimeout(holdTimer);
      if (fadeTimer) clearTimeout(fadeTimer);
    };
  }, [seed, step]);

  // The clock is polled coarsely and the rendered string changes once a minute; an unchanged
  // string is discarded by React without a re-render.
  useEffect(() => {
    const readClock = () => setClock(watermarkClock(new Date()));
    readClock();
    const clockTimer = setInterval(readClock, WATERMARK_CLOCK_TICK_MS);
    return () => clearInterval(clockTimer);
  }, []);

  const zone = watermarkZoneAt(seed, step);

  return (
    <div
      aria-hidden
      data-video-watermark
      className="pointer-events-none absolute inset-0 z-10 select-none overflow-hidden"
    >
      <span
        data-testid="video-watermark"
        data-watermark-zone={zone}
        className={cn(
          "absolute max-w-[46%] select-none text-[10px] font-medium leading-tight text-white sm:text-[12px]",
          // Faint enough to be forgotten, with a soft dark outline so it survives a bright slide
          // as well as a dark one without needing a plate behind it.
          "opacity-[0.16] [text-shadow:0_1px_2px_rgb(0_0_0/0.7)]",
          "transition-opacity ease-out",
          WATERMARK_ZONE_CLASSES[zone],
          fading && "opacity-0",
        )}
        style={{ transitionDuration: `${WATERMARK_FADE_MS}ms` }}
      >
        {watermark.display_name ? <span className="block">{watermark.display_name}</span> : null}
        {/* The address, code and clock are Latin text in both locales, so this line is pinned LTR
            even when the surrounding Lesson reads right-to-left. */}
        <span dir="ltr" className="block tabular-nums">
          {watermarkDetailLine(watermark.masked_identifier, watermark.code, clock)}
        </span>
      </span>
    </div>
  );
}
