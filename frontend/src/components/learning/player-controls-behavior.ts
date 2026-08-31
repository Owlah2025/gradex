import { clampMediaValue, isPlaybackRate } from "./player-controls-model";

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

/**
 * Skipping by a fixed interval.
 *
 * Expressed in terms of `seekMedia` so a skip cannot land anywhere a drag of the timeline could
 * not: it is bounded by the same media, refused on the same unknown duration, and does not mark
 * completion either. A skip past the end lands on the end, and a skip before the start lands on
 * the start; neither is an error the Student has to notice.
 */
export function seekBy(target: MediaSeekTarget, offsetSeconds: number): number | null {
  if (!Number.isFinite(offsetSeconds)) return null;
  const from = Number.isFinite(target.currentTime) ? target.currentTime : 0;
  return seekMedia(target, from + offsetSeconds);
}

export type MediaRateTarget = { playbackRate: number };

/**
 * The rate the media element plays at.
 *
 * A rate outside the offered set is refused rather than clamped: the only rates that reach here
 * come from the control or the shortcut set, so an unrecognised one is a defect and silently
 * rounding it would hide it. The applied rate is returned so React state records what the element
 * actually took, never what it was asked for.
 */
export function setMediaPlaybackRate(target: MediaRateTarget, rate: number): number | null {
  if (!isPlaybackRate(rate)) return null;
  target.playbackRate = rate;
  return rate;
}

export type FullscreenCapableElement = { requestFullscreen?: unknown };
export type FullscreenCapableDocument = { exitFullscreen?: unknown };

/**
 * Whether this browser can put *this* element into fullscreen.
 *
 * It takes the element, not just the document, because the capability is a property of the pair
 * and because the answer is worthless before the element exists. The Lesson Player renders a
 * placeholder while playback authorisation is in flight, so the first time this question was asked
 * there was no player node to ask it about — and a `null` element answered "unsupported", which is
 * what hid the fullscreen control on browsers that support it perfectly well. Answering `false`
 * for a missing element is correct; the fix is that the caller must ask again once the node is
 * mounted, which is why the capability is keyed to the node rather than to the mount.
 */
export function supportsFullscreen(
  element: FullscreenCapableElement | null | undefined,
  fullscreenDocument: FullscreenCapableDocument | null | undefined,
): boolean {
  if (!element || !fullscreenDocument) return false;
  return typeof element.requestFullscreen === "function" && typeof fullscreenDocument.exitFullscreen === "function";
}

export type PictureInPictureVideo = {
  requestPictureInPicture?: unknown;
  disablePictureInPicture?: boolean;
};

export type PictureInPictureDocument = {
  pictureInPictureEnabled?: boolean;
  pictureInPictureElement?: unknown;
  exitPictureInPicture?: unknown;
};

/**
 * Whether Picture-in-Picture is available for this video in this document.
 *
 * Three separate things have to hold, and each of them is false somewhere real: the API may be
 * absent entirely (Firefox exposes no `requestPictureInPicture`), the document may have it
 * disabled by permissions policy (`pictureInPictureEnabled === false`), and the element itself may
 * opt out (`disablePictureInPicture`). The control is not rendered unless all three pass, so no
 * Student is offered a button that cannot do anything.
 */
export function supportsPictureInPicture(
  pipDocument: PictureInPictureDocument | null | undefined,
  video: PictureInPictureVideo | null | undefined,
): boolean {
  if (!pipDocument || !video) return false;
  if (pipDocument.pictureInPictureEnabled !== true) return false;
  if (video.disablePictureInPicture === true) return false;
  return typeof video.requestPictureInPicture === "function" && typeof pipDocument.exitPictureInPicture === "function";
}

/**
 * Enter or leave Picture-in-Picture.
 *
 * Shaped like `toggleFullscreen`: the document owns the truth about which element is presented, a
 * refusal is a transient outcome reported to the caller, and the return value names the intent
 * rather than the result — the player's own `enterpictureinpicture` / `leavepictureinpicture`
 * events are what move the UI, so a request the browser declines leaves no state claiming it
 * succeeded.
 */
export function togglePictureInPicture(
  video: PictureInPictureVideo,
  pipDocument: PictureInPictureDocument,
  onFailure: () => void,
): "enter" | "exit" | "unsupported" {
  if (!supportsPictureInPicture(pipDocument, video)) return "unsupported";
  if (pipDocument.pictureInPictureElement === video) {
    const exit = pipDocument.exitPictureInPicture as () => Promise<unknown>;
    void Promise.resolve(exit.call(pipDocument)).catch(onFailure);
    return "exit";
  }
  const request = video.requestPictureInPicture as () => Promise<unknown>;
  void Promise.resolve(request.call(video)).catch(onFailure);
  return "enter";
}

/**
 * Put playback back the way it was, without asking the element where it currently is.
 *
 * `play()` is asynchronous. Between a click that requested play and the `dblclick` that completes
 * the gesture, the element is still `paused` — the request has not settled — so anything that
 * decides what to do by *toggling* the element's present state requests play a second time and
 * leaves a Lesson running that the Student never started. The pre-gesture state is therefore the
 * only input: this restores it outright rather than inferring it.
 *
 * The paused branch calls `pause()` unconditionally, which is also how an in-flight `play()` is
 * neutralised: the HTML media spec has `pause()` abort a pending play request and reject its
 * promise with `AbortError`. That rejection is a transient control outcome like any other and is
 * routed to `onFailure`, never to the Lesson's availability.
 */
export function restoreMediaPlayback(
  target: MediaPlaybackTarget,
  playingBefore: boolean,
  onFailure: () => void,
): "play" | "pause" {
  if (!playingBefore) {
    target.pause();
    return "pause";
  }
  // Unconditional, for the mirror case: a first click that paused a running Lesson must be undone
  // even though the element already reports `paused`. Calling `play()` on a running element is a
  // no-op that resolves immediately.
  void target.play().catch(onFailure);
  return "play";
}

/**
 * The click / double-click gesture on the media surface.
 *
 * A double-click arrives as a click, a second click, and then a `dblclick`, and the gesture is not
 * knowable until the last of the three. Single-click play/pause therefore stays immediate — no
 * timer sits in front of it deciding whether a second click is coming — and the `dblclick`, once it
 * is known to be a `dblclick`, undoes what the first click did. What makes that deterministic is
 * that the state to undo *to* was recorded before the first click acted, rather than read back off
 * an element whose `play()` may still be in flight.
 */
export type SurfaceGesture = { playingBeforeGesture: boolean | null };

export function createSurfaceGesture(): SurfaceGesture {
  return { playingBeforeGesture: null };
}

/** A source replacement or an unmount ends any gesture in progress. */
export function resetSurfaceGesture(gesture: SurfaceGesture): void {
  gesture.playingBeforeGesture = null;
}

/**
 * A click on the media surface. `clickCount` is the event's `detail`: the second click of a
 * double-click is ignored outright, and must not overwrite what the first one recorded.
 */
export function gestureClick(
  gesture: SurfaceGesture,
  target: MediaPlaybackTarget,
  clickCount: number,
  onFailure: () => void,
): "play" | "pause" | "ignored" {
  if (clickCount > 1) return "ignored";
  gesture.playingBeforeGesture = !target.paused && !target.ended;
  return toggleMediaPlayback(target, onFailure);
}

/**
 * The gesture completed. Playback returns to what it was before the first click, and the gesture
 * state is spent — a later `dblclick` with nothing recorded changes no playback at all.
 */
export function gestureDoubleClick(
  gesture: SurfaceGesture,
  target: MediaPlaybackTarget,
  onFailure: () => void,
): "play" | "pause" | "unknown" {
  const playingBefore = gesture.playingBeforeGesture;
  gesture.playingBeforeGesture = null;
  if (playingBefore === null) return "unknown";
  return restoreMediaPlayback(target, playingBefore, onFailure);
}
