/**
 * Handing a Student off to an external destination without losing Gradex.
 *
 * The purchase confirmation cannot be an ordinary link: the destination does
 * not exist until the server has created the purchase request and answered
 * with the URL. Everything here follows from that one fact.
 *
 * `window.location.assign` — what this replaces — navigated the Gradex tab
 * itself, so pressing "confirm" threw away the page the Student was reading.
 * The obvious repair, calling `window.open` once the request resolves, does not
 * survive contact with a real browser: by then the user activation from the
 * click has been spent on the `await`, and Safari in particular refuses the
 * popup. So the context is opened *synchronously inside the click*, while the
 * activation is still live, and is pointed at the destination afterwards.
 *
 * The opened context starts at `about:blank`, which is same-origin, so it is
 * still reachable and can be navigated or closed once the answer arrives.
 */

/**
 * A browsing context opened during a click and waiting for its destination.
 *
 * `null` handles the case that matters most: the browser refused. Every method
 * is safe to call on a refusal, so callers state what should happen and never
 * branch on whether the popup exists.
 */
export type PendingHandoff = {
  /** Whether a context was actually opened and is still usable. */
  readonly opened: boolean;
  /** Points the context at `url` and severs its reference back to Gradex. */
  complete: (url: string) => boolean;
  /** Closes the blank context. Used when the request failed and it has no destination. */
  abandon: () => void;
};

/**
 * Opens a blank browsing context. Call this synchronously from the event
 * handler, before any `await`.
 */
export function openHandoffContext(): PendingHandoff {
  let handle: Window | null = null;
  try {
    // `noopener` is deliberately NOT passed here. Chrome returns `null` for a
    // window opened with it, which would throw away the handle this whole
    // approach depends on. The opener reference is severed in `complete`
    // instead, which reaches the same end state.
    handle = window.open("", "_blank");
  } catch {
    handle = null;
  }

  const usable = () => handle !== null && !handle.closed;

  return {
    get opened() {
      return usable();
    },
    complete(url: string) {
      if (!usable()) return false;
      try {
        // Severed before navigation, so the external page never observes a
        // window it could reach back through. This is the `rel="noopener"`
        // guarantee expressed against a handle we had to keep.
        handle!.opener = null;
        // `replace` rather than an assignment: the blank step is scaffolding
        // and does not belong in the new context's history.
        handle!.location.replace(url);
        return true;
      } catch {
        return false;
      }
    },
    abandon() {
      if (!usable()) return;
      try {
        handle!.close();
      } catch {
        // A context we cannot close is not worth failing a purchase over.
      }
    },
  };
}
