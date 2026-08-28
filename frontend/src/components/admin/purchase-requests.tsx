"use client";

import * as React from "react";
import {
  cancelPurchaseRequest,
  confirmPurchaseRequestPayment,
  listAdminPurchaseRequests,
  type PurchaseRequest,
  type PurchaseRequestState,
} from "@/lib/api/access";
import { describeApiError } from "@/lib/api/api-error";
import { formatFils } from "@/lib/formatters/currency";
import { formatDate } from "@/lib/i18n/format";
import { useLocale } from "@/lib/i18n/locale-provider";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/common/empty-state";
import { ErrorState } from "@/components/common/error-state";
import { Input } from "@/components/ui/input";
import { LoadingState } from "@/components/common/loading-state";
import { StatusBadge } from "@/components/common/status-badge";
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableContainer,
  TableHead,
  TableHeaderCell,
  TableRow,
} from "@/components/ui/table";
import { WorkspaceSection } from "@/components/layout/workspace-page";

/**
 * The manual purchase queue.
 *
 * # THE THING THIS SCREEN MUST NOT PRETEND
 *
 * Gradex takes no money. A Student asks for a Course, pays somewhere the product cannot see, and an
 * Administrator records that it happened. "Confirm payment" is therefore a statement about the
 * world, not a transaction — the confirmation says so in as many words, because a button that reads
 * like a payment control on a screen that settles nothing is the single most dangerous sentence
 * this workspace could contain.
 *
 * The reference, not the identifier, is how a request is named. `reference` is a short code the
 * Student was given and can quote; the row's `id` exists only to call the API with.
 */

const TONE: Record<PurchaseRequestState, "default" | "accent" | "success" | "neutral"> = {
  WAITING_PAYMENT: "accent",
  INVITATION_CREATED: "default",
  ACCESS_GRANTED: "success",
  CANCELLED: "neutral",
};

type Pending = { kind: "confirm" | "cancel"; request: PurchaseRequest };

export function PurchaseRequestsPanel() {
  const { locale, t } = useLocale();
  const copy = t.adminAccess.purchases;

  const [requests, setRequests] = React.useState<PurchaseRequest[]>([]);
  const [query, setQuery] = React.useState("");
  // What the listed rows were actually filtered by, so the empty state can tell "nothing to do"
  // apart from "nothing matched what you typed".
  const [appliedQuery, setAppliedQuery] = React.useState("");
  const [state, setState] = React.useState<"loading" | "ready" | "failed">("loading");
  const [notice, setNotice] = React.useState<{ tone: "success" | "error"; text: string } | null>(
    null,
  );
  const [pending, setPending] = React.useState<Pending | null>(null);
  const [busy, setBusy] = React.useState(false);

  const load = React.useCallback(
    async (search: string) => {
      setState("loading");
      try {
        const result = await listAdminPurchaseRequests({ query: search }, locale);
        setRequests(result?.purchase_requests ?? []);
        setAppliedQuery(search);
        setState("ready");
      } catch {
        setState("failed");
      }
    },
    [locale],
  );

  // The listing is locale-invariant except for the Course title the server resolves, so it is read
  // once per locale and not on every keystroke.
  React.useEffect(() => {
    void load("");
  }, [load]);

  /**
   * Carries out whichever decision was confirmed. One path for both, so both call the API exactly
   * once and both report what the server actually answered.
   */
  const act = async () => {
    if (!pending || busy) return;
    setBusy(true);
    setNotice(null);
    try {
      if (pending.kind === "confirm") {
        await confirmPurchaseRequestPayment(pending.request.id, locale);
        setNotice({ tone: "success", text: copy.confirmed });
      } else {
        await cancelPurchaseRequest(pending.request.id, locale);
        setNotice({ tone: "success", text: copy.cancelled });
      }
      await load(appliedQuery);
    } catch (cause) {
      setNotice({ tone: "error", text: describeApiError(cause, locale) });
    } finally {
      setBusy(false);
      setPending(null);
    }
  };

  return (
    <WorkspaceSection
      title={copy.title}
      description={copy.lead}
      testID="purchase-requests"
      actions={
        <form
          className="flex flex-wrap items-center gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            void load(query);
          }}
        >
          <label className="sr-only" htmlFor="purchase-request-search">
            {copy.searchLabel}
          </label>
          <Input
            id="purchase-request-search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={copy.searchPlaceholder}
            className="w-56"
          />
          <Button type="submit" variant="outline" size="sm">
            {copy.search}
          </Button>
        </form>
      }
    >
      {notice ? (
        <div className="mb-4" data-testid="purchase-request-notice" data-tone={notice.tone}>
          {notice.tone === "error" ? (
            <ErrorState title={notice.text} />
          ) : (
            <p role="status" className="text-sm font-semibold text-foreground">
              {notice.text}
            </p>
          )}
        </div>
      ) : null}

      {state === "loading" ? (
        <LoadingState label={copy.loading} testID="purchase-requests-loading" />
      ) : null}
      {state === "failed" ? (
        <ErrorState
          title={copy.loadFailed}
          retryLabel={copy.retry}
          onRetry={() => void load(appliedQuery)}
          testID="purchase-requests-failed"
        />
      ) : null}
      {state === "ready" && requests.length === 0 ? (
        <EmptyState
          density="compact"
          title={appliedQuery === "" ? copy.emptyTitle : copy.emptySearchTitle}
          description={appliedQuery === "" ? copy.emptyBody : copy.emptySearchBody}
          testID="purchase-requests-empty"
        />
      ) : null}

      {state === "ready" && requests.length > 0 ? (
        <TableContainer>
          <Table>
            <TableCaption>{copy.caption}</TableCaption>
            <TableHead>
              <TableRow>
                <TableHeaderCell scope="col">{copy.reference}</TableHeaderCell>
                <TableHeaderCell scope="col">{copy.student}</TableHeaderCell>
                <TableHeaderCell scope="col">{copy.course}</TableHeaderCell>
                <TableHeaderCell scope="col">{copy.price}</TableHeaderCell>
                <TableHeaderCell scope="col">{copy.requested}</TableHeaderCell>
                <TableHeaderCell scope="col">{copy.state}</TableHeaderCell>
                <TableHeaderCell scope="col">{copy.actions}</TableHeaderCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {requests.map((request) => (
                <TableRow key={request.id} data-testid={`purchase-request-${request.reference}`}>
                  {/* The reference is the name of this request — a code the Student was given and
                      can quote back. Latin in either language, so `bdi`. */}
                  <TableHeaderCell scope="row">
                    <bdi className="font-mono">{request.reference}</bdi>
                  </TableHeaderCell>
                  <TableCell>
                    <bdi>{request.email}</bdi>
                  </TableCell>
                  <TableCell>{request.course_title || copy.course}</TableCell>
                  <TableCell dir="ltr">{formatFils(request.price_minor_units, locale)}</TableCell>
                  <TableCell>{formatDate(request.requested_at, locale)}</TableCell>
                  <TableCell>
                    <StatusBadge
                      tone={TONE[request.state]}
                      label={copy.status[request.state]}
                      detail={copy.statusDetail[request.state]}
                      labelTestID="purchase-request-state"
                    />
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-2">
                      {request.state === "WAITING_PAYMENT" ? (
                        <Button
                          type="button"
                          size="sm"
                          disabled={busy}
                          onClick={() => setPending({ kind: "confirm", request })}
                          aria-label={`${copy.confirm} — ${request.reference}`}
                        >
                          {copy.confirm}
                        </Button>
                      ) : null}
                      {request.state === "WAITING_PAYMENT" ||
                      request.state === "INVITATION_CREATED" ? (
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          disabled={busy}
                          onClick={() => setPending({ kind: "cancel", request })}
                          aria-label={`${copy.cancel} — ${request.reference}`}
                        >
                          {copy.cancel}
                        </Button>
                      ) : null}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      ) : null}

      {pending ? (
        <ConfirmDialog
          open
          onOpenChange={(next) => {
            if (!next && !busy) setPending(null);
          }}
          title={pending.kind === "confirm" ? copy.confirmTitle : copy.cancelTitle}
          body={pending.kind === "confirm" ? copy.confirmBody : copy.cancelBody}
          confirmLabel={pending.kind === "confirm" ? copy.confirmAccept : copy.cancelAccept}
          cancelLabel={copy.keep}
          tone={pending.kind === "confirm" ? "default" : "destructive"}
          busy={busy}
          onConfirm={() => void act()}
          testID="purchase-request-confirm"
        />
      ) : null}
    </WorkspaceSection>
  );
}
