"use client";

import * as React from "react";
import { Phone } from "lucide-react";

import { Button } from "@/components/ui/button";
import { getMyProfile } from "@/lib/api/account-profile";
import { useLocale } from "@/lib/i18n/locale-provider";
import { isE164ContactNumber } from "@/lib/contact/phone";
import { ContactNumberForm, contactNumberSavedEvent } from "./contact-number-form";
import { contactPromptDismissalKey } from "./contact-state";

type PromptState = "hidden" | "visible" | "editing";

/**
 * The in-app reminder for a Student with no WhatsApp number (D-117).
 *
 * It is derived, not stored: it renders exactly when the Student's own profile
 * has no number, so there is no notification row to create on each sign-in,
 * nothing to grow without bound, and nothing to clean up — saving a number is
 * what removes it, everywhere at once.
 *
 * It is a card on a page the Student already reached, never a modal and never a
 * gate: purchased Courses stay one click away whether or not a number is given.
 * "Later" hides it for this browser session only, so it comes back next time
 * until the number exists. A profile that cannot be read renders nothing — the
 * safe direction for a reminder.
 */
export function ContactNumberPrompt({ className }: { className?: string }) {
  const { locale, t } = useLocale();
  const labels = t.contact;
  const [state, setState] = React.useState<PromptState>("hidden");

  React.useEffect(() => {
    let active = true;
    let dismissed = false;
    try {
      dismissed = window.sessionStorage.getItem(contactPromptDismissalKey) === "1";
    } catch {
      dismissed = false;
    }
    if (dismissed) return;
    getMyProfile(locale)
      .then((profile) => {
        if (active && !isE164ContactNumber(profile.whatsapp_number)) setState("visible");
      })
      .catch(() => undefined);
    const hide = () => setState("hidden");
    window.addEventListener(contactNumberSavedEvent, hide);
    return () => {
      active = false;
      window.removeEventListener(contactNumberSavedEvent, hide);
    };
  }, [locale]);

  if (state === "hidden") return null;

  return (
    <section
      data-testid="contact-number-prompt"
      aria-labelledby="contact-number-prompt-title"
      className={`rounded-lg border border-gx-orange-100 bg-gx-orange-50 p-5 dark:border-border dark:bg-card ${className ?? ""}`}
    >
      <div className="flex gap-3">
        <Phone aria-hidden className="mt-1 size-5 shrink-0 text-gx-orange-700 dark:text-primary" />
        <div className="min-w-0 flex-1">
          <h2 id="contact-number-prompt-title" className="font-display text-lg font-bold text-foreground">
            {labels.promptTitle}
          </h2>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">{labels.promptBody}</p>
          {state === "editing" ? (
            <div className="mt-4">
              <ContactNumberForm
                idPrefix="prompt-contact"
                locale={locale}
                labels={labels}
                showWhy={false}
                onCancel={() => setState("visible")}
                onSaved={() => setState("hidden")}
              />
            </div>
          ) : (
            <div className="mt-4 flex flex-wrap gap-3">
              <Button type="button" onClick={() => setState("editing")} data-testid="contact-number-prompt-action">
                {labels.promptAction}
              </Button>
              <Button
                type="button"
                variant="ghost"
                onClick={() => {
                  try {
                    window.sessionStorage.setItem(contactPromptDismissalKey, "1");
                  } catch {
                    // A browser that refuses storage just shows the reminder again next page.
                  }
                  setState("hidden");
                }}
                data-testid="contact-number-prompt-later"
              >
                {labels.promptLater}
              </Button>
            </div>
          )}
        </div>
      </div>
    </section>
  );
}
