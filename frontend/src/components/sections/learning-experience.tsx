"use client";

import * as React from "react";
import { ListChecks, MonitorPlay, FileText } from "lucide-react";
import { Section, SectionHeader } from "@/components/layout/section";
import { Reveal } from "@/components/common/reveal";
import { useLocale } from "@/lib/i18n/locale-provider";
import { cn } from "@/lib/utils";

const STEP_ICONS = [MonitorPlay, FileText, ListChecks];

/**
 * The numbered section follows the three supported learning steps without
 * advertising a deferred community feature.
 */
export function LearningExperience() {
  const { t } = useLocale();

  return (
    <Section id="learn" aria-labelledby="learn-title">
      <SectionHeader
        eyebrow={t.learn.eyebrow}
        title={t.learn.title}
        lead={t.learn.subtitle}
        headingId="learn-title"
      />

      <ol className="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
        {t.learn.steps.map((step, i) => {
          const Icon = STEP_ICONS[i] ?? MonitorPlay;
          return (
            <Reveal as="li" key={step.title} delay={(i % 3) as 0 | 1 | 2}>
              <div className="relative">
                <span
                  dir="ltr"
                  className="font-mono text-[13px] font-semibold tracking-wide text-primary"
                >
                  {String(i + 1).padStart(2, "0")}
                </span>
                <span
                  className={cn(
                    "mb-4 mt-3.5 flex size-12 items-center justify-center rounded-md",
                    "bg-gx-blue-50 text-gx-blue-600",
                  )}
                >
                  <Icon className="size-6" aria-hidden />
                </span>
                <h3 className="font-display text-[19px] font-bold text-foreground">
                  {step.title}
                </h3>
                <p className="mt-2 text-[15px] leading-relaxed text-muted-foreground">
                  {step.body}
                </p>
              </div>
            </Reveal>
          );
        })}
      </ol>
    </Section>
  );
}
