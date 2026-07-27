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
import { AuthActions, type AuthState } from "./auth-actions";
import { navItems } from "./nav-items";
import { useLocale } from "@/lib/i18n/locale-provider";

export function MobileNav({ authState }: { authState?: AuthState }) {
  const { t } = useLocale();
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
        <nav aria-label="Mobile" className="mt-8 flex flex-col gap-1">
          {navItems.map((item) => (
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
          <AuthActions state={authState} stacked />
        </div>
      </SheetContent>
    </Sheet>
  );
}
