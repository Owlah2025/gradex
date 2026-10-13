"use client";

import * as React from "react";
import type { CountryCode } from "libphonenumber-js/mobile";

import { getMyProfile } from "@/lib/api/account-profile";
import { listStudentPurchaseRequests, type PurchaseRequest } from "@/lib/api/access";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { defaultContactRegion, formatContactNumber, isE164ContactNumber, splitContactNumber } from "@/lib/contact/phone";
import { ContactNumberInput } from "./contact-number-input";
import { contactNumberSavedEvent } from "./contact-number-form";
import { planPurchaseContact, readContactInput, type PurchaseContactPlan } from "./contact-state";

type Labels = Dictionary["contact"];

export type PurchaseContactState = {
  /** `loading` until the profile has been read; the confirm button waits for it. */
  status: "loading" | "ready";
  profileNumber: string | null;
  editing: boolean;
  setEditing: (editing: boolean) => void;
  region: CountryCode;
  setRegion: (region: CountryCode) => void;
  national: string;
  setNational: (value: string) => void;
  error: "required" | "invalid" | null;
  setError: (error: "required" | "invalid" | null) => void;
  /** What a confirm press would send, decided from the current inputs. */
  plan: () => PurchaseContactPlan;
  /** Called after a successful request that carried a new number. */
  remember: (e164: string) => void;
};

/**
 * The Student's contact for a purchase confirmation (D-117).
 *
 * The profile is read when the panel opens so a Student who already gave a
 * number is simply shown it, and one who has not is asked before anything is
 * created. A profile that cannot be read is treated as "no number on file": the
 * Student is asked, and the server — which reads the profile itself — remains
 * the authority either way.
 */
export function usePurchaseContact(locale: "ar" | "en", enabled: boolean): PurchaseContactState {
  const [status, setStatus] = React.useState<"loading" | "ready">("loading");
  const [profileNumber, setProfileNumber] = React.useState<string | null>(null);
  const [editing, setEditing] = React.useState(false);
  const [region, setRegion] = React.useState<CountryCode>(defaultContactRegion);
  const [national, setNational] = React.useState("");
  const [error, setError] = React.useState<"required" | "invalid" | null>(null);

  React.useEffect(() => {
    if (!enabled) return;
    let active = true;
    getMyProfile(locale)
      .then((profile) => {
        if (!active) return;
        setProfileNumber(isE164ContactNumber(profile.whatsapp_number) ? profile.whatsapp_number : null);
      })
      .catch(() => {
        if (active) setProfileNumber(null);
      })
      .finally(() => {
        if (active) setStatus("ready");
      });
    const onSaved = (event: Event) => {
      const detail = (event as CustomEvent<string | null>).detail;
      if (isE164ContactNumber(detail)) setProfileNumber(detail);
    };
    window.addEventListener(contactNumberSavedEvent, onSaved);
    return () => {
      active = false;
      window.removeEventListener(contactNumberSavedEvent, onSaved);
    };
  }, [enabled, locale]);

  const editingNow = editing || (status === "ready" && !profileNumber);

  return {
    status,
    profileNumber,
    editing: editingNow,
    setEditing: (next) => {
      setEditing(next);
      setError(null);
      if (next && profileNumber) {
        const split = splitContactNumber(profileNumber);
        setRegion(split.region);
        setNational("");
      }
    },
    region,
    setRegion: (next) => {
      setRegion(next);
      setError(null);
    },
    national,
    setNational: (next) => {
      setNational(next);
      setError(null);
    },
    error,
    setError,
    plan: () => planPurchaseContact(profileNumber, editingNow, readContactInput(national, region)),
    remember: (e164) => {
      setProfileNumber(e164);
      setEditing(false);
      setNational("");
    },
  };
}

/** The contact part of a purchase confirmation. */
export function PurchaseContactSection({
  contact,
  locale,
  labels,
  disabled,
  idPrefix,
}: {
  contact: PurchaseContactState;
  locale: "ar" | "en";
  labels: Labels;
  disabled?: boolean;
  idPrefix: string;
}) {
  if (contact.status === "loading") return null;
  const error = contact.error === "invalid" ? labels.invalid : contact.error === "required" ? labels.required : null;

  if (!contact.editing && contact.profileNumber) {
    return (
      <div className="mt-4 rounded-md border border-border bg-background/60 p-3 text-sm" data-testid={`${idPrefix}-contact-current`}>
        <p className="text-muted-foreground">{labels.usingNumber}</p>
        <div className="mt-1 flex flex-wrap items-center justify-between gap-2">
          <span className="font-semibold text-foreground" dir="ltr">
            {formatContactNumber(contact.profileNumber)}
          </span>
          <button
            type="button"
            className="text-sm font-semibold text-primary underline underline-offset-4 disabled:opacity-50"
            disabled={disabled}
            onClick={() => contact.setEditing(true)}
            data-testid={`${idPrefix}-contact-change`}
          >
            {labels.useDifferent}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="mt-4">
      <ContactNumberInput
        idPrefix={`${idPrefix}-contact`}
        locale={locale}
        labels={labels}
        region={contact.region}
        national={contact.national}
        onRegionChange={contact.setRegion}
        onNationalChange={contact.setNational}
        error={error}
        disabled={disabled}
      />
      {contact.profileNumber ? (
        <button
          type="button"
          className="mt-2 text-sm font-semibold text-primary underline underline-offset-4"
          disabled={disabled}
          onClick={() => contact.setEditing(false)}
        >
          {labels.cancel}
        </button>
      ) : null}
    </div>
  );
}

/**
 * The Student's own purchase requests, for finding one still waiting for
 * payment. `null` while loading or when the read failed; the panel then simply
 * offers a confirmation, and the server reuses any live request anyway.
 */
export function useOwnPurchaseRequests(locale: "ar" | "en", enabled: boolean) {
  const [requests, setRequests] = React.useState<PurchaseRequest[] | null>(null);
  React.useEffect(() => {
    if (!enabled) return;
    let active = true;
    listStudentPurchaseRequests(locale)
      .then((result) => {
        if (active) setRequests(result?.purchase_requests ?? []);
      })
      .catch(() => {
        if (active) setRequests(null);
      });
    return () => {
      active = false;
    };
  }, [enabled, locale]);
  return requests;
}
