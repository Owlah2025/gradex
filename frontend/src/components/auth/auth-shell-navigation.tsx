"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { routes } from "@/components/layout/nav-items";
import { safeReturnTo } from "@/lib/identity/return-to";
import { useLocale } from "@/lib/i18n/locale-provider";

/**
 * The way out of an admission screen.
 *
 * These screens had exactly one exit — a link to the catalogue — so a visitor
 * who opened sign-in by mistake, or who wanted to look at the Course again
 * before creating an account, had no offered route back to either the start of
 * the product or the page they came from.
 *
 * The "back to the course" link is offered only when the journey actually came
 * from one. `safeReturnTo` revalidates the destination here rather than
 * trusting that an earlier screen checked it: every hop is an entry point, and
 * a value that could leave this origin is dropped rather than rendered as a
 * link the visitor is invited to follow.
 */
export function AuthShellNavigation() {
  const { locale, t } = useLocale();
  const searchParams = useSearchParams();
  const destination = safeReturnTo(searchParams.get("returnTo"));

  return (
    <nav
      aria-label={t.nav.primaryNavigation}
      data-testid="auth-shell-navigation"
      className="flex flex-wrap items-center gap-x-4 gap-y-2"
    >
      {destination ? (
        <Link
          className="font-semibold hover:text-foreground"
          href={destination}
          data-testid="auth-back-to-course"
        >
          {t.auth.common.backToCourse}
        </Link>
      ) : null}
      <Link
        className="font-semibold hover:text-foreground"
        href={routes.home(locale)}
        data-testid="auth-home"
      >
        {t.auth.common.homeLink}
      </Link>
      {/* The copy says courses, so the link goes to the courses. It used to
          point at the landing page, which is a different promise. */}
      <Link
        className="font-semibold hover:text-foreground"
        href={routes.catalogue(locale)}
        data-testid="auth-catalogue"
      >
        {t.auth.common.backHome}
      </Link>
    </nav>
  );
}
