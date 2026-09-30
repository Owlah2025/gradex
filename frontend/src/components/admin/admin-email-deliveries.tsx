"use client";

import { useCallback, useEffect, useState, type FormEvent } from "react";
import {
  listAdminEmailDeliveries,
  type AdminEmailDeliveryFilters,
  type AdminEmailDeliveryPage,
} from "@/lib/api/admin-operations";
import { describeApiError } from "@/lib/api/api-error";
import { useLocale } from "@/lib/i18n/locale-provider";
import { formatDateTime } from "@/lib/i18n/format";
import { EmptyState } from "@/components/common/empty-state";
import { ErrorState } from "@/components/common/error-state";
import { LoadingState } from "@/components/common/loading-state";
import { StatusBadge } from "@/components/common/status-badge";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Table, TableBody, TableCaption, TableCell, TableContainer, TableHead, TableHeaderCell, TableRow } from "@/components/ui/table";
import { WorkspacePage, WorkspacePageHeader, WorkspaceToolbar } from "@/components/layout/workspace-page";

const PAGE_LIMIT = 20;

export function AdminEmailDeliveries() {
  const { locale, t } = useLocale();
  const copy = t.adminEmailDeliveries;
  const [draft, setDraft] = useState<AdminEmailDeliveryFilters>({ page: 1, limit: PAGE_LIMIT });
  const [filters, setFilters] = useState<AdminEmailDeliveryFilters>({ page: 1, limit: PAGE_LIMIT });
  const [result, setResult] = useState<AdminEmailDeliveryPage | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "failed">("loading");
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setState("loading");
    setError(null);
    try {
      setResult(await listAdminEmailDeliveries(locale, filters));
      setState("ready");
    } catch (cause) {
      setError(describeApiError(cause, locale));
      setState("failed");
    }
  }, [filters, locale]);

  useEffect(() => { void load(); }, [load]);

  const apply = (event: FormEvent) => {
    event.preventDefault();
    setFilters({ ...draft, page: 1, limit: PAGE_LIMIT });
  };
  const clear = () => {
    const next = { page: 1, limit: PAGE_LIMIT };
    setDraft(next);
    setFilters(next);
  };

  return <WorkspacePage testID="admin-email-deliveries-page">
    <WorkspacePageHeader title={copy.title} description={copy.description} />
    <WorkspaceToolbar>
      <form onSubmit={apply} className="grid w-full gap-3 rounded-lg border border-border bg-card p-4 sm:grid-cols-2 lg:grid-cols-5">
        <Field label={copy.stateLabel} htmlFor="email-deliveries-state">
          <Select id="email-deliveries-state" value={draft.state ?? ""} onChange={(event) => setDraft((current) => ({ ...current, state: event.target.value as AdminEmailDeliveryFilters["state"] }))}>
            <option value="">{copy.allStates}</option>
            <option value="queued">{copy.states.queued}</option>
            <option value="attempted">{copy.states.attempted}</option>
            <option value="delivered">{copy.states.delivered}</option>
            <option value="failed">{copy.states.failed}</option>
          </Select>
        </Field>
        <Field label={copy.kindLabel} htmlFor="email-deliveries-kind">
          <Select id="email-deliveries-kind" value={draft.kind ?? ""} onChange={(event) => setDraft((current) => ({ ...current, kind: event.target.value }))}>
            <option value="">{copy.allKinds}</option>
            {Object.entries(copy.kinds).map(([kind, label]) => <option key={kind} value={kind}>{label}</option>)}
          </Select>
        </Field>
        <Field label={copy.from} htmlFor="email-deliveries-from"><Input id="email-deliveries-from" type="date" value={draft.from ?? ""} onChange={(event) => setDraft((current) => ({ ...current, from: event.target.value }))} /></Field>
        <Field label={copy.to} htmlFor="email-deliveries-to"><Input id="email-deliveries-to" type="date" value={draft.to ?? ""} onChange={(event) => setDraft((current) => ({ ...current, to: event.target.value }))} /></Field>
        <div className="flex items-end gap-2"><Button type="submit">{copy.apply}</Button><Button type="button" variant="outline" onClick={clear}>{copy.clear}</Button></div>
      </form>
    </WorkspaceToolbar>

    {state === "failed" ? <ErrorState className="mt-6" title={copy.loadFailed} detail={error} retryLabel={copy.retry} onRetry={() => void load()} /> : null}
    {state === "loading" ? <LoadingState className="mt-6" label={copy.loading} /> : null}
    {state === "ready" && result && result.items.length === 0 ? <div className="mt-6"><EmptyState title={copy.empty} description={copy.emptyDescription} /></div> : null}
    {state === "ready" && result && result.items.length > 0 ? <>
      <div className="mt-6 hidden md:block"><DeliveryTable result={result} copy={copy} locale={locale} /></div>
      <ul className="mt-6 space-y-3 md:hidden" aria-label={copy.title}>{result.items.map((item) => <li key={item.id}><DeliveryCard item={item} copy={copy} locale={locale} /></li>)}</ul>
      <nav className="mt-6 flex items-center justify-between gap-3" aria-label={copy.pagination}>
        <Button type="button" variant="outline" size="sm" disabled={result.page <= 1} onClick={() => setFilters((current) => ({ ...current, page: result.page - 1 }))}>{copy.previous}</Button>
        <span className="text-sm font-semibold text-muted-foreground">{copy.page} {result.page}</span>
        <Button type="button" variant="outline" size="sm" disabled={!result.has_more} onClick={() => setFilters((current) => ({ ...current, page: result.page + 1 }))}>{copy.next}</Button>
      </nav>
    </> : null}
  </WorkspacePage>;
}

function DeliveryTable({ result, copy, locale }: { result: AdminEmailDeliveryPage; copy: ReturnType<typeof useLocale>["t"]["adminEmailDeliveries"]; locale: "ar" | "en" }) {
  return <TableContainer><Table><TableCaption>{copy.caption}</TableCaption><TableHead><TableRow>
    <TableHeaderCell scope="col">{copy.kindLabel}</TableHeaderCell><TableHeaderCell scope="col">{copy.recipient}</TableHeaderCell><TableHeaderCell scope="col">{copy.stateLabel}</TableHeaderCell><TableHeaderCell scope="col">{copy.queued}</TableHeaderCell><TableHeaderCell scope="col">{copy.attempts}</TableHeaderCell><TableHeaderCell scope="col">{copy.lastError}</TableHeaderCell>
  </TableRow></TableHead><TableBody>{result.items.map((item) => <DeliveryRow key={item.id} item={item} copy={copy} locale={locale} />)}</TableBody></Table></TableContainer>;
}

function DeliveryRow({ item, copy, locale }: { item: AdminEmailDeliveryPage["items"][number]; copy: ReturnType<typeof useLocale>["t"]["adminEmailDeliveries"]; locale: "ar" | "en" }) {
  const kinds = copy.kinds as Record<string, string>;
  const errors = copy.errorClasses as Record<string, string>;
  return <TableRow><TableCell>{kinds[item.kind] ?? copy.unknownKind}</TableCell><TableCell dir="ltr"><bdi>{item.recipient}</bdi></TableCell><TableCell><StatusBadge tone={item.state === "delivered" ? "success" : item.state === "failed" ? "accent" : "neutral"} label={copy.states[item.state]} /></TableCell><TableCell><time dateTime={item.queued_at}>{formatDateTime(item.queued_at, locale)}</time></TableCell><TableCell>{item.attempt_count.toLocaleString(locale)}</TableCell><TableCell>{item.last_error_class ? (errors[item.last_error_class] ?? copy.unknownError) : copy.none}</TableCell></TableRow>;
}

function DeliveryCard({ item, copy, locale }: { item: AdminEmailDeliveryPage["items"][number]; copy: ReturnType<typeof useLocale>["t"]["adminEmailDeliveries"]; locale: "ar" | "en" }) {
  const kinds = copy.kinds as Record<string, string>;
  return <article className="rounded-lg border border-border bg-card p-4"><div className="flex items-start justify-between gap-3"><div><p className="font-semibold text-foreground">{kinds[item.kind] ?? copy.unknownKind}</p><p className="mt-1 text-sm text-muted-foreground" dir="ltr"><bdi>{item.recipient}</bdi></p></div><StatusBadge tone={item.state === "delivered" ? "success" : item.state === "failed" ? "accent" : "neutral"} label={copy.states[item.state]} /></div><dl className="mt-4 grid gap-2 text-sm"><div className="flex justify-between gap-3"><dt className="text-muted-foreground">{copy.queued}</dt><dd><time dateTime={item.queued_at}>{formatDateTime(item.queued_at, locale)}</time></dd></div><div className="flex justify-between gap-3"><dt className="text-muted-foreground">{copy.attempts}</dt><dd>{item.attempt_count.toLocaleString(locale)}</dd></div></dl></article>;
}
