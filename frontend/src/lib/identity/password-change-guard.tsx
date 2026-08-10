"use client";

import * as React from "react";
import { usePathname, useRouter } from "next/navigation";
import { passwordChangePath, withReturnTo } from "./return-to";
import { isPrivilegedSurface } from "./restricted-navigation";
import { useSessionView } from "./use-session";

/**
 * Keeps a restricted principal on the one screen it can act on.
 *
 * Login redirects there already, but that alone is not enough: a bookmark, a
 * refresh, a shared link, or a stale tab can all put a CHANGE_REQUIRED
 * principal on an Admin or Instructor surface directly. Each of those screens
 * would then issue privileged requests, collect 403s, and render as broken.
 *
 * This is a usability control, not the authorization boundary — the server
 * refuses those requests whether or not this component runs, and nothing here
 * grants anything. It moves the visitor to the screen that resolves the state,
 * carrying the surface they were trying to reach so they arrive there once the
 * change commits.
 *
 * Public pages and the identity screens are deliberately untouched, so signing
 * out stays reachable from anywhere.
 */
export function PasswordChangeGuard() {
  const session = useSessionView();
  const pathname = usePathname();
  const router = useRouter();

  React.useEffect(() => {
    if (!session?.password_change_required) return;
    if (!pathname || !isPrivilegedSurface(pathname)) return;
    // replace, not push: the surface being left is unusable in this state, so
    // it must not sit in history for the back button to return to.
    router.replace(withReturnTo(passwordChangePath, pathname));
  }, [session?.password_change_required, pathname, router]);

  return null;
}
