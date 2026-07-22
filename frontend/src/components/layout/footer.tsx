"use client";

import * as React from "react";
import Link from "next/link";
import { Instagram, MessageCircle, Twitter } from "lucide-react";
import { Container } from "./container";
import { BirdMark } from "@/components/brand/bird-mark";
import { Wordmark } from "@/components/brand/wordmark";
import { useLocale } from "@/lib/i18n/locale-provider";
import { siteConfig } from "@/config/site";
import { navItems } from "./nav-items";

export function Footer() {
  const { t } = useLocale();

  const companyLinks = [
    { href: "/about", label: t.footer.links.about },
    { href: "/teach", label: t.footer.links.teach },
    { href: "/contact", label: t.footer.links.contact },
  ];
  // Legal links required by the Kuwait Digital Commerce Law (see BUSINESS_RULES).
  const legalLinks = [
    { href: "/legal/terms", label: t.footer.links.terms },
    { href: "/legal/privacy", label: t.footer.links.privacy },
    { href: "/legal/refund", label: t.footer.links.refund },
  ];
  const socials = [
    { href: siteConfig.links.discord, label: t.footer.social.discord, Icon: MessageCircle },
    { href: siteConfig.links.x, label: t.footer.social.x, Icon: Twitter },
    { href: siteConfig.links.instagram, label: t.footer.social.instagram, Icon: Instagram },
  ];

  return (
    <footer className="bg-gx-navy text-white/70">
      <Container className="py-16">
        <div className="grid gap-10 md:grid-cols-2 lg:grid-cols-[1.6fr_1fr_1fr_1fr]">
          {/* Brand */}
          <div>
            <div className="flex items-center gap-2.5">
              <BirdMark className="size-7" />
              <Wordmark className="text-white" />
            </div>
            <p className="mt-3.5 max-w-sm leading-relaxed text-white/60">
              {t.footer.tagline}
            </p>
            <ul className="mt-5 flex gap-2.5">
              {socials.map(({ href, label, Icon }) => (
                <li key={label}>
                  <Link
                    href={href}
                    aria-label={label}
                    className="flex size-10 items-center justify-center rounded-pill border border-white/15 bg-white/5 text-white transition-colors hover:bg-white/10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/60"
                  >
                    <Icon className="size-5" aria-hidden />
                  </Link>
                </li>
              ))}
            </ul>
          </div>

          <FooterColumn title={t.footer.explore}>
            {navItems.map((item) => (
              <FooterLink key={item.href} href={item.href}>
                {item.label(t)}
              </FooterLink>
            ))}
          </FooterColumn>

          <FooterColumn title={t.footer.company}>
            {companyLinks.map((l) => (
              <FooterLink key={l.href} href={l.href}>
                {l.label}
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
