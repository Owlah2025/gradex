"use client";

import * as React from "react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { createStudentPurchaseRequest, type PurchaseRequestCreated } from "@/lib/api/access";
import { ProblemError } from "@/lib/api/problem";
import { formatFils } from "@/lib/formatters/currency";
import type { PublicPrice } from "@/lib/api/public-catalog";
import { PriceDisplay } from "./price-display";
import { withReturnTo } from "@/lib/identity/return-to";
import { useLocale } from "@/lib/i18n/locale-provider";
import { PurchaseHandoff } from "@/components/contact/purchase-handoff";
import {
  PurchaseContactSection,
  useOwnPurchaseRequests,
  usePurchaseContact,
} from "@/components/contact/purchase-contact";
import { contactProblem, pendingHandoffURL } from "@/components/contact/contact-state";
import { ContactNumberPrompt } from "@/components/contact/contact-number-prompt";

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
 *
 * D-117 adds the Student's WhatsApp number to that confirmation. A Student who
 * has none is asked for it before anything is created, and the server saves it
 * with the request, so the Gradex team can reach every request whether or not
 * WhatsApp ever opens. WhatsApp is then one explicit press on a real link in the
 * success state, never a window opened around the request: that automatic
 * window is what popup blockers, in-app browsers and app-switching defeated,
 * leaving a saved request the Student could not see and nobody could follow up.
 * A request already waiting for payment is shown with its link again instead of
 * a second confirmation.
 */
export function PurchaseAction({
  courseId,
  courseTitle,
  priceMinorUnits,
  price,
  locale,
  labels,
  authenticated,
  className,
}: {
  courseId: string;
  courseTitle: string;
  /** `null` where the Course lists no price; the confirmation still states the Course. */
  priceMinorUnits: number | null;
  price?: PublicPrice | null;
  locale: "ar" | "en";
  labels: Labels;
  /** Whether the visitor's session has resolved to a signed-in principal. */
  authenticated: boolean;
  className?: string;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const { t } = useLocale();
  const contactLabels = t.contact;
  const [error, setError] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);
  // The server's answer, set only once the request is persisted. The success
  // state is never shown on anything less.
  const [created, setCreated] = React.useState<PurchaseRequestCreated | null>(null);
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

  const contact = usePurchaseContact(locale, authenticated && open);
  // A request for this Course already waiting for payment is shown with its
  // WhatsApp link again, so a Student whose WhatsApp never opened can continue.
  const ownRequests = useOwnPurchaseRequests(locale, authenticated);
  const pending = ownRequests?.find(
    (request) => request.target_kind !== "BUNDLE" && request.course_id === courseId && pendingHandoffURL(request),
  );

  /** Where the auth journey must return to: this Course, still buying. */
  const destination = React.useMemo(() => {
    const params = new URLSearchParams(searchParams.toString());
    params.set(purchaseIntentParameter, "1");
    return `${pathname ?? ""}?${params.toString()}`;
  }, [pathname, searchParams]);

  async function confirm() {
    if (inFlight.current) return;
    // The contact is settled before anything is created: a request with no
    // way to reach its Student is the failure this panel exists to prevent.
    const plan = contact.plan();
    if (plan.kind === "blocked") {
      contact.setError(plan.reason);
      return;
    }
    inFlight.current = true;
    setSubmitting(true);
    setError(null);
    try {
      const result = await createStudentPurchaseRequest(
        courseId,
        locale,
        undefined,
        plan.kind === "send" ? { whatsappNumber: plan.e164 } : {},
      );
      if (plan.kind === "send") contact.remember(plan.e164);
      // The handoff URL is the server's, and the success state renders only
      // from this confirmed answer.
      if (pendingHandoffURL(result)) {
        setCreated(result);
      } else {
        setError(labels.failed);
      }
    } catch (caught) {
      const reason = contactProblem(caught);
      if (reason) {
        contact.setError(reason);
        if (!contact.editing) contact.setEditing(true);
      } else {
        setError(purchaseMessage(caught, labels));
      }
    } finally {
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

  if (created) {
    return (
      <div className={panelClassName(className)} data-testid="purchase-confirmation">
        <PurchaseHandoff
          variant={created.reused ? "pending" : "created"}
          target="course"
          reference={created.reference}
          whatsappURL={pendingHandoffURL(created) ?? ""}
          locale={locale}
          labels={contactLabels}
        />
      </div>
    );
  }

  if (pending) {
    return (
      <div className={panelClassName(className)} data-testid="purchase-pending">
        <PurchaseHandoff
          variant="pending"
          target="course"
          reference={pending.reference}
          whatsappURL={pendingHandoffURL(pending) ?? ""}
          locale={locale}
          labels={contactLabels}
        />
        {/* A request from before D-117 may have no number behind it. The
            reminder renders only when the Student's profile has none. */}
        <ContactNumberPrompt className="mt-4" />
      </div>
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
              {price ? <PriceDisplay price={price} locale={locale} compact /> : <bdi>{formatFils(priceMinorUnits, locale)}</bdi>}
            </dd>
          </div>
        ) : null}
      </dl>

      <p className="mt-4 text-sm leading-6 text-muted-foreground">{labels.intro}</p>

      {/* The Student's WhatsApp number: shown when on file, asked for when not. */}
      <PurchaseContactSection
        contact={contact}
        locale={locale}
        labels={contactLabels}
        disabled={submitting}
        idPrefix="purchase"
      />

      <div className="mt-5 flex flex-wrap gap-3">
        <Button
          type="button"
          onClick={confirm}
          disabled={submitting || contact.status === "loading"}
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
            contact.setError(null);
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
