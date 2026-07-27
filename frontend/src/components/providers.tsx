"use client";

import * as React from "react";
import { ThemeProvider } from "next-themes";
import { LocaleProvider } from "@/lib/i18n/locale-provider";
import { SessionRehydrator } from "@/lib/identity/session-rehydrator";

/** App-wide client providers: colour theme (next-themes) + locale/direction. */
export function Providers({ children }: { children: React.ReactNode }) {
  return (
    <ThemeProvider
      attribute="class"
      defaultTheme="light"
      enableSystem
      disableTransitionOnChange
    >
      <LocaleProvider>
        <SessionRehydrator />
        {children}
      </LocaleProvider>
    </ThemeProvider>
  );
}
