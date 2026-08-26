"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { Button, type ButtonProps } from "@/components/ui/button";
import { useLocale } from "@/lib/i18n/locale-provider";
import { deleteSession } from "@/lib/api/identity";
import { clearSession, currentCSRFToken } from "@/lib/identity/session";
import { routes } from "./nav-items";
import { cn } from "@/lib/utils";

/**
 * Ending the session, in one place.
 *
 * The header and the Student learning frame both need it and must behave identically — a sign-out
 * that clears local state on one surface and not the other is the kind of difference nobody notices
 * until a shared machine is involved. Extracted rather than duplicated for that reason.
 */
export function SignOutButton({
  size = "sm",
  variant = "outline",
  className,
}: {
  size?: ButtonProps["size"];
  variant?: ButtonProps["variant"];
  className?: string;
}) {
  const { locale, t } = useLocale();
  const router = useRouter();
  const [signingOut, setSigningOut] = React.useState(false);

  async function signOut() {
    const csrf = currentCSRFToken();
    if (!csrf) return;
    setSigningOut(true);
    try {
      await deleteSession(csrf, locale);
    } catch {
      // Logout is best-effort from the browser's side. The server is authoritative, and a failed
      // call must still drop local state rather than leave a signed-out-looking header holding a
      // live CSRF token.
    } finally {
      clearSession();
      setSigningOut(false);
      router.push(`${routes.login}?reason=signed-out`);
    }
  }

  return (
    <Button
      variant={variant}
      size={size}
      onClick={signOut}
      disabled={signingOut}
      className={cn(className)}
    >
      {signingOut ? t.auth.session.signingOut : t.auth.session.signOut}
    </Button>
  );
}
