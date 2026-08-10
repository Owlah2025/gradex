/**
 * Which surfaces a principal that still owes a password change may not enter.
 *
 * The authoritative control is the server: a CHANGE_REQUIRED principal is
 * refused every capability except changing its password and ending its session,
 * whatever the browser does. This module exists because being refused is not
 * the same as being told why. Without it the founder's bootstrap Administrator
 * signed in, landed on an Admin screen, and watched every panel fail with no
 * explanation and no way forward.
 *
 * So this is a deny-list of privileged application roots rather than an
 * allow-list of public ones. A restricted visitor is redirected off the screens
 * that would only produce refusals, and is left alone on the public catalogue,
 * the legal pages, and the identity screens — including sign-out, which must
 * stay reachable.
 */

/** Path segments that name an authenticated application surface. */
const privilegedSegments = ["admin", "instructor", "learn", "access", "staff"];

const locales = ["ar", "en"];

/**
 * Reports whether a path is a privileged surface that a restricted principal
 * should be moved off.
 *
 * Both shapes are matched, because the application has both: locale-addressable
 * routes such as `/en/instructor/courses`, and legacy unprefixed ones such as
 * `/staff` and `/instructor/courses`.
 */
export function isPrivilegedSurface(pathname: string): boolean {
  if (!pathname.startsWith("/")) return false;
  const segments = pathname.split("/").filter(Boolean);
  if (segments.length === 0) return false;

  const first = segments[0];
  if (locales.includes(first)) {
    return segments.length > 1 && privilegedSegments.includes(segments[1]);
  }
  return privilegedSegments.includes(first);
}
