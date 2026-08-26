"use client";

import * as React from "react";
import Link from "next/link";
import { Bell } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { useLocale } from "@/lib/i18n/locale-provider";
import { useSessionView } from "@/lib/identity/use-session";
import { roleHomeNavigation } from "./role-workspace-navigation";
import { routes } from "./nav-items";
import { SignOutButton } from "./sign-out-button";
import { cn } from "@/lib/utils";

export type AuthState = "guest" | "authenticated";

interface AuthActionsProps {
  /**
   * Forces a state instead of reading the current session. The landing page uses
   * this to preview the returning-user header; leave it unset everywhere else
   * so the header follows the real session.
   */
  state?: AuthState;
  /** Stacked, full-width layout for the mobile sheet. */
  stacked?: boolean;
}

/**
 * Header actions for both audiences (SCREENS.md → Landing supports guests and
 * returning users). Guests get log-in / register / browse; authenticated users
 * get notifications + dashboard + sign out.
 */
export function AuthActions({ state, stacked = false }: AuthActionsProps) {
  const { locale, t } = useLocale();
  const session = useSessionView();
  const resolved: AuthState = state ?? (session ? "authenticated" : "guest");
  const authenticatedHome = session
    ? roleHomeNavigation(session.role, locale)
    : { key: "dashboard" as const, href: routes.dashboard(locale) };


  if (resolved === "authenticated") {
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
        {/* No workspace control at all when the session names no role this application knows.
            There is no honest destination to offer — every candidate either invents a role for the
            visitor or is a link the server refuses — and an anchor with no `href` is a control that
            looks operable and does nothing. Sign out stays, which is the action that applies. */}
        {authenticatedHome && (
          <Button asChild size={stacked ? "default" : "sm"} className={cn(stacked && "w-full")}>
            <Link href={authenticatedHome.href}>{t.nav[authenticatedHome.key]}</Link>
          </Button>
        )}
        <SignOutButton
          size={stacked ? "default" : "sm"}
          className={cn(stacked && "w-full")}
        />
        {!stacked && (
          <Avatar size="sm" aria-hidden>
            <AvatarFallback>
              {session?.display_name?.trim().charAt(0) || "F"}
            </AvatarFallback>
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
        <Link href={routes.catalogue(locale)}>{t.nav.browse}</Link>
      </Button>
    </div>
  );
}
