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

/**
 * The stable landing route for a role.
 *
 * Every role currently resolves to the catalog root because no authenticated
 * shell exists yet: the Student, Instructor, and Admin dashboards described in
 * NAVIGATION_RULES.md are built in later slices. Update this map when those
 * routes land, rather than inventing destinations that would 404 today.
 */
export function roleRoot(_role: SessionRole): string {
  return "/";
}

/** Prefers a validated caller destination, else the role root. */
export function postLoginDestination(
  role: SessionRole,
  requested: unknown,
): string {
  return safeReturnTo(requested) ?? roleRoot(role);
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
  passwordChangeRequired: boolean,
): string {
  if (passwordChangeRequired) {
    return withReturnTo(passwordChangePath, requested);
  }
  return postLoginDestination(role, requested);
}

/**
 * Where a principal goes once its password change has committed.
 *
 * A requested destination still wins when it is safe, because the visitor was
 * interrupted rather than redirected. Otherwise the role decides, and unlike
 * `roleRoot` these are the real authenticated surfaces: the point of the change
 * is that the principal now holds its full authority, so landing it on the
 * public home page would hide exactly what it just unlocked.
 */
export function postPasswordChangeDestination(
  role: SessionRole,
  requested: unknown,
  locale: "ar" | "en",
): string {
  const requestedDestination = safeReturnTo(requested);
  if (requestedDestination) return requestedDestination;
  switch (role) {
    case "INSTRUCTOR":
      return `/${locale}/instructor/courses`;
    case "ADMIN":
      // Staff management is where an Administrator's first task after the
      // bootstrap change lives: inviting the first Instructor.
      return "/staff";
    default:
      return roleRoot(role);
  }
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
