"use client";

import Link from "next/link";
import { CheckCircle2, Clock, MessageCircle } from "lucide-react";

import { Button } from "@/components/ui/button";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";

type Labels = Dictionary["contact"];

/**
 * The state after a purchase request exists: the request is saved, here is its
 * reference, and here is the button that opens WhatsApp (D-117).
 *
 * The button is an ordinary link the Student presses. Nothing opens on its own:
 * an automatic window opened around an asynchronous request is exactly what
 * popup blockers, in-app browsers and mobile app-switching defeat, and when it
 * failed the Student was left with no request on screen and no way back to the
 * conversation. A real `href` survives all of those, can be pressed again, and
 * is the same server-built URL the request returned. Gradex itself is never
 * navigated away from.
 *
 * Rendered only after the server confirmed the request — never optimistically.
 */
export function PurchaseHandoff({
  variant,
  target,
  reference,
  whatsappURL,
  locale,
  labels,
  testID = "purchase-handoff",
}: {
  /** `created`: this press saved the request. `pending`: one was already waiting. */
  variant: "created" | "pending";
  target: "course" | "bundle";
  reference: string;
  whatsappURL: string;
  locale: "ar" | "en";
  labels: Labels;
  testID?: string;
}) {
  const created = variant === "created";
  const Icon = created ? CheckCircle2 : Clock;
  const title = created
    ? target === "bundle"
      ? labels.bundleSuccessTitle
      : labels.successTitle
    : labels.pendingTitle;

  return (
    <section
      className="rounded-lg border border-gx-success/25 bg-gx-success-soft p-5 text-gx-navy dark:border-border dark:bg-card dark:text-foreground"
      aria-labelledby={`${testID}-title`}
      data-testid={testID}
      data-handoff-variant={variant}
    >
      <div className="flex gap-3">
        <Icon aria-hidden className="mt-0.5 size-5 shrink-0 text-gx-success dark:text-primary" />
        <div className="min-w-0 flex-1">
          <h2 id={`${testID}-title`} role="status" className="font-display text-lg font-bold">
            {title}
          </h2>
          <p className="mt-1 text-sm leading-6">{created ? labels.successBody : labels.pendingBody}</p>
          <p className="mt-3 text-sm">
            <span className="text-muted-foreground">{labels.referenceLabel}: </span>
            <bdi dir="ltr" className="font-semibold" data-testid={`${testID}-reference`}>
              {reference}
            </bdi>
          </p>
        </div>
      </div>

      <Button asChild size="lg" className="mt-5 h-auto min-h-12 w-full whitespace-normal py-3 text-center">
        <a href={whatsappURL} target="_blank" rel="noopener noreferrer" data-testid={`${testID}-link`}>
          <MessageCircle aria-hidden className="size-5" />
          {labels.continueWhatsApp}
        </a>
      </Button>

      <p className="mt-4 text-xs leading-5 text-muted-foreground">{labels.handoffNote}</p>
      <p className="mt-2 text-xs leading-5 text-muted-foreground">
        {labels.handoffRecovery}{" "}
        <Link href={`/${locale}/access`} className="font-semibold text-primary underline underline-offset-4">
          {labels.recoveryLink}
        </Link>
      </p>
    </section>
  );
}
