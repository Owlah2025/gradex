"use client";

import { useLocale } from "@/lib/i18n/locale-provider";

export function SkipLink() {
  const { t } = useLocale();
  return (
    <nav aria-label={t.meta.skipToContent}>
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:fixed focus:start-4 focus:top-4 focus:z-[100] focus:rounded-md focus:bg-background focus:px-4 focus:py-2 focus:font-display focus:font-bold focus:text-foreground focus:shadow-lg focus:ring-2 focus:ring-ring"
      >
        {t.meta.skipToContent}
      </a>
    </nav>
  );
}
