"use client";

import { formatMediaTime } from "./player-controls-model";
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
  fullscreen: string;
  exitFullscreen: string;
};

type PlayerControlsProps = {
  labels: PlayerControlLabels;
  playing: boolean;
  currentTime: number;
  duration: number;
  volume: number;
  muted: boolean;
  fullscreen: boolean;
  fullscreenSupported: boolean;
  quality: QualityState;
  onPlayPause: () => void;
  onSeek: (value: number) => void;
  onVolume: (value: number) => void;
  onToggleMute: () => void;
  onQuality: (selectValue: string) => void;
  onToggleFullscreen: () => void;
};

export function PlayerControls({
  labels,
  playing,
  currentTime,
  duration,
  volume,
  muted,
  fullscreen,
  fullscreenSupported,
  quality,
  onPlayPause,
  onSeek,
  onVolume,
  onToggleMute,
  onQuality,
  onToggleFullscreen,
}: PlayerControlsProps) {
  const safeDuration = Number.isFinite(duration) && duration > 0 ? duration : 0;
  const safeTime = safeDuration > 0 ? Math.min(safeDuration, Math.max(0, currentTime)) : 0;
  const safeVolume = Number.isFinite(volume) ? Math.min(1, Math.max(0, volume)) : 0;
  const qualityLabel = qualityValueText(quality, labels);

  return (
    <div className="mt-3 space-y-3 rounded-xl border border-border bg-card p-3" data-player-controls>
      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={onPlayPause}
          aria-label={playing ? labels.pause : labels.play}
          className="rounded-md border border-border px-3 py-2 font-semibold text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          {playing ? labels.pause : labels.play}
        </button>
        <span className="text-sm tabular-nums text-muted-foreground" aria-label={`${labels.elapsed}: ${formatMediaTime(safeTime)}; ${labels.duration}: ${formatMediaTime(safeDuration)}`}>
          {formatMediaTime(safeTime)} / {formatMediaTime(safeDuration)}
        </span>
      </div>

      <label className="block text-sm text-muted-foreground">
        <span className="sr-only">{labels.seek}</span>
        <input
          type="range"
          min={0}
          max={safeDuration}
          step={0.1}
          value={safeTime}
          onChange={(event) => onSeek(Number(event.target.value))}
          aria-label={labels.seek}
          aria-valuetext={`${formatMediaTime(safeTime)} / ${formatMediaTime(safeDuration)}`}
          disabled={safeDuration === 0}
          className="w-full accent-primary focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        />
      </label>

      <div className="flex flex-wrap items-center gap-3">
        <button
          type="button"
          onClick={onToggleMute}
          aria-label={muted || safeVolume === 0 ? labels.unmute : labels.mute}
          className="rounded-md border border-border px-3 py-2 font-semibold text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          {muted || safeVolume === 0 ? labels.unmute : labels.mute}
        </button>
        <label className="flex min-w-48 flex-1 items-center gap-2 text-sm text-muted-foreground">
          <span className="sr-only">{labels.volume}</span>
          <input
            type="range"
            min={0}
            max={1}
            step={0.01}
            value={safeVolume}
            onChange={(event) => onVolume(Number(event.target.value))}
            aria-label={labels.volume}
            aria-valuetext={`${Math.round(safeVolume * 100)}%`}
            className="w-full accent-primary focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          />
        </label>
        {quality.options.length > 0 ? (
          <label className="flex items-center gap-2 text-sm text-muted-foreground">
            <span>{labels.quality}</span>
            <select
              value={qualitySelectValue(quality)}
              onChange={(event) => onQuality(event.target.value)}
              aria-label={`${labels.quality}: ${qualityLabel}`}
              data-quality-mode={quality.selection.mode}
              className="rounded-md border border-border bg-background px-2 py-2 text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            >
              <option value="auto">{labels.auto}</option>
              {quality.options.map((option) => (
                <option key={`level-${option.levelIndex}`} value={`level-${option.levelIndex}`}>{option.label}</option>
              ))}
            </select>
          </label>
        ) : null}
        {fullscreenSupported ? (
          <button
            type="button"
            onClick={onToggleFullscreen}
            aria-label={fullscreen ? labels.exitFullscreen : labels.fullscreen}
            className="rounded-md border border-border px-3 py-2 font-semibold text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          >
            {fullscreen ? labels.exitFullscreen : labels.fullscreen}
          </button>
        ) : null}
      </div>
    </div>
  );
}
