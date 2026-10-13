"use client";

import * as React from "react";
import type { CountryCode } from "libphonenumber-js/mobile";

import { Button } from "@/components/ui/button";
import { updateMyProfile, type MyProfile } from "@/lib/api/account-profile";
import { currentCSRFToken } from "@/lib/identity/session";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { splitContactNumber } from "@/lib/contact/phone";
import { ContactNumberInput } from "./contact-number-input";
import { contactProblem, readContactInput } from "./contact-state";

type Labels = Dictionary["contact"];

/**
 * Every surface that shows a Student's number listens for this, so saving it in
 * one place updates the others without a reload — the reminder disappears the
 * moment the number exists.
 */
export const contactNumberSavedEvent = "gradex:contact-number-saved";

/** Saves the Student's own number through the profile endpoint. */
export function ContactNumberForm({
  idPrefix,
  locale,
  labels,
  initialNumber,
  onSaved,
  onCancel,
  showWhy,
}: {
  idPrefix: string;
  locale: "ar" | "en";
  labels: Labels;
  initialNumber?: string | null;
  onSaved: (profile: MyProfile) => void;
  onCancel?: () => void;
  showWhy?: boolean;
}) {
  const initial = React.useMemo(() => splitContactNumber(initialNumber), [initialNumber]);
  const [region, setRegion] = React.useState<CountryCode>(initial.region);
  const [national, setNational] = React.useState(initial.national);
  const [error, setError] = React.useState<string | null>(null);
  const [saving, setSaving] = React.useState(false);

  async function save(event: React.FormEvent) {
    event.preventDefault();
    if (saving) return;
    const input = readContactInput(national, region);
    if (input.kind !== "valid") {
      setError(input.kind === "empty" ? labels.required : labels.invalid);
      return;
    }
    const csrf = currentCSRFToken();
    if (!csrf) {
      setError(labels.sessionEnded);
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const profile = await updateMyProfile({ locale, csrf, whatsappNumber: input.e164 });
      window.dispatchEvent(new CustomEvent(contactNumberSavedEvent, { detail: profile.whatsapp_number }));
      onSaved(profile);
    } catch (caught) {
      const reason = contactProblem(caught);
      setError(reason === "invalid" ? labels.invalid : reason === "required" ? labels.required : labels.saveFailed);
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={(event) => void save(event)} noValidate data-testid={`${idPrefix}-form`}>
      <ContactNumberInput
        idPrefix={idPrefix}
        locale={locale}
        labels={labels}
        region={region}
        national={national}
        onRegionChange={(next) => {
          setRegion(next);
          setError(null);
        }}
        onNationalChange={(next) => {
          setNational(next);
          setError(null);
        }}
        error={error}
        disabled={saving}
        showWhy={showWhy}
      />
      <div className="mt-4 flex flex-wrap gap-3">
        <Button type="submit" disabled={saving} data-testid={`${idPrefix}-save`}>
          {saving ? labels.saving : labels.save}
        </Button>
        {onCancel ? (
          <Button type="button" variant="outline" disabled={saving} onClick={onCancel}>
            {labels.cancel}
          </Button>
        ) : null}
      </div>
    </form>
  );
}
