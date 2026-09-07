"use client";

import { Check, CircleAlert, CircleDashed, Loader2 } from "lucide-react";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";

/**
 * Whether what is on screen is what the server holds.
 *
 * `SAVED` is set from the resolution of the save call and from nowhere else. The failure this
 * guards against is the ordinary one: a studio that says "Saved" the moment a key is pressed, so an
 * instructor closes the tab on a network error they were told did not happen. `UNSAVED` is likewise
 * derived by comparing the form against the revision the server returned, not from an "edited" flag
 * that a successful save could forget to clear.
 */
export type AuthoringSaveState = "IDLE" | "UNSAVED" | "SAVING" | "SAVED" | "FAILED";

export function AuthoringSaveStateLine({
  state,
  labels,
}: {
  state: AuthoringSaveState;
  labels: Dictionary["instructor"]["authoring"];
}) {
  if (state === "IDLE") return null;

  const { Icon, text, tone } = describe(state, labels);
  return (
    <p
      // A polite status, not an alert: this narrates the outcome of something the reader just did.
      role="status"
      data-testid="authoring-save-state"
      data-save-state={state}
      className={`inline-flex items-center gap-1.5 text-xs font-semibold ${tone}`}
    >
      <Icon
        className={`size-3.5 shrink-0 ${state === "SAVING" ? "animate-spin" : ""}`}
        aria-hidden
      />
      {text}
    </p>
  );
}

function describe(
  state: Exclude<AuthoringSaveState, "IDLE">,
  labels: Dictionary["instructor"]["authoring"],
) {
  switch (state) {
    case "SAVING":
      return { Icon: Loader2, text: labels.savingState, tone: "text-muted-foreground" };
    case "SAVED":
      return { Icon: Check, text: labels.savedState, tone: "text-muted-foreground" };
    case "FAILED":
      return { Icon: CircleAlert, text: labels.failedState, tone: "text-destructive" };
    case "UNSAVED":
    default:
      return { Icon: CircleDashed, text: labels.unsavedState, tone: "text-foreground" };
  }
}
