"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { getAdminAccount, type AdminAccount } from "@/lib/api/admin-operations";
import { describeApiError } from "@/lib/api/api-error";
import { formatDate, formatDateTime } from "@/lib/i18n/format";
import { useLocale } from "@/lib/i18n/locale-provider";
import { ErrorState } from "@/components/common/error-state";
import { LoadingState } from "@/components/common/loading-state";
import { StatusBadge } from "@/components/common/status-badge";
import { Button } from "@/components/ui/button";
import {
  WorkspacePage,
  WorkspacePageHeader,
  WorkspaceSection,
} from "@/components/layout/workspace-page";

export function AdminUserDetail({ accountID }: { accountID: string }) {
  const { locale, t } = useLocale();
  const copy = t.adminUserDetail;
  const [account, setAccount] = useState<AdminAccount | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "failed">("loading");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    setState("loading");
    setError(null);
    void getAdminAccount(accountID, locale)
      .then((response) => {
        if (!active) return;
        setAccount(response.identity);
        setState("ready");
      })
      .catch((cause) => {
        if (!active) return;
        setError(describeApiError(cause, locale));
        setState("failed");
      });
    return () => {
      active = false;
    };
  }, [accountID, locale]);

  if (state === "loading") {
    return (
      <WorkspacePage testID="admin-user-detail-page">
        <LoadingState label={copy.loading} testID="admin-user-detail-loading" />
      </WorkspacePage>
    );
  }

  if (state === "failed" || !account) {
    return (
      <WorkspacePage testID="admin-user-detail-page">
        <ErrorState
          title={copy.loadFailed}
          detail={error}
          retryLabel={copy.backToUsers}
          onRetry={() => window.location.reload()}
          testID="admin-user-detail-error"
        />
      </WorkspacePage>
    );
  }

  return (
    <WorkspacePage testID="admin-user-detail-page">
      <WorkspacePageHeader
        title={account.display_name}
        description={copy.description}
        breadcrumb={
          <Button asChild variant="ghost" size="sm">
            <Link href={"/" + locale + "/admin/users"}>{copy.backToUsers}</Link>
          </Button>
        }
        status={
          <StatusBadge
            tone={account.status === "ACTIVE" ? "success" : account.status === "SUSPENDED" ? "accent" : "neutral"}
            label={copy.status[account.status]}
            detail={copy.roles[account.role]}
          />
        }
      />

      <WorkspaceSection title={copy.identitySection} description={copy.identityDescription}>
        <div className="rounded-lg border border-border bg-card p-5">
          <dl className="grid gap-5 sm:grid-cols-2">
            <IdentityFact label={copy.name} value={account.display_name} />
            <IdentityFact label={copy.email} value={account.email} direction="ltr" />
            <IdentityFact label={copy.role} value={copy.roles[account.role]} />
            <IdentityFact label={copy.statusLabel} value={copy.status[account.status]} />
            <IdentityFact label={copy.locale} value={account.locale === "ar" ? copy.arabic : copy.english} />
            <IdentityFact
              label={copy.emailVerified}
              value={account.email_verified ? copy.verified : copy.notVerified}
            />
            <IdentityFact
              label={copy.joined}
              value={formatDate(account.created_at, locale)}
            />
            <IdentityFact
              label={copy.institution}
              value={account.institution_label || copy.noInstitution}
            />
            <IdentityFact
              label={copy.lastActivity}
              value={
                account.last_sign_in_activity_at
                  ? formatDateTime(account.last_sign_in_activity_at, locale)
                  : copy.noActivity
              }
            />
          </dl>
        </div>
      </WorkspaceSection>
    </WorkspacePage>
  );
}

function IdentityFact({
  label,
  value,
  direction,
}: {
  label: string;
  value: string;
  direction?: "ltr";
}) {
  return (
    <div>
      <dt className="text-sm font-semibold text-muted-foreground">{label}</dt>
      <dd className="mt-1 font-medium text-foreground" dir={direction}>
        <bdi>{value}</bdi>
      </dd>
    </div>
  );
}
