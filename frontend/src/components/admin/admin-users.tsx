"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { listInstitutions, type Institution } from "@/lib/api/academic";
import {
  listAdminAccounts,
  type AccountRole,
  type AccountStatus,
  type AdminAccount,
  type AdminAccountFilters,
  type AdminAccountPage,
} from "@/lib/api/admin-operations";
import { describeApiError } from "@/lib/api/api-error";
import { useLocale } from "@/lib/i18n/locale-provider";
import { formatDate, formatDateTime } from "@/lib/i18n/format";
import { EmptyState } from "@/components/common/empty-state";
import { ErrorState } from "@/components/common/error-state";
import { LoadingState } from "@/components/common/loading-state";
import { StatusBadge } from "@/components/common/status-badge";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
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
import {
  WorkspacePage,
  WorkspacePageHeader,
  WorkspaceToolbar,
} from "@/components/layout/workspace-page";

type DirectoryDraft = {
  q: string;
  role: AccountRole | "";
  status: AccountStatus | "";
  institutionId: string;
  joinedFrom: string;
  joinedTo: string;
};

const EMPTY_DRAFT: DirectoryDraft = {
  q: "",
  role: "",
  status: "",
  institutionId: "",
  joinedFrom: "",
  joinedTo: "",
};

const PAGE_LIMIT = 20;

export function AdminUsers() {
  const { locale, t } = useLocale();
  const copy = t.adminUsers;
  const [draft, setDraft] = useState<DirectoryDraft>(EMPTY_DRAFT);
  const [filters, setFilters] = useState<AdminAccountFilters>({ page: 1, limit: PAGE_LIMIT });
  const [result, setResult] = useState<AdminAccountPage | null>(null);
  const [loadState, setLoadState] = useState<"loading" | "ready" | "failed">("loading");
  const [error, setError] = useState<string | null>(null);
  const [institutions, setInstitutions] = useState<Institution[]>([]);
  const [institutionsError, setInstitutionsError] = useState<string | null>(null);
  const requestSequence = useRef(0);

  const loadAccounts = useCallback(async () => {
    const requestID = ++requestSequence.current;
    setLoadState("loading");
    setError(null);
    try {
      const next = await listAdminAccounts(locale, filters);
      if (requestID !== requestSequence.current) return;
      setResult(next);
      setLoadState("ready");
    } catch (cause) {
      if (requestID !== requestSequence.current) return;
      setError(describeApiError(cause, locale));
      setLoadState("failed");
    }
  }, [filters, locale]);

  useEffect(() => {
    void loadAccounts();
  }, [loadAccounts]);

  useEffect(() => {
    let active = true;
    setInstitutionsError(null);
    void listInstitutions(locale)
      .then((next) => {
        if (active) setInstitutions(next.filter((institution) => !institution.retired_at));
      })
      .catch((cause) => {
        if (active) setInstitutionsError(describeApiError(cause, locale));
      });
    return () => {
      active = false;
    };
  }, [locale]);

  const updateDraft = <K extends keyof DirectoryDraft>(key: K, value: DirectoryDraft[K]) => {
    setDraft((current) => ({ ...current, [key]: value }));
  };

  const applyFilters = (event: FormEvent) => {
    event.preventDefault();
    setFilters({ ...draft, page: 1, limit: PAGE_LIMIT });
  };

  const clearFilters = () => {
    setDraft(EMPTY_DRAFT);
    setFilters({ page: 1, limit: PAGE_LIMIT });
  };

  const goToPage = (page: number) => {
    setFilters((current) => ({ ...current, page }));
  };
  const hasActiveFilters = Boolean(
    filters.q ||
      filters.role ||
      filters.status ||
      filters.institutionId ||
      filters.joinedFrom ||
      filters.joinedTo,
  );

  return (
    <WorkspacePage testID="admin-users-page">
      <WorkspacePageHeader
        title={copy.title}
        description={copy.description}
        status={
          result ? (
            <span className="text-sm font-semibold text-muted-foreground" aria-live="polite">
              {result.total} {copy.resultCount}
            </span>
          ) : null
        }
      />

      <WorkspaceToolbar>
        <form onSubmit={applyFilters} className="grid w-full gap-3 rounded-lg border border-border bg-card p-4 sm:grid-cols-2 lg:grid-cols-6">
          <Field label={copy.searchLabel} htmlFor="admin-users-search" className="sm:col-span-2 lg:col-span-2">
            <Input
              id="admin-users-search"
              value={draft.q}
              onChange={(event) => updateDraft("q", event.target.value)}
              placeholder={copy.searchPlaceholder}
              autoComplete="off"
            />
          </Field>
          <Field label={copy.roleLabel} htmlFor="admin-users-role">
            <Select
              id="admin-users-role"
              value={draft.role}
              onChange={(event) => updateDraft("role", event.target.value as AccountRole | "")}
            >
              <option value="">{copy.allRoles}</option>
              <option value="STUDENT">{copy.roles.STUDENT}</option>
              <option value="INSTRUCTOR">{copy.roles.INSTRUCTOR}</option>
              <option value="ADMIN">{copy.roles.ADMIN}</option>
            </Select>
          </Field>
          <Field label={copy.statusLabel} htmlFor="admin-users-status">
            <Select
              id="admin-users-status"
              value={draft.status}
              onChange={(event) => updateDraft("status", event.target.value as AccountStatus | "")}
            >
              <option value="">{copy.allStatuses}</option>
              <option value="ACTIVE">{copy.status.ACTIVE}</option>
              <option value="PENDING_VERIFICATION">{copy.status.PENDING_VERIFICATION}</option>
              <option value="SUSPENDED">{copy.status.SUSPENDED}</option>
            </Select>
          </Field>
          <Field label={copy.institutionLabel} htmlFor="admin-users-institution">
            <Select
              id="admin-users-institution"
              value={draft.institutionId}
              onChange={(event) => updateDraft("institutionId", event.target.value)}
              disabled={institutionsError !== null}
            >
              <option value="">{copy.allInstitutions}</option>
              {institutions.map((institution) => (
                <option key={institution.id} value={institution.id}>
                  {locale === "ar" ? institution.name_ar : institution.name_en}
                </option>
              ))}
            </Select>
          </Field>
          <div className="grid grid-cols-2 gap-3 sm:col-span-2 lg:col-span-2">
            <Field label={copy.joinedFrom} htmlFor="admin-users-joined-from">
              <Input
                id="admin-users-joined-from"
                type="date"
                value={draft.joinedFrom}
                onChange={(event) => updateDraft("joinedFrom", event.target.value)}
              />
            </Field>
            <Field label={copy.joinedTo} htmlFor="admin-users-joined-to">
              <Input
                id="admin-users-joined-to"
                type="date"
                value={draft.joinedTo}
                onChange={(event) => updateDraft("joinedTo", event.target.value)}
              />
            </Field>
          </div>
          <div className="flex flex-wrap items-end gap-2 sm:col-span-2 lg:col-span-6">
            <Button type="submit">{copy.applyFilters}</Button>
            <Button type="button" variant="outline" onClick={clearFilters}>
              {copy.clearFilters}
            </Button>
          </div>
        </form>
      </WorkspaceToolbar>

      {institutionsError ? (
        <div className="mt-4">
          <Alert tone="info" title={copy.institutionUnavailable}>
            {institutionsError}
          </Alert>
        </div>
      ) : null}

      {loadState === "failed" ? (
        <ErrorState
          className="mt-6"
          testID="admin-users-error"
          title={copy.loadFailed}
          detail={error}
          retryLabel={copy.retry}
          onRetry={() => setFilters((current) => ({ ...current }))}
        />
      ) : null}

      {loadState === "loading" ? (
        <LoadingState className="mt-6" testID="admin-users-loading" label={copy.loading} />
      ) : null}

      {loadState === "ready" && result && result.accounts.length === 0 ? (
        <div className="mt-6">
          <EmptyState
            density="compact"
            headingLevel={2}
            testID="admin-users-empty"
            title={hasActiveFilters ? copy.noMatches : copy.empty}
            description={copy.emptyDescription}
            action={
              hasActiveFilters ? (
                <Button variant="outline" onClick={clearFilters}>
                  {copy.clearFilters}
                </Button>
              ) : undefined
            }
          />
        </div>
      ) : null}

      {loadState === "ready" && result && result.accounts.length > 0 ? (
        <>
          <div className="mt-6 hidden md:block">
            <AccountTable accounts={result.accounts} locale={locale} copy={copy} />
          </div>
          <ul className="mt-6 space-y-3 md:hidden" aria-label={copy.title}>
            {result.accounts.map((account) => (
              <li key={account.id}>
                <AccountCard account={account} locale={locale} copy={copy} />
              </li>
            ))}
          </ul>
          <Pagination
            page={result.page}
            hasMore={result.has_more}
            onPageChange={goToPage}
            copy={copy}
          />
        </>
      ) : null}
    </WorkspacePage>
  );
}

function AccountTable({
  accounts,
  locale,
  copy,
}: {
  accounts: AdminAccount[];
  locale: "ar" | "en";
  copy: ReturnType<typeof useLocale>["t"]["adminUsers"];
}) {
  return (
    <TableContainer>
      <Table>
        <TableCaption>{copy.tableCaption}</TableCaption>
        <TableHead>
          <TableRow>
            <TableHeaderCell scope="col">{copy.name}</TableHeaderCell>
            <TableHeaderCell scope="col">{copy.roleLabel}</TableHeaderCell>
            <TableHeaderCell scope="col">{copy.statusLabel}</TableHeaderCell>
            <TableHeaderCell scope="col">{copy.institutionLabel}</TableHeaderCell>
            <TableHeaderCell scope="col">{copy.joined}</TableHeaderCell>
            <TableHeaderCell scope="col">{copy.lastActivity}</TableHeaderCell>
            <TableHeaderCell scope="col">{copy.open}</TableHeaderCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {accounts.map((account) => (
            <TableRow key={account.id} interactive>
              <TableCell>
                <AccountName account={account} locale={locale} copy={copy} />
              </TableCell>
              <TableCell>{copy.roles[account.role]}</TableCell>
              <TableCell>
                <AccountStatusBadge account={account} copy={copy} />
              </TableCell>
              <TableCell>{account.institution_label || copy.noInstitution}</TableCell>
              <TableCell>
                <time dateTime={account.created_at}>{formatDate(account.created_at, locale)}</time>
              </TableCell>
              <TableCell>
                {account.last_sign_in_activity_at ? (
                  <time dateTime={account.last_sign_in_activity_at}>
                    {formatDateTime(account.last_sign_in_activity_at, locale)}
                  </time>
                ) : (
                  copy.noActivity
                )}
              </TableCell>
              <TableCell>
                <Button asChild variant="outline" size="sm">
                  <Link href={accountHref(locale, account.id)}>{copy.open}</Link>
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </TableContainer>
  );
}

function AccountCard({
  account,
  locale,
  copy,
}: {
  account: AdminAccount;
  locale: "ar" | "en";
  copy: ReturnType<typeof useLocale>["t"]["adminUsers"];
}) {
  return (
    <article className="rounded-lg border border-border bg-card p-4 shadow-sm">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <AccountName account={account} locale={locale} copy={copy} />
        <AccountStatusBadge account={account} copy={copy} />
      </div>
      <dl className="mt-4 grid gap-2 text-sm text-muted-foreground">
        <div className="flex flex-wrap gap-2">
          <dt>{copy.roleLabel}:</dt>
          <dd className="font-semibold text-foreground">{copy.roles[account.role]}</dd>
        </div>
        <div className="flex flex-wrap gap-2">
          <dt>{copy.institutionLabel}:</dt>
          <dd className="font-semibold text-foreground">{account.institution_label || copy.noInstitution}</dd>
        </div>
        <div className="flex flex-wrap gap-2">
          <dt>{copy.joined}:</dt>
          <dd><time dateTime={account.created_at}>{formatDate(account.created_at, locale)}</time></dd>
        </div>
        <div className="flex flex-wrap gap-2">
          <dt>{copy.lastActivity}:</dt>
          <dd>
            {account.last_sign_in_activity_at
              ? formatDateTime(account.last_sign_in_activity_at, locale)
              : copy.noActivity}
          </dd>
        </div>
      </dl>
      <Button asChild className="mt-4 w-full" variant="outline">
        <Link href={accountHref(locale, account.id)}>{copy.open}</Link>
      </Button>
    </article>
  );
}

function AccountName({
  account,
  locale,
  copy,
}: {
  account: AdminAccount;
  locale: "ar" | "en";
  copy: ReturnType<typeof useLocale>["t"]["adminUsers"];
}) {
  return (
    <div className="min-w-0">
      <Link
        href={accountHref(locale, account.id)}
        className="font-display font-bold text-foreground underline-offset-4 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
      >
        {account.display_name}
      </Link>
      <p className="mt-1 max-w-[18rem] truncate text-sm text-muted-foreground" dir="ltr">
        <bdi>{account.email}</bdi>
      </p>
    </div>
  );
}

function AccountStatusBadge({
  account,
  copy,
}: {
  account: AdminAccount;
  copy: ReturnType<typeof useLocale>["t"]["adminUsers"];
}) {
  const tone = account.status === "ACTIVE" ? "success" : account.status === "SUSPENDED" ? "accent" : "neutral";
  return <StatusBadge tone={tone} label={copy.status[account.status]} />;
}

function Pagination({
  page,
  hasMore,
  onPageChange,
  copy,
}: {
  page: number;
  hasMore: boolean;
  onPageChange: (page: number) => void;
  copy: ReturnType<typeof useLocale>["t"]["adminUsers"];
}) {
  return (
    <nav className="mt-6 flex items-center justify-between gap-3" aria-label={copy.pagination}>
      <Button type="button" variant="outline" size="sm" disabled={page <= 1} onClick={() => onPageChange(page - 1)}>
        {copy.previous}
      </Button>
      <span className="text-sm font-semibold text-muted-foreground">
        {copy.page} {page}
      </span>
      <Button type="button" variant="outline" size="sm" disabled={!hasMore} onClick={() => onPageChange(page + 1)}>
        {copy.next}
      </Button>
    </nav>
  );
}

function accountHref(locale: "ar" | "en", accountID: string): string {
  return "/" + locale + "/admin/users/" + encodeURIComponent(accountID);
}
