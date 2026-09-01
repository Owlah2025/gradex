"use client";

import * as React from "react";
import Link from "next/link";
import { Menu } from "lucide-react";
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { usePathname } from "next/navigation";
import { AuthActions } from "./auth-actions";
import { primaryNavigation } from "./nav-items";
import { useLocale } from "@/lib/i18n/locale-provider";
import { useSessionView } from "@/lib/identity/use-session";

export function MobileNav() {
  const { locale, t } = useLocale();
  const pathname = usePathname();
  const session = useSessionView();
  // Parity with the desktop bar is the requirement, not a nicety: below `lg`
  // this sheet is the only primary navigation there is, so anything the wide
  // header offers and this one does not is simply unreachable on a phone.
  const primary = primaryNavigation(pathname ?? "/", locale, {
    studentSession: session?.role === "STUDENT",
  });
  const [open, setOpen] = React.useState(false);

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger asChild>
        <Button
          variant="outline"
          size="icon"
          className="lg:hidden"
          aria-label={t.meta.openMenu}
        >
          <Menu className="size-5" aria-hidden />
        </Button>
      </SheetTrigger>
      <SheetContent side="right" closeLabel={t.meta.closeMenu}>
        <SheetTitle className="sr-only">{t.nav.browse}</SheetTitle>
        <nav
          aria-label={t.nav.primaryNavigation}
          className="mt-8 flex flex-col gap-1"
        >
          {primary.map((item) => (
            <SheetClose asChild key={item.href}>
              <Link
                href={item.href}
                className="rounded-md px-2.5 py-3 font-display text-[17px] font-semibold text-foreground hover:bg-accent"
              >
                {item.label(t)}
              </Link>
            </SheetClose>
          ))}
        </nav>
        <div className="my-4 h-px bg-border" />
        <div onClick={() => setOpen(false)}>
          <AuthActions stacked />
        </div>
      </SheetContent>
    </Sheet>
  );
}
