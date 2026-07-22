"use client";

import * as React from "react";
import Link from "next/link";
import { Container } from "@/components/layout/container";
import { Button } from "@/components/ui/button";
import { Reveal } from "@/components/common/reveal";
import { useLocale } from "@/lib/i18n/locale-provider";
import { routes } from "@/components/layout/nav-items";

export function FinalCta() {
  const { t } = useLocale();

  return (
    <section
      aria-labelledby="final-cta-title"
      className="relative overflow-hidden bg-gx-navy text-white"
    >
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 bg-[radial-gradient(80%_120%_at_50%_-10%,rgba(79,124,255,0.28),transparent_60%)]"
      />
      <Container className="relative py-16 md:py-20 lg:py-24">
        <Reveal className="mx-auto max-w-2xl text-center">
          <h2
            id="final-cta-title"
            className="font-display text-[clamp(1.875rem,4vw,2.875rem)] font-bold text-white [text-wrap:balance]"
          >
            {t.finalCta.title}
          </h2>
          <p className="mx-auto mt-4 max-w-xl text-lg text-white/80">
            {t.finalCta.body}
          </p>
          <div className="mt-8 flex flex-col justify-center gap-3.5 sm:flex-row">
            <Button asChild variant="accent" size="lg" className="max-sm:w-full">
              <Link href={routes.courses}>{t.finalCta.browse}</Link>
            </Button>
            <Button asChild variant="onDark" size="lg" className="max-sm:w-full">
              <Link href={routes.register}>{t.finalCta.register}</Link>
            </Button>
          </div>
        </Reveal>
      </Container>
    </section>
  );
}
