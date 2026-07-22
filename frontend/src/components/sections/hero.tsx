"use client";

import * as React from "react";
import Link from "next/link";
import { CheckSquare, Check, Languages, Wallet } from "lucide-react";
import { Container } from "@/components/layout/container";
import { Button } from "@/components/ui/button";
import { Eyebrow } from "@/components/ui/typography";
import { Scribble } from "@/components/brand/scribble";
import { BirdMark } from "@/components/brand/bird-mark";
import { useLocale } from "@/lib/i18n/locale-provider";
import { routes } from "@/components/layout/nav-items";

const TRUST_ICONS = [Languages, CheckSquare, Wallet, Check];

export function Hero() {
  const { t } = useLocale();

  return (
    <section
      aria-labelledby="hero-title"
      className="relative overflow-hidden bg-gx-navy text-white"
    >
      {/* Brand glow — one gradient moment per view. */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 bg-[radial-gradient(120%_90%_at_85%_10%,rgba(79,124,255,0.28),transparent_55%),radial-gradient(90%_80%_at_10%_100%,rgba(255,126,77,0.12),transparent_50%)]"
      />
      <Container className="relative grid items-center gap-10 py-16 md:py-24 lg:grid-cols-[1.05fr_0.95fr] lg:gap-14">
        <div>
          <Eyebrow className="text-gx-blue-200">{t.hero.eyebrow}</Eyebrow>
          <h1
            id="hero-title"
            className="mt-4 font-display text-[clamp(2.5rem,6vw,4.25rem)] font-extrabold leading-[1.1] text-white [text-wrap:balance]"
          >
            {t.hero.titleLead}{" "}
            <Scribble>{t.hero.titleAccent}</Scribble>
          </h1>
          <p className="mt-5 max-w-[32rem] text-[clamp(1.03rem,1.7vw,1.25rem)] leading-relaxed text-white/80">
            {t.hero.subtitle}
          </p>

          <div className="mt-8 flex flex-col gap-3.5 sm:flex-row">
            <Button asChild variant="accent" size="lg" className="max-sm:w-full">
              <Link href={routes.courses}>{t.nav.browse}</Link>
            </Button>
            <Button asChild variant="onDark" size="lg" className="max-sm:w-full">
              <Link href={routes.register}>{t.nav.register}</Link>
            </Button>
          </div>

          <ul
            aria-label={t.hero.trustAria}
            className="mt-9 flex flex-wrap gap-x-6 gap-y-2.5"
          >
            {t.hero.trust.map((item, i) => {
              const Icon = TRUST_ICONS[i] ?? Check;
              return (
                <li key={item} className="flex items-center gap-2 text-sm text-white/80">
                  <Icon className="size-[17px] text-gx-orange-200" aria-hidden />
                  {item}
                </li>
              );
            })}
          </ul>
        </div>

        {/* Visual: course-card mock + code island + ascending bird (no photos). */}
        <HeroVisual cardTitle={t.hero.cardTitle} cardMeta={t.hero.cardMeta} />
      </Container>
    </section>
  );
}

function HeroVisual({
  cardTitle,
  cardMeta,
}: {
  cardTitle: string;
  cardMeta: string;
}) {
  return (
    <div aria-hidden className="relative hidden h-[440px] sm:block">
      <div className="absolute start-0 top-6 w-[270px] rounded-lg bg-white p-4 text-gx-navy shadow-lg">
        <div className="flex h-[110px] items-end rounded-md bg-gradient-brand p-2.5">
          <span dir="ltr" className="rounded-sm bg-gx-navy/35 px-2 py-0.5 font-mono text-xs font-semibold text-white">
            CS 101
          </span>
        </div>
        <h4 className="mt-3 text-base font-bold">{cardTitle}</h4>
        <p className="mt-1 text-[13px] text-gx-ink-500" dir="auto">
          {cardMeta}
        </p>
        <p dir="ltr" className="mt-3 font-mono font-semibold text-gx-navy">
          38.000 KWD
        </p>
      </div>

      <div
        dir="ltr"
        className="absolute bottom-2 end-0 w-[250px] rounded-lg border border-white/10 bg-[#0b1622] p-4 text-start font-mono text-[12.5px] leading-relaxed shadow-lg"
      >
        <span className="text-gx-ink-400">{"// lab 03 — arrays"}</span>
        <br />
        <span className="text-gx-blue-300">function</span>{" "}
        <span className="text-[#8fd3b6]">average</span>(xs){" {"}
        <br />
        &nbsp;&nbsp;<span className="text-gx-blue-300">return</span> sum(xs) / xs.
        <span className="text-[#8fd3b6]">length</span>;
        <br />
        {"}"}
        <br />
        <span className="text-gx-ink-400">{"// grade: passed ✓"}</span>
      </div>

      <BirdMark className="absolute end-9 top-0 size-[120px] motion-safe:animate-bird-float" />
    </div>
  );
}
