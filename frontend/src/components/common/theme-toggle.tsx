"use client";

import * as React from "react";
import { useTheme } from "next-themes";
import { Moon, Sun } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/lib/i18n/locale-provider";

export function ThemeToggle() {
  const { resolvedTheme, setTheme } = useTheme();
  const { t } = useLocale();
  const [mounted, setMounted] = React.useState(false);

  React.useEffect(() => setMounted(true), []);

  const isDark = resolvedTheme === "dark";

  return (
    <Button
      variant="outline"
      size="icon"
      aria-label={t.meta.themeToggleAria}
      aria-pressed={mounted ? isDark : undefined}
      onClick={() => setTheme(isDark ? "light" : "dark")}
    >
      {/* Both icons render; CSS swaps them so there's no hydration flash. */}
      <Sun className="size-5 rotate-0 scale-100 dark:-rotate-90 dark:scale-0" aria-hidden />
      <Moon className="absolute size-5 rotate-90 scale-0 dark:rotate-0 dark:scale-100" aria-hidden />
    </Button>
  );
}
