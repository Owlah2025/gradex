"use client";

import * as React from "react";
import Link from "next/link";
import { Container } from "./container";
import { BirdMark } from "@/components/brand/bird-mark";
import { Wordmark } from "@/components/brand/wordmark";
import { usePathname } from "next/navigation";
import { useLocale } from "@/lib/i18n/locale-provider";
import { exploreNavigation } from "./nav-items";

export function Footer() {
  const { locale, t } = useLocale();
  const pathname = usePathname();
  // The footer is under the workspaces too, where the landing page's own
  // section anchors resolve to nothing.
  const explore = exploreNavigation(pathname ?? "/", locale);

  // LG-011 uses Terms §8 for the no-commerce launch disclosure; there is no
  // separate Refund Policy artifact in the approved package.
  const legalLinks = [
    { href: `/${locale}/terms`, label: t.footer.links.terms },
    { href: `/${locale}/privacy`, label: t.footer.links.privacy },
  ];
  return (
    <footer className="bg-gx-navy text-white/70">
      <Container className="py-16">
        <div className="grid gap-10 md:grid-cols-2 lg:grid-cols-[1.6fr_1fr_1fr]">
          {/* Brand */}
          <div>
            <div className="flex items-center gap-2.5">
              <BirdMark className="size-7" />
              <Wordmark className="text-white" />
            </div>
            <p className="mt-3.5 max-w-sm leading-relaxed text-white/60">
              {t.footer.tagline}
            </p>
          </div>

          <FooterColumn title={t.footer.explore}>
            {explore.map((item) => (
              <FooterLink key={item.href} href={item.href}>
                {item.label(t)}
              </FooterLink>
            ))}
          </FooterColumn>

          <FooterColumn title={t.footer.legal}>
            {legalLinks.map((l) => (
              <FooterLink key={l.href} href={l.href}>
                {l.label}
              </FooterLink>
            ))}
          </FooterColumn>
        </div>

        <div className="mt-11 flex flex-wrap justify-between gap-4 border-t border-white/10 pt-6 text-[13.5px] text-white/50">
          <span>{t.footer.copyright}</span>
          <span>{t.footer.pricingNote}</span>
        </div>
      </Container>
    </footer>
  );
}

function FooterColumn({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div>
      <h2 className="mb-3.5 font-display text-sm font-bold uppercase tracking-[0.06em] text-white">
        {title}
      </h2>
      <ul className="flex flex-col gap-2.5 text-[15px]">{children}</ul>
    </div>
  );
}

function FooterLink({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <li>
      <Link
        href={href}
        className="text-white/70 transition-colors hover:text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/60 focus-visible:ring-offset-2 focus-visible:ring-offset-gx-navy"
      >
        {children}
      </Link>
    </li>
  );
}
