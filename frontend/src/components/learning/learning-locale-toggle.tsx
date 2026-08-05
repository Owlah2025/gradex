"use client";

import { usePathname, useRouter } from "next/navigation";
import { useLocale } from "@/lib/i18n/locale-provider";
import { localeSwitchLabel, type Locale } from "@/lib/i18n/config";

function withLocale(pathname: string | null, locale: Locale): string {
  const path = pathname || "/";
  const segments = path.split("/");
  if (segments[1] === "ar" || segments[1] === "en") {
    segments[1] = locale;
    return segments.join("/") || `/${locale}`;
  }
  return `/${locale}${path.startsWith("/") ? path : `/${path}`}`;
}

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
    router.push(withLocale(pathname, nextLocale));
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
