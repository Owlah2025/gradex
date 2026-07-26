"use client";

import * as React from "react";
import Link from "next/link";
import { Logo } from "@/components/brand/logo";
import { ThemeToggle } from "@/components/common/theme-toggle";
import { LanguageToggle } from "@/components/common/language-toggle";
import { AuthActions, type AuthState } from "./auth-actions";
import { MobileNav } from "./mobile-nav";
import { navItems } from "./nav-items";
import { useLocale } from "@/lib/i18n/locale-provider";

/**
 * Sticky, frosted 64px header. Primary nav collapses into a sheet below lg;
 * theme + language toggles stay visible at every breakpoint.
 */
export function Navbar({ authState = "guest" }: { authState?: AuthState }) {
  const { t } = useLocale();

  return (
    <header className="sticky top-0 z-50 h-16 border-b border-border bg-background/85 backdrop-blur-md supports-[backdrop-filter]:bg-background/70">
      <div className="mx-auto flex h-full max-w-container items-center gap-5 px-5 sm:px-6">
        <Logo ariaLabel={t.meta.logoHomeAria} />

        <nav aria-label="Primary" className="ms-2 hidden lg:flex lg:items-center lg:gap-1">
          {navItems.map((item) => (
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
            <AuthActions state={authState} />
          </div>
          <MobileNav authState={authState} />
        </div>
      </div>
    </header>
  );
}
