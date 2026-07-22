"use client";

import * as React from "react";
import { Section, SectionHeader } from "@/components/layout/section";
import { Card } from "@/components/ui/card";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Reveal } from "@/components/common/reveal";
import { useLocale } from "@/lib/i18n/locale-provider";
import { testimonials } from "@/data/testimonials";

export function Testimonials() {
  const { t, locale } = useLocale();

  return (
    <Section id="testimonials" aria-labelledby="testimonials-title">
      <SectionHeader
        eyebrow={t.testimonials.eyebrow}
        title={t.testimonials.title}
        lead={t.testimonials.subtitle}
        headingId="testimonials-title"
      />

      <ul className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
        {testimonials.map((item, i) => (
          <Reveal as="li" key={item.id} delay={(i % 3) as 0 | 1 | 2}>
            <Card className="h-full p-6">
              <figure className="flex h-full flex-col">
                <blockquote className="text-base leading-relaxed text-foreground/90">
                  {item.quote[locale]}
                </blockquote>
                <figcaption className="mt-auto flex items-center gap-3 pt-5">
                  <Avatar aria-hidden>
                    <AvatarFallback>{item.initial}</AvatarFallback>
                  </Avatar>
                  <div>
                    <span className="block font-display text-[15px] font-bold text-foreground">
                      {item.name[locale]}
                    </span>
                    <span className="text-[13px] text-muted-foreground">
                      {item.meta[locale]}
                    </span>
                  </div>
                </figcaption>
              </figure>
            </Card>
          </Reveal>
        ))}
      </ul>
    </Section>
  );
}
