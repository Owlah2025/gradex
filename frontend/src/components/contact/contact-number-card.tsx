"use client";

import * as React from "react";

import { StatusBadge } from "@/components/common/status-badge";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { MyProfile } from "@/lib/api/account-profile";
import { useLocale } from "@/lib/i18n/locale-provider";
import { formatContactNumber, isE164ContactNumber } from "@/lib/contact/phone";
import { ContactNumberForm } from "./contact-number-form";

/**
 * The Student's WhatsApp number on their own profile (D-117).
 *
 * It says plainly whether a number is on file — "Provided" or "Not provided",
 * never "Verified", because Gradex does not verify ownership — and why Gradex
 * asks for it. The `contact` anchor is where the reminder sends a Student.
 */
export function ContactNumberCard({
  profile,
  onSaved,
}: {
  profile: MyProfile;
  onSaved: (profile: MyProfile) => void;
}) {
  const { locale, t } = useLocale();
  const labels = t.contact;
  const number = isE164ContactNumber(profile.whatsapp_number) ? profile.whatsapp_number : null;
  const [editing, setEditing] = React.useState(!number);
  const [saved, setSaved] = React.useState(false);

  React.useEffect(() => {
    if (!number) setEditing(true);
  }, [number]);

  // `?focus=contact` is where the D-118 reminder email lands: bring the card
  // into view and put the cursor in the number field (or on "Change" when a
  // number is already saved), once, after the profile has rendered.
  const [highlighted, setHighlighted] = React.useState(false);
  React.useEffect(() => {
    let focus: string | null = null;
    try {
      focus = new URLSearchParams(window.location.search).get("focus");
    } catch {
      focus = null;
    }
    if (focus !== "contact") return;
    setHighlighted(true);
    const frame = window.requestAnimationFrame(() => {
      const card = document.getElementById("contact");
      card?.scrollIntoView({ behavior: "smooth", block: "center" });
      const target = document.getElementById("profile-contact-number") ?? document.querySelector<HTMLElement>('[data-testid="contact-number-change"]');
      target?.focus({ preventScroll: true });
    });
    const timer = window.setTimeout(() => setHighlighted(false), 4000);
    return () => {
      window.cancelAnimationFrame(frame);
      window.clearTimeout(timer);
    };
  }, []);

  return (
    <Card
      id="contact"
      className={`scroll-mt-24 transition-shadow ${highlighted ? "ring-2 ring-primary ring-offset-2 ring-offset-background" : ""}`}
      data-testid="contact-number-card"
      data-focused={highlighted ? "true" : undefined}
    >
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <CardTitle>{labels.sectionTitle}</CardTitle>
          <StatusBadge
            tone={number ? "success" : "accent"}
            label={number ? labels.statusProvided : labels.statusMissing}
            labelTestID="contact-number-status"
          />
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <p className="text-sm leading-6 text-muted-foreground">{labels.why}</p>
        {saved ? <Alert tone="success" title={labels.saved} /> : null}
        {number && !editing ? (
          <div className="flex flex-wrap items-center justify-between gap-4">
            <div>
              <p className="text-xs font-semibold text-muted-foreground">{labels.currentLabel}</p>
              <p className="mt-1 font-semibold text-foreground" dir="ltr" data-testid="contact-number-current">
                {formatContactNumber(number)}
              </p>
            </div>
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                setSaved(false);
                setEditing(true);
              }}
              data-testid="contact-number-change"
            >
              {labels.change}
            </Button>
          </div>
        ) : (
          <ContactNumberForm
            idPrefix="profile-contact"
            locale={locale}
            labels={labels}
            initialNumber={number}
            showWhy={false}
            onCancel={number ? () => setEditing(false) : undefined}
            onSaved={(updated) => {
              onSaved(updated);
              setSaved(true);
              setEditing(false);
            }}
          />
        )}
      </CardContent>
    </Card>
  );
}
