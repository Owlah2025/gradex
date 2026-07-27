export type SessionRole = "STUDENT" | "INSTRUCTOR" | "ADMIN";

const maximumLength = 512;

// Parsing against an opaque base lets the URL parser reject anything that
// escapes this origin without hand-rolling scheme detection.
const opaqueBase = "https://return-to.invalid";

// Sending a signed-in browser back to an admission screen loops the journey,
// and /api is the server surface rather than a page.
const blockedRoots = ["/api", "/login", "/register", "/verify-email"];

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
