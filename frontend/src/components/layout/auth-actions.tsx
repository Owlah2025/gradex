"use client";

import * as React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { useLocale } from "@/lib/i18n/locale-provider";
import { useSessionView } from "@/lib/identity/use-session";
import { roleHomeNavigation } from "./role-workspace-navigation";
import { routes } from "./nav-items";
import { SignOutButton } from "./sign-out-button";
import { PERSONALIZE_ANCHOR } from "@/components/landing/anchors";
import { cn } from "@/lib/utils";

interface AuthActionsProps {
  /** Stacked, full-width layout for the mobile sheet. */
  stacked?: boolean;
}

/**
 * Header actions for both audiences (SCREENS.md → Landing supports guests and
 * returning users). Guests get log-in / register / browse; authenticated users
 * get notifications + dashboard + sign out.
 */
export function AuthActions({ stacked = false }: AuthActionsProps) {
  const { locale, t } = useLocale();
  const session = useSessionView();
  const pathname = usePathname();
  // There used to be a `state` prop that forced "authenticated" without a
  // session, for a landing-page preview of the returning-user header. No caller
  // ever passed it, and its fallback pointed at the Student dashboard — naming a
  // role for a principal that had not been read at all.
  const authenticatedHome = session
    ? roleHomeNavigation(session.role, locale)
    : null;

  if (session) {
    return (
      <div
        className={cn(
          "flex items-center gap-2.5",
          stacked && "flex-col items-stretch",
        )}
      >
        {/* There was a notifications button here. It had no href, no handler
            and no feature behind it: a control that looked operable, was
            reachable by keyboard, was announced as a button, and did nothing
            when pressed. The Student learning frame had already refused to
            carry it for exactly that reason. */}
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

  /**
   * One dominant action, and it is the journey rather than a destination.
   *
   * The guest header used to carry three equally sized buttons — sign in, create account, browse —
   * next to a language switch and a theme switch, which is five controls and no answer to "what do
   * I do here". Now there is one primary: find my courses. On the landing page that is the
   * personalization the page is built around, so it is an in-page jump; everywhere else the
   * catalogue is the honest equivalent, and it stays a real route.
   *
   * Creating an account survives at the width that has room for it. Below `lg` this component is
   * the sheet's account block and can afford three stacked controls; the wide bar cannot afford a
   * third button beside the one that matters, and registration is offered by the sign-in screen,
   * by Course Details and by checkout — every point where it is actually the next step.
   */
  const onLanding = pathname === "/";
  const findCoursesHref = onLanding
    ? `#${PERSONALIZE_ANCHOR}`
    : routes.catalogue(locale);

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
      {stacked && (
        <Button asChild variant="outline" size="default" className="w-full">
          <Link href={routes.register}>{t.nav.register}</Link>
        </Button>
      )}
      <Button
        asChild
        size={stacked ? "default" : "sm"}
        className={cn(stacked && "w-full")}
      >
        {onLanding ? (
          // An in-page anchor, so it works before hydration and honours the reader's own
          // scroll-behaviour preference rather than scripting the scroll.
          <a href={findCoursesHref}>{t.nav.findMyCourses}</a>
        ) : (
          <Link href={findCoursesHref}>{t.nav.findMyCourses}</Link>
        )}
      </Button>
    </div>
  );
}
