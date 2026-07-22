"use client";

import * as React from "react";
import Link from "next/link";
import { Section } from "@/components/layout/section";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Eyebrow, SectionHeading, Prose } from "@/components/ui/typography";
import { Reveal } from "@/components/common/reveal";
import { useLocale } from "@/lib/i18n/locale-provider";
import { routes } from "@/components/layout/nav-items";

export function InstructorSpotlight() {
  const { t } = useLocale();
  const i = t.instructor;

  return (
    <Section id="instructors" tone="muted" aria-labelledby="instructors-title">
      <div className="grid items-center gap-8 lg:grid-cols-[0.9fr_1.1fr] lg:gap-12">
        <Reveal>
          <Card className="p-6">
            <div className="flex items-center gap-4">
              <Avatar size="lg" aria-hidden>
                <AvatarFallback>S</AvatarFallback>
              </Avatar>
              <div>
                <p className="font-display text-xl font-bold text-foreground">{i.name}</p>
                <p className="text-sm text-muted-foreground">{i.role}</p>
              </div>
            </div>

            <blockquote className="mt-5 text-[17px] leading-relaxed text-foreground/90">
              {i.quote}
            </blockquote>

            <ul className="mt-5 flex flex-wrap gap-2">
              {i.creds.map((c) => (
                <li key={c}>
                  <Badge>{c}</Badge>
                </li>
              ))}
            </ul>

            <dl className="mt-5 flex gap-7 border-t border-border pt-5">
              {i.stats.map((s) => (
                <div key={s.label}>
                  <dt className="sr-only">{s.label}</dt>
                  <dd>
                    <span dir="ltr" className="block font-display text-2xl font-extrabold text-foreground">
                      {s.value}
                    </span>
                    <span className="text-[13px] text-muted-foreground">{s.label}</span>
                  </dd>
                </div>
              ))}
            </dl>
          </Card>
        </Reveal>

        <Reveal delay={1}>
          <Eyebrow>{i.eyebrow}</Eyebrow>
          <SectionHeading id="instructors-title" className="mt-3">
            {i.title}
          </SectionHeading>
          <Prose className="mt-4">{i.body1}</Prose>
          <Prose className="mt-4">{i.body2}</Prose>
          <Button asChild className="mt-6">
            <Link href={routes.courses}>{i.cta}</Link>
          </Button>
        </Reveal>
      </div>
    </Section>
  );
}
