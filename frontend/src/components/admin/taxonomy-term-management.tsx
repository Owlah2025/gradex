"use client";

import { useState } from "react";
import { Pencil, Plus, Trash2, Archive } from "lucide-react";
import {
  createTaxonomyTerm,
  deleteTaxonomyTerm,
  renameTaxonomyTerm,
  retireTaxonomyTerm,
} from "@/lib/api/catalog";
import { taxonomyTermLabel } from "@/components/catalog/taxonomy-term-select";
import { describeApiError } from "@/lib/api/api-error";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { StatusBadge } from "@/components/common/status-badge";
import type { TaxonomyKind, TaxonomyTerm } from "@/lib/api/catalog";

/**
 * Managing the legacy catalogue vocabulary.
 *
 * # THE FOUR ACTIONS WERE FOUR COLOURS
 *
 * Add was violet, rename slate, retire amber, delete rose — four solid buttons of equal weight
 * whose only hierarchy was hue, with the irreversible one sitting flush beside the routine one. A
 * reader who cannot separate amber from rose had nothing but the label, and the label was the same
 * size on all four.
 *
 * They are ranked now: adding is the primary action, renaming and retiring are ordinary ones,
 * deleting is the only destructive one. Each carries an icon as well as its word, so the *kind* of
 * action is never carried by colour alone.
 *
 * # AND TWO OF THEM WERE UNCONFIRMED
 *
 * Retiring and deleting both happened on a single click. They are confirmed now, and each states
 * what the server actually does: retiring sets `retired_at`, so the term stops being offered, the
 * courses already carrying it keep it, and there is no route back. Deleting is refused outright by
 * the server if any course revision references the term — so the copy promises exactly that, rather
 * than warning about a data loss the contract will not permit.
 */
type TaxonomyTermManagementProps = {
  terms: TaxonomyTerm[];
  refresh: () => Promise<void>;
};

export function TaxonomyTermManagement({ terms, refresh }: TaxonomyTermManagementProps) {
  const { locale, t } = useLocale();
  const copy = t.adminTaxonomy;
  const [kind, setKind] = useState<TaxonomyKind>("MAJOR");
  const [labelAr, setLabelAr] = useState("");
  const [labelEn, setLabelEn] = useState("");
  const [academicCode, setAcademicCode] = useState("");
  const [selectedID, setSelectedID] = useState("");
  const [notice, setNotice] = useState<{ tone: "success" | "error"; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState<"retire" | "remove" | null>(null);

  const selected = terms.find((term) => term.id === selectedID) ?? null;

  const csrf = () => {
    const token = currentCSRFToken();
    if (!token) setNotice({ tone: "error", text: copy.csrfMissing });
    return token;
  };

  const run = async (action: (token: string) => Promise<unknown>, success: string) => {
    const token = csrf();
    if (!token) return;
    setBusy(true);
    setNotice(null);
    try {
      await action(token);
      await refresh();
      setNotice({ tone: "success", text: success });
    } catch (error) {
      setNotice({ tone: "error", text: describeApiError(error, locale) || copy.failed });
    } finally {
      setBusy(false);
      setPending(null);
    }
  };

  const create = () => {
    if (!labelAr.trim() || !labelEn.trim()) {
      setNotice({ tone: "error", text: copy.needLabels });
      return;
    }
    void run(async (token) => {
      await createTaxonomyTerm({
        kind,
        labelAr: labelAr.trim(),
        labelEn: labelEn.trim(),
        academicCode: academicCode.trim() || undefined,
        locale,
        csrf: token,
      });
      setLabelAr("");
      setLabelEn("");
      setAcademicCode("");
    }, copy.created);
  };

  const rename = () => {
    if (!selectedID || !labelAr.trim() || !labelEn.trim()) {
      setNotice({ tone: "error", text: copy.needTermAndLabels });
      return;
    }
    void run(
      (token) =>
        renameTaxonomyTerm({
          termID: selectedID,
          labelAr: labelAr.trim(),
          labelEn: labelEn.trim(),
          locale,
          csrf: token,
        }),
      copy.renamed,
    );
  };

  const confirmPending = () => {
    if (!selectedID || !pending) return;
    if (pending === "retire") {
      void run((token) => retireTaxonomyTerm({ termID: selectedID, locale, csrf: token }), copy.retired);
      return;
    }
    void run((token) => deleteTaxonomyTerm({ termID: selectedID, locale, csrf: token }), copy.removed);
  };

  const openDestructive = (action: "retire" | "remove") => {
    if (!selectedID) {
      setNotice({ tone: "error", text: copy.needTerm });
      return;
    }
    setPending(action);
  };

  return (
    <div className="space-y-4">
      <div className="grid gap-4 md:grid-cols-2">
        <Field htmlFor="taxonomy-term-kind" label={copy.kind}>
          <Select
            id="taxonomy-term-kind"
            data-testid="taxonomy-term-kind"
            value={kind}
            onChange={(event) => setKind(event.target.value as TaxonomyKind)}
          >
            <option value="MAJOR">{copy.major}</option>
            <option value="SUBJECT">{copy.subject}</option>
          </Select>
        </Field>
        <Field htmlFor="taxonomy-term-existing" label={copy.existing}>
          <Select
            id="taxonomy-term-existing"
            data-testid="taxonomy-term-existing"
            value={selectedID}
            onChange={(event) => setSelectedID(event.target.value)}
          >
            <option value="">{copy.choose}</option>
            {terms.map((term) => (
              // The kind in words. The option used to end with the raw enum — "… — SUBJECT".
              <option key={term.id} value={term.id}>
                {taxonomyTermLabel(term, locale)} — {term.kind === "MAJOR" ? copy.major : copy.subject}
              </option>
            ))}
          </Select>
        </Field>
        <Field htmlFor="taxonomy-term-label-ar" label={copy.labelAr}>
          <Input
            id="taxonomy-term-label-ar"
            data-testid="taxonomy-term-label-ar"
            value={labelAr}
            onChange={(event) => setLabelAr(event.target.value)}
          />
        </Field>
        <Field htmlFor="taxonomy-term-label-en" label={copy.labelEn}>
          <Input
            id="taxonomy-term-label-en"
            data-testid="taxonomy-term-label-en"
            value={labelEn}
            onChange={(event) => setLabelEn(event.target.value)}
          />
        </Field>
        {kind === "SUBJECT" ? (
          <Field
            htmlFor="taxonomy-term-academic-code"
            label={copy.academicCode}
            hint={copy.academicCodeHint}
          >
            <Input
              id="taxonomy-term-academic-code"
              data-testid="taxonomy-term-academic-code"
              value={academicCode}
              onChange={(event) => setAcademicCode(event.target.value)}
            />
          </Field>
        ) : null}
      </div>

      {/* The selected term's own state, so retiring something already retired is visibly not the
          action to reach for. */}
      {selected ? (
        <div className="flex flex-wrap items-center gap-2" data-testid="taxonomy-term-selected">
          <span className="text-sm font-semibold text-foreground">
            {taxonomyTermLabel(selected, locale)}
          </span>
          <StatusBadge
            tone={selected.retired_at ? "neutral" : "success"}
            size="sm"
            label={selected.retired_at ? copy.retiredBadge : copy.activeBadge}
            labelTestID="taxonomy-term-state"
          />
        </div>
      ) : null}

      {/* Ranked, and each carrying an icon beside its word: the kind of action is never the colour
          alone. Delete is the only destructive variant, and it is last. */}
      <div className="flex flex-wrap gap-2">
        <Button type="button" size="sm" disabled={busy} onClick={create} data-testid="taxonomy-term-create">
          <Plus className="size-4" aria-hidden />
          {busy ? copy.working : copy.create}
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={busy}
          onClick={rename}
          data-testid="taxonomy-term-rename"
        >
          <Pencil className="size-4" aria-hidden />
          {copy.rename}
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={busy}
          onClick={() => openDestructive("retire")}
          data-testid="taxonomy-term-retire"
        >
          <Archive className="size-4" aria-hidden />
          {copy.retire}
        </Button>
        <Button
          type="button"
          variant="destructive"
          size="sm"
          disabled={busy}
          onClick={() => openDestructive("remove")}
          data-testid="taxonomy-term-delete"
        >
          <Trash2 className="size-4" aria-hidden />
          {copy.remove}
        </Button>
      </div>

      {notice ? (
        <div data-testid="taxonomy-term-message" data-tone={notice.tone}>
          <Alert tone={notice.tone} title={notice.text} />
        </div>
      ) : null}

      {pending ? (
        <ConfirmDialog
          open
          onOpenChange={(next) => {
            if (!next && !busy) setPending(null);
          }}
          title={pending === "retire" ? copy.retireTitle : copy.removeTitle}
          body={pending === "retire" ? copy.retireBody : copy.removeBody}
          confirmLabel={pending === "retire" ? copy.retireConfirm : copy.removeConfirm}
          cancelLabel={copy.keep}
          tone="destructive"
          busy={busy}
          onConfirm={confirmPending}
          testID="taxonomy-term-confirm"
        />
      ) : null}
    </div>
  );
}
