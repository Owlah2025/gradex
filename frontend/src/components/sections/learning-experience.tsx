"use client";

import * as React from "react";
import { ListChecks, MonitorPlay, FileText, Users } from "lucide-react";
import { Section, SectionHeader } from "@/components/layout/section";
import { Reveal } from "@/components/common/reveal";
import { useLocale } from "@/lib/i18n/locale-provider";
import { cn } from "@/lib/utils";

const STEP_ICONS = [MonitorPlay, FileText, ListChecks, Users];

/**
 * The only numbered section — the steps are a genuine sequence, so 01–04 carry
 * real meaning. Step 4 (community follow-up) tints orange to land the payoff.
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

      <ol className="grid gap-6 sm:grid-cols-2 lg:grid-cols-4">
        {t.learn.steps.map((step, i) => {
          const Icon = STEP_ICONS[i] ?? MonitorPlay;
          const isLast = i === t.learn.steps.length - 1;
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
                    isLast
                      ? "bg-gx-orange-50 text-gx-orange-700"
                      : "bg-gx-blue-50 text-gx-blue-600",
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
