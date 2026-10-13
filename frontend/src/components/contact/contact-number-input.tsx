"use client";

import * as React from "react";
import type { CountryCode } from "libphonenumber-js/mobile";

import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { contactRegionOptions, isContactRegion } from "@/lib/contact/phone";

type Labels = Dictionary["contact"];

/**
 * A country code and a mobile number, entered separately (D-117).
 *
 * Kuwait is preselected because Gradex serves Kuwaiti universities first, but
 * every country is listed and a number typed with "+" or "00" carries its own
 * country. The number itself is always laid out left-to-right, including on an
 * Arabic page: a phone number is not text that reverses.
 */
export function ContactNumberInput({
  idPrefix,
  locale,
  labels,
  region,
  national,
  onRegionChange,
  onNationalChange,
  error,
  disabled,
  showWhy = true,
}: {
  idPrefix: string;
  locale: "ar" | "en";
  labels: Labels;
  region: CountryCode;
  national: string;
  onRegionChange: (region: CountryCode) => void;
  onNationalChange: (value: string) => void;
  /** Already translated. */
  error?: string | null;
  disabled?: boolean;
  /** Whether to state why the number is collected; a surrounding card may already say it. */
  showWhy?: boolean;
}) {
  const options = React.useMemo(() => contactRegionOptions(locale), [locale]);
  const regionID = `${idPrefix}-region`;
  const numberID = `${idPrefix}-number`;
  const hintID = `${idPrefix}-hint`;
  const errorID = `${idPrefix}-error`;

  return (
    <div className="space-y-2" data-testid={`${idPrefix}-contact-input`}>
      {showWhy ? <p className="text-sm leading-6 text-muted-foreground">{labels.why}</p> : null}
      <div className="grid gap-3 sm:grid-cols-[minmax(0,13rem)_minmax(0,1fr)]">
        <div className="space-y-2">
          <label htmlFor={regionID} className="block font-display text-sm font-bold">
            {labels.countryLabel}
          </label>
          <Select
            id={regionID}
            value={region}
            disabled={disabled}
            onChange={(event) => {
              if (isContactRegion(event.target.value)) onRegionChange(event.target.value);
            }}
            data-testid={`${idPrefix}-region`}
          >
            {options.map((option) => (
              <option key={option.region} value={option.region}>
                {`${option.label} (+${option.callingCode})`}
              </option>
            ))}
          </Select>
        </div>
        <div className="space-y-2">
          <label htmlFor={numberID} className="block font-display text-sm font-bold">
            {labels.numberLabel}
          </label>
          <Input
            id={numberID}
            type="tel"
            inputMode="tel"
            autoComplete="tel-national"
            dir="ltr"
            maxLength={32}
            value={national}
            disabled={disabled}
            placeholder={labels.numberPlaceholder}
            onChange={(event) => onNationalChange(event.target.value)}
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? errorID : hintID}
            className="text-start"
            data-testid={`${idPrefix}-number`}
          />
        </div>
      </div>
      {error ? (
        <p id={errorID} className="text-sm font-medium text-destructive" role="alert" data-testid={`${idPrefix}-error`}>
          {error}
        </p>
      ) : (
        <p id={hintID} className="text-sm text-muted-foreground">
          {labels.numberHelp}
        </p>
      )}
    </div>
  );
}
