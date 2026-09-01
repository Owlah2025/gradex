"use client";

import * as React from "react";
import Link from "next/link";
import { Menu } from "lucide-react";
import { Logo } from "@/components/brand/logo";
import { LanguageToggle } from "@/components/common/language-toggle";
import { ThemeToggle } from "@/components/common/theme-toggle";
import { SignOutButton } from "@/components/layout/sign-out-button";
import { Button } from "@/components/ui/button";
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import type { ShellLabels } from "./learning-label-sets";

/**
 * The frame every Student learning screen sits in.
 *
 * # WHY NOT THE PUBLIC HEADER
 *
 * `Navbar` is the site header, and its primary navigation is the landing page's own anchors —
 * `#courses`, `#why`, `#faq`. Rendered over `/learn/...` those resolve to nothing: three controls
 * that look like navigation and move the reader nowhere. It also carries a notifications control
 * this product does not yet implement. A Student inside a Course needs the opposite set: the way
 * back to their Courses, the language and theme they read in, and the way out of the session.
 *
 * # WHY NOT THE WORKSPACE FRAME EITHER
 *
 * `WorkspacePage` says so itself — an operational workspace and a reading surface want different
 * measures. Admin and Instructor screens are read for their rows; a Lesson is read for its content.
 *
 * So this is a third frame, built from the same Tranche B parts (`Logo`, `LanguageToggle`,
 * `ThemeToggle`, `SignOutButton`, `Sheet`, `Button`) so it stays the same product. It introduces no
 * navigation of its own beyond the route back to My courses, because everything else a Student
 * needs is inside the screen they are already on.
 *
 * The marketing footer is deliberately absent from all three learning screens: a Student who is
 * signed in and working through a Course is not being persuaded to sign up.
 */
export function LearningShell({
  locale,
  dir,
  labels,
  children,
}: {
  locale: "ar" | "en";
  dir: "ltr" | "rtl";
  labels: ShellLabels;
  children: React.ReactNode;
}) {
  const [menuOpen, setMenuOpen] = React.useState(false);
  const dashboardHref = `/${locale}/learn/dashboard`;
  // A Student inside a Lesson could reach their own Courses and nothing else.
  // Finding another Course meant editing the address bar or leaving through the
  // browser's history, so the two destinations that were missing are here: the
  // catalogue, and the start of the product.
  //
  // The landing page is not locale-addressed — there is no `/[locale]/page.tsx`
  // — so its language comes from the preference every `/[locale]/…` visit
  // persists, which is exactly what got the reader here.
  const learningNavigation: Array<{ href: string; label: string }> = [
    { href: dashboardHref, label: labels.myCourses },
    { href: `/${locale}/catalog`, label: labels.catalogue },
    { href: "/", label: labels.home },
  ];

  return (
    <div dir={dir} className="flex min-h-screen flex-col bg-background">
      <header className="sticky top-0 z-40 border-b border-border bg-background/90 backdrop-blur-md supports-[backdrop-filter]:bg-background/75">
        <div className="mx-auto flex h-16 max-w-container items-center gap-3 px-5 sm:px-6">
          <Logo href={dashboardHref} className="shrink-0" />

          {/* The Course used to be named here as a link back to it. The Lesson
              screen now carries a breadcrumb that names the Course, the Lesson,
              and the way up to My Learning, so this was a second link with the
              same accessible name and the same destination on one screen. */}

          <div className="ms-auto flex shrink-0 items-center gap-2">
            {/* Three destinations where there was one, so the row now has to
                satisfy the target-size rule it previously met by having almost
                nothing in it: each link is a full 44px target and the gap
                between adjacent targets is wide enough that axe does not read
                them as obscuring one another. */}
            <nav
              aria-label={labels.learningNavigation}
              data-testid="learning-navigation"
              className="hidden items-center gap-2 md:flex"
            >
              {learningNavigation.map((item) => (
                <Link
                  key={item.href}
                  href={item.href}
                  className="inline-flex min-h-11 items-center whitespace-nowrap rounded-md px-3 font-display text-[15px] font-semibold text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  {item.label}
                </Link>
              ))}
            </nav>
            <LanguageToggle />
            <ThemeToggle />
            {/* Sign out, and nothing else. The site header also offers notifications and a route
                to "your dashboard"; on this frame the first is a control the product does not yet
                implement and the second is the logo and the link beside it. */}
            <div className="hidden md:block">
              <SignOutButton />
            </div>

            {/* Below `md` the account controls move into a sheet rather than shrinking: three
                buttons and an avatar do not fit beside a logo at 390px without one of them
                becoming an unlabelled icon. */}
            <Sheet open={menuOpen} onOpenChange={setMenuOpen}>
              <SheetTrigger asChild>
                <Button variant="outline" size="icon" className="md:hidden" aria-label={labels.openMenu}>
                  <Menu className="size-5" aria-hidden />
                </Button>
              </SheetTrigger>
              <SheetContent side="right" closeLabel={labels.closeMenu}>
                <SheetTitle className="sr-only">{labels.learningNavigation}</SheetTitle>
                {/* Parity with the wide header. Below `md` this sheet is the only
                    navigation there is, so anything offered above and not here is
                    unreachable on a phone. */}
                <nav
                  aria-label={labels.learningNavigation}
                  data-testid="learning-navigation-mobile"
                  className="mt-8 flex flex-col gap-1"
                >
                  {learningNavigation.map((item) => (
                    <SheetClose asChild key={item.href}>
                      <Link
                        href={item.href}
                        className="flex min-h-11 items-center rounded-md px-2.5 py-3 font-display text-[17px] font-semibold text-foreground hover:bg-accent"
                      >
                        {item.label}
                      </Link>
                    </SheetClose>
                  ))}
                </nav>
                <div className="my-4 h-px bg-border" />
                <div onClick={() => setMenuOpen(false)}>
                  <SignOutButton size="default" className="w-full" />
                </div>
              </SheetContent>
            </Sheet>
          </div>
        </div>
      </header>

      {/* The direction is declared on the reading surface as well as on the frame. `main` is what
          the learning suites read it from, and it is the element whose contents the value is
          actually about. */}
      <main id="main" dir={dir} tabIndex={-1} className="flex-1 outline-none">
        {children}
      </main>
    </div>
  );
}
