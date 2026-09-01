"use client";

import {
  Maximize,
  Minimize,
  Pause,
  Play,
  RotateCcw,
  RotateCw,
  Volume1,
  Volume2,
  VolumeX,
} from "lucide-react";
import type { FocusEvent, ReactNode } from "react";
import { cn } from "@/lib/utils";
import {
  formatMediaTime,
  PLAYBACK_RATES,
  playbackRateLabel,
  playbackRateSelectValue,
  SEEK_STEP_SECONDS,
} from "./player-controls-model";
import { qualitySelectValue, qualityValueText, type QualityState } from "./quality-state";

export type PlayerControlLabels = {
  play: string;
  pause: string;
  seek: string;
  elapsed: string;
  duration: string;
  volume: string;
  mute: string;
  unmute: string;
  quality: string;
  auto: string;
  speed: string;
  rewind: string;
  forward: string;
  fullscreen: string;
  exitFullscreen: string;
};

type PlayerControlsProps = {
  labels: PlayerControlLabels;
  /** The reading direction of the Student's language, for the parts of the bar that are words. */
  locale: "ar" | "en";
  playing: boolean;
  currentTime: number;
  duration: number;
  volume: number;
  muted: boolean;
  playbackRate: number;
  fullscreen: boolean;
  fullscreenSupported: boolean;
  quality: QualityState;
  /** Whether the overlay is currently on screen. Hidden is still mounted, and still reachable. */
  visible: boolean;
  onPlayPause: () => void;
  onSeek: (value: number) => void;
  onSeekBy: (offsetSeconds: number) => void;
  onVolume: (value: number) => void;
  onToggleMute: () => void;
  onQuality: (selectValue: string) => void;
  onPlaybackRate: (selectValue: string) => void;
  onToggleFullscreen: () => void;
  /** The Student is holding the controls open — see `controls-visibility`. */
  onInteractionHold: (held: boolean) => void;
  /** Any use of the bar counts as activity, which restarts the idle countdown. */
  onActivity: () => void;
};

/**
 * One surface for every control on the bar.
 *
 * 44px on touch, the WCAG 2.5.5 target size, dropping to 40px from the small breakpoint where a
 * pointer is doing the aiming. Below that the bar is a row of controls a thumb cannot reliably hit,
 * which on a Lesson watched on a phone is most of the product.
 */
const CONTROL_SURFACE =
  "inline-flex size-11 shrink-0 items-center justify-center rounded-md text-gx-ink-50 transition-colors duration-fast ease-out-brand hover:bg-white/15 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent sm:size-10";

const SELECT_SURFACE =
  "h-11 max-w-[9rem] shrink-0 truncate rounded-md border border-white/25 bg-gx-navy/85 px-2 text-sm font-medium text-gx-ink-50 transition-colors duration-fast hover:bg-white/15 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring sm:h-10";

function ControlButton({
  label,
  onClick,
  disabled,
  children,
}: {
  label: string;
  onClick: () => void;
  disabled?: boolean;
  children: ReactNode;
}) {
  return (
    <button type="button" onClick={onClick} disabled={disabled} aria-label={label} title={label} className={CONTROL_SURFACE}>
      {children}
    </button>
  );
}

/**
 * Whether a focus event should pin the overlay open.
 *
 * Two cases, and only two. A keyboard focus must pin it, or the controls a Student is tabbing
 * through disappear from under them. An open `<select>` must pin it, because its list is painted
 * outside the bar and an idle timeout would close the menu the Student is reading. A pointer click
 * on a button is neither: it leaves focus behind on the control, and treating that as "held" is
 * what stops a bar from ever withdrawing again once anything has been clicked.
 */
function focusPinsControls(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.tagName === "SELECT") return true;
  try {
    return target.matches(":focus-visible");
  } catch {
    return false;
  }
}

/**
 * The control overlay.
 *
 * It is a bar over the media, not a card beneath it: the controls belong to the picture they
 * operate, they withdraw while the Lesson is running, and the gradient behind them exists so that
 * white on an arbitrary video frame stays legible.
 *
 * The bar is laid out left-to-right in both languages. It is an instrument for moving through time,
 * not a paragraph — the timeline runs the way time runs, the elapsed/duration pair does not
 * reorder, and the rewind and forward controls keep pointing the way they move. What is localised
 * is every word in it: the accessible names, the titles, and the text inside the two menus, which
 * carry the Student's own reading direction.
 */
export function PlayerControls({
  labels,
  locale,
  playing,
  currentTime,
  duration,
  volume,
  muted,
  playbackRate,
  fullscreen,
  fullscreenSupported,
  quality,
  visible,
  onPlayPause,
  onSeek,
  onSeekBy,
  onVolume,
  onToggleMute,
  onQuality,
  onPlaybackRate,
  onToggleFullscreen,
  onInteractionHold,
  onActivity,
}: PlayerControlsProps) {
  const safeDuration = Number.isFinite(duration) && duration > 0 ? duration : 0;
  const safeTime = safeDuration > 0 ? Math.min(safeDuration, Math.max(0, currentTime)) : 0;
  const safeVolume = Number.isFinite(volume) ? Math.min(1, Math.max(0, volume)) : 0;
  const silent = muted || safeVolume === 0;
  const qualityLabel = qualityValueText(quality, labels);
  const textDirection = locale === "ar" ? "rtl" : "ltr";
  const timeReading = `${labels.elapsed}: ${formatMediaTime(safeTime)}; ${labels.duration}: ${formatMediaTime(safeDuration)}`;
  const VolumeIcon = silent ? VolumeX : safeVolume < 0.5 ? Volume1 : Volume2;

  const releaseHold = (event: FocusEvent<HTMLDivElement>) => {
    if (event.currentTarget.contains(event.relatedTarget)) return;
    onInteractionHold(false);
  };

  return (
    <div
      data-player-controls
      data-controls-visible={visible ? "true" : "false"}
      dir="ltr"
      onFocusCapture={(event) => {
        if (focusPinsControls(event.target)) onInteractionHold(true);
        onActivity();
      }}
      onBlurCapture={releaseHold}
      onPointerEnter={() => onInteractionHold(true)}
      onPointerLeave={() => onInteractionHold(false)}
      onPointerMove={onActivity}
      className={cn(
        "absolute inset-x-0 bottom-0 z-20 flex flex-col gap-0.5 bg-gradient-to-t from-gx-navy via-gx-navy/80 to-transparent px-1.5 pb-1.5 pt-12 transition-opacity duration-base ease-out-brand sm:px-3 sm:pb-2.5",
        visible ? "opacity-100" : "pointer-events-none opacity-0",
      )}
    >
      {/* The timeline runs the way time runs, in both languages. Mirroring it in Arabic would put
          the start of the Lesson on the right of a control whose meaning is the passage of time,
          not the reading order of a paragraph. */}
      <div className="flex h-8 items-center px-1">
        <input
          type="range"
          min={0}
          max={safeDuration}
          step={0.1}
          value={safeTime}
          onChange={(event) => {
            onActivity();
            onSeek(Number(event.target.value));
          }}
          aria-label={labels.seek}
          aria-valuetext={`${formatMediaTime(safeTime)} / ${formatMediaTime(safeDuration)}`}
          disabled={safeDuration === 0}
          className="h-1.5 w-full cursor-pointer accent-primary focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring disabled:cursor-not-allowed disabled:opacity-50"
        />
      </div>

      <div className="flex flex-wrap items-center gap-0.5 sm:gap-1">
        <ControlButton label={playing ? labels.pause : labels.play} onClick={onPlayPause}>
          {playing ? <Pause aria-hidden className="size-5 fill-current" /> : <Play aria-hidden className="size-5 fill-current" />}
        </ControlButton>
        <ControlButton label={labels.rewind} onClick={() => onSeekBy(-SEEK_STEP_SECONDS)} disabled={safeDuration === 0}>
          <RotateCcw aria-hidden className="size-5" />
        </ControlButton>
        <ControlButton label={labels.forward} onClick={() => onSeekBy(SEEK_STEP_SECONDS)} disabled={safeDuration === 0}>
          <RotateCw aria-hidden className="size-5" />
        </ControlButton>
        <ControlButton label={silent ? labels.unmute : labels.mute} onClick={onToggleMute}>
          <VolumeIcon aria-hidden className="size-5" />
        </ControlButton>
        {/* The volume slider is the first thing to go on a phone: the mute control carries the
            capability, and a 20mm slider between two 44px targets is a control nobody can set. */}
        <label className="hidden items-center sm:flex">
          <span className="sr-only">{labels.volume}</span>
          <input
            type="range"
            min={0}
            max={1}
            step={0.01}
            value={safeVolume}
            onChange={(event) => {
              onActivity();
              onVolume(Number(event.target.value));
            }}
            aria-label={labels.volume}
            aria-valuetext={`${Math.round(safeVolume * 100)}%`}
            className="h-1.5 w-20 cursor-pointer accent-primary focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring"
          />
        </label>
        {/* Elapsed over duration is a media reading, not a sentence: in Arabic the neutral "/"
            between two numbers let the pair reorder, so a Lesson five seconds in read "0:30 / 0:05".
            The pair is isolated and laid out left-to-right in both languages, because playback
            direction is not language direction. */}
        <span dir="ltr" className="px-1 text-xs tabular-nums text-gx-ink-50 sm:text-sm" aria-label={timeReading}>
          {formatMediaTime(safeTime)} / {formatMediaTime(safeDuration)}
        </span>

        <div className="ms-auto flex items-center gap-0.5 sm:gap-1">
          <label className="flex items-center">
            <span className="sr-only">{labels.speed}</span>
            <select
              dir={textDirection}
              value={playbackRateSelectValue(playbackRate)}
              onChange={(event) => {
                onActivity();
                onPlaybackRate(event.target.value);
              }}
              aria-label={`${labels.speed}: ${playbackRateLabel(playbackRate)}`}
              title={labels.speed}
              data-playback-rate={playbackRate}
              className={SELECT_SURFACE}
            >
              {PLAYBACK_RATES.map((rate) => (
                <option key={playbackRateSelectValue(rate)} value={playbackRateSelectValue(rate)} className="bg-gx-navy text-gx-ink-50">
                  {playbackRateLabel(rate)}
                </option>
              ))}
            </select>
          </label>
          {/* Only the renditions the master playlist actually offers. A Lesson encoded at one
              height has nothing to choose between, so it is offered no choice. */}
          {quality.options.length > 0 ? (
            <label className="flex items-center">
              <span className="sr-only">{labels.quality}</span>
              <select
                dir={textDirection}
                value={qualitySelectValue(quality)}
                onChange={(event) => {
                  onActivity();
                  onQuality(event.target.value);
                }}
                aria-label={`${labels.quality}: ${qualityLabel}`}
                title={labels.quality}
                data-quality-mode={quality.selection.mode}
                className={SELECT_SURFACE}
              >
                <option value="auto" className="bg-gx-navy text-gx-ink-50">{labels.auto}</option>
                {quality.options.map((option) => (
                  <option key={`level-${option.levelIndex}`} value={`level-${option.levelIndex}`} className="bg-gx-navy text-gx-ink-50">
                    {option.label}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          {/* There is deliberately no Picture-in-Picture control here. The watermark is a DOM
              layer over the media surface and browser PiP presents the bare `<video>` element, so
              the picture would leave the watermark behind. See `video-watermark.tsx`. */}
          {fullscreenSupported ? (
            <ControlButton label={fullscreen ? labels.exitFullscreen : labels.fullscreen} onClick={onToggleFullscreen}>
              {fullscreen ? <Minimize aria-hidden className="size-5" /> : <Maximize aria-hidden className="size-5" />}
            </ControlButton>
          ) : null}
        </div>
      </div>
    </div>
  );
}
