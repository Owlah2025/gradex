"use client";

import * as React from "react";
import Link from "next/link";
import { ArrowLeft, ArrowRight, BookOpen, Play } from "lucide-react";
import { Container } from "@/components/layout/container";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Scribble } from "@/components/brand/scribble";
import { HeroMedia } from "./hero-media";
import { HeroAcademicPrompt } from "@/components/academic/hero-academic-prompt";
import { useLandingJourney } from "@/components/landing/landing-journey";
import { useLocale } from "@/lib/i18n/locale-provider";
import { routes } from "@/components/layout/nav-items";
import { cn } from "@/lib/utils";
import { PERSONALIZE_ANCHOR } from "@/components/landing/anchors";

/**
 * The landing page's first three seconds.
 *
 * ## What was cut, and why
 *
 * The hero used to carry four trust pills, two equally weighted buttons and a subtitle naming three
 * separate product facts. Everything in it was true and none of it was the point: a student landing
 * here is deciding whether Gradex knows their university, and that decision is made by the headline
 * or not at all. So there is one heading, one supporting line, one dominant action, and the
 * university selector as the next step rather than a stack of competing facts.
 *
 * ## The composition, in both directions
 *
 * Copy on the inline-start side, media filling the inline-end half of the *viewport* — not a card
 * beside the text — and dissolving into the navy band as it approaches the words. The mask that
 * does the dissolving is bound to `dir` (see `.hero-media-mask`), so Arabic is a genuine mirror:
 * the copy sits right, the media fills the left, and the fade still runs towards the headline
 * rather than away from it.
 *
 * Below `lg` the media stops being a background and becomes an ordinary block under the copy, at a
 * height that leaves the headline owning the first screen. It is one element in one place in the
 * DOM at every breakpoint, so nothing is mounted twice and no video is ever decoded twice.
 */
export function Hero() {
  const { dir, t } = useLocale();
  const Arrow = dir === "rtl" ? ArrowLeft : ArrowRight;
  const journey = useLandingJourney();
  const titleLines = t.hero.title.split("\n");

  return (
    <section
      aria-labelledby="hero-title"
      className="relative isolate flex min-h-[calc(100svh-4rem)] flex-col overflow-hidden bg-gx-navy text-white"
    >
      {/* Brand glow — one gradient moment per view. */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 bg-[radial-gradient(120%_90%_at_85%_10%,rgba(79,124,255,0.28),transparent_55%),radial-gradient(90%_80%_at_10%_100%,rgba(255,126,77,0.12),transparent_50%)]"
      />

      <div className="pointer-events-none absolute inset-x-0 top-5 z-10 flex justify-center lg:top-8">
        <Badge
          variant="neutral"
          size="default"
          dir="ltr"
          className="pointer-events-auto gap-2 border border-white/15 bg-white/[0.08] px-3 py-1.5 text-[13px] text-white/80 shadow-none backdrop-blur-sm"
        >
          <span
            aria-hidden
            className="size-2 animate-pulse rounded-full bg-white shadow-[0_0_0_2px_rgba(255,126,77,0.35),0_0_8px_rgba(255,255,255,0.75)]"
          />
          <span dir={dir} className="whitespace-nowrap">
            {t.hero.launchBadge}
          </span>
          <span aria-hidden className="shrink-0 text-[13px] leading-none">
            🇰🇼
          </span>
        </Badge>
      </div>

      {/* Media. Absolute and full-bleed from `lg`; an ordinary block below it (see the DOM order —
          it follows the copy, so the small-screen stacking needs no reordering). */}
      <div className="hero-media-mask pointer-events-none order-2 h-[46svh] min-h-[280px] px-5 pb-12 sm:px-6 lg:absolute lg:inset-y-0 lg:end-0 lg:order-none lg:m-0 lg:h-auto lg:w-[54%] lg:p-0">
        <HeroMedia className="lg:absolute lg:inset-0" />
      </div>

      {/**
       * Centred, then lifted.
       *
       * The extra bottom padding is what raises the copy off the true vertical centre: the band
       * below it now carries the university strip, and a headline centred against the whole hero
       * sat lower than it looked like it should against the part of the hero that is actually
       * empty. It also opens the room the strip needed to come up out of the very bottom edge.
       */}
      {/**
       * A wider measure than the shared container, on very wide screens only.
       *
       * The hero is the one full-bleed band on the page — the media runs off the viewport edge —
       * so holding the copy to the 1200px column left it stranded in the middle with ~375px of
       * dead navy outboard of it at 1900px. The band gets its own measure so the headline sits
       * nearer the edge it belongs to, while every other section keeps the shared one.
       */}
      <Container className="relative z-10 order-1 flex flex-1 flex-col justify-center py-14 md:py-20 lg:py-20 lg:pb-44 2xl:max-w-[92rem]">
        {/* A wider measure on large screens: the headline sets to longer lines, which in Arabic
            carries it further into the band rather than stacking it against its own edge. */}
        <div className="relative max-w-[34rem] lg:max-w-[45rem]">
          <div
            aria-hidden
            className={cn(
              "pointer-events-none absolute -translate-x-5 -translate-y-2.5 top-0 z-0 flex size-10 sm:-translate-x-[170px] sm:-translate-y-[75px] sm:top-24 sm:size-14",
              dir === "rtl"
                ? "start-1 sm:-start-16 lg:-start-20"
                : "end-1 sm:-end-16 lg:-end-20",
            )}
          >
            <div
              className="flex size-10 items-center justify-center rounded-xl border border-white/15 bg-white/[0.08] text-gx-blue-200 shadow-[0_10px_24px_rgba(13,27,42,0.25)] backdrop-blur-sm motion-safe:animate-hero-float motion-reduce:animate-none sm:size-14"
            >
              <Play className="ms-0.5 size-5 fill-current sm:size-6" strokeWidth={1.8} />
            </div>
          </div>

          <div
            aria-hidden
            className={cn(
              "pointer-events-none absolute -bottom-16 -translate-x-2.5 translate-y-[90px] z-0 flex size-10 sm:-translate-x-[30px] sm:translate-y-[130px] sm:bottom-8 sm:size-14",
              dir === "rtl"
                ? "end-1 sm:-end-16 lg:-end-20"
                : "start-1 sm:-start-16 lg:-start-20",
            )}
          >
            <div
              className="flex size-10 items-center justify-center rounded-xl border border-white/15 bg-white/[0.08] text-gx-orange-200 shadow-[0_10px_24px_rgba(13,27,42,0.25)] backdrop-blur-sm motion-safe:animate-hero-float motion-reduce:animate-none sm:size-14"
              style={{ animationDelay: "-1.25s" }}
            >
              <BookOpen className="size-5 sm:size-6" strokeWidth={1.8} />
            </div>
          </div>

          {/* The real lockup keeps the brand present without adding another text label above the title. */}
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img
            src="/media/gradex-logo-dark.webp"
            alt="GradeX"
            loading="eager"
            fetchPriority="high"
            className="ms-0 me-auto mb-7 block -translate-y-2 aspect-[15/7] w-[160px] object-contain sm:w-[180px] lg:w-[232px]"
          />

          <h1
            id="hero-title"
            className={cn(
              "font-display text-[clamp(2.5rem,5.2vw,3.25rem)] font-extrabold tracking-[-0.02em] text-white [text-wrap:balance]",
              dir === "rtl" ? "leading-[1.42]" : "leading-[1.08]",
            )}
          >
            {titleLines.map((line, lineIndex) => (
              <React.Fragment key={line}>
                {lineIndex > 0 ? <br /> : null}
                {lineIndex === 1 ? (
                  <Scribble
                    className="max-w-full whitespace-normal underline decoration-gx-orange decoration-2 decoration-wavy underline-offset-4 sm:no-underline"
                  >
                    {line}
                  </Scribble>
                ) : (
                  line
                )}
              </React.Fragment>
            ))}
          </h1>

          <p className="mt-6 max-w-[30rem] text-[clamp(1.03rem,1.7vw,1.2rem)] leading-relaxed text-white/80">
            {t.hero.subtitle}
          </p>

          <div className="mt-9 flex flex-col gap-3.5 sm:flex-row sm:items-center">
            {/* A plain anchor, not a route. The personalization it opens is on this page, so this
                has to be an in-page jump — and as an `href` it keeps working before hydration and
                honours the reader's own scroll-behaviour preference. */}
            <Button asChild variant="accent" size="lg" className="max-sm:w-full">
              <a href={`#${PERSONALIZE_ANCHOR}`}>
                {t.hero.primaryCta}
                <Arrow aria-hidden />
              </a>
            </Button>
            <Button asChild variant="onDark" size="lg" className="max-sm:w-full">
              <Link href={routes.register}>{t.hero.secondaryCta}</Link>
            </Button>
          </div>
        </div>
      </Container>

      {/**
       * The academic onboarding, layered over the band.
       *
       * A sibling of the copy rather than a child of it, and absolutely positioned inside this
       * section: the trigger sits at the hero's bottom edge, centred in both writing directions,
       * and the card it opens floats over the whole band. Nothing it renders is in normal flow, so
       * the hero's height and every line inside it are identical whether the card is closed, open
       * or resolved.
       */}
      <HeroAcademicPrompt onResolved={() => journey?.scrollToCourses()} />
    </section>
  );
}
