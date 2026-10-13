"use client";

import * as React from "react";
import { Copy, MessageCircle } from "lucide-react";

import { StatusBadge } from "@/components/common/status-badge";
import { Button } from "@/components/ui/button";
import { formatDateTime } from "@/lib/i18n/format";
import { useLocale } from "@/lib/i18n/locale-provider";
import { contactChatLink, formatContactNumber, isE164ContactNumber } from "@/lib/contact/phone";

/**
 * A Student's WhatsApp contact as an Admin sees it (D-117).
 *
 * The chat link is built only from a stored E.164 number; anything else renders
 * "Not provided" and no link, because a broken link is worse than none. The
 * number is described as provided by the Student, never as verified.
 */
export function AdminStudentContact({
  number,
  updatedAt,
}: {
  number?: string | null;
  updatedAt?: string | null;
}) {
  const { locale, t } = useLocale();
  const labels = t.contact;
  const stored = isE164ContactNumber(number) ? number : null;
  const link = contactChatLink(stored);

  return (
    <section
      className="rounded-lg border border-border bg-card p-5"
      aria-labelledby="admin-student-contact-title"
      data-testid="admin-student-contact"
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 id="admin-student-contact-title" className="font-display font-bold">
          {labels.adminTitle}
        </h3>
        <StatusBadge
          tone={stored ? "success" : "accent"}
          label={stored ? labels.statusProvided : labels.statusMissing}
          labelTestID="admin-student-contact-status"
        />
      </div>
      {stored && link ? (
        <>
          <p className="mt-3 text-sm text-muted-foreground">{labels.sectionTitle}</p>
          <p className="mt-1 text-lg font-semibold text-foreground" dir="ltr" data-testid="admin-student-contact-number">
            {formatContactNumber(stored)}
          </p>
          {updatedAt ? (
            <p className="mt-1 text-xs text-muted-foreground">
              {labels.adminUpdatedAt}: {formatDateTime(updatedAt, locale)}
            </p>
          ) : null}
          <div className="mt-4 flex flex-wrap gap-2">
            <Button asChild size="sm">
              <a href={link} target="_blank" rel="noopener noreferrer" data-testid="admin-student-contact-chat">
                <MessageCircle aria-hidden className="size-4" />
                {labels.adminOpenChat}
              </a>
            </Button>
            <CopyContactNumber number={stored} />
          </div>
          <p className="mt-3 text-xs leading-5 text-muted-foreground">{labels.adminProvidedNote}</p>
        </>
      ) : (
        <p className="mt-3 text-sm text-muted-foreground" data-testid="admin-student-contact-missing">
          {labels.adminMissingNote}
        </p>
      )}
    </section>
  );
}

/** Copies the E.164 number, which pastes cleanly into any dialler or chat app. */
export function CopyContactNumber({ number }: { number: string }) {
  const { t } = useLocale();
  const labels = t.contact;
  const [state, setState] = React.useState<"idle" | "copied" | "failed">("idle");

  React.useEffect(() => {
    if (state === "idle") return;
    const timer = window.setTimeout(() => setState("idle"), 2500);
    return () => window.clearTimeout(timer);
  }, [state]);

  return (
    <>
      <Button
        type="button"
        size="sm"
        variant="outline"
        onClick={() => {
          if (!navigator.clipboard) {
            setState("failed");
            return;
          }
          navigator.clipboard.writeText(number).then(
            () => setState("copied"),
            () => setState("failed"),
          );
        }}
        data-testid="admin-contact-copy"
      >
        <Copy aria-hidden className="size-4" />
        {labels.adminCopy}
      </Button>
      <span className="sr-only" role="status" aria-live="polite">
        {state === "copied" ? labels.adminCopied : state === "failed" ? labels.adminCopyFailed : ""}
      </span>
      {state !== "idle" ? (
        <span className="self-center text-xs text-muted-foreground" aria-hidden>
          {state === "copied" ? labels.adminCopied : labels.adminCopyFailed}
        </span>
      ) : null}
    </>
  );
}

/**
 * The contact for one purchase request: the number captured with the request,
 * else the Student's current profile number (said so), else a clear "no
 * contact number". Historical requests are never rewritten to get here.
 */
export function AdminRequestContact({
  snapshot,
  profileNumber,
  compact = false,
}: {
  snapshot?: string | null;
  profileNumber?: string | null;
  compact?: boolean;
}) {
  const { t } = useLocale();
  const labels = t.contact;
  const fromRequest = isE164ContactNumber(snapshot) ? snapshot : null;
  const fromProfile = !fromRequest && isE164ContactNumber(profileNumber) ? profileNumber : null;
  const number = fromRequest ?? fromProfile;
  const link = contactChatLink(number);

  if (!number || !link) {
    return (
      <span className="text-sm font-semibold text-destructive" data-testid="admin-request-contact-missing">
        {labels.adminNoContact}
      </span>
    );
  }
  return (
    <span className="inline-flex flex-wrap items-center gap-x-2 gap-y-1 text-sm" data-testid="admin-request-contact">
      <a
        href={link}
        target="_blank"
        rel="noopener noreferrer"
        className="inline-flex items-center gap-1 font-semibold text-primary underline underline-offset-4"
        aria-label={`${labels.adminOpenChat} ${formatContactNumber(number)}`}
      >
        <MessageCircle aria-hidden className="size-4" />
        <span dir="ltr">{formatContactNumber(number)}</span>
      </a>
      {compact ? null : (
        <span className="text-xs text-muted-foreground">
          {fromRequest ? labels.adminFromRequest : labels.adminFromProfile}
        </span>
      )}
    </span>
  );
}

/**
 * The directory's compact contact cell for one account: a chat link for a
 * Student with a number, "Not provided" for one without, and the count of
 * requests still waiting for payment. Staff accounts have no contact here.
 */
export function AdminDirectoryContact({
  role,
  number,
  pendingPaymentRequests,
}: {
  role: string;
  number?: string | null;
  pendingPaymentRequests?: number;
}) {
  const { t } = useLocale();
  const labels = t.contact;
  if (role !== "STUDENT") return <span className="text-sm text-muted-foreground">—</span>;
  const pending = pendingPaymentRequests ?? 0;
  return (
    <div className="space-y-1" data-testid="admin-directory-contact">
      {isE164ContactNumber(number) ? (
        <AdminRequestContact snapshot={number} compact />
      ) : (
        <StatusBadge tone="accent" size="sm" label={labels.statusMissing} />
      )}
      {pending > 0 ? (
        <p className="text-xs font-semibold text-muted-foreground" data-testid="admin-directory-pending-payments">
          {labels.pendingPaymentCount.replace("{count}", String(pending))}
        </p>
      ) : null}
    </div>
  );
}
