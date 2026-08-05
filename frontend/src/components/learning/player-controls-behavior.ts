import { clampMediaValue } from "./player-controls-model";

export type MediaPlaybackTarget = {
  paused: boolean;
  ended: boolean;
  play: () => Promise<void>;
  pause: () => void;
};

export function toggleMediaPlayback(target: MediaPlaybackTarget, onFailure: () => void): "play" | "pause" {
  if (target.paused || target.ended) {
    void target.play().catch(onFailure);
    return "play";
  }
  target.pause();
  return "pause";
}

export type MediaSeekTarget = {
  currentTime: number;
  duration: number;
};

export function seekMedia(target: MediaSeekTarget, value: number): number | null {
  const nextTime = clampMediaValue(value, target.duration);
  if (!Number.isFinite(target.duration) || target.duration <= 0) return null;
  target.currentTime = nextTime;
  return nextTime;
}

export type MediaVolumeTarget = {
  volume: number;
  muted: boolean;
};

export function setMediaVolume(target: MediaVolumeTarget, value: number): number {
  const nextVolume = clampMediaValue(value, 1);
  target.volume = nextVolume;
  target.muted = nextVolume === 0;
  return nextVolume;
}

export function toggleMediaMute(target: MediaVolumeTarget, lastAudibleVolume: number): number {
  if (target.muted || target.volume === 0) {
    target.muted = false;
    target.volume = lastAudibleVolume > 0 ? lastAudibleVolume : 1;
    return target.volume;
  }
  target.muted = true;
  return target.volume;
}

export type QualityTarget = { currentLevel: number };

export function setQualityLevel(target: QualityTarget, levelIndex: number, availableLevels: readonly number[]): boolean {
  if (levelIndex !== -1 && !availableLevels.includes(levelIndex)) return false;
  target.currentLevel = levelIndex;
  return true;
}

export type FullscreenTarget = { requestFullscreen: () => Promise<void> };
export type FullscreenDocument = {
  fullscreenElement: unknown;
  exitFullscreen: () => Promise<void>;
};

export function toggleFullscreen(
  target: FullscreenTarget,
  fullscreenDocument: FullscreenDocument,
  onFailure: () => void,
): "enter" | "exit" {
  if (fullscreenDocument.fullscreenElement === target) {
    void fullscreenDocument.exitFullscreen().catch(onFailure);
    return "exit";
  }
  void target.requestFullscreen().catch(onFailure);
  return "enter";
}
