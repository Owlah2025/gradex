"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Bell } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { useLocale } from "@/lib/i18n/locale-provider";
import { deleteSession } from "@/lib/api/identity";
import { clearSession, currentCSRFToken } from "@/lib/identity/session";
import { useSessionView } from "@/lib/identity/use-session";
import { routes } from "./nav-items";
import { cn } from "@/lib/utils";

export type AuthState = "guest" | "authenticated";

interface AuthActionsProps {
  /**
   * Forces a state instead of reading the live session. The landing page uses
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
  const router = useRouter();
  const session = useSessionView();
  const [signingOut, setSigningOut] = React.useState(false);
  const resolved: AuthState = state ?? (session ? "authenticated" : "guest");

  async function signOut() {
    const csrf = currentCSRFToken();
    if (!csrf) return;
    setSigningOut(true);
    try {
      await deleteSession(csrf, locale);
    } catch {
      // Logout is best-effort from the browser's side. The server is
      // authoritative, and a failed call must still drop local state rather
      // than leave a signed-out-looking header holding a live CSRF token.
    } finally {
      clearSession();
      setSigningOut(false);
      router.push(`${routes.login}?reason=signed-out`);
    }
  }

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
        <Button asChild size={stacked ? "default" : "sm"} className={cn(stacked && "w-full")}>
          <Link href={routes.dashboard}>{t.nav.dashboard}</Link>
        </Button>
        <Button
          variant="outline"
          size={stacked ? "default" : "sm"}
          className={cn(stacked && "w-full")}
          onClick={signOut}
          disabled={signingOut}
        >
          {signingOut ? t.auth.session.signingOut : t.auth.session.signOut}
        </Button>
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
        <Link href={routes.courses}>{t.nav.browse}</Link>
      </Button>
    </div>
  );
}
