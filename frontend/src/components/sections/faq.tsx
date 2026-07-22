"use client";

import * as React from "react";
import { Section, SectionHeader } from "@/components/layout/section";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import { Reveal } from "@/components/common/reveal";
import { useLocale } from "@/lib/i18n/locale-provider";
import { faqItems } from "@/data/faq";

export function Faq() {
  const { t, locale } = useLocale();

  return (
    <Section id="faq" tone="muted" aria-labelledby="faq-title">
      <SectionHeader
        eyebrow={t.faq.eyebrow}
        title={t.faq.title}
        headingId="faq-title"
        align="center"
      />

      <Reveal className="mx-auto max-w-3xl">
        <Accordion
          type="single"
          collapsible
          defaultValue={faqItems[0]?.id}
          className="flex flex-col gap-3"
        >
          {faqItems.map((item) => (
            <AccordionItem key={item.id} value={item.id}>
              <AccordionTrigger>{item.question[locale]}</AccordionTrigger>
              <AccordionContent>{item.answer[locale]}</AccordionContent>
            </AccordionItem>
          ))}
        </Accordion>
      </Reveal>
    </Section>
  );
}
