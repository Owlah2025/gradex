export type SessionRole = "STUDENT" | "INSTRUCTOR" | "ADMIN";

const maximumLength = 512;

// Parsing against an opaque base lets the URL parser reject anything that
// escapes this origin without hand-rolling scheme detection.
const opaqueBase = "https://return-to.invalid";

/**
 * The mandatory password-change screen.
 *
 * A restricted principal is sent here after signing in and may go nowhere else
 * until its credential is ACTIVE.
 */
export const passwordChangePath = "/password-change";

// Sending a signed-in browser back to an admission screen loops the journey,
// and /api is the server surface rather than a page.
//
// The password-change screen is blocked for the same reason as the rest: it is
// a step the application decides to show, never a destination a link may ask
// for. Allowing it would let a crafted returnTo park an unrestricted visitor on
// a form they cannot complete.
const blockedRoots = [
  "/api",
  "/login",
  "/register",
  "/verify-email",
  passwordChangePath,
];

/**
 * Validates a caller-supplied post-login destination.
 *
 * Returns the normalized internal path, or null when the value could leave
 * this origin, is unusable, or would loop back into admission. Callers must
 * treat null as "use the role root" rather than as an error.
 */
export function safeReturnTo(value: unknown): string | null {
  if (typeof value !== "string") return null;
  if (value.length === 0 || value.length > maximumLength) return null;

  // A destination must be origin-relative. "//host" is protocol-relative and
  // would navigate off-origin, and a backslash is treated as a separator by
  // some parsers but not others.
  if (!value.startsWith("/")) return null;
  if (value.startsWith("//")) return null;
  if (value.includes("\\")) return null;

  // Control characters can be used to smuggle a second header or to hide the
  // real target from a human reviewing the link.
  // eslint-disable-next-line no-control-regex
  if (/[\u0000-\u001f\u007f]/.test(value)) return null;

  let parsed: URL;
  try {
    parsed = new URL(value, opaqueBase);
  } catch {
    return null;
  }
  if (parsed.origin !== opaqueBase) return null;

  const path = parsed.pathname;
  const blocked = blockedRoots.some(
    (root) => path === root || path.startsWith(`${root}/`),
  );
  if (blocked) return null;

  return `${parsed.pathname}${parsed.search}${parsed.hash}`;
}

/** The stable existing application surface for a role and locale. */
export function roleRoot(
  role: SessionRole,
  locale: "ar" | "en",
): string {
  switch (role) {
    case "STUDENT":
      return `/${locale}/learn/dashboard`;
    case "INSTRUCTOR":
      return `/${locale}/instructor/courses`;
    case "ADMIN":
      return `/${locale}/admin/catalog`;
  }
}

/** Prefers a validated caller destination, else the role root. */
export function postLoginDestination(
  role: SessionRole,
  requested: unknown,
  locale: "ar" | "en",
): string {
  return safeReturnTo(requested) ?? roleRoot(role, locale);
}

/**
 * Where a browser goes immediately after signing in.
 *
 * A restricted principal goes to the mandatory password-change screen and
 * nowhere else — not even to a destination it legitimately asked for. That
 * destination is not discarded: it is carried across the change as `returnTo`,
 * so a visitor who followed a link, was made to change their password, and
 * finished still lands where they were going.
 *
 * This is the fix for the founder's finding. Before it, a bootstrap
 * Administrator signed in successfully and was routed into an application
 * surface that answered 403 to every request it made.
 */
export function postAuthenticationDestination(
  role: SessionRole,
  requested: unknown,
  locale: "ar" | "en",
  passwordChangeRequired: boolean,
): string {
  if (passwordChangeRequired) {
    return withReturnTo(passwordChangePath, requested);
  }
  return postLoginDestination(role, requested, locale);
}

/**
 * Where a principal goes once its password change has committed.
 *
 * A requested destination still wins when it is safe, because the visitor was
 * interrupted rather than redirected. Otherwise the same role root used by an
 * ordinary login applies now that the principal holds its full authority.
 */
export function postPasswordChangeDestination(
  role: SessionRole,
  requested: unknown,
  locale: "ar" | "en",
): string {
  return postLoginDestination(role, requested, locale);
}

/**
 * Carries a caller destination across one admission hop.
 *
 * The Student journey crosses several screens before a session exists —
 * register, verify, sign in — and a destination requested at the start has to
 * survive to the end without becoming a redirect the attacker controls.
 *
 * `requested` is re-validated here rather than trusted because it has just come
 * off a URL. Every hop is an entry point, so every hop revalidates: a hostile
 * value is dropped and the plain step path is returned, which loses the
 * destination and keeps the journey working. It is never propagated
 * unvalidated on the assumption that an earlier screen already checked it.
 *
 * `step` is an internal path this code chooses, never caller input.
 */
export function withReturnTo(step: string, requested: unknown): string {
  const destination = safeReturnTo(requested);
  if (destination === null) return step;
  return `${step}?returnTo=${encodeURIComponent(destination)}`;
}
