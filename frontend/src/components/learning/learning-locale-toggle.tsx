"use client";

import { usePathname, useRouter } from "next/navigation";
import { useLocale } from "@/lib/i18n/locale-provider";
import { localeSwitchLabel, type Locale } from "@/lib/i18n/config";
import { switchLocalePath } from "@/lib/i18n/locale-path";

export function LearningLocaleToggle({
  locale,
  label,
}: {
  locale: Locale;
  label: string;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const { setLocale } = useLocale();
  const nextLocale: Locale = locale === "ar" ? "en" : "ar";

  function switchLocale() {
    setLocale(nextLocale);
    // Learning routes are always locale-addressed, so the helper never returns null here; the
    // fallback keeps the control honest rather than navigating to a manufactured path.
    const search = typeof window === "undefined" ? "" : window.location.search;
    const target = switchLocalePath(pathname, search, nextLocale);
    if (target !== null) router.push(target);
  }

  return (
    <button
      type="button"
      onClick={switchLocale}
      aria-label={label}
      className="rounded-md border border-border px-3 py-2 font-display text-sm font-bold text-foreground transition-colors hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
    >
      <span dir="ltr">{localeSwitchLabel[locale]}</span>
    </button>
  );
}
