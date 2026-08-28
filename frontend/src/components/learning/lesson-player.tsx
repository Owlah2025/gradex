"use client";

import Hls from "hls.js";
import { AlertCircle } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { requestPlayback, type PlaybackAuthorization } from "@/lib/api/learning";
import { currentCSRFToken } from "@/lib/identity/session";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import {
  seekMedia,
  setMediaVolume,
  setQualityLevel,
  toggleFullscreen as toggleFullscreenBehavior,
  toggleMediaMute,
  toggleMediaPlayback,
} from "./player-controls-behavior";
import { clampMediaValue } from "./player-controls-model";
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

type LessonPlayerProps = {
  lessonID: string;
  locale: "ar" | "en";
  labels: Dictionary["player"];
  initialPositionSeconds?: number;
};

export function LessonPlayer({ lessonID, locale, labels, initialPositionSeconds = 0 }: LessonPlayerProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<HTMLVideoElement>(null);
  const hlsRef = useRef<Hls | null>(null);
  const lastAudibleVolumeRef = useRef(1);
  const [playback, setPlayback] = useState<PlaybackAuthorization | null>(null);
  const [failed, setFailed] = useState(false);
  const [playing, setPlaying] = useState(false);
  const [currentTime, setCurrentTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const [volume, setVolume] = useState(1);
  const [muted, setMuted] = useState(false);
  const [fullscreen, setFullscreen] = useState(false);
  const [fullscreenSupported, setFullscreenSupported] = useState(false);
  // A source replacement mints a new key so a level event still in flight from the destroyed
  // hls.js instance cannot reach the new player's quality state.
  const sourceSequenceRef = useRef(0);
  const [quality, setQuality] = useState<QualityState>(() => initialQualityState("pending"));

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
    const video = videoRef.current;
    if (!video || !playback) return;
    let active = true;
    let hls: Hls | null = null;

    const syncMediaState = () => {
      if (!active) return;
      setPlaying(!video.paused && !video.ended);
      setCurrentTime(Number.isFinite(video.currentTime) ? Math.max(0, video.currentTime) : 0);
      setDuration(Number.isFinite(video.duration) && video.duration > 0 ? video.duration : 0);
      setVolume(Number.isFinite(video.volume) ? clampMediaValue(video.volume, 1) : 0);
      setMuted(video.muted);
      if (!video.muted && video.volume > 0) lastAudibleVolumeRef.current = video.volume;
    };

    const mediaEvents = [
      "play", "playing", "pause", "ended", "timeupdate", "durationchange",
      "loadedmetadata", "seeking", "seeked", "volumechange",
    ];
    mediaEvents.forEach((eventName) => video.addEventListener(eventName, syncMediaState));
    syncMediaState();

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
      video.src = playback.manifest_url;
    } else {
      setFailed(true);
    }

    const seekToSavedPosition = () => {
      const savedPosition = clampMediaValue(initialPositionSeconds, Number.isFinite(video.duration) ? video.duration : 0);
      if (savedPosition > 0) video.currentTime = savedPosition;
      syncMediaState();
    };
    video.addEventListener("loadedmetadata", seekToSavedPosition, { once: true });

    return () => {
      active = false;
      video.pause();
      video.removeEventListener("loadedmetadata", seekToSavedPosition);
      mediaEvents.forEach((eventName) => video.removeEventListener(eventName, syncMediaState));
      video.removeEventListener("error", mediaFailed);
      if (hls) {
        hls.off(Hls.Events.MANIFEST_PARSED, levelsParsed);
        hls.off(Hls.Events.LEVEL_SWITCHED, levelDidSwitch);
        hls.destroy();
      }
      if (hlsRef.current === hls) hlsRef.current = null;
      video.removeAttribute("src");
      video.load();
      setPlaying(false);
      setCurrentTime(0);
      setDuration(0);
    };
  }, [initialPositionSeconds, lessonID, playback]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    setFullscreenSupported(
      typeof container.requestFullscreen === "function" && typeof document.exitFullscreen === "function",
    );
    const syncFullscreen = () => setFullscreen(document.fullscreenElement === container);
    document.addEventListener("fullscreenchange", syncFullscreen);
    syncFullscreen();
    return () => document.removeEventListener("fullscreenchange", syncFullscreen);
  }, []);

  // A rejected `play()` is a transient outcome of one interaction — the media element is still
  // loaded and still authorised. Treating it as the fatal "content unavailable" state replaced
  // the entire control set with an alert, so a single interrupted play attempt permanently
  // destroyed a working player. `failed` is reserved for a Lesson that genuinely has no playable
  // media: denied playback authorisation, unsupported HLS, or a media `error` event. Play state
  // itself needs no repair here, because it is resynchronised from the element's own events.
  const togglePlayPause = () => {
    const video = videoRef.current;
    if (!video) return;
    toggleMediaPlayback(video, () => {});
  };

  const seek = (value: number) => {
    const video = videoRef.current;
    if (!video) return;
    seekMedia(video, value);
  };

  const changeVolume = (value: number) => {
    const video = videoRef.current;
    if (!video) return;
    const nextVolume = setMediaVolume(video, value);
    if (nextVolume > 0) lastAudibleVolumeRef.current = nextVolume;
  };

  const toggleMute = () => {
    const video = videoRef.current;
    if (!video) return;
    const nextVolume = toggleMediaMute(video, lastAudibleVolumeRef.current);
    if (!video.muted && nextVolume > 0) lastAudibleVolumeRef.current = nextVolume;
  };

  const changeQuality = (selectValue: string) => {
    const next = applySelectValue(quality, selectValue);
    if (next === quality) return;
    const hls = hlsRef.current;
    // Auto drives hls.js back to its adaptive sentinel; a manual selection pins the real level.
    if (hls) setQualityLevel(hls, intendedHlsLevel(next), next.options.map((option) => option.levelIndex));
    setQuality(next);
  };

  // A refused fullscreen request is likewise transient — the browser declining a presentation
  // change says nothing about whether the Lesson's media is available.
  const toggleFullscreen = () => {
    const container = containerRef.current;
    if (!container) return;
    toggleFullscreenBehavior(container, document, () => {});
  };

  useProgressReporter(videoRef, lessonID, playback?.asset_version_id ?? null);

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
    <div ref={containerRef} className="space-y-3" data-lesson-player>
      {/* `gx-navy` rather than a stock slate: the letterbox around a video is a brand surface, and
          it is the one colour on this screen that is not the page background. */}
      <video ref={videoRef} controls={false} aria-label={labels.video} className="aspect-video w-full rounded-lg bg-gx-navy" />
      <PlayerControls
        labels={labels}
        playing={playing}
        currentTime={currentTime}
        duration={duration}
        volume={volume}
        muted={muted}
        fullscreen={fullscreen}
        fullscreenSupported={fullscreenSupported}
        quality={quality}
        onPlayPause={togglePlayPause}
        onSeek={seek}
        onVolume={changeVolume}
        onToggleMute={toggleMute}
        onQuality={changeQuality}
        onToggleFullscreen={toggleFullscreen}
      />
    </div>
  );
}
