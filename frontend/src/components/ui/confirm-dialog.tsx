"use client";

import * as React from "react";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/**
 * "Are you sure?", asked only where the answer can matter.
 *
 * This exists for actions that destroy work the product cannot give back. Deleting a section takes
 * its lessons with it, and each of those lessons may carry a video the instructor spent an upload
 * on; the server has no undo for any of it. That is a different class of action from removing a
 * row that can be typed again in five seconds, and only the first kind belongs here. Wrapping every
 * destructive-looking control in a dialog teaches people to dismiss dialogs, which costs exactly
 * the protection it was meant to buy.
 *
 * Built on the Radix dialog already in the tree — the one the navigation sheet uses — so focus is
 * trapped while it is open, returned to the trigger when it closes, and Escape works. The
 * destructive choice is not the default focus: Radix focuses the first tabbable element, and the
 * cancel button is deliberately first in the DOM so a reflexive Enter does not delete anything.
 */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  body,
  confirmLabel,
  cancelLabel,
  busy = false,
  tone = "destructive",
  onConfirm,
  testID,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  /** What will actually happen, in the reader's own terms. Not a restatement of the title. */
  body: string;
  confirmLabel: string;
  cancelLabel: string;
  busy?: boolean;
  tone?: "destructive" | "default";
  onConfirm: () => void;
  testID?: string;
}) {
  const titleID = React.useId();
  const bodyID = React.useId();

  /*
    Radix restores focus to its own `Trigger`. This dialog is opened from controlled state instead
    — the curriculum's delete controls are ordinary buttons inside rows — so there is no trigger for
    it to return to, and closing dropped focus on the document body. A keyboard user who opened the
    confirmation on lesson nine and cancelled it landed back at the top of the page.
  */
  const openerRef = React.useRef<HTMLElement | null>(null);
  React.useEffect(() => {
    if (open) openerRef.current = document.activeElement as HTMLElement | null;
  }, [open]);

  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-gx-navy/45 backdrop-blur-sm data-[state=closed]:animate-out data-[state=open]:animate-in data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0" />
        <DialogPrimitive.Content
          aria-labelledby={titleID}
          aria-describedby={bodyID}
          data-testid={testID}
          onCloseAutoFocus={(event) => {
            const opener = openerRef.current;
            if (!opener?.isConnected) return;
            event.preventDefault();
            opener.focus();
          }}
          className={cn(
            "fixed start-1/2 top-1/2 z-50 w-[min(28rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-card p-6 shadow-lg rtl:translate-x-1/2",
            "data-[state=closed]:animate-out data-[state=open]:animate-in data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0",
          )}
        >
          <DialogPrimitive.Title
            id={titleID}
            className="font-display text-lg font-bold text-foreground"
          >
            {title}
          </DialogPrimitive.Title>
          <DialogPrimitive.Description
            id={bodyID}
            className="mt-2 text-sm leading-6 text-muted-foreground"
          >
            {body}
          </DialogPrimitive.Description>
          <div className="mt-6 flex flex-wrap justify-end gap-2">
            {/* Cancel is first in the DOM so it takes initial focus. */}
            <DialogPrimitive.Close asChild>
              <Button type="button" variant="outline" size="sm" data-testid="confirm-cancel">
                {cancelLabel}
              </Button>
            </DialogPrimitive.Close>
            <Button
              type="button"
              variant={tone === "destructive" ? "destructive" : "default"}
              size="sm"
              disabled={busy}
              onClick={onConfirm}
              data-testid="confirm-accept"
            >
              {confirmLabel}
            </Button>
          </div>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
