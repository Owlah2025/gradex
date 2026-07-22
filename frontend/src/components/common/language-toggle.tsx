"use client";

import * as React from "react";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/lib/i18n/locale-provider";
import { localeSwitchLabel } from "@/lib/i18n/config";

export function LanguageToggle() {
  const { locale, toggleLocale, t } = useLocale();

  return (
    <Button
      variant="outline"
      size="icon"
      onClick={toggleLocale}
      aria-label={t.meta.switchToAria}
      className="font-display text-[13px] font-bold"
    >
      <span dir="ltr">{localeSwitchLabel[locale]}</span>
    </Button>
  );
}
