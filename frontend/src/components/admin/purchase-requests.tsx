"use client";

import * as React from "react";
import {
	cancelPurchaseRequest,
  confirmPurchaseRequestPayment,
  listAdminPurchaseRequests,
  type PurchaseRequest,
  type PurchaseRequestState,
} from "@/lib/api/access";
import { useLocale } from "@/lib/i18n/locale-provider";
import { formatFils } from "@/lib/formatters/currency";
import { ProblemError } from "@/lib/api/problem";

const stateLabel: Record<PurchaseRequestState, string> = {
  WAITING_PAYMENT: "Waiting for payment",
  INVITATION_CREATED: "Invitation sent — waiting for Student",
  ACCESS_GRANTED: "Access granted",
  CANCELLED: "Cancelled",
};

function message(error: unknown) {
  if (error instanceof ProblemError)
    return error.problem.detail || error.problem.title;
  return "Purchase requests could not be updated.";
}

export function PurchaseRequestsPanel() {
  const { locale } = useLocale();
  const [requests, setRequests] = React.useState<PurchaseRequest[]>([]);
  const [query, setQuery] = React.useState("");
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);
  const [notice, setNotice] = React.useState<string | null>(null);
  const [busy, setBusy] = React.useState<string | null>(null);

  const load = React.useCallback(
    async (search = query) => {
      setLoading(true);
      setError(null);
      try {
        const result = await listAdminPurchaseRequests(
          { query: search },
          locale,
        );
        setRequests(result?.purchase_requests ?? []);
      } catch (caught) {
        setError(message(caught));
      } finally {
        setLoading(false);
      }
    },
    [locale, query],
  );

  React.useEffect(() => {
    void load("");
  }, [locale]);

  async function confirm(request: PurchaseRequest) {
    setBusy(request.id);
    setError(null);
    setNotice(null);
    try {
      const result = await confirmPurchaseRequestPayment(request.id, locale);
      setNotice(
        `Payment confirmed and invitation queued for ${result?.purchase_request.email ?? request.email}.`,
      );
      await load();
    } catch (caught) {
      setError(message(caught));
    } finally {
      setBusy(null);
    }
  }

  async function cancel(request: PurchaseRequest) {
    setBusy(request.id);
    setError(null);
    setNotice(null);
    try {
      await cancelPurchaseRequest(request.id, locale);
      setNotice(`Purchase request ${request.reference} was cancelled.`);
      await load();
    } catch (caught) {
      setError(message(caught));
    } finally {
      setBusy(null);
    }
  }

  return (
    <section
      className="bg-white p-6 rounded-lg border shadow-sm"
      aria-labelledby="purchase-requests-heading"
    >
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h2
            id="purchase-requests-heading"
            className="text-xl font-semibold text-gray-800"
          >
            Purchase requests
          </h2>
          <p className="mt-1 text-sm text-gray-600">
            Find a request by reference, email, Course title, or state.
          </p>
        </div>
        <form
          className="flex gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            void load(query);
          }}
        >
          <label className="sr-only" htmlFor="purchase-request-search">
            Search purchase requests
          </label>
          <input
            id="purchase-request-search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            className="rounded-md border border-gray-300 px-3 py-2 text-sm"
            placeholder="Reference, email, Course"
          />
          <button
            type="submit"
            className="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium"
          >
            Search
          </button>
        </form>
      </div>
      {error ? (
        <p role="alert" className="mt-4 text-sm text-red-700">
          {error}
        </p>
      ) : null}
      {notice ? (
        <p role="status" className="mt-4 text-sm text-green-700">
          {notice}
        </p>
      ) : null}
      {loading ? (
        <p className="mt-4 text-sm text-gray-600">Loading purchase requests…</p>
      ) : null}
      {!loading ? (
        <div className="mt-4 overflow-x-auto">
          <div
            role="table"
            aria-label="Purchase requests"
            className="min-w-[860px] text-left text-sm"
          >
            <div
              role="row"
              className="grid grid-cols-[0.9fr_1.6fr_1.4fr_0.7fr_1.2fr_1.2fr_1.6fr] border-b text-gray-600"
            >
              {[
                "Reference",
                "Email",
                "Course",
                "Price",
                "Requested",
                "Status",
                "Action",
              ].map((label) => (
                <div key={label} role="columnheader" className="p-2">
                  {label}
                </div>
              ))}
            </div>
            {requests.map((request) => (
              <div
                key={request.id}
                role="row"
                data-testid={`purchase-request-${request.reference}`}
                className="grid grid-cols-[0.9fr_1.6fr_1.4fr_0.7fr_1.2fr_1.2fr_1.6fr] border-b align-top"
              >
                <div role="cell" className="p-2 font-mono">
                  {request.reference}
                </div>
                <div role="cell" className="p-2">
                  {request.email}
                </div>
                <div role="cell" className="p-2">
                  {request.course_title || "Course"}
                </div>
                <div role="cell" className="p-2" dir="ltr">
                  {formatFils(request.price_minor_units, locale)}
                </div>
                <div role="cell" className="p-2">
                  {new Date(request.requested_at).toLocaleString(locale)}
                </div>
                <div role="cell" className="p-2">
                  {stateLabel[request.state]}
                </div>
                <div role="cell" className="p-2">
                  {request.state === "WAITING_PAYMENT" ? (
                    <div className="flex flex-wrap gap-2">
                      <button
                        type="button"
                        disabled={busy === request.id}
                        onClick={() => void confirm(request)}
                        className="rounded-md bg-emerald-600 px-3 py-2 font-medium text-white disabled:opacity-50"
                      >
                        {busy === request.id
                          ? "Confirming…"
                          : "Confirm payment & send invitation"}
                      </button>
                      <button
                        type="button"
                        disabled={busy === request.id}
                        onClick={() => void cancel(request)}
                        className="rounded-md border border-red-300 px-3 py-2 font-medium text-red-700 disabled:opacity-50"
                      >
                        Cancel request
                      </button>
                    </div>
                  ) : request.state === "INVITATION_CREATED" ? (
                    <button
                      type="button"
                      disabled={busy === request.id}
                      onClick={() => void cancel(request)}
                      className="rounded-md border border-red-300 px-3 py-2 font-medium text-red-700 disabled:opacity-50"
                    >
                      {busy === request.id ? "Cancelling…" : "Cancel request"}
                    </button>
                  ) : null}
                </div>
              </div>
            ))}
            {requests.length === 0 ? (
              <div role="row">
                <div role="cell" className="p-4 text-gray-600">
                  No purchase requests found.
                </div>
              </div>
            ) : null}
          </div>
        </div>
      ) : null}
    </section>
  );
}
