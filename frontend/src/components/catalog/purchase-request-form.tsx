"use client";

import * as React from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Alert } from "@/components/ui/alert";
import { createPurchaseRequest } from "@/lib/api/access";
import { validEmail } from "@/lib/identity/validation";

type Labels = {
  heading: string;
  intro: string;
  action: string;
  email: string;
  invalidEmail: string;
  submit: string;
  submitting: string;
  failed: string;
};

export function PurchaseRequestForm({
  courseId,
  locale,
  labels,
}: {
  courseId: string;
  locale: "ar" | "en";
  labels: Labels;
}) {
  const [open, setOpen] = React.useState(false);
  const [email, setEmail] = React.useState("");
  const [error, setError] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (!validEmail(email)) {
      setError(labels.invalidEmail);
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      const result = await createPurchaseRequest(courseId, email, locale);
      // Direct navigation preserves the user's click intent after the async
      // persistence step; unlike a popup it is not subject to popup blocking.
      window.location.assign(result.whatsapp_url);
    } catch {
      setError(labels.failed);
      setSubmitting(false);
    }
  }

  if (!open) {
    return (
      <Button
        type="button"
        className="mt-6"
        onClick={() => setOpen(true)}
        data-testid="purchase-request-open"
      >
        {labels.action}
      </Button>
    );
  }

  return (
    <section
      className="mt-6 rounded-lg border border-border bg-card p-5"
      aria-labelledby="purchase-request-heading"
    >
      <h2
        id="purchase-request-heading"
        className="font-display text-xl font-bold text-foreground"
      >
        {labels.heading}
      </h2>
      <p className="mt-2 text-sm leading-6 text-muted-foreground">
        {labels.intro}
      </p>
      <form className="mt-4 space-y-4" noValidate onSubmit={submit}>
        {error ? <Alert tone="error" title={error} /> : null}
        <label
          className="block text-sm font-semibold"
          htmlFor="purchase-request-email"
        >
          {labels.email}
        </label>
        <Input
          id="purchase-request-email"
          data-testid="purchase-request-email"
          type="email"
          inputMode="email"
          autoComplete="email"
          dir="ltr"
          value={email}
          onChange={(event) => setEmail(event.target.value)}
          aria-invalid={Boolean(error)}
        />
        <Button
          type="submit"
          disabled={submitting}
          data-testid="purchase-request-submit"
        >
          {submitting ? labels.submitting : labels.submit}
        </Button>
      </form>
    </section>
  );
}
