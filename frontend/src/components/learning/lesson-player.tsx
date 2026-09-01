"use client";

import Hls from "hls.js";
import { AlertCircle, Loader2, Play } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type MouseEvent } from "react";
import { requestPlayback, type PlaybackAuthorization } from "@/lib/api/learning";
import { currentCSRFToken } from "@/lib/identity/session";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { cn } from "@/lib/utils";
import { CONTROLS_IDLE_MS, controlsVisible, pointerHidden } from "./controls-visibility";
import {
  createSurfaceGesture,
  gestureClick,
  gestureDoubleClick,
  resetSurfaceGesture,
  seekBy,
  seekMedia,
  setMediaPlaybackRate,
  setMediaVolume,
  setQualityLevel,
  supportsFullscreen,
  toggleFullscreen as toggleFullscreenBehavior,
  toggleMediaMute,
  toggleMediaPlayback,
} from "./player-controls-behavior";
import {
  clampMediaValue,
  DEFAULT_PLAYBACK_RATE,
  isPlaybackRate,
  playbackRateFromSelectValue,
  SEEK_STEP_SECONDS,
} from "./player-controls-model";
import { playerShortcutFor, VOLUME_STEP, type ShortcutTarget } from "./player-shortcuts";
import {
  applySelectValue,
  initialQualityState,
  intendedHlsLevel,
  levelSwitched,
  levelsLoaded,
  sourceReplaced,
  type QualityState,
} from "./quality-state";
import { PlayerControls } from "./player-controls";
import { useProgressReporter } from "./progress-reporter";
import { VideoWatermark } from "./video-watermark";

type LessonPlayerProps = {
  lessonID: string;
  locale: "ar" | "en";
  labels: Dictionary["player"];
  initialPositionSeconds?: number;
};

/** What the shortcut rule needs to know about whatever had focus, read defensively from the DOM. */
function shortcutTarget(target: EventTarget | null): ShortcutTarget | null {
  if (!(target instanceof HTMLElement)) return null;
  return {
    tagName: target.tagName,
    isContentEditable: target.isContentEditable,
    role: target.getAttribute("role"),
  };
}

export function LessonPlayer({ lessonID, locale, labels, initialPositionSeconds = 0 }: LessonPlayerProps) {
  /**
   * The player container and the media element are held as state, not as plain refs.
   *
   * A ref is filled during commit but never tells anyone it was filled, so an effect that asks a
   * question about the node has to run in the same commit that mounted it. This component does not
   * mount its node in its first commit — it renders a placeholder while playback authorisation is
   * in flight — which is exactly how fullscreen support came to be decided against a container that
   * did not exist yet and answered "unsupported" on every browser. A callback ref that stores the
   * node in state makes the node itself the dependency, so every capability keyed to it is asked
   * again the moment it is really there, and again if it is ever replaced.
   */
  const [playerElement, setPlayerElement] = useState<HTMLDivElement | null>(null);
  const [videoElement, setVideoElement] = useState<HTMLVideoElement | null>(null);
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const hlsRef = useRef<Hls | null>(null);
  const lastAudibleVolumeRef = useRef(1);
  /** The playback state the current click / double-click gesture began from. */
  const surfaceGestureRef = useRef(createSurfaceGesture());
  const attachVideo = useCallback((node: HTMLVideoElement | null) => {
    videoRef.current = node;
    setVideoElement(node);
  }, []);

  const [playback, setPlayback] = useState<PlaybackAuthorization | null>(null);
  const [failed, setFailed] = useState(false);
  const [playing, setPlaying] = useState(false);
  const [buffering, setBuffering] = useState(false);
  const [currentTime, setCurrentTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const [volume, setVolume] = useState(1);
  const [muted, setMuted] = useState(false);
  const [playbackRate, setPlaybackRate] = useState<number>(DEFAULT_PLAYBACK_RATE);
  const [fullscreen, setFullscreen] = useState(false);
  const [fullscreenSupported, setFullscreenSupported] = useState(false);
  // A source replacement mints a new key so a level event still in flight from the destroyed
  // hls.js instance cannot reach the new player's quality state.
  const sourceSequenceRef = useRef(0);
  const [quality, setQuality] = useState<QualityState>(() => initialQualityState("pending"));

  /**
   * The Student's chosen rate outlives the media element it was chosen on.
   *
   * Quality deliberately resets with the source — a pinned rendition belongs to the media it was
   * chosen for. Speed is the opposite: it is a way of watching, not a property of a file, and a
   * Student working through a Course at 1.5× does not want to reset it at every Lesson. It is kept
   * in a ref as well as in state so the media effect can re-apply it to a new element without
   * taking the rate as a dependency, which would tear down and rebuild hls.js on every change.
   */
  const playbackRateRef = useRef<number>(DEFAULT_PLAYBACK_RATE);

  const [recentActivity, setRecentActivity] = useState(true);
  const [interactionHeld, setInteractionHeld] = useState(false);
  const idleTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const markActivity = useCallback(() => {
    setRecentActivity(true);
    if (idleTimerRef.current) clearTimeout(idleTimerRef.current);
    idleTimerRef.current = setTimeout(() => setRecentActivity(false), CONTROLS_IDLE_MS);
  }, []);

  useEffect(() => {
    return () => {
      if (idleTimerRef.current) clearTimeout(idleTimerRef.current);
      idleTimerRef.current = null;
    };
  }, []);

  // Starting or stopping playback is itself activity: the bar appears, then begins its countdown.
  useEffect(() => {
    markActivity();
  }, [markActivity, playing]);

  useEffect(() => {
    let active = true;
    setPlayback(null);
    setFailed(false);
    void requestPlayback(lessonID, locale, currentCSRFToken())
      .then((authorization) => {
        if (active) setPlayback(authorization);
      })
      .catch(() => {
        if (active) setFailed(true);
      });
    return () => { active = false; };
  }, [lessonID, locale]);

  useEffect(() => {
    const video = videoElement;
    if (!video || !playback) return;
    let active = true;
    let hls: Hls | null = null;
    // Read once, so the cleanup ends the gesture this mount owned rather than whatever the ref
    // happens to hold by the time it runs.
    const surfaceGesture = surfaceGestureRef.current;

    const syncMediaState = () => {
      if (!active) return;
      setPlaying(!video.paused && !video.ended);
      setCurrentTime(Number.isFinite(video.currentTime) ? Math.max(0, video.currentTime) : 0);
      setDuration(Number.isFinite(video.duration) && video.duration > 0 ? video.duration : 0);
      setVolume(Number.isFinite(video.volume) ? clampMediaValue(video.volume, 1) : 0);
      setMuted(video.muted);
      if (!video.muted && video.volume > 0) lastAudibleVolumeRef.current = video.volume;
      // The element is the authority on its own rate, including a rate it refused to take.
      if (isPlaybackRate(video.playbackRate)) {
        playbackRateRef.current = video.playbackRate;
        setPlaybackRate(video.playbackRate);
      }
    };

    const mediaEvents = [
      "play", "playing", "pause", "ended", "timeupdate", "durationchange",
      "loadedmetadata", "seeking", "seeked", "volumechange", "ratechange",
    ];
    mediaEvents.forEach((eventName) => video.addEventListener(eventName, syncMediaState));
    syncMediaState();

    /**
     * Buffering is a separate signal from playing.
     *
     * `waiting` and `stalled` mean the element wants to run and cannot; every event that proves it
     * can run again, or that it has stopped wanting to, clears it. Deriving it from `playing`
     * instead would leave a spinner over a paused Lesson forever.
     */
    const mediaWaiting = () => { if (active) setBuffering(true); };
    const mediaReady = () => { if (active) setBuffering(false); };
    const waitingEvents = ["waiting", "stalled"];
    const readyEvents = ["playing", "canplay", "canplaythrough", "seeked", "pause", "ended", "error"];
    waitingEvents.forEach((eventName) => video.addEventListener(eventName, mediaWaiting));
    readyEvents.forEach((eventName) => video.addEventListener(eventName, mediaReady));

    // The element's own `error` event is the authoritative signal that this Lesson has no
    // playable media, so genuine media failure still reaches the unavailable state even though a
    // rejected `play()` no longer does.
    const mediaFailed = () => {
      if (active) setFailed(true);
    };
    video.addEventListener("error", mediaFailed);

    sourceSequenceRef.current += 1;
    const sourceKey = `${lessonID}#${sourceSequenceRef.current}`;
    // A new source always returns the Student to Auto: a manual pin belongs to the media it was
    // chosen for and must not carry into the next Lesson.
    setQuality(sourceReplaced(sourceKey));

    /**
     * The renditions this master playlist actually carries.
     *
     * The manifest the backend returns is a real adaptive master, so the levels are whatever it
     * declares — nothing here assumes a ladder, names a height the source does not have, or builds
     * a storage URL of its own. hls.js discovers the child playlists through the same protected
     * endpoint the master was served from.
     */
    const levelsParsed = () => {
      if (!hls || !active) return;
      const levels = hls.levels.map((level) => ({ height: level.height, width: level.width, bitrate: level.bitrate }));
      setQuality((current) => levelsLoaded(current, levels, sourceKey));
    };

    // `LEVEL_SWITCHED` reports what hls.js is rendering, including its own adaptive switches. It
    // records the active level only; it never changes the Student's selected mode.
    const levelDidSwitch = (_event: unknown, data: { level: number }) => {
      if (!active) return;
      setQuality((current) => levelSwitched(current, data.level, sourceKey));
    };

    if (Hls.isSupported()) {
      hls = new Hls();
      hlsRef.current = hls;
      hls.on(Hls.Events.MANIFEST_PARSED, levelsParsed);
      hls.on(Hls.Events.LEVEL_SWITCHED, levelDidSwitch);
      hls.loadSource(playback.manifest_url);
      hls.attachMedia(video);
    } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
      // Native HLS — Safari and iOS. The element loads the same protected manifest URL and does its
      // own adaptive selection, so there is no level list to offer and Auto is the only mode.
      video.src = playback.manifest_url;
    } else {
      setFailed(true);
    }

    const seekToSavedPosition = () => {
      const savedPosition = clampMediaValue(initialPositionSeconds, Number.isFinite(video.duration) ? video.duration : 0);
      if (savedPosition > 0) video.currentTime = savedPosition;
      // The rate the Student was already watching at is re-applied to the new element rather than
      // reset, and `ratechange` reports back whatever it actually took.
      setMediaPlaybackRate(video, playbackRateRef.current);
      syncMediaState();
    };
    video.addEventListener("loadedmetadata", seekToSavedPosition, { once: true });

    return () => {
      active = false;
      video.pause();
      video.removeEventListener("loadedmetadata", seekToSavedPosition);
      mediaEvents.forEach((eventName) => video.removeEventListener(eventName, syncMediaState));
      waitingEvents.forEach((eventName) => video.removeEventListener(eventName, mediaWaiting));
      readyEvents.forEach((eventName) => video.removeEventListener(eventName, mediaReady));
      video.removeEventListener("error", mediaFailed);
      if (hls) {
        hls.off(Hls.Events.MANIFEST_PARSED, levelsParsed);
        hls.off(Hls.Events.LEVEL_SWITCHED, levelDidSwitch);
        hls.destroy();
      }
      if (hlsRef.current === hls) hlsRef.current = null;
      video.removeAttribute("src");
      video.load();
      // A gesture cannot span two sources, and must not survive an unmount.
      resetSurfaceGesture(surfaceGesture);
      setPlaying(false);
      setBuffering(false);
      setCurrentTime(0);
      setDuration(0);
    };
  }, [initialPositionSeconds, lessonID, playback, videoElement]);

  /**
   * Fullscreen capability and state, keyed to the mounted container.
   *
   * Both halves depend on the node: the capability is a property of the element, and the state is
   * the question "is *this* element the one the document is presenting". Keying the effect to the
   * node rather than to the mount is the whole fix — no delay, no retry, no polling.
   */
  useEffect(() => {
    if (!playerElement) {
      setFullscreenSupported(false);
      setFullscreen(false);
      return;
    }
    setFullscreenSupported(supportsFullscreen(playerElement, document));
    const syncFullscreen = () => setFullscreen(document.fullscreenElement === playerElement);
    document.addEventListener("fullscreenchange", syncFullscreen);
    syncFullscreen();
    return () => document.removeEventListener("fullscreenchange", syncFullscreen);
  }, [playerElement]);

  // A rejected `play()` is a transient outcome of one interaction — the media element is still
  // loaded and still authorised. Treating it as the fatal "content unavailable" state replaced
  // the entire control set with an alert, so a single interrupted play attempt permanently
  // destroyed a working player. `failed` is reserved for a Lesson that genuinely has no playable
  // media: denied playback authorisation, unsupported HLS, or a media `error` event. Play state
  // itself needs no repair here, because it is resynchronised from the element's own events.
  const togglePlayPause = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;
    toggleMediaPlayback(video, () => {});
  }, []);

  const seek = useCallback((value: number) => {
    const video = videoRef.current;
    if (!video) return;
    seekMedia(video, value);
  }, []);

  /** A skip is bounded by the media it is skipping through — never past the end, never before 0. */
  const skip = useCallback((offsetSeconds: number) => {
    const video = videoRef.current;
    if (!video) return;
    seekBy(video, offsetSeconds);
  }, []);

  const changeVolume = useCallback((value: number) => {
    const video = videoRef.current;
    if (!video) return;
    const nextVolume = setMediaVolume(video, value);
    if (nextVolume > 0) lastAudibleVolumeRef.current = nextVolume;
  }, []);

  const nudgeVolume = useCallback((delta: number) => {
    const video = videoRef.current;
    if (!video) return;
    const from = Number.isFinite(video.volume) ? video.volume : 0;
    const nextVolume = setMediaVolume(video, from + delta);
    if (nextVolume > 0) lastAudibleVolumeRef.current = nextVolume;
  }, []);

  const toggleMute = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;
    const nextVolume = toggleMediaMute(video, lastAudibleVolumeRef.current);
    if (!video.muted && nextVolume > 0) lastAudibleVolumeRef.current = nextVolume;
  }, []);

  const changeQuality = useCallback(
    (selectValue: string) => {
      const next = applySelectValue(quality, selectValue);
      if (next === quality) return;
      const hls = hlsRef.current;
      // Auto drives hls.js back to its adaptive sentinel; a manual selection pins the real level.
      if (hls) setQualityLevel(hls, intendedHlsLevel(next), next.options.map((option) => option.levelIndex));
      setQuality(next);
    },
    [quality],
  );

  const changePlaybackRate = useCallback((selectValue: string) => {
    const video = videoRef.current;
    const rate = playbackRateFromSelectValue(selectValue);
    if (!video || rate === null) return;
    // `ratechange` writes the state; this only asks. A browser that refuses the rate therefore
    // leaves the control reading what is really playing rather than what was requested.
    setMediaPlaybackRate(video, rate);
  }, []);

  // A refused fullscreen request is likewise transient — the browser declining a presentation
  // change says nothing about whether the Lesson's media is available.
  const toggleFullscreen = useCallback(() => {
    // Feature-gated at the point of use as well as at the point of rendering, because the
    // double-click gesture and the `F` shortcut reach this without going through the control.
    if (!playerElement || !fullscreenSupported) return;
    toggleFullscreenBehavior(playerElement, document, () => {});
  }, [fullscreenSupported, playerElement]);

  /**
   * The player's keyboard, and the keys it is not allowed to take.
   *
   * The handler is on the player, not the document, so a Student typing in the report dialog or
   * tabbing through the Course contents never loses a keystroke to it. Within the player it asks
   * `playerShortcutFor` whether the key may be claimed at all: a focused button, the seek slider, a
   * `<select>`, or anything else that owns its own keyboard keeps it, which is why ArrowRight on
   * the timeline still scrubs by a step instead of jumping ten seconds. The browser default is
   * suppressed only for a key the player actually consumed, so Space still scrolls the Lesson page
   * everywhere the player did not claim it.
   */
  const handleKeyDown = useCallback(
    (event: KeyboardEvent<HTMLDivElement>) => {
      markActivity();
      const shortcut = playerShortcutFor({
        key: event.key,
        target: shortcutTarget(event.target),
        altKey: event.altKey,
        ctrlKey: event.ctrlKey,
        metaKey: event.metaKey,
      });
      if (!shortcut) return;
      event.preventDefault();
      switch (shortcut) {
        case "playPause":
          togglePlayPause();
          return;
        case "seekBackward":
          skip(-SEEK_STEP_SECONDS);
          return;
        case "seekForward":
          skip(SEEK_STEP_SECONDS);
          return;
        case "volumeUp":
          nudgeVolume(VOLUME_STEP);
          return;
        case "volumeDown":
          nudgeVolume(-VOLUME_STEP);
          return;
        case "toggleMute":
          toggleMute();
          return;
        case "toggleFullscreen":
          toggleFullscreen();
          return;
        default:
          return;
      }
    },
    [markActivity, nudgeVolume, skip, toggleFullscreen, toggleMute, togglePlayPause],
  );

  /**
   * Click plays, double-click goes fullscreen, and the double-click leaves playback exactly as it
   * found it.
   *
   * A double-click arrives as a click, a second click, and then a `dblclick`, so single-click
   * play/pause stays immediate and the gesture undoes the first click once it is known to be a
   * gesture. It undoes it by *restoring the recorded pre-gesture state*, not by toggling again:
   * `play()` is asynchronous, the element is still `paused` while the request is in flight, and a
   * second toggle read off that in-flight state would request play again and leave a Lesson
   * running that the Student never started.
   */
  const surfaceClicked = useCallback(
    (event: MouseEvent<HTMLDivElement>) => {
      markActivity();
      const video = videoRef.current;
      if (!video) return;
      gestureClick(surfaceGestureRef.current, video, event.detail, () => {});
    },
    [markActivity],
  );

  const surfaceDoubleClicked = useCallback(() => {
    const video = videoRef.current;
    if (video) gestureDoubleClick(surfaceGestureRef.current, video, () => {});
    toggleFullscreen();
  }, [toggleFullscreen]);

  useProgressReporter(videoRef, lessonID, playback?.asset_version_id ?? null);

  const activity = { playing, recentActivity, interactionHeld };
  const showControls = controlsVisible(activity);

  // A Lesson with no playable media is a real dead end, so it is stated in the media's own place
  // rather than as a line of text where a player used to be — the surrounding Lesson, its contents
  // and its previous/next controls all remain usable.
  if (failed) {
    return (
      <div
        role="alert"
        data-testid="lesson-media-unavailable"
        className="flex aspect-video w-full flex-col items-center justify-center gap-2 rounded-lg border border-border bg-muted px-6 text-center"
      >
        <AlertCircle aria-hidden className="size-6 text-muted-foreground" />
        <p className="text-sm font-semibold text-foreground">{labels.unavailable}</p>
      </div>
    );
  }
  // The placeholder holds the exact space the media will occupy. Announcing without reserving the
  // space meant the controls, the materials and the navigation all jumped once playback resolved.
  if (!playback) {
    return (
      <div
        data-testid="lesson-media-loading"
        className="flex aspect-video w-full animate-pulse items-center justify-center rounded-lg bg-muted"
      >
        <p role="status" aria-live="polite" className="text-sm text-muted-foreground">
          {labels.loading}
        </p>
      </div>
    );
  }
  return (
    <div
      ref={setPlayerElement}
      data-lesson-player
      data-fullscreen={fullscreen ? "true" : "false"}
      role="group"
      aria-label={labels.video}
      tabIndex={0}
      onKeyDown={handleKeyDown}
      onPointerMove={markActivity}
      onTouchStart={markActivity}
      onPointerLeave={() => setRecentActivity(false)}
      className={cn(
        // `gx-navy` rather than a stock slate: the letterbox around a video is a brand surface, and
        // it is the one colour on this screen that is not the page background.
        "relative w-full select-none overflow-hidden rounded-lg bg-gx-navy focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring",
        fullscreen && "h-full rounded-none",
        pointerHidden(activity) && "cursor-none",
      )}
    >
      <div
        onClick={surfaceClicked}
        onDoubleClick={surfaceDoubleClicked}
        // Deterrence, not a boundary. Suppressing the menu over the picture removes the one-click
        // "Save video as…" route a curious Student would otherwise find, and nothing more: it is
        // scoped to the media surface, so right-click still works everywhere else on the Lesson
        // page, and the keyboard context-menu key is untouched because it fires on the focused
        // player container rather than here.
        onContextMenu={(event) => event.preventDefault()}
        className={cn("relative w-full", fullscreen ? "h-full" : "aspect-video")}
      >
        <video
          ref={attachVideo}
          controls={false}
          playsInline
          // Picture-in-Picture is refused for protected Student playback, deliberately.
          //
          // The watermark is a DOM layer over the media surface, and browser PiP presents the raw
          // `<video>` element in a window the page does not draw into — the picture would leave the
          // watermark behind. The control and its shortcut are gone from the player, and this
          // attribute is the browser-level hint that also removes the menu item a browser offers on
          // the element itself. Fullscreen is unaffected: it presents the whole player container,
          // watermark included.
          disablePictureInPicture
          // A hint only, and inert here because the custom controls mean the native set is never
          // shown. It costs nothing and states the intent where a future edit would look.
          controlsList="nodownload"
          // The element is a picture, not a file to be dragged onto a desktop. Also deterrence.
          draggable={false}
          onDragStart={(event) => event.preventDefault()}
          aria-label={labels.video}
          className="size-full bg-gx-navy object-contain"
        />
        {/* Inside the fullscreen surface, so it stays on the picture in fullscreen as well as
            inline. It is `pointer-events-none`, so the click and double-click gestures on this
            surface pass straight through it. */}
        {playback.watermark ? <VideoWatermark watermark={playback.watermark} /> : null}
        {/* The affordance is a picture of the control, not a second copy of it: the bar below owns
            the accessible play button, and offering the same action twice to a screen reader would
            be noise. The whole media surface is the target, so the glyph never intercepts a click. */}
        {!playing && !buffering ? (
          <span
            aria-hidden
            className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center"
          >
            <span className="flex size-16 items-center justify-center rounded-full bg-gx-navy/70 text-gx-ink-50 shadow-lg backdrop-blur-sm transition-transform duration-base ease-out-brand sm:size-20">
              <Play aria-hidden className="size-7 fill-current ps-0.5 sm:size-9" />
            </span>
          </span>
        ) : null}
        {/* Buffering is reported where it is happening, at the size of a control rather than of the
            Lesson: the frame stays visible and nothing is covered but the middle of the picture. */}
        {buffering ? (
          <span className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center">
            <span role="status" className="flex size-14 items-center justify-center rounded-full bg-gx-navy/70 text-gx-ink-50 sm:size-16">
              <Loader2 aria-hidden className="size-7 animate-spin sm:size-8" />
              <span className="sr-only">{labels.buffering}</span>
            </span>
          </span>
        ) : null}
      </div>
      <PlayerControls
        labels={labels}
        locale={locale}
        playing={playing}
        currentTime={currentTime}
        duration={duration}
        volume={volume}
        muted={muted}
        playbackRate={playbackRate}
        fullscreen={fullscreen}
        fullscreenSupported={fullscreenSupported}
        quality={quality}
        visible={showControls}
        onPlayPause={togglePlayPause}
        onSeek={seek}
        onSeekBy={skip}
        onVolume={changeVolume}
        onToggleMute={toggleMute}
        onQuality={changeQuality}
        onPlaybackRate={changePlaybackRate}
        onToggleFullscreen={toggleFullscreen}
        onInteractionHold={setInteractionHeld}
        onActivity={markActivity}
      />
    </div>
  );
}
