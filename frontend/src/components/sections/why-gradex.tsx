"use client";

import * as React from "react";
import { Bird, ShieldCheck, Users, Wallet } from "lucide-react";
import { Section, SectionHeader } from "@/components/layout/section";
import { Card } from "@/components/ui/card";
import { Reveal } from "@/components/common/reveal";
import { useLocale } from "@/lib/i18n/locale-provider";

const PILLAR_ICONS = [Bird, Users, ShieldCheck];

export function WhyGradex() {
  const { t } = useLocale();

  return (
    <Section id="why" tone="muted" aria-labelledby="why-title">
      <SectionHeader
        eyebrow={t.why.eyebrow}
        title={t.why.title}
        headingId="why-title"
        align="center"
      />

      <ul className="grid gap-6 md:grid-cols-3">
        {t.why.pillars.map((pillar, i) => {
          const Icon = PILLAR_ICONS[i] ?? Bird;
          return (
            <Reveal as="li" key={pillar.title} delay={(i % 3) as 0 | 1 | 2}>
              <Card className="h-full p-6">
                <span className="flex size-12 items-center justify-center rounded-md bg-gx-blue-50 text-gx-blue-600">
                  <Icon className="size-6" aria-hidden />
                </span>
                <h3 className="mt-4 font-display text-[19px] font-bold text-foreground">
                  {pillar.title}
                </h3>
                <p className="mt-2 leading-relaxed text-muted-foreground">
                  {pillar.body}
                </p>
              </Card>
            </Reveal>
          );
        })}
      </ul>

      <p className="mx-auto mt-8 flex max-w-2xl items-center justify-center gap-3 text-center text-[14.5px] text-muted-foreground">
        <Wallet className="size-[18px] shrink-0 text-gx-orange" aria-hidden />
        {t.why.note}
      </p>
    </Section>
  );
}
