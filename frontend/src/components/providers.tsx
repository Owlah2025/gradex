"use client";

import * as React from "react";
import { ThemeProvider } from "next-themes";
import { LocaleProvider } from "@/lib/i18n/locale-provider";
import { SessionRehydrator } from "@/lib/identity/session-rehydrator";
import { PasswordChangeGuard } from "@/lib/identity/password-change-guard";

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
        {/* Runs after rehydration, so a reload onto a privileged surface with a
            restricted credential is redirected rather than left to 403. */}
        <PasswordChangeGuard />
        {children}
      </LocaleProvider>
    </ThemeProvider>
  );
}
