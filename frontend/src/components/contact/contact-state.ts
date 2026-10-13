import type { CountryCode } from "libphonenumber-js/mobile";

import { ProblemError } from "../../lib/api/problem";
import { isE164ContactNumber, normalizeContactNumber } from "../../lib/contact/phone";

/**
 * Decisions about the Student's WhatsApp contact that every surface shares
 * (D-117). They are plain functions so the rules are tested once, here, rather
 * than re-derived inside each component.
 */

export type ContactInput =
  | { kind: "empty" }
  | { kind: "invalid" }
  | { kind: "valid"; e164: string };

/** What the Student has typed, read as a number or not. */
export function readContactInput(national: string, region: CountryCode): ContactInput {
  if (!national.trim()) return { kind: "empty" };
  const e164 = normalizeContactNumber(national, region);
  return e164 ? { kind: "valid", e164 } : { kind: "invalid" };
}

/**
 * What a purchase confirmation sends, or why it cannot be sent yet.
 *
 * A Student who already has a number and is not changing it sends nothing: the
 * server reads the number off their profile inside the same transaction, which
 * is the authoritative copy. A Student without one, or who chose to use a
 * different number, must supply a valid one before the request is attempted.
 */
export type PurchaseContactPlan =
  | { kind: "use-profile" }
  | { kind: "send"; e164: string }
  | { kind: "blocked"; reason: "required" | "invalid" };

export function planPurchaseContact(
  profileNumber: string | null | undefined,
  editing: boolean,
  input: ContactInput,
): PurchaseContactPlan {
  if (!editing && isE164ContactNumber(profileNumber)) return { kind: "use-profile" };
  switch (input.kind) {
    case "valid":
      return { kind: "send", e164: input.e164 };
    case "invalid":
      return { kind: "blocked", reason: "invalid" };
    default:
      return { kind: "blocked", reason: "required" };
  }
}

/** The contact-field reason a server refusal names, if it names one. */
export function contactProblem(caught: unknown): "required" | "invalid" | null {
  if (!(caught instanceof ProblemError)) return null;
  for (const violation of caught.problem.errors ?? []) {
    if (violation.code === "WHATSAPP_NUMBER_REQUIRED") return "required";
    if (violation.code === "INVALID_WHATSAPP_NUMBER") return "invalid";
  }
  return null;
}

/**
 * The handoff a pending request offers its Student, or `null`.
 *
 * Only a request still waiting for payment carries one, and only a URL the
 * server built is ever used: the browser never assembles a WhatsApp link for a
 * purchase. Anything that is not an https wa.me link is refused rather than
 * rendered, so a malformed value can never become a clickable destination.
 */
export function pendingHandoffURL(request: { state: string; whatsapp_url?: string } | null | undefined): string | null {
  if (!request || request.state !== "WAITING_PAYMENT" || !request.whatsapp_url) return null;
  try {
    const parsed = new URL(request.whatsapp_url);
    return parsed.protocol === "https:" && parsed.hostname === "wa.me" ? parsed.toString() : null;
  } catch {
    return null;
  }
}

/** Session-scoped "later" for the reminder: it returns next session until a number is saved. */
export const contactPromptDismissalKey = "gradex.contact-prompt.dismissed";
