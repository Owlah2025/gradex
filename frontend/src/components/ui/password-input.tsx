"use client";

import * as React from "react";
import { Eye, EyeOff } from "lucide-react";
import { Input } from "./input";
import { useLocale } from "@/lib/i18n/locale-provider";
import { cn } from "@/lib/utils";

/**
 * A password field that can be read back.
 *
 * Every password on this product is at least fifteen characters, and several of
 * the screens asking for one ask twice. Typing a long passphrase blind, twice,
 * on a phone keyboard is where "both passwords must match" comes from — the
 * reveal is not a convenience here, it is what makes the length requirement
 * usable.
 *
 * The control is a real `<button>` inside the field, so it is reachable by
 * keyboard and named to a screen reader. `aria-pressed` carries the state:
 * an icon swap alone says nothing to anyone not looking at it.
 *
 * `dir="ltr"` stays on the input regardless of page direction — a password is
 * an opaque sequence, not prose, and revealing it in an Arabic page must not
 * reorder it. The button sits at the logical `end`, so it follows the page.
 */
export const PasswordInput = React.forwardRef<
  HTMLInputElement,
  Omit<React.InputHTMLAttributes<HTMLInputElement>, "type">
>(({ className, ...props }, ref) => {
  const { t } = useLocale();
  const [revealed, setRevealed] = React.useState(false);
  const Icon = revealed ? EyeOff : Eye;
  const label = revealed
    ? t.auth.common.hidePassword
    : t.auth.common.showPassword;

  return (
    <div className="relative">
      <Input
        ref={ref}
        type={revealed ? "text" : "password"}
        dir="ltr"
        className={cn("pe-12", className)}
        {...props}
      />
      <button
        type="button"
        onClick={() => setRevealed((previous) => !previous)}
        aria-pressed={revealed}
        aria-label={label}
        title={label}
        className="absolute inset-y-0 end-0 grid w-12 place-items-center rounded-e-md text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
      >
        <Icon className="size-5" aria-hidden />
      </button>
    </div>
  );
});
PasswordInput.displayName = "PasswordInput";
