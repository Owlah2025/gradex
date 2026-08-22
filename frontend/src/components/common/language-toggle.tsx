"use client";

import * as React from "react";
import { usePathname, useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/lib/i18n/locale-provider";
import { localeSwitchLabel } from "@/lib/i18n/config";
import { switchLocalePath } from "@/lib/i18n/locale-path";

/**
 * The one language switch for the whole product.
 *
 * On a locale-addressed route (`/[locale]/…`) the URL is part of the application's state, so
 * switching language performs real navigation to the equivalent path. Mutating `lang`/`dir` while
 * the URL still says `/ar/…` leaves the document disagreeing with its own address: a reload, a
 * shared link, or a Back press all return the visitor to the previous language.
 *
 * Routes that carry no locale segment — the landing page, the auth screens, `/staff` — have no
 * locale-addressed equivalent (there is no `/[locale]/page.tsx`), so the saved preference is
 * switched in place rather than navigating somewhere that would 404.
 */
export function LanguageToggle() {
  const { locale, setLocale, toggleLocale, t } = useLocale();
  const router = useRouter();
  const pathname = usePathname();

  function switchLanguage() {
    const next = locale === "ar" ? "en" : "ar";
    // Read the query at click time rather than through `useSearchParams`, which would force every
    // page carrying the header into dynamic rendering for a value only this handler needs.
    const search = typeof window === "undefined" ? "" : window.location.search;
    const target = switchLocalePath(pathname, search, next);

    if (target === null) {
      toggleLocale();
      return;
    }

    setLocale(next);
    router.push(target);
  }

  return (
    <Button
      variant="outline"
      size="icon"
      onClick={switchLanguage}
      aria-label={t.meta.switchToAria}
      className="font-display text-[13px] font-bold"
    >
      <span dir="ltr">{localeSwitchLabel[locale]}</span>
    </Button>
  );
}
