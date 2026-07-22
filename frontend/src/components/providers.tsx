"use client";

import * as React from "react";
import { ThemeProvider } from "next-themes";
import { LocaleProvider } from "@/lib/i18n/locale-provider";

/** App-wide client providers: colour theme (next-themes) + locale/direction. */
export function Providers({ children }: { children: React.ReactNode }) {
  return (
    <ThemeProvider
      attribute="class"
      defaultTheme="light"
      enableSystem
      disableTransitionOnChange
    >
      <LocaleProvider>{children}</LocaleProvider>
    </ThemeProvider>
  );
}
