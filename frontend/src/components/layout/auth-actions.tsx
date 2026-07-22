"use client";

import * as React from "react";
import Link from "next/link";
import { Bell } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { useLocale } from "@/lib/i18n/locale-provider";
import { routes } from "./nav-items";
import { cn } from "@/lib/utils";

export type AuthState = "guest" | "authenticated";

interface AuthActionsProps {
  state?: AuthState;
  /** Stacked, full-width layout for the mobile sheet. */
  stacked?: boolean;
}

/**
 * Header actions for both audiences (SCREENS.md → Landing supports guests and
 * returning users). Guests get log-in / register / browse; authenticated users
 * get notifications + dashboard + avatar.
 */
export function AuthActions({ state = "guest", stacked = false }: AuthActionsProps) {
  const { t } = useLocale();

  if (state === "authenticated") {
    return (
      <div
        className={cn(
          "flex items-center gap-2.5",
          stacked && "flex-col items-stretch",
        )}
      >
        {!stacked && (
          <Button variant="outline" size="icon" aria-label={t.nav.notifications}>
            <Bell className="size-5" aria-hidden />
          </Button>
        )}
        <Button asChild size={stacked ? "default" : "sm"} className={cn(stacked && "w-full")}>
          <Link href={routes.dashboard}>{t.nav.dashboard}</Link>
        </Button>
        {!stacked && (
          <Avatar size="sm" aria-hidden>
            <AvatarFallback>F</AvatarFallback>
          </Avatar>
        )}
      </div>
    );
  }

  return (
    <div
      className={cn(
        "flex items-center gap-2.5",
        stacked && "flex-col items-stretch",
      )}
    >
      <Button
        asChild
        variant="ghost"
        size={stacked ? "default" : "sm"}
        className={cn(stacked && "w-full")}
      >
        <Link href={routes.login}>{t.nav.login}</Link>
      </Button>
      <Button
        asChild
        variant="outline"
        size={stacked ? "default" : "sm"}
        className={cn(stacked && "w-full")}
      >
        <Link href={routes.register}>{t.nav.register}</Link>
      </Button>
      <Button
        asChild
        size={stacked ? "default" : "sm"}
        className={cn(stacked && "w-full")}
      >
        <Link href={routes.courses}>{t.nav.browse}</Link>
      </Button>
    </div>
  );
}
