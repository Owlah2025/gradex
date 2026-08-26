import * as React from "react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/**
 * A failed read, presented the same way wherever it happens.
 *
 * The screens this replaces ranged from a rose-tinted paragraph carrying only the raw client error
 * to a bare `role="alert"` with no way forward at all. Neither told the reader what had failed, and
 * neither offered a retry, so the only recovery was a full page reload.
 *
 * The shape is deliberate: `title` says what could not be loaded, in the product's own words;
 * `detail` carries the server's message when there is one worth showing. A refusal an Admin can act
 * on — a validation message, a permission refusal — belongs in `detail` and is never swallowed. A
 * transport failure with nothing human in it should be passed as `detail: undefined` rather than
 * surfaced as an internal string.
 *
 * All copy is required from the caller. There are no built-in English defaults.
 */
export function ErrorState({
  title,
  detail,
  retryLabel,
  onRetry,
  className,
  testID,
}: {
  /** What failed, in product language. */
  title: string;
  /** The server's own message, when it says something the reader can act on. */
  detail?: string | null;
  retryLabel?: string;
  onRetry?: () => void;
  className?: string;
  testID?: string;
}) {
  return (
    <div data-testid={testID} className={cn(className)}>
      <Alert tone="error" title={title}>
        {detail ? <p className={onRetry ? "mb-3" : undefined}>{detail}</p> : null}
        {onRetry && retryLabel ? (
          <Button type="button" variant="outline" size="sm" onClick={onRetry}>
            {retryLabel}
          </Button>
        ) : null}
      </Alert>
    </div>
  );
}
