"use client";

import * as React from "react";
import { ThumbnailImage } from "@/components/catalog/thumbnail-image";
import Link from "next/link";
import { ChevronLeft, ChevronRight, Play } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { formatFilsParts } from "@/lib/formatters/currency";
import { cn } from "@/lib/utils";
import type { PublicCourse } from "@/lib/api/public-catalog";

/**
 * The landing catalogue carousel — a horizontal, snap-scrolled row of real published Courses.
 *
 * Approved custom thumbnails take priority. The fallback is generated from the Course's own
 * identity — a light brand gradient, a subject-aware abstract motif, a monogram off its Subject code
 * — which stays honest, keeps the page's light key, and reads as a set rather than a row of
 * mismatched photos.
 *
 * Everything here is presentation over data the caller already fetched. The one stateful piece is
 * `useCarousel`, and it holds a scroll position, nothing about the catalogue. The neighbour-push
 * hover is pure CSS and lives in `globals.css` under `.course-rail`.
 */

export type CourseCardLabels = {
  /** Screen-reader-only role prefix for the instructor's name. */
  instructor: string;
  /** The short "Preview" pill. */
  preview: string;
  /** Screen-reader-only role prefix for the price, preserving its "guidance" meaning. */
  priceGuidance: string;
};

/* -------------------------------------------------------------------------------------------------
 * useCarousel — the scroll controller.
 *
 * It exposes whether either edge can still move and a paged scroll in each logical direction. The
 * whole thing is written against *travel distance*, never a signed `scrollLeft`, because RTL scroll
 * positions are negative: reading the magnitude makes "how far from the start" identical in both
 * writing directions, and only the sign of a programmatic scroll has to flip.
 * ------------------------------------------------------------------------------------------------- */
export function useCarousel(dir: "ltr" | "rtl", itemCount = 0) {
  const scrollerRef = React.useRef<HTMLUListElement>(null);
  const [canPrev, setCanPrev] = React.useState(false);
  const [canNext, setCanNext] = React.useState(false);

  const measure = React.useCallback(() => {
    const el = scrollerRef.current;
    if (!el) return;
    const max = el.scrollWidth - el.clientWidth;
    const travelled = Math.abs(el.scrollLeft);
    setCanPrev(travelled > 1);
    setCanNext(travelled < max - 1);
  }, []);

  // `itemCount` is a dependency, not decoration: the scroller does not exist on the first render —
  // the strip is still loading and the ref is null — so a one-shot effect would measure nothing and
  // never see the row appear. Re-running when the item count lands re-attaches to the real element,
  // and the rAF re-measures once layout has actually placed the cards.
  React.useEffect(() => {
    const el = scrollerRef.current;
    if (!el) return;
    measure();
    const raf = requestAnimationFrame(measure);
    el.addEventListener("scroll", measure, { passive: true });
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => {
      cancelAnimationFrame(raf);
      el.removeEventListener("scroll", measure);
      observer.disconnect();
    };
  }, [measure, itemCount]);

  const page = React.useCallback(
    (towards: 1 | -1) => {
      const el = scrollerRef.current;
      if (!el) return;
      const reduced =
        typeof window !== "undefined" &&
        window.matchMedia("(prefers-reduced-motion: reduce)").matches;
      // A little under one viewport, so a card from the previous page stays on screen as an anchor.
      // `towards` is +1 for the end (next) and -1 for the start (prev); in RTL the end is reached by
      // scrolling to a more-negative offset, so the sign flips.
      const step = el.clientWidth * 0.85 * towards * (dir === "rtl" ? -1 : 1);
      el.scrollBy({ left: step, behavior: reduced ? "auto" : "smooth" });
    },
    [dir],
  );

  return {
    scrollerRef,
    canPrev,
    canNext,
    scrollable: canPrev || canNext,
    scrollPrev: () => page(-1),
    scrollNext: () => page(1),
  };
}

/* -------------------------------------------------------------------------------------------------
 * The controls.
 * ------------------------------------------------------------------------------------------------- */
export function CarouselArrows({
  dir,
  canPrev,
  canNext,
  onPrev,
  onNext,
  labels,
  className,
}: {
  dir: "ltr" | "rtl";
  canPrev: boolean;
  canNext: boolean;
  onPrev: () => void;
  onNext: () => void;
  labels: { previous: string; next: string };
  className?: string;
}) {
  // The glyphs point along the page, so the "previous" arrow faces the start edge in each direction.
  const PrevIcon = dir === "rtl" ? ChevronRight : ChevronLeft;
  const NextIcon = dir === "rtl" ? ChevronLeft : ChevronRight;
  return (
    <div className={cn("flex items-center gap-2", className)}>
      <ArrowButton label={labels.previous} disabled={!canPrev} onClick={onPrev}>
        <PrevIcon aria-hidden />
      </ArrowButton>
      <ArrowButton label={labels.next} disabled={!canNext} onClick={onNext}>
        <NextIcon aria-hidden />
      </ArrowButton>
    </div>
  );
}

function ArrowButton({
  label,
  disabled,
  onClick,
  children,
}: {
  label: string;
  disabled: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      disabled={disabled}
      onClick={onClick}
      className={cn(
        "grid size-10 place-items-center rounded-pill border border-border bg-card text-foreground shadow-sm",
        "transition-[color,border-color,box-shadow,transform] duration-base ease-out-brand",
        "hover:border-gx-blue-200 hover:text-primary hover:shadow-md motion-safe:hover:-translate-y-0.5",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        "disabled:pointer-events-none disabled:opacity-40 disabled:shadow-none",
        "[&_svg]:size-5",
      )}
    >
      {children}
    </button>
  );
}

/* -------------------------------------------------------------------------------------------------
 * The card.
 * ------------------------------------------------------------------------------------------------- */
export function CourseCard({
  course,
  href,
  locale,
  labels,
}: {
  course: PublicCourse;
  href: string;
  locale: "ar" | "en";
  labels: CourseCardLabels;
}) {
  const price = course.price
    ? formatFilsParts(course.price.minor_units, locale)
    : null;
  const level = course.study_year?.label;

  return (
    <Link
      href={href}
      className={cn(
        // `w-full` is load-bearing: the card is a flex child of its <li>, and without it the card
        // shrinks to its content width and sits left-aligned, turning the leftover item width into
        // a huge phantom gap. Filling the item is what makes the visible gap equal the real `gap`.
        "relative flex h-full w-full flex-col overflow-hidden rounded-lg border border-border bg-card text-card-foreground shadow-sm",
        "transition-[transform,box-shadow,border-color] duration-slow ease-out-brand will-change-transform",
        // The lift/scale is driven by the rail's `data-push="active"` (see globals.css); here we keep
        // only the non-motion cues, which also read under reduced motion.
        "hover:border-gx-blue-200 hover:shadow-[0_16px_30px_-10px_rgba(13,27,42,0.22)]",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
      )}
    >
      <CourseCover course={course} previewLabel={labels.preview} />

      <div className="flex flex-1 flex-col p-4">
        {/* Two lines are reserved whether the title needs them or not, so a one-line title and a
            wrapping one produce cards of the same height and the row's footers align. */}
        <h3 className="min-h-[2.6em] font-display text-[16px] font-bold leading-[1.3] text-foreground [display:-webkit-box] [-webkit-box-orient:vertical] [-webkit-line-clamp:2] overflow-hidden">
          {course.title}
        </h3>
        {course.instructor_display_name ? (
          <p className="mt-1 truncate text-[13px] text-muted-foreground">
            <span className="sr-only">{labels.instructor}: </span>
            {course.instructor_display_name}
          </p>
        ) : null}

        <div className="mt-auto flex items-end justify-between gap-2 pt-3">
          {level ? (
            <span className="text-[13px] font-medium text-muted-foreground">
              {level}
            </span>
          ) : (
            <span aria-hidden />
          )}
          {price && price.priced ? (
            <p dir="ltr" className="flex items-baseline gap-1">
              <span className="sr-only">{labels.priceGuidance}: </span>
              <span className="font-display text-lg font-extrabold tracking-tight text-foreground">
                {price.amount}
              </span>
              <span className="text-[11px] font-semibold text-muted-foreground">
                {price.unit}
              </span>
            </p>
          ) : null}
        </div>
      </div>
    </Link>
  );
}

/**
 * The generated cover.
 *
 * The generated fallback remains under the custom thumbnail and survives image failure. Each Subject
 * resolves to its own visual language rather than one template tinted a different colour:
 * `resolveCover` returns a background, an art layer, and whether that art is dark, so a subject can
 * bring a full navy engineering plate (Digital Logic) while the rest keep the light accent-on-white
 * treatment. The pill/preview chrome and the bottom seam adapt to the `dark` flag. Nothing a student
 * needs to read lives on the art — the panel below carries it on guaranteed contrast.
 */
function CourseCover({
  course,
  previewLabel,
}: {
  course: PublicCourse;
  previewLabel: string;
}) {
  const code = course.subject?.code;
  const primaryPill = code || course.major?.label || course.subject?.label;
  const cover = resolveCover(course);

  return (
    <div className="relative h-[168px] shrink-0 overflow-hidden sm:h-[180px]">
      <div
        aria-hidden
        data-cover
        className="absolute inset-0 transition-transform duration-slow ease-out-brand will-change-transform"
        style={{ backgroundImage: cover.background }}
      >
        {cover.node}
        {/* Light covers dissolve into the white panel; a dark plate meets it as a clean two-tone edge. */}
        {cover.dark ? null : (
          <div className="absolute inset-x-0 bottom-0 h-14 bg-gradient-to-b from-transparent to-card" />
        )}
      </div>

      {course.thumbnail?.card_url ? <ThumbnailImage key={course.thumbnail.card_url} src={course.thumbnail.card_url} className="absolute inset-0 size-full object-cover" /> : null}
      <div className="absolute inset-x-0 top-0 flex items-start justify-between gap-2 p-3">
        {primaryPill ? (
          <Badge
            size="sm"
            className={cn(
              cover.dark && !course.thumbnail
                ? "border border-white/20 bg-white/10 text-white backdrop-blur-sm"
                : "bg-card/85 shadow-sm backdrop-blur-sm",
              code && "dir-ltr",
            )}
          >
            {primaryPill}
          </Badge>
        ) : (
          <span aria-hidden />
        )}
        {course.has_preview ? (
          <span
            className={cn(
              "inline-flex shrink-0 items-center gap-1 rounded-pill px-2 py-1 font-display text-[11px] font-bold shadow-sm backdrop-blur-sm [&_svg]:size-3",
              cover.dark && !course.thumbnail ? "bg-white/10 text-white" : "bg-card/85 text-primary",
            )}
          >
            <Play aria-hidden />
            {previewLabel}
          </span>
        ) : null}
      </div>
    </div>
  );
}

/* -------------------------------------------------------------------------------------------------
 * The cover registry — one visual language per subject.
 *
 * `resolveCover` is the single place a subject earns a bespoke plate. Today Digital Logic has one;
 * every other subject falls back to the light accent-tinted motif. Adding Programming, Data
 * Structures, Networking, Math… is a new branch here plus its own art component — the card, the
 * rail and the API contract stay untouched.
 * ------------------------------------------------------------------------------------------------- */
type CoverArt = { dark: boolean; background: string; node: React.ReactNode };

function resolveCover(course: PublicCourse): CoverArt {
  if (isLogicCourse(course)) {
    return {
      dark: true,
      background: "linear-gradient(140deg,#15293e 0%,#0c1a29 55%,#091420 100%)",
      node: <LogicCover />,
    };
  }
  const accent = coverAccent(course);
  return {
    dark: false,
    background: `linear-gradient(140deg, color-mix(in oklab, ${accent} 22%, white), color-mix(in oklab, ${accent} 7%, white))`,
    node: <DefaultCover course={course} accent={accent} />,
  };
}

function isLogicCourse(course: PublicCourse): boolean {
  const hay = `${course.subject?.code ?? ""} ${course.subject?.label ?? ""} ${course.major?.label ?? ""} ${course.title}`.toLowerCase();
  return /logic|منطق/.test(hay);
}

/** The light, accent-tinted treatment: an abstract motif under a faint Subject monogram. */
function DefaultCover({ course, accent }: { course: PublicCourse; accent: string }) {
  return (
    <>
      <CoverMotif family={coverFamily(course)} className="absolute inset-0 h-full w-full" style={{ color: accent }} />
      <span
        className="pointer-events-none absolute inset-y-0 end-[-0.28em] flex select-none items-center font-display text-[5.5rem] font-black leading-none tracking-tight opacity-[0.12]"
        style={{ color: accent }}
      >
        {monogram(course)}
      </span>
    </>
  );
}

/**
 * Digital Logic — a navy engineering plate, composed for the card ratio (not cropped from a square).
 *
 * The hero is the circuit itself: an AND and an OR gate converging into an XOR, the immediately
 * readable shape of digital logic, drawn as clean line-art with teal junction nodes and a teal
 * output. Faint binary and a truth-table frame sit back as supporting texture over a dot field, and
 * a soft teal glow gives depth. No title — the card names the course.
 */
function LogicCover() {
  const uid = React.useId().replace(/[^a-zA-Z0-9]/g, "");
  return (
    <svg viewBox="0 0 340 180" preserveAspectRatio="xMidYMid slice" fill="none" aria-hidden className="absolute inset-0 h-full w-full">
      <defs>
        <pattern id={`ld-${uid}`} width="15" height="15" patternUnits="userSpaceOnUse">
          <circle cx="1.4" cy="1.4" r="1" fill="#66809f" opacity="0.16" />
        </pattern>
        <radialGradient id={`lg-${uid}`} cx="76%" cy="44%" r="55%">
          <stop offset="0%" stopColor="#2fd6c3" stopOpacity="0.2" />
          <stop offset="100%" stopColor="#2fd6c3" stopOpacity="0" />
        </radialGradient>
      </defs>

      <rect width="340" height="180" fill={`url(#ld-${uid})`} />
      <rect width="340" height="180" fill={`url(#lg-${uid})`} />

      {/* supporting texture: binary + a truth-table frame, held back */}
      <g fill="#7c93b0" opacity="0.32" fontFamily="ui-monospace, monospace" fontSize="9.5" letterSpacing="2.5">
        <text x="250" y="52">01010</text>
        <text x="250" y="66">10011</text>
      </g>
      <g stroke="#5f7c9c" strokeWidth="1" opacity="0.22">
        <rect x="250" y="118" width="58" height="42" rx="3" />
        <line x1="279" y1="118" x2="279" y2="160" />
        <line x1="250" y1="132" x2="308" y2="132" />
        <line x1="250" y1="146" x2="308" y2="146" />
      </g>

      {/* the circuit — the hero */}
      <g stroke="#cbd8e8" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" opacity="0.85">
        <circle cx="26" cy="52" r="3" />
        <circle cx="26" cy="72" r="3" />
        <circle cx="26" cy="112" r="3" />
        <circle cx="26" cy="132" r="3" />
        <path d="M29 52H72M29 72H72M29 112H74M29 132H74" />
        <path d="M72 41H98A21 21 0 0 1 98 83H72Z" />
        <path d="M70 103Q86 122 70 143Q104 141 124 122Q104 105 70 103Z" />
        <path d="M119 62H152V92H176" />
        <path d="M124 122H152V104H176" />
        <path d="M168 75Q180 98 168 121" />
        <path d="M174 75Q186 98 174 121Q210 119 232 98Q210 77 174 75Z" />
        <path d="M232 98H286" />
      </g>

      {/* teal accents: junctions + output */}
      <circle cx="152" cy="92" r="3.6" fill="#2fd6c3" />
      <circle cx="152" cy="104" r="3.6" fill="#2fd6c3" />
      <circle cx="289" cy="98" r="4.6" fill="none" stroke="#2fd6c3" strokeWidth="1.7" />
      <circle cx="289" cy="98" r="1.7" fill="#2fd6c3" />

      <g fill="#93a6c0" fontFamily="ui-sans-serif, system-ui, sans-serif" fontSize="10" opacity="0.6">
        <text x="12" y="56">A</text>
        <text x="12" y="76">B</text>
        <text x="12" y="116">A</text>
        <text x="12" y="136">B</text>
      </g>
    </svg>
  );
}

/* -------------------------------------------------------------------------------------------------
 * Subject-aware cover motifs — abstract, restrained, brand-coloured via `currentColor`.
 *
 * Three editorial geometries, chosen by subject family: a node/grid mesh for computing and
 * engineering, function curves over a faint grid for mathematics, and layered panels for
 * everything else. Pattern ids are per-instance (`useId`) so many covers can share the page.
 * ------------------------------------------------------------------------------------------------- */
type CoverFamily = "mesh" | "curves" | "panels";

function CoverMotif({
  family,
  className,
  style,
}: {
  family: CoverFamily;
  className?: string;
  style?: React.CSSProperties;
}) {
  const uid = React.useId().replace(/[^a-zA-Z0-9_-]/g, "");
  if (family === "curves") {
    return (
      <svg viewBox="0 0 320 180" preserveAspectRatio="xMidYMid slice" fill="none" aria-hidden className={className} style={style}>
        <defs>
          <pattern id={`g-${uid}`} width="26" height="26" patternUnits="userSpaceOnUse">
            <path d="M26 0H0V26" stroke="currentColor" strokeWidth="1" opacity="0.06" fill="none" />
          </pattern>
        </defs>
        <rect width="320" height="180" fill={`url(#g-${uid})`} />
        <path d="M-8 128 C60 60 110 60 168 104 S268 150 332 78" stroke="currentColor" strokeWidth="2.5" opacity="0.16" />
        <path d="M-8 150 C70 104 128 150 196 120 S300 92 332 118" stroke="currentColor" strokeWidth="2" opacity="0.1" />
      </svg>
    );
  }
  if (family === "mesh") {
    return (
      <svg viewBox="0 0 320 180" preserveAspectRatio="xMidYMid slice" fill="none" aria-hidden className={className} style={style}>
        <defs>
          <pattern id={`d-${uid}`} width="24" height="24" patternUnits="userSpaceOnUse">
            <circle cx="2" cy="2" r="1.5" fill="currentColor" opacity="0.14" />
          </pattern>
        </defs>
        <rect width="320" height="180" fill={`url(#d-${uid})`} />
        <g stroke="currentColor" strokeWidth="2" opacity="0.16">
          <path d="M54 120 L120 74 L186 104 L250 60" />
        </g>
        <g fill="currentColor" opacity="0.22">
          <circle cx="54" cy="120" r="4.5" />
          <circle cx="120" cy="74" r="4.5" />
          <circle cx="186" cy="104" r="4.5" />
          <circle cx="250" cy="60" r="4.5" />
        </g>
      </svg>
    );
  }
  return (
    <svg viewBox="0 0 320 180" preserveAspectRatio="xMidYMid slice" fill="none" aria-hidden className={className} style={style}>
      <g stroke="currentColor" opacity="0.16" strokeWidth="2">
        <rect x="42" y="52" width="150" height="96" rx="12" />
        <rect x="86" y="30" width="150" height="96" rx="12" opacity="0.7" />
      </g>
      <rect x="86" y="30" width="150" height="20" rx="10" fill="currentColor" opacity="0.08" />
    </svg>
  );
}

/* -------------------------------------------------------------------------------------------------
 * Cover derivation — pure, deterministic, brand-only.
 * ------------------------------------------------------------------------------------------------- */

// blue-600, blue-500, orange, ink-600 — from the Gradex ramp; every one stays pale once mixed into
// white on the cover, so the row never turns loud.
const COVER_ACCENTS = ["#1e4ed8", "#4f7cff", "#ff7e4d", "#4c5a6b"] as const;

function hashString(value: string): number {
  let hash = 0;
  for (let i = 0; i < value.length; i += 1) {
    hash = (hash * 31 + value.charCodeAt(i)) | 0;
  }
  return Math.abs(hash);
}

function coverAccent(course: PublicCourse): string {
  const seed = course.subject?.code || course.major?.label || course.id;
  return COVER_ACCENTS[hashString(seed) % COVER_ACCENTS.length];
}

/**
 * The motif family for a Course. Keyed off the Subject code prefix and the academic labels, in both
 * English and Arabic where a keyword exists; anything unrecognised falls to "panels", so the choice
 * is always deterministic and never depends on data that might be missing.
 */
function coverFamily(course: PublicCourse): CoverFamily {
  const hay = `${course.subject?.code ?? ""} ${course.major?.label ?? ""} ${course.subject?.label ?? ""}`.toLowerCase();
  if (/\b(math|stat|calc|phys)\b|رياض|تفاضل|تكامل|جبر|إحصا|احصا|فيزي/.test(hay)) return "curves";
  if (/\b(cs|ce|ee|comp|info|data|soft|elec)\b|حاسوب|حاسب|برمج|بيانات|كهرب|منطق|شبك/.test(hay)) return "mesh";
  return "panels";
}

/**
 * A two-to-three character monogram, from the Subject code's letters where there is one ("CE 342"
 * → "CE") and otherwise the initials of the most specific academic label. Unicode-aware, so an
 * Arabic catalogue yields an Arabic monogram; only ever a last-resort "GX", never an invented name.
 */
function monogram(course: PublicCourse): string {
  const code = course.subject?.code;
  const codeLetters = code?.match(/\p{L}+/u)?.[0];
  if (codeLetters) return codeLetters.slice(0, 3).toUpperCase();

  const label =
    course.major?.label || course.subject?.label || course.title || "";
  const words = label
    .replace(/[^\p{L}\p{N}]+/gu, " ")
    .trim()
    .split(/\s+/)
    .filter(Boolean);
  if (words.length === 0) return "GX";
  if (words.length === 1) return words[0].slice(0, 2).toUpperCase();
  return (words[0][0] + words[1][0]).toUpperCase();
}
