"use client";

import * as React from "react";
import Link from "next/link";
import { Logo } from "@/components/brand/logo";
import { ThemeToggle } from "@/components/common/theme-toggle";
import { LanguageToggle } from "@/components/common/language-toggle";
import { usePathname } from "next/navigation";
import { AuthActions } from "./auth-actions";
import { MobileNav } from "./mobile-nav";
import { primaryNavigation } from "./nav-items";
import { useLocale } from "@/lib/i18n/locale-provider";

/**
 * Sticky, frosted 64px header. Primary nav collapses into a sheet below lg;
 * theme + language toggles stay visible at every breakpoint.
 */
export function Navbar() {
  const { locale, t } = useLocale();
  const pathname = usePathname();
  // The header is shared by the landing page, the public catalogue, Course
  // Details and every workspace. What counts as primary navigation is not the
  // same on all of them.
  const primary = primaryNavigation(pathname ?? "/", locale);

  return (
    <header className="sticky top-0 z-50 h-16 border-b border-border bg-background/85 backdrop-blur-md supports-[backdrop-filter]:bg-background/70">
      <div className="mx-auto flex h-full max-w-container items-center gap-5 px-5 sm:px-6">
        <Logo ariaLabel={t.meta.logoHomeAria} />

        <nav
          aria-label={t.nav.primaryNavigation}
          className="ms-2 hidden lg:flex lg:items-center lg:gap-1"
        >
          {primary.map((item) => (
            <Link
              key={item.href}
              href={item.href}
              className="rounded-md px-3 py-2 font-display text-[15px] font-semibold text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
            >
              {item.label(t)}
            </Link>
          ))}
        </nav>

        <div className="ms-auto flex items-center gap-2.5">
          <LanguageToggle />
          <ThemeToggle />
          <div className="hidden lg:block">
            <AuthActions />
          </div>
          <MobileNav />
        </div>
      </div>
    </header>
  );
}
