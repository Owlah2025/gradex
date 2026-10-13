"use client";

import * as React from "react";
import { MessageCircle } from "lucide-react";

import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { listStudentPurchaseRequests, type PurchaseRequest } from "@/lib/api/access";
import { ProblemError } from "@/lib/api/problem";
import { formatFils } from "@/lib/formatters/currency";
import { useLocale } from "@/lib/i18n/locale-provider";
import { pendingHandoffURL } from "./contact-state";

type LoadState =
  | { kind: "loading" }
  | { kind: "ready"; requests: PurchaseRequest[] }
  | { kind: "failed" }
  | { kind: "anonymous" };

/**
 * The Student's requests still waiting for payment, each with its WhatsApp
 * link (D-117).
 *
 * This is the recovery path: a Student whose WhatsApp never opened, who closed
 * it, or who switched devices can come back here and continue. The link is the
 * one the server built for that request. Nothing here changes a request —
 * opening WhatsApp is not payment, and only an Admin confirmation grants access.
 * With nothing waiting, the section renders nothing.
 */
export function PendingPaymentRequests() {
  const { locale, t } = useLocale();
  const labels = t.contact;
  const [state, setState] = React.useState<LoadState>({ kind: "loading" });

  const load = React.useCallback(async () => {
    setState({ kind: "loading" });
    try {
      const result = await listStudentPurchaseRequests(locale);
      setState({ kind: "ready", requests: result?.purchase_requests ?? [] });
    } catch (caught) {
      setState(caught instanceof ProblemError && caught.problem.status === 401 ? { kind: "anonymous" } : { kind: "failed" });
    }
  }, [locale]);

  React.useEffect(() => {
    void load();
  }, [load]);

  if (state.kind === "loading" || state.kind === "anonymous") return null;
  if (state.kind === "failed") {
    return (
      <div className="mt-6">
        <Alert tone="error" title={labels.pendingPaymentsLoadFailed}>
          <Button type="button" variant="outline" size="sm" className="mt-2" onClick={() => void load()}>
            {labels.retry}
          </Button>
        </Alert>
      </div>
    );
  }

  const waiting = state.requests.filter((request) => pendingHandoffURL(request));
  if (waiting.length === 0) return null;

  return (
    <section className="mt-6" aria-labelledby="pending-payments-title" data-testid="pending-payment-requests">
      <h2 id="pending-payments-title" className="font-display text-lg font-bold text-foreground">
        {labels.pendingPaymentsTitle}
      </h2>
      <p className="mt-1 text-sm text-muted-foreground">{labels.pendingPaymentsBody}</p>
      <ul className="mt-4 space-y-3">
        {waiting.map((request) => (
          <li
            key={request.id}
            className="flex flex-col gap-3 rounded-lg border border-border bg-card p-4 sm:flex-row sm:items-center sm:justify-between"
            data-testid="pending-payment-request"
          >
            <div className="min-w-0">
              <p className="font-semibold text-foreground">
                <bdi>{request.bundle_title || request.course_title}</bdi>
              </p>
              <p className="mt-1 text-sm text-muted-foreground">
                {labels.referenceLabel}:{" "}
                <bdi dir="ltr" className="font-semibold text-foreground">
                  {request.reference}
                </bdi>
                {" · "}
                <bdi>{formatFils(request.price_minor_units, locale)}</bdi>
              </p>
            </div>
            <Button asChild className="shrink-0">
              <a
                href={pendingHandoffURL(request) ?? undefined}
                target="_blank"
                rel="noopener noreferrer"
                data-testid="pending-payment-request-link"
              >
                <MessageCircle aria-hidden className="size-4" />
                {labels.continueWhatsApp}
              </a>
            </Button>
          </li>
        ))}
      </ul>
      <p className="mt-3 text-xs leading-5 text-muted-foreground">{labels.handoffNote}</p>
    </section>
  );
}
