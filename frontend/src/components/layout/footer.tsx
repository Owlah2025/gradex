"use client";

import * as React from "react";
import Link from "next/link";
import { Container } from "./container";
import { BirdMark } from "@/components/brand/bird-mark";
import { Wordmark } from "@/components/brand/wordmark";
import { StudentLogo } from "@/components/brand/logo";
import { usePathname } from "next/navigation";
import { useLocale } from "@/lib/i18n/locale-provider";
import { exploreNavigation, isWorkspacePath } from "./nav-items";

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
            {/* The same real lockup the rest of the Student surfaces carry.
                Reconstructing it here was visibly a different mark from the one
                in the header directly above it — and in Arabic the bird mirrored
                to the far side of the wordmark, which the real lockup, being one
                image, cannot do.

                Always the dark-background asset: this footer paints its own navy
                and stays navy in light mode, so following the theme would put the
                light-ground logo on a dark ground half the time.

                Gated on the surface for the same reason the header is — the
                workspaces keep the branding they had. */}
            {isWorkspacePath(pathname ?? "/") ? (
              <div className="flex items-center gap-2.5">
                <BirdMark className="size-7" />
                <Wordmark className="text-white" />
              </div>
            ) : (
              <StudentLogo
                surface="dark"
                ariaLabel={t.meta.logoHomeAria}
                // Larger than the header's, and without the optical nudge that
                // aligns it to a 64px bar it is not in.
                imageClassName="w-[124px] translate-x-0 translate-y-0 sm:w-[132px] rtl:translate-x-0"
              />
            )}
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
          {/* Both lines mix scripts — the Arabic copyright carries "© 2026
              Gradex" and the pricing note names KWD — and an unisolated Latin
              run inside an Arabic line has its trailing punctuation resolved
              against the line rather than against the run. That is what rendered
              the copyright as ".Gradex 2026 ©". Isolating each line lets the
              embedded run keep its own punctuation without altering a word of
              either translation. */}
          <bdi>{t.footer.copyright}</bdi>
          <bdi>{t.footer.pricingNote}</bdi>
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
