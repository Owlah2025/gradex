"use client";

import * as React from "react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { createStudentPurchaseRequest } from "@/lib/api/access";
import { ProblemError } from "@/lib/api/problem";
import { formatFils } from "@/lib/formatters/currency";
import { withReturnTo } from "@/lib/identity/return-to";

type Labels = Dictionary["access"]["purchase"];

/**
 * The query flag that says "this visitor came here to buy".
 *
 * It is not a secret and it grants nothing: it selects which panel a public
 * page renders. Putting it in the URL is what lets the intent survive sign in,
 * registration, verification, a reload, and the back button, without any of
 * those steps having to carry state they should not be holding.
 */
export const purchaseIntentParameter = "purchase";

/**
 * "Buy this course", and everything that follows from pressing it.
 *
 * The old form asked an anonymous visitor for an email address and, on submit,
 * created a purchase request and navigated to WhatsApp in one step. Three
 * things were wrong with that and all three are fixed here: the address decided
 * where Course access would eventually be sent and was typed by whoever was
 * looking at the page; the request existed before anyone had confirmed
 * anything; and WhatsApp opened as a side effect of a button labelled "I want
 * to buy", not of a confirmation.
 *
 * So: an anonymous visitor is sent to sign in, carrying the Course and the
 * intent. A signed-in Student is shown what they are about to request — the
 * title and the price, both from the server's own representation of the Course —
 * and nothing is created and nothing is opened until they confirm.
 */
export function PurchaseAction({
  courseId,
  courseTitle,
  priceMinorUnits,
  locale,
  labels,
  authenticated,
  className,
}: {
  courseId: string;
  courseTitle: string;
  /** `null` where the Course lists no price; the confirmation still states the Course. */
  priceMinorUnits: number | null;
  locale: "ar" | "en";
  labels: Labels;
  /** Whether the visitor's session has resolved to a signed-in principal. */
  authenticated: boolean;
  className?: string;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [error, setError] = React.useState<string | null>(null);
  const [notice, setNotice] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);
  // `submitting` is state and does not close the window between two submits
  // dispatched in the same render pass. This does, and it matters: the second
  // one would reach a route that creates operational work.
  const inFlight = React.useRef(false);

  // The intent survives in the URL, so returning from the auth journey lands
  // straight on the confirmation rather than on a button the Student has
  // already pressed once.
  const intended = searchParams.get(purchaseIntentParameter) === "1";
  const [open, setOpen] = React.useState(intended);
  React.useEffect(() => {
    if (intended) setOpen(true);
  }, [intended]);

  /** Where the auth journey must return to: this Course, still buying. */
  const destination = React.useMemo(() => {
    const params = new URLSearchParams(searchParams.toString());
    params.set(purchaseIntentParameter, "1");
    return `${pathname ?? ""}?${params.toString()}`;
  }, [pathname, searchParams]);

  async function confirm() {
    if (inFlight.current) return;
    inFlight.current = true;
    setSubmitting(true);
    setError(null);
    setNotice(null);
    try {
      const result = await createStudentPurchaseRequest(courseId, locale);
      // Direct assignment rather than a popup: it preserves the click intent
      // across the await and is not subject to popup blocking. This is the only
      // place in the product that navigates to WhatsApp, and it is reached only
      // from the confirmation press above.
      window.location.assign(result.whatsapp_url);
    } catch (caught) {
      setError(purchaseMessage(caught, labels));
      setSubmitting(false);
      inFlight.current = false;
    }
  }

  if (!authenticated) {
    return (
      <section
        className={panelClassName(className)}
        aria-labelledby="purchase-sign-in-heading"
        data-testid="purchase-sign-in-required"
      >
        {open ? (
          <>
            <h2 id="purchase-sign-in-heading" className="font-display text-lg font-bold text-foreground">
              {labels.signInRequiredTitle}
            </h2>
            <p className="mt-2 text-sm leading-6 text-muted-foreground">
              {labels.signInRequiredBody}
            </p>
            <div className="mt-4 flex flex-wrap gap-3">
              <Button asChild data-testid="purchase-sign-in">
                <Link href={withReturnTo("/login", destination)}>{labels.signIn}</Link>
              </Button>
              <Button asChild variant="outline" data-testid="purchase-create-account">
                <Link href={withReturnTo("/register", destination)}>{labels.createAccount}</Link>
              </Button>
            </div>
          </>
        ) : (
          <Button
            type="button"
            className="w-full"
            onClick={() => setOpen(true)}
            data-testid="purchase-request-open"
          >
            {labels.action}
          </Button>
        )}
      </section>
    );
  }

  if (!open) {
    return (
      <Button
        type="button"
        className={className ?? "mt-6"}
        onClick={() => setOpen(true)}
        data-testid="purchase-request-open"
      >
        {labels.action}
      </Button>
    );
  }

  return (
    <section
      className={panelClassName(className)}
      aria-labelledby="purchase-confirm-heading"
      data-testid="purchase-confirmation"
    >
      <h2 id="purchase-confirm-heading" className="font-display text-lg font-bold text-foreground">
        {labels.heading}
      </h2>

      {notice ? (
        <div className="mt-3">
          <Alert tone="info" title={notice} />
        </div>
      ) : null}
      {error ? (
        <div className="mt-3">
          <Alert tone="error" title={error} />
        </div>
      ) : null}

      {/* What is being requested, stated before it is requested. Both values
          come from the Course as the server describes it; neither is typed by
          the browser and neither is sent back to the server on confirm. */}
      <dl className="mt-4 space-y-2 text-sm">
        <div className="flex flex-wrap justify-between gap-2">
          <dt className="text-muted-foreground">{labels.courseLabel}</dt>
          <dd className="font-semibold text-foreground" data-testid="purchase-course-title">
            {courseTitle}
          </dd>
        </div>
        {priceMinorUnits !== null ? (
          <div className="flex flex-wrap justify-between gap-2">
            <dt className="text-muted-foreground">{labels.priceLabel}</dt>
            <dd className="font-semibold text-foreground" data-testid="purchase-price">
              <bdi>{formatFils(priceMinorUnits, locale)}</bdi>
            </dd>
          </div>
        ) : null}
      </dl>

      <p className="mt-4 text-sm leading-6 text-muted-foreground">{labels.intro}</p>

      <div className="mt-5 flex flex-wrap gap-3">
        <Button
          type="button"
          onClick={confirm}
          disabled={submitting}
          data-testid="purchase-request-submit"
        >
          {submitting ? labels.submitting : labels.submit}
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={submitting}
          onClick={() => {
            setOpen(false);
            setError(null);
            // The flag comes out of the address bar with the panel, so a
            // reload after cancelling does not reopen what was cancelled.
            if (intended) {
              const params = new URLSearchParams(searchParams.toString());
              params.delete(purchaseIntentParameter);
              const query = params.toString();
              router.replace(query ? `${pathname}?${query}` : (pathname ?? "/"));
            }
          }}
          data-testid="purchase-request-cancel"
        >
          {labels.cancel}
        </Button>
      </div>
    </section>
  );
}

function panelClassName(className: string | undefined): string {
  return className === undefined
    ? "mt-6 rounded-lg border border-border bg-card p-5"
    : `${className} rounded-lg border border-border bg-muted/40 p-5`;
}

function purchaseMessage(caught: unknown, labels: Labels): string {
  if (!(caught instanceof ProblemError)) return labels.failed;
  switch (caught.problem.code) {
    case "COURSE_ACCESS_ALREADY_ACTIVE":
      return labels.alreadyActive;
    case "NOT_FOUND":
      return labels.notPurchasable;
    default:
      return labels.failed;
  }
}
