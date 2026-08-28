"use client";

import Link from "next/link";
import { Button } from "@/components/ui/button";
import { useSessionView } from "@/lib/identity/use-session";
import { useLocale } from "@/lib/i18n/locale-provider";

/**
 * What the product can honestly tell a Student about their own account.
 *
 * Two things, because two things are what exists: the name they are signed in
 * under, and the one security action the identity API implements. There is no
 * email here — the session view does not carry one and inventing a field to
 * display it would mean either a request with no route or a blank labelled box.
 * There is no role, no identifier, and no settings that nothing would save.
 *
 * The reason this exists at all is that changing a password had no route
 * through the product. `/password-change` was reachable only by being sent
 * there after signing in with a credential the server had already marked as
 * needing one — so a Student who simply wanted to change their password had
 * nowhere to go, on a capability the backend has always supported.
 */
export function AccountSummary() {
  const { locale, t } = useLocale();
  const session = useSessionView();
  if (!session) return null;

  return (
    <section
      data-testid="account-summary"
      aria-labelledby="account-summary-heading"
      className="mt-6 rounded-2xl border border-border bg-card p-6 shadow-sm"
    >
      <h2
        id="account-summary-heading"
        className="font-display text-xl font-bold text-foreground"
      >
        {t.account.title}
      </h2>

      <p className="mt-3 text-sm text-muted-foreground">
        {t.account.signedInAs}{" "}
        <span className="font-semibold text-foreground">
          {session.display_name}
        </span>
      </p>

      <div className="mt-5 border-t border-border pt-5">
        <h3 className="font-display text-sm font-bold text-foreground">
          {t.account.security}
        </h3>
        <p className="mt-1 text-sm text-muted-foreground">
          {t.account.securityBody}
        </p>
        <Button asChild variant="outline" className="mt-4">
          <Link
            href={`/password-change?returnTo=${encodeURIComponent(
              `/${locale}/learn/profile`,
            )}`}
          >
            {t.account.changePassword}
          </Link>
        </Button>
      </div>
    </section>
  );
}
