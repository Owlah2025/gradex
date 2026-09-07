"use client";

import * as React from "react";
import { Eye, EyeOff } from "lucide-react";
import { Input } from "./input";
import { useLocale } from "@/lib/i18n/locale-provider";
import { cn } from "@/lib/utils";

/**
 * A password field that can be read back.
 *
 * Several of the screens asking for a password ask twice. Typing a passphrase
 * blind, twice, on a phone keyboard is where "both passwords must match" comes
 * from — the reveal is not a convenience here, it is what keeps a password the
 * reader cannot see from becoming a password they cannot enter.
 *
 * The control is a real `<button>` inside the field, so it is reachable by
 * keyboard and named to a screen reader. `aria-pressed` carries the state:
 * an icon swap alone says nothing to anyone not looking at it.
 *
 * `dir="ltr"` stays on the input regardless of page direction — a password is
 * an opaque sequence, not prose, and revealing it in an Arabic page must not
 * reorder it. The button sits at the logical `end` of the wrapper, which
 * inherits the page direction, so the control lands on the reader's trailing
 * edge in both languages: the right in English, the left in Arabic.
 *
 * Those two facts pull in opposite directions, and that is the whole reason
 * this file has to say anything about padding at all. `padding-inline-end` on
 * the input resolves against the *input's* direction, which is pinned to LTR,
 * so it always reserved space on the right. In Arabic the button was on the
 * left, over unpadded text: the reported overlap. The reserved space therefore
 * has to be chosen against the page direction rather than the field's, which is
 * what `dir` from the locale gives. It is one decision in the one shared
 * component, not a locale branch repeated on every auth screen — every password
 * field in the product renders through here.
 */
export const PasswordInput = React.forwardRef<
  HTMLInputElement,
  Omit<React.InputHTMLAttributes<HTMLInputElement>, "type">
>(({ className, ...props }, ref) => {
  const { dir, t } = useLocale();
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
        className={cn(dir === "rtl" ? "ps-12" : "pe-12", className)}
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
