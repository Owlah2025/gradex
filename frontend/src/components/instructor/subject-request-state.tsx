"use client";

import { useCallback, useEffect, useState } from "react";
import { listOwnSubjectRequests, type SubjectRequestWire } from "@/lib/api/subject-requests";
import { useLocale } from "@/lib/i18n/locale-provider";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";

export type MissingSubjectInput = {
  proposedOfficialCode?: string;
  proposedTitleAr: string;
  proposedTitleEn: string;
  note?: string;
};

/**
 * Asking for a subject the Academic Catalog does not carry yet.
 *
 * The four fields here were `placeholder`-only, like the ones on the create form: unnamed to a
 * screen reader, and unnamed to everyone else the moment anything was typed into them. Two of them
 * gate the send button, so the form could sit disabled while pointing at fields whose purpose had
 * disappeared.
 *
 * The pending state is worth stating plainly rather than as a status word. A subject request is
 * the one thing on this surface that genuinely does block submission — the server refuses without
 * `subject_id` — and it is blocked on an administrator, not on the Instructor. Saying so is the
 * difference between waiting knowingly and wondering.
 */
export function SubjectRequestState({
  courseID,
  busy,
  onRequest,
}: {
  courseID: string;
  busy: boolean;
  onRequest: (input: MissingSubjectInput) => Promise<boolean>;
}) {
  const { locale, t } = useLocale();
  const copy = t.instructor.request;
  const [requests, setRequests] = useState<SubjectRequestWire[]>([]);
  const [error, setError] = useState("");
  const [showForm, setShowForm] = useState(false);
  const [code, setCode] = useState("");
  const [titleAr, setTitleAr] = useState("");
  const [titleEn, setTitleEn] = useState("");
  const [note, setNote] = useState("");

  const refresh = useCallback(async () => {
    const current = await listOwnSubjectRequests(locale, courseID);
    setRequests(current);
    setError("");
  }, [courseID, locale]);

  useEffect(() => {
    refresh().catch(() => setError(copy.loadFailed));
  }, [refresh, copy.loadFailed]);

  const submit = async () => {
    const saved = await onRequest({
      proposedOfficialCode: code,
      proposedTitleAr: titleAr,
      proposedTitleEn: titleEn,
      note,
    });
    if (!saved) return;
    try {
      await refresh();
      setShowForm(false);
    } catch {
      setError(copy.sentButStale);
    }
  };

  const latest = requests[0];

  return (
    <div className="space-y-3" data-testid="academic-course-subject-request-state">
      {error ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {error}
        </p>
      ) : null}

      {latest?.status === "PENDING" ? (
        <div data-testid="subject-request-pending">
          <Alert tone="info" title={copy.pendingTitle}>
            <p>{copy.pendingBody}</p>
          </Alert>
        </div>
      ) : (
        <>
          {latest?.status === "REJECTED" && (
            <div data-testid="subject-request-rejected">
              <Alert tone="error" title={copy.rejectedTitle}>
                {/* The administrator's own words, not a restatement of them. */}
                {latest.resolution_reason ? <p>{latest.resolution_reason}</p> : null}
              </Alert>
            </div>
          )}
          {!showForm ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setShowForm(true)}
              disabled={busy}
              data-testid="academic-course-request-subject"
            >
              {copy.open}
            </Button>
          ) : (
            <div className="space-y-4" data-testid="academic-course-subject-request-form">
              <Field label={copy.code} htmlFor="academic-course-request-code">
                <Input
                  id="academic-course-request-code"
                  value={code}
                  onChange={(event) => setCode(event.target.value)}
                  data-testid="academic-course-request-code"
                />
              </Field>
              <Field label={copy.titleAr} htmlFor="academic-course-request-title-ar">
                <Input
                  id="academic-course-request-title-ar"
                  lang="ar"
                  dir="rtl"
                  value={titleAr}
                  onChange={(event) => setTitleAr(event.target.value)}
                  data-testid="academic-course-request-title-ar"
                />
              </Field>
              <Field label={copy.titleEn} htmlFor="academic-course-request-title-en">
                <Input
                  id="academic-course-request-title-en"
                  lang="en"
                  dir="ltr"
                  value={titleEn}
                  onChange={(event) => setTitleEn(event.target.value)}
                  data-testid="academic-course-request-title-en"
                />
              </Field>
              <Field label={copy.note} htmlFor="academic-course-request-note">
                <Textarea
                  id="academic-course-request-note"
                  rows={2}
                  value={note}
                  onChange={(event) => setNote(event.target.value)}
                  data-testid="academic-course-request-note"
                />
              </Field>
              <Button
                type="button"
                size="sm"
                disabled={busy || !titleAr.trim() || !titleEn.trim()}
                onClick={() => void submit()}
                data-testid="academic-course-submit-subject-request"
              >
                {copy.send}
              </Button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
